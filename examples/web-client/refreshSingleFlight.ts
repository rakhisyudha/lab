/**
 * Single-flight refresh coordinator.
 *
 * Invariant: at most ONE POST /auth/refresh is in flight for a given refresh
 * token, across all parallel callers in the tab AND across all tabs. The server
 * has no grace window, so a second refresh with the same token revokes the whole
 * token family. See README.md.
 */
import { getLocalStorage, type TokenPair, type TokenStorage } from './tokenStorage';

export interface RefreshCoordinatorOptions {
  endpoint: string; // full URL, e.g. 'https://api.example.com/api/v1/auth/refresh'
  storage: TokenStorage;
  onFatal?: () => void; // tokens were cleared / family revoked: navigate to login
}

const LOCK_NAME = 'backendlab-refresh';
const CHANNEL_NAME = 'backendlab-auth';
const LOCK_KEY = 'backendlab-refresh-lock';
const STALE_LOCK_MS = 10_000; // a refresh must finish well within this
const POLL_MS = 25;

const FATAL_CODES = new Set(['refresh_token_reuse_detected', 'invalid_refresh_token']);

interface LockRecord {
  owner: string;
  at: number;
}

// Structural types keep this file dependency-free (and testable with fakes).
type LockManagerLike = {
  request: (name: string, callback: () => Promise<unknown> | unknown) => Promise<unknown>;
};
type BroadcastLike = {
  postMessage(message: unknown): void;
  addEventListener?(type: 'message', listener: (event: { data?: unknown }) => void): void;
  close?(): void;
};

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null;
}

function firstString(source: Record<string, unknown>, ...keys: string[]): string | undefined {
  for (const key of keys) {
    const value = source[key];
    if (typeof value === 'string' && value.length > 0) return value;
  }
  return undefined;
}

/** Defensive parser: accept camelCase TokenPair or the server's snake_case envelope. */
function toTokenPair(value: unknown): TokenPair | null {
  if (!isRecord(value)) return null;
  const source = isRecord(value.tokens) ? value.tokens : value; // tolerate { tokens: {...} }
  const accessToken = firstString(source, 'accessToken', 'access_token');
  const refreshToken = firstString(source, 'refreshToken', 'refresh_token');
  if (!accessToken || !refreshToken) return null;
  const pair: TokenPair = { accessToken, refreshToken };
  if (typeof source.expiresAt === 'number') pair.expiresAt = source.expiresAt;
  else if (typeof source.expires_in === 'number') pair.expiresAt = Date.now() + source.expires_in * 1000;
  return pair;
}

function errorCode(value: unknown): string | undefined {
  if (!isRecord(value) || !isRecord(value.error)) return undefined;
  return typeof value.error.code === 'string' ? value.error.code : undefined;
}

function webLocks(): LockManagerLike | null {
  try {
    const nav = typeof navigator === 'undefined' ? null : (navigator as any);
    const locks = nav?.locks;
    return locks && typeof locks.request === 'function' ? (locks as LockManagerLike) : null;
  } catch {
    return null;
  }
}

function makeId(): string {
  try {
    if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') return crypto.randomUUID();
  } catch {
    /* ignore */
  }
  return `${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

type RefreshOutcome = { kind: 'ok'; pair: TokenPair } | { kind: 'fatal' } | { kind: 'transient' };

export class RefreshCoordinator {
  /** One shared promise per tab (per coordinator instance). */
  private inflight: Promise<TokenPair | null> | null = null;
  private channel: BroadcastLike | null = null;

  constructor(private readonly options: RefreshCoordinatorOptions) {
    try {
      if (typeof BroadcastChannel === 'function') {
        const channel = new BroadcastChannel(CHANNEL_NAME) as unknown as BroadcastLike;
        channel.addEventListener?.('message', (event) => {
          const message = event.data;
          if (!isRecord(message)) return;
          if (message.type === 'updated') {
            const pair = toTokenPair(message.pair);
            if (pair) this.options.storage.set(pair); // keep non-localStorage stores in sync too
          } else if (message.type === 'cleared') {
            this.options.storage.clear();
          }
        });
        this.channel = channel;
      }
    } catch {
      this.channel = null;
    }
  }

  /** Always returns the shared in-tab promise; resets it once settled. */
  refresh(): Promise<TokenPair | null> {
    if (!this.inflight) {
      this.inflight = this.refreshOnce().finally(() => {
        this.inflight = null;
      });
    }
    return this.inflight;
  }

  /** Optional teardown (useful in tests). */
  close(): void {
    try {
      this.channel?.close?.();
    } catch {
      /* ignore */
    }
  }

  private async refreshOnce(): Promise<TokenPair | null> {
    const before = this.options.storage.get();
    if (!before?.refreshToken) return null; // nothing to rotate

    const effect: { announcement?: TokenPair | null } = {};
    let result: TokenPair | null = null;
    try {
      result = await this.withCrossTabLock(async () => {
        // CRITICAL: re-read AFTER the lock. Another tab may have rotated already.
        const current = this.options.storage.get();
        if (!current) return null; // e.g. another tab logged out
        if (current.refreshToken !== before.refreshToken) {
          return current; // already rotated elsewhere: reuse it, do NOT call /auth/refresh
        }

        const outcome = await this.callRefresh(current.refreshToken);
        if (outcome.kind === 'ok') {
          this.options.storage.set(outcome.pair); // persist BEFORE releasing the lock
          effect.announcement = outcome.pair;
          return outcome.pair;
        }
        if (outcome.kind === 'fatal') {
          this.options.storage.clear();
          effect.announcement = null; // tell other tabs to clear as well
          try {
            this.options.onFatal?.(); // e.g. navigate('/login')
          } catch {
            // A failing navigation callback must not turn a fatal refresh into a rejection.
          }
        }
        // Transient error: keep tokens so a later call can retry.
        return null;
      });
    } finally {
      if ('announcement' in effect) this.broadcast(effect.announcement ?? null);
    }
    return result;
  }

  private async callRefresh(refreshToken: string): Promise<RefreshOutcome> {
    try {
      const response = await fetch(this.options.endpoint, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ refresh_token: refreshToken }),
        // Deliberately no `credentials` assumption: the token travels in the body.
      });
      const body: unknown = await response.json().catch(() => null);
      if (!response.ok) {
        const code = errorCode(body);
        return code && FATAL_CODES.has(code) ? { kind: 'fatal' } : { kind: 'transient' };
      }
      const pair = toTokenPair(body);
      return pair ? { kind: 'ok', pair } : { kind: 'transient' }; // malformed success = retryable
    } catch {
      return { kind: 'transient' }; // network error = retryable
    }
  }

  private async withCrossTabLock<T>(work: () => Promise<T>): Promise<T | null> {
    const locks = webLocks();
    if (locks) {
      // Web Locks releases automatically when the callback settles.
      return (await locks.request(LOCK_NAME, async () => work())) as T;
    }
    return this.withLocalStorageMutex(work);
  }

  /** Fallback mutex for browsers without navigator.locks. */
  private async withLocalStorageMutex<T>(work: () => Promise<T>): Promise<T | null> {
    const ls = getLocalStorage();
    if (!ls) return work(); // no cross-tab primitive: in-tab single-flight is all we can offer

    const owner = makeId();
    const deadline = Date.now() + STALE_LOCK_MS;
    for (;;) {
      if (tryAcquire(ls, owner)) {
        try {
          return await work();
        } finally {
          release(ls, owner); // always release, even if work throws
        }
      }
      if (Date.now() >= deadline) return null; // never refresh unguarded
      await sleep(POLL_MS);
    }
  }

  private broadcast(pair: TokenPair | null): void {
    try {
      this.channel?.postMessage(pair ? { type: 'updated', pair } : { type: 'cleared' });
    } catch {
      /* ignore */
    }
  }
}

/** Non-atomic but verified acquire; the stale timestamp handles crashed tabs. */
function tryAcquire(ls: Storage, owner: string): boolean {
  try {
    const raw = ls.getItem(LOCK_KEY);
    if (raw) {
      const held = JSON.parse(raw) as LockRecord | null;
      if (held && typeof held.at === 'number' && Date.now() - held.at < STALE_LOCK_MS) return false;
    }
    ls.setItem(LOCK_KEY, JSON.stringify({ owner, at: Date.now() } satisfies LockRecord));
    const confirmed = ls.getItem(LOCK_KEY);
    const current = confirmed ? (JSON.parse(confirmed) as LockRecord | null) : null;
    return current?.owner === owner;
  } catch {
    return false;
  }
}

function release(ls: Storage, owner: string): void {
  try {
    const raw = ls.getItem(LOCK_KEY);
    const held = raw ? (JSON.parse(raw) as LockRecord | null) : null;
    if (held?.owner === owner) ls.removeItem(LOCK_KEY);
  } catch {
    /* ignore */
  }
}
