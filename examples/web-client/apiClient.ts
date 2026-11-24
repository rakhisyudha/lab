/** Fetch wrapper: refresh once on 401 `unauthenticated`, then retry exactly once. */
import type { RefreshCoordinator } from './refreshSingleFlight';
import type { TokenStorage } from './tokenStorage';

export interface ApiClientOptions {
  baseUrl: string; // e.g. 'https://api.example.com/api/v1'
  coordinator: RefreshCoordinator;
  storage: TokenStorage;
}

export class AuthError extends Error {
  constructor(
    message: string,
    readonly status?: number,
    readonly code?: string,
  ) {
    super(message);
    this.name = 'AuthError';
  }
}

function errorInfo(text: string): { code?: string; message?: string } {
  try {
    const data = JSON.parse(text) as { error?: { code?: unknown; message?: unknown } } | null;
    const code = data?.error?.code;
    const message = data?.error?.message;
    return {
      code: typeof code === 'string' ? code : undefined,
      message: typeof message === 'string' ? message : undefined,
    };
  } catch {
    return {};
  }
}

async function parseBody<T>(response: Response): Promise<T> {
  if (response.status === 204) return undefined as T;
  const text = await response.text();
  if (!text) return undefined as T;
  try {
    return JSON.parse(text) as T;
  } catch {
    return text as T; // non-JSON endpoints still resolve
  }
}

/** Fresh Headers object; method, body, mode, etc. are preserved by the spread. */
function cloneInit(init: RequestInit): RequestInit {
  return { ...init, headers: new Headers(init.headers) };
}

export class ApiClient {
  constructor(private readonly options: ApiClientOptions) {}

  async request<T>(path: string, init: RequestInit = {}): Promise<T> {
    const url = this.resolve(path);
    const first = await this.send(url, init, this.options.storage.get()?.accessToken);

    if (first.status !== 401) {
      this.assertOk(first);
      return parseBody<T>(first);
    }

    // Only an expired/invalid ACCESS token triggers refresh. Other 401s (bad
    // credentials, disabled user, fatal refresh codes) are not retried.
    const info = errorInfo(await first.clone().text());
    if (info.code !== 'unauthenticated') {
      throw new AuthError(info.message ?? 'Authentication failed', 401, info.code);
    }

    const pair = await this.options.coordinator.refresh();
    if (!pair) throw new AuthError('Session expired; please sign in again', 401, 'unauthenticated');

    const retried = await this.send(url, cloneInit(init), pair.accessToken);
    this.assertOk(retried);
    return parseBody<T>(retried); // exactly one retry
  }

  private resolve(path: string): string {
    if (/^https?:\/\//i.test(path)) return path;
    const base = this.options.baseUrl.replace(/\/+$/, '');
    return `${base}/${path.replace(/^\/+/, '')}`;
  }

  private send(url: string, init: RequestInit, accessToken?: string): Promise<Response> {
    const headers = new Headers(init.headers);
    if (accessToken) headers.set('Authorization', `Bearer ${accessToken}`);
    return fetch(url, { ...init, headers });
  }

  private assertOk(response: Response): void {
    if (response.ok) return;
    if (response.status === 401) throw new AuthError('Unauthenticated', 401);
    throw new Error(`API request failed with status ${response.status}`);
  }
}
