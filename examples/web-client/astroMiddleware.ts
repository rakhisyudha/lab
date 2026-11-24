/**
 * Dependency-free Astro middleware example. Astro types are intentionally
 * avoided (`any`) so this file can be dropped into a project as-is.
 *
 * Server-side code must supply a server-safe TokenStorage (e.g. an HttpOnly
 * cookie backed BFF store). Never use browser localStorage on the server.
 */
import type { TokenStorage } from './tokenStorage';

export interface AuthMiddlewareOptions {
  storage: TokenStorage;
  loginPath?: string; // default '/login'
  /** Override which paths require an access token. */
  isProtected?: (pathname: string) => boolean;
}

export type Middleware = (context: any, next: any) => Promise<any>;

export function createAuthMiddleware(options: AuthMiddlewareOptions): Middleware {
  const loginPath = options.loginPath ?? '/login';
  const isProtected = options.isProtected ?? ((pathname: string) =>
    pathname !== loginPath && !pathname.startsWith('/_astro/') && pathname !== '/favicon.ico');

  return async (context, next) => {
    const url = context?.url as URL | undefined;
    const pathname = url?.pathname ?? '/';
    if (!isProtected(pathname)) return next();
    if (options.storage.get()?.accessToken) return next();

    const target = `${loginPath}?redirect=${encodeURIComponent(pathname + (url?.search ?? ''))}`;
    if (typeof context?.redirect === 'function') return context.redirect(target, 302);
    return new Response(null, { status: 302, headers: { Location: target } });
  };
}

let configured: AuthMiddlewareOptions | null = null;

/** Call once at server startup: configureAuthMiddleware({ storage: myCookieStore }). */
export function configureAuthMiddleware(options: AuthMiddlewareOptions): void {
  configured = options;
}

/** Named Astro middleware export (src/middleware.ts re-export). */
export const middleware: Middleware = (context, next) => {
  if (!configured) {
    throw new Error('Call configureAuthMiddleware({ storage }) at startup, or use createAuthMiddleware(options).');
  }
  return createAuthMiddleware(configured)(context, next);
};

export default middleware;
