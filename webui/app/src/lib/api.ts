import { base } from '$app/paths';
import type { SearchCapabilities } from '$lib/search-schema';

export interface AppConfig {
  basePath?: string;
  wsUrl: string;
  title: string;
  subtitle: string;
  colorScheme: 'automatic' | 'dark' | 'light';
  searchUrl: string;
  openResultsOnNewTab: boolean;
  hotkeys: Record<string, string>;
  authMode: 'token' | 'user' | 'none';
  authenticated: boolean;
  public: boolean;
  canWrite: boolean;
  historyEnabled: boolean;
  username?: string;
  userId?: number;
  oauthOnly?: boolean;
  disablePreviews?: boolean;
  semanticEnabled?: boolean;
  semanticWeight?: number;
  similarityThreshold?: number;
  search: SearchCapabilities;
}

export interface ExtractorInfo {
  name: string;
  description: string;
  enabled: boolean;
  capabilities: {
    enrich: boolean;
    extract: boolean;
    preview: boolean;
  };
  options?: Record<string, unknown>;
}

let _config: AppConfig | null = null;
let _configRequest: Promise<AppConfig> | null = null;
let _csrf: string = '';

function apiPath(path: string): string {
  if (path === '') {
    return `${base}/api`;
  }
  return `${base}/api${path.startsWith('/') ? path : `/${path}`}`;
}

function clearLegacyAccessToken(): void {
  localStorage.removeItem('access-token');
}

function redirectToAuth(reason: 'auth_required' | 'invalid_token' = 'auth_required'): void {
  const params = new URLSearchParams({ reason });
  window.location.href = `${base}/auth?${params.toString()}`;
}

function getCsrf(): string {
  return _csrf;
}

function setCsrf(tok: string): void {
  _csrf = tok;
}

function getAuthMode(): string {
  return _config?.authMode ?? 'none';
}

function getUsername(): string {
  return _config?.username ?? '';
}

export function getUserId(): number | undefined {
  return _config?.userId;
}

export function resetConfig(): void {
  _config = null;
  _configRequest = null;
}

export async function fetchConfig(): Promise<AppConfig> {
  if (_config) return _config;
  if (_configRequest) return _configRequest;

  const request = (async () => {
    clearLegacyAccessToken();
    const res = await fetch(apiPath('/config'), { credentials: 'include' });
    if (res.status === 403) {
      redirectToAuth();
      throw new Error('Authentication required');
    }
    const tok = res.headers.get('X-CSRF-Token');
    if (tok) _csrf = tok;
    return (await res.json()) as AppConfig;
  })();
  _configRequest = request;

  try {
    const config = await request;
    if (_configRequest === request) _config = config;
    return config;
  } finally {
    if (_configRequest === request) _configRequest = null;
  }
}

export async function login(username: string, password: string): Promise<{ username: string }> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  if (_csrf) headers['X-CSRF-Token'] = _csrf;
  const res = await fetch(apiPath('/login'), {
    method: 'POST',
    headers,
    credentials: 'include',
    body: JSON.stringify({ username, password }),
  });
  if (!res.ok) {
    throw new Error('Invalid credentials');
  }
  resetConfig();
  return res.json();
}

export async function loginWithToken(token: string): Promise<void> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  if (_csrf) headers['X-CSRF-Token'] = _csrf;
  const res = await fetch(apiPath('/token-login'), {
    method: 'POST',
    headers,
    credentials: 'include',
    body: JSON.stringify({ token }),
  });
  if (!res.ok) {
    throw new Error('Invalid credentials');
  }
  clearLegacyAccessToken();
  resetConfig();
}

export async function logout(): Promise<void> {
  try {
    await apiFetch('/logout', { method: 'POST', redirectOnForbidden: false });
  } finally {
    clearLegacyAccessToken();
    resetConfig();
  }
}

interface ApiFetchOptions extends RequestInit {
  redirectOnForbidden?: boolean;
}

export async function apiFetch(url: string, options: ApiFetchOptions = {}): Promise<Response> {
  const { redirectOnForbidden = true, ...fetchOptions } = options;
  const headers: Record<string, string> = {
    ...(fetchOptions.headers as Record<string, string>),
  };
  if (_csrf && fetchOptions.method && fetchOptions.method.toUpperCase() !== 'GET') {
    headers['X-CSRF-Token'] = _csrf;
  }
  clearLegacyAccessToken();
  const res = await fetch(apiPath(url), { ...fetchOptions, headers, credentials: 'include' });
  if (res.status === 403 && redirectOnForbidden) {
    redirectToAuth(getAuthMode() === 'token' ? 'invalid_token' : 'auth_required');
    throw new Error('Authentication required');
  }
  const newTok = res.headers.get('X-CSRF-Token');
  if (newTok) _csrf = newTok;
  return res;
}

export interface DomainStat {
  domain: string;
  pages: number;
  /** Length of the indexed text: an estimate of this domain's share of the search index. */
  text_bytes: number;
  /** Exact sizes on disk, counting only files this domain alone references. */
  html_bytes: number;
  favicon_bytes: number;
  /** Referenced but shared with other domains, so deleting this one releases none of it. */
  shared_bytes: number;
  total_bytes: number;
}

export async function fetchDomainStats(): Promise<DomainStat[]> {
  const res = await apiFetch('/stats/domains');
  if (!res.ok) {
    throw new Error('Failed to fetch domain statistics');
  }
  const data = await res.json();
  return data.domains ?? [];
}

export async function fetchExtractors(): Promise<ExtractorInfo[]> {
  const res = await apiFetch('/extractors');
  if (!res.ok) {
    throw new Error('Failed to fetch extractors');
  }
  return res.json();
}
