import { resolve } from '$app/paths';

// Keep return destinations on UI pages. Auth and API routes can start another redirect.
const returnPages = new Set([
  '',
  '/add',
  '/rules',
  '/history',
  '/profile',
  '/help',
  '/about',
  '/api-docs',
  '/preview',
  '/extractors',
  '/settings',
]);

export function authReturnPath(target: string | null, basePath = resolve('/')): string {
  const prefix = basePath.replace(/\/$/, '');
  const home = `${prefix}/`;
  if (!target?.startsWith('/') || target.startsWith('//') || /[\x00-\x1f\x7f\\]/.test(target)) {
    return home;
  }
  try {
    // Validate the original path before URL parsing can normalize dot segments.
    const path = decodeURIComponent(target.split(/[?#]/, 1)[0]);
    if (path !== prefix && !path.startsWith(`${prefix}/`)) return home;
    const page = path.slice(prefix.length).replace(/\/$/, '');
    if (!returnPages.has(page)) return home;
    const url = new URL(target, window.location.origin);
    return `${prefix}${page || '/'}${url.search}${url.hash}`;
  } catch {
    return home;
  }
}

export function redirectToAuth(reason: 'auth_required' | 'invalid_token' = 'auth_required'): void {
  const target = window.location.pathname + window.location.search + window.location.hash;
  const params = new URLSearchParams({ reason, return_to: authReturnPath(target) });
  window.location.href = `${resolve('/')}auth?${params}`;
}
