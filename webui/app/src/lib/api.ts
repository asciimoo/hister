export interface AppConfig {
  wsUrl: string;
  searchUrl: string;
  openResultsOnNewTab: boolean;
  hotkeys: Record<string, string>;
}

let _config: AppConfig | null = null;
let _csrf: string = '';

export function getCsrf(): string {
  return _csrf;
}

export function setCsrf(tok: string): void {
  _csrf = tok;
}

export async function fetchConfig(): Promise<AppConfig> {
  if (_config) return _config;
  const headers: Record<string, string> = {};
  const token = localStorage.getItem('access-token');
  if (token) {
    headers['X-Access-Token'] = token;
  }
  const res = await fetch('api/config', { headers });
  if (res.status === 403) {
    window.location.href = '/auth';
    throw new Error('Authentication required');
  }
  const tok = res.headers.get('X-CSRF-Token');
  if (tok) _csrf = tok;
  _config = await res.json();
  return _config!;
}

export async function apiFetch(url: string, options: RequestInit = {}): Promise<Response> {
  const headers: Record<string, string> = {
    ...(options.headers as Record<string, string>),
  };
  if (_csrf && options.method && options.method.toUpperCase() !== 'GET') {
    headers['X-CSRF-Token'] = _csrf;
  }
  const token = localStorage.getItem('access-token');
  if (token) {
    headers['X-Access-Token'] = token;
  }
  const res = await fetch('api' + url, { ...options, headers });
  if (res.status === 403) {
    window.location.href = '/auth';
    throw new Error('Authentication required');
  }
  const newTok = res.headers.get('X-CSRF-Token');
  if (newTok) _csrf = newTok;
  return res;
}
