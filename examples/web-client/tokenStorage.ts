/**
 * Token storage abstraction shared by the refresh coordinator and the API client.
 *
 * The default implementation uses `localStorage` when it can be reached and
 * falls back to a module-local map in SSR / privacy-mode / sandboxed contexts.
 * The in-memory fallback is per JS realm: cross-tab coordination is impossible
 * without a shared storage, so in that case only in-tab single-flight holds.
 */

export interface TokenPair {
  accessToken: string;
  refreshToken: string;
  expiresAt?: number; // epoch milliseconds (optional)
}

export interface TokenStorage {
  get(): TokenPair | null;
  set(pair: TokenPair): void;
  clear(): void;
}

export const DEFAULT_TOKEN_KEY = 'backendlab.tokens';

/** In-memory fallback, keyed by storage key so multiple keys stay independent. */
const memory = new Map<string, TokenPair | null>();

/** `localStorage` when it can be referenced safely, otherwise null. */
export function getLocalStorage(): Storage | null {
  try {
    return typeof localStorage === 'undefined' ? null : localStorage;
  } catch {
    // Some browsers expose the global but throw on access (e.g. sandboxed iframes).
    return null;
  }
}

function isPair(value: unknown): value is TokenPair {
  if (typeof value !== 'object' || value === null) return false;
  const candidate = value as Partial<TokenPair>;
  return typeof candidate.accessToken === 'string' && typeof candidate.refreshToken === 'string';
}

function copyPair(pair: TokenPair): TokenPair {
  return typeof pair.expiresAt === 'number'
    ? { accessToken: pair.accessToken, refreshToken: pair.refreshToken, expiresAt: pair.expiresAt }
    : { accessToken: pair.accessToken, refreshToken: pair.refreshToken };
}

export function localStorageTokenStorage(key: string = DEFAULT_TOKEN_KEY): TokenStorage {
  return {
    get() {
      const ls = getLocalStorage();
      if (ls) {
        try {
          const raw = ls.getItem(key);
          if (raw === null) return memory.get(key) ?? null;
          const parsed: unknown = JSON.parse(raw);
          return isPair(parsed) ? copyPair(parsed) : null; // ignore corrupt payloads
        } catch {
          return memory.get(key) ?? null;
        }
      }
      return memory.get(key) ?? null;
    },

    set(pair) {
      const ls = getLocalStorage();
      if (ls) {
        try {
          ls.setItem(key, JSON.stringify(pair));
          return;
        } catch {
          // Quota / security error: keep this realm consistent via memory.
        }
      }
      memory.set(key, copyPair(pair));
    },

    clear() {
      const ls = getLocalStorage();
      if (ls) {
        try {
          ls.removeItem(key);
        } catch {
          /* ignore */
        }
      }
      memory.set(key, null);
    },
  };
}
