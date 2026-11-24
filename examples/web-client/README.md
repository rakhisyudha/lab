# Single-flight refresh example (Astro + Svelte)

Illustrative TypeScript sources for the BackendLab client contract. This directory is **not** a build target: no install, build, or test is run here. The dev dependencies in `package.json` are only there so a consumer (or the Vitest task) can type-check/test the examples.

## Files

| File | Purpose |
| --- | --- |
| `tokenStorage.ts` | `TokenPair`, `TokenStorage`, and `localStorageTokenStorage()` with an in-memory fallback. |
| `refreshSingleFlight.ts` | `RefreshCoordinator`: the single-flight guarantee and fatal-error handling. |
| `apiClient.ts` | `ApiClient.request<T>()` + `AuthError`: 401 `unauthenticated` → refresh → one retry. |
| `authStore.svelte.ts` | Thin Svelte 5 runes store (`tokenPair`, `login`, `logout`, `state`). |
| `astroMiddleware.ts` | Dependency-free Astro middleware; redirects to `/login` without an access token. |

## The invariant

> At most one `POST /api/v1/auth/refresh` is in flight **per refresh token**, across all parallel callers in a tab and across all tabs. Everyone else waits for that promise and uses the returned pair.

`RefreshCoordinator.refresh()` enforces this in three layers:

1. **In-tab:** a single shared promise (`inflight`) is returned to every caller and cleared in `finally`.
2. **Cross-tab:** `navigator.locks.request('backendlab-refresh', ...)` serialises tabs where Web Locks exist. The new pair is persisted inside the lock and broadcast on `BroadcastChannel('backendlab-auth')`.
3. **Fallback:** without Web Locks, a `localStorage` mutex at key `backendlab-refresh-lock` is acquired by polling (owner id + timestamp, stolen after 10 s to recover from a crashed tab), always released in `finally`.

**The re-read is mandatory:** after acquiring any lock, the coordinator calls `storage.get()` again. If the stored refresh token differs from the one captured before waiting, another tab already rotated it, so the stored pair is returned **without** calling `/auth/refresh`. This is what makes duplicate refreshes harmless even if two tabs were queued behind the same lock.

Locks and the broadcast are always cleaned up in `finally`; a broadcast of the new pair is sent on success, and a `cleared` broadcast is sent on fatal errors so other tabs drop their tokens too.

## Why the server has no grace window

Rotation marks the old refresh token revoked immediately. Because reuse of a revoked token is treated as theft, the server revokes the **entire token family** and returns `401 refresh_token_reuse_detected` — no time-based tolerance, no "second call with the old token is fine" window. Two concurrent refreshes with the same token would therefore log the user out of every device in that family. Preventing that is a client obligation, not a server feature. Token rotation is atomic server-side; the client side is this coordinator.

## Error handling

- `refresh_token_reuse_detected` / `invalid_refresh_token` are final: `storage.clear()`, `onFatal?.()` (navigate to `/login`), return `null`, no retry.
- Network failures, 5xx, malformed success bodies: return `null`, **keep** the tokens so a later call can retry.
- `ApiClient` only refreshes on a 401 whose envelope code is `unauthenticated`. Other 401s and all other statuses are surfaced without retrying. The original method/body/headers are cloned for the single retry (a streaming `ReadableStream` body cannot be replayed — callers must recreate such bodies).

## Wiring

```ts
// Svelte app
const auth = createAuthStore({
  baseUrl: 'https://api.example.com/api/v1',
  onSessionEnded: () => goto('/login'),
});
await auth.login(email, password);
const data = await api.request<Me>('/me');
```

```ts
// src/middleware.ts (Astro) — app provides a server-safe store (e.g. HttpOnly cookie BFF)
import { configureAuthMiddleware } from '../../examples/web-client/astroMiddleware';
configureAuthMiddleware({ storage: cookieTokenStore });
export { middleware as onRequest } from '../../examples/web-client/astroMiddleware';
```

## Assumptions

- `baseUrl` already includes the API prefix (e.g. `.../api/v1`); the coordinator endpoint is `baseUrl + '/auth/refresh'`.
- The refresh response is the token pair directly, `{ access_token, refresh_token, token_type, expires_in }`; camelCase fields and a nested `{ tokens: ... }` shape are also accepted defensively. `expires_in` seconds are converted to `expiresAt` milliseconds.
- Errors follow the `{ error: { code, message, details, request_id } }` envelope.
- One `RefreshCoordinator` instance per app/realm is recommended; multiple instances would still be serialised by the cross-tab lock, but would not share the in-tab promise.
- The `localStorage` mutex is a best-effort fallback: `localStorage` has no atomic compare-and-set, so a narrow race remains if two tabs acquire it in the same tick. The mandatory re-read closes the harmful case; apps on older browsers should prefer waiting for a broadcast over refreshing proactively during that window. Web Locks (the primary path) has no such race.
- In non-browser SSR there is no shared storage and no `navigator.locks`; only in-tab single-flight applies there. Astro middleware therefore expects the app to supply its own server-side `TokenStorage` and per-session locking (BFF pattern).
- A fatal broadcast clears tokens in other tabs but does not invoke their `onFatal` callback; their next authed request fails with `AuthError`, which the app can use to navigate to `/login`.
