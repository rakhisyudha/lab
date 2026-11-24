/**
 * Minimal Svelte 5 (runes) example store. Illustrative only: `$state` is
 * compiled by the Svelte plugin in the consuming app, not in this example dir.
 */
import { ApiClient } from './apiClient';
import { RefreshCoordinator } from './refreshSingleFlight';
import { localStorageTokenStorage, type TokenPair, type TokenStorage } from './tokenStorage';

export interface AuthStoreOptions {
  baseUrl: string; // e.g. 'https://api.example.com/api/v1'
  storage?: TokenStorage;
  onSessionEnded?: () => void; // e.g. () => goto('/login'); called on fatal refresh
  /** Advanced: share pre-built instances (their onFatal wiring is the caller's job). */
  coordinator?: RefreshCoordinator;
  api?: ApiClient;
}

/** Normalise the server's snake_case token envelope into TokenPair. */
function toPair(value: unknown): TokenPair | null {
  if (typeof value !== 'object' || value === null) return null;
  const source = value as Record<string, unknown>;
  const tokens = typeof source.tokens === 'object' && source.tokens !== null
    ? (source.tokens as Record<string, unknown>)
    : source;
  const accessToken = tokens.accessToken ?? tokens.access_token;
  const refreshToken = tokens.refreshToken ?? tokens.refresh_token;
  if (typeof accessToken !== 'string' || typeof refreshToken !== 'string') return null;
  const pair: TokenPair = { accessToken, refreshToken };
  if (typeof tokens.expiresAt === 'number') pair.expiresAt = tokens.expiresAt;
  else if (typeof tokens.expires_in === 'number') pair.expiresAt = Date.now() + tokens.expires_in * 1000;
  return pair;
}

export function createAuthStore(options: AuthStoreOptions) {
  const storage = options.storage ?? localStorageTokenStorage();

  // Runes: reactive inside Svelte islands / .svelte.ts modules.
  let pair = $state<TokenPair | null>(storage.get());
  let status = $state<'anonymous' | 'authenticated' | 'loading'>(pair ? 'authenticated' : 'anonymous');
  let error = $state<string | null>(null);

  const coordinator = options.coordinator ?? new RefreshCoordinator({
    endpoint: `${options.baseUrl.replace(/\/+$/, '')}/auth/refresh`,
    storage,
    onFatal: () => {
      pair = null;
      status = 'anonymous';
      error = null;
      options.onSessionEnded?.();
    },
  });
  const api = options.api ?? new ApiClient({ baseUrl: options.baseUrl, coordinator, storage });

  /** Re-read storage (e.g. after a broadcast, focus event, or manual refresh). */
  function sync(): void {
    pair = storage.get();
    status = pair ? 'authenticated' : 'anonymous';
  }

  // Other tabs write the same storage key; `storage` events keep islands in sync.
  if (typeof window !== 'undefined') window.addEventListener('storage', sync);

  const state = {
    get status() {
      return status;
    },
    get isAuthenticated() {
      return status === 'authenticated' && !!pair;
    },
    get error() {
      return error;
    },
  };

  return {
    get tokenPair() {
      return pair;
    },
    state,
    sync,
    async login(email: string, password: string): Promise<TokenPair> {
      status = 'loading';
      error = null;
      try {
        const raw = await api.request<unknown>('/auth/login', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ email, password }),
        });
        const next = toPair(raw);
        if (!next) throw new Error('Login response did not contain a token pair');
        storage.set(next);
        pair = next;
        status = 'authenticated';
        return next;
      } catch (cause) {
        error = cause instanceof Error ? cause.message : 'Login failed';
        status = 'anonymous';
        throw cause;
      }
    },
    logout(): void {
      storage.clear();
      pair = null;
      status = 'anonymous';
      error = null;
    },
  };
}
