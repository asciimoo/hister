interface HotkeyConfig {
  show_hotkeys?: boolean;
  [key: string]: string | boolean | undefined;
}

interface SearchConfig {
  wsUrl: string;
  csrf: string;
  searchUrl: string;
  openResultsOnNewTab: boolean;
  hotkeys: HotkeyConfig;
}

interface SearchMessage {
  text: string;
  sort?: string;
  highlight?: string;
  semantic_enabled?: boolean;
  semantic_threshold?: number;
}

export interface SearchResult {
  url: string;
  title: string;
  domain: string;
  score?: number;
  text?: string;
  favicon?: string;
  favicon_key?: string;
  added?: number;
  updated?: number;
  add_count?: number;
  label?: string;
  pinned?: boolean;
}

export interface SemanticHit {
  doc_id: string;
  similarity: number;
  matched_chunk?: string;
  document?: SearchResult;
}

export interface TermCount {
  term: string;
  count: number;
  label?: string;
}

export interface RangeCount {
  name: string;
  count: number;
}

export interface TermFacet {
  terms?: TermCount[];
  other?: number;
}

export interface FacetsResult {
  terms?: Record<string, TermFacet>;
  date_histogram?: RangeCount[];
}

export interface SearchResults {
  documents?: SearchResult[];
  history?: SearchResult[];
  total?: number;
  search_duration?: string;
  query?: { text: string };
  query_suggestion?: string;
  semantic_hits?: SemanticHit[];
  semantic_enabled?: boolean;
  page_key?: string;
  facets?: FacetsResult;
}

export function escapeHTML(s: string): string {
  const pre = document.createElement('pre');
  pre.appendChild(document.createTextNode(s));
  return pre.innerHTML;
}

function escapeXML(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&apos;');
}

function escape(s: string): string {
  return encodeURIComponent(s);
}

export function formatTimestamp(unixTimestamp: number): string {
  const d = new Date(unixTimestamp * 1000);
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

export function formatRelativeTime(unixTimestamp: number): string {
  if (!unixTimestamp) return '';

  const now = Date.now();
  const timestamp = unixTimestamp * 1000;
  const secondsAgo = Math.floor((now - timestamp) / 1000);

  if (secondsAgo < 0) return 'just now';
  if (secondsAgo < 60) return 'just now';

  const minutesAgo = Math.floor(secondsAgo / 60);
  if (minutesAgo < 60) return minutesAgo === 1 ? '1 minute ago' : `${minutesAgo} minutes ago`;

  const hoursAgo = Math.floor(minutesAgo / 60);
  if (hoursAgo < 24) return hoursAgo === 1 ? '1 hour ago' : `${hoursAgo} hours ago`;

  const daysAgo = Math.floor(hoursAgo / 24);
  if (daysAgo < 7) return daysAgo === 1 ? 'yesterday' : `${daysAgo} days ago`;

  const weeksAgo = Math.floor(daysAgo / 7);
  if (weeksAgo < 4) return weeksAgo === 1 ? '1 week ago' : `${weeksAgo} weeks ago`;

  const monthsAgo = Math.floor(daysAgo / 30);
  if (monthsAgo < 12) return monthsAgo === 1 ? '1 month ago' : `${monthsAgo} months ago`;

  const yearsAgo = Math.floor(daysAgo / 365);
  return yearsAgo === 1 ? '1 year ago' : `${yearsAgo} years ago`;
}

export function formatMetaDate(iso: string | undefined): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (isNaN(d.getTime())) return iso;
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

function downloadFile(content: string, filename: string, mimeType: string): void {
  const blob = new Blob([content], { type: mimeType });
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download = filename;
  a.click();
  URL.revokeObjectURL(a.href);
}

export function exportJSON(results: SearchResults): void {
  if (!results) return;
  downloadFile(JSON.stringify(results, null, 2), 'results.json', 'application/json');
}

export function exportCSV(results: SearchResults, query: string): void {
  if (!results?.documents) return;
  downloadFile(
    [
      ['url', 'title', 'domain', 'score'],
      ...results.documents.map((d) => [d.url, d.title, d.domain, String(d.score || '')]),
    ]
      .map((r) => r.map((v) => `"${String(v || '').replace(/"/g, '""')}"`).join(','))
      .join('\n'),
    'results.csv',
    'text/csv',
  );
}

export function exportRSS(results: SearchResults, query: string): void {
  if (!results?.documents) return;
  downloadFile(
    `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Hister search: ${escapeXML(query)}</title>
    ${results.documents.map((d) => `<item><title>${escapeXML(d.title || '')}</title><link>${escapeXML(d.url || '')}</link></item>`).join('')}
  </channel>
</rss>`,
    'results.rss',
    'application/rss+xml',
  );
}

export function getSearchUrl(searchUrl: string, query: string): string {
  return searchUrl.replace('{query}', escape(query));
}

export function scrollTo(el: Element): void {
  el.scrollIntoView({ block: 'nearest' });
}

interface WebSocketManagerCallbacks {
  onOpen: () => void;
  onMessage: (event: MessageEvent) => void;
  onClose: () => void;
  onError: (event: Event) => void;
}

export class WebSocketManager {
  private ws: WebSocket | null = null;
  private wsUrl: string;
  private callbacks: WebSocketManagerCallbacks;
  private reconnectTimer: number | null = null;
  private debounceTimer: number | null = null;
  private inFlight: boolean = false;
  private pendingMessage: string | null = null;
  private readonly debounceMs: number;

  constructor(wsUrl: string, callbacks: WebSocketManagerCallbacks, debounceMs: number = 100) {
    this.wsUrl = wsUrl;
    this.callbacks = callbacks;
    this.debounceMs = debounceMs;
  }

  connect(): void {
    this.ws = new WebSocket(this.wsUrl);

    this.ws.onopen = () => {
      this.callbacks.onOpen();
    };

    this.ws.onmessage = (event) => {
      this.inFlight = false;
      if (this.pendingMessage !== null) {
        const msg = this.pendingMessage;
        this.pendingMessage = null;
        this.dispatch(msg);
      }
      this.callbacks.onMessage(event);
    };

    this.ws.onclose = () => {
      this.inFlight = false;
      this.pendingMessage = null;
      this.callbacks.onClose();
      this.scheduleReconnect();
    };

    this.ws.onerror = (event) => {
      this.callbacks.onError(event);
    };
  }

  send(message: string): void {
    if (this.debounceTimer !== null) {
      clearTimeout(this.debounceTimer);
    }
    this.debounceTimer = window.setTimeout(() => {
      this.debounceTimer = null;
      if (this.inFlight) {
        this.pendingMessage = message;
      } else {
        this.dispatch(message);
      }
    }, this.debounceMs);
  }

  // sendImmediate sends without debouncing. Use for load-more requests so that
  // a pending debounced query (from the user typing) is not cancelled.
  sendImmediate(message: string): void {
    if (this.inFlight) {
      this.pendingMessage = message;
    } else {
      this.dispatch(message);
    }
  }

  private dispatch(message: string): void {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.inFlight = true;
      this.ws.send(message);
    }
  }

  close(): void {
    if (this.debounceTimer !== null) {
      clearTimeout(this.debounceTimer);
      this.debounceTimer = null;
    }
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    this.ws?.close();
  }

  private scheduleReconnect(): void {
    this.reconnectTimer = window.setTimeout(() => {
      this.connect();
    }, 1000);
  }

  get isOpen(): boolean {
    return this.ws?.readyState === WebSocket.OPEN;
  }
}

interface APIRequestParams {
  method?: string;
  headers?: Record<string, string>;
  body?: string | URLSearchParams;
}

interface APIRequestOptions {
  url: string;
  params?: APIRequestParams;
  callback?: (response: Response) => void;
  csrfToken?: string;
  csrfCallback?: (token: string) => void;
}

function apiRequest(options: APIRequestOptions): void {
  const headers = options.params?.headers || {};
  if (options.csrfToken) {
    (headers as Record<string, string>)['X-CSRF-Token'] = options.csrfToken;
  }

  const params: RequestInit = {
    method: options.params?.method || 'GET',
    headers,
    body: options.params?.body as BodyInit,
  };

  fetch('api' + options.url, params).then((r) => {
    const newToken = r.headers.get('X-CSRF-Token');
    if (newToken && options.csrfToken !== undefined) {
      options.csrfCallback?.(newToken);
    }
    options.callback?.(r);
  });
}

export class KeyHandler {
  private hotkeys: Record<string, string>;
  private actions: Record<string, (e?: KeyboardEvent, isInputFocus?: boolean) => void | boolean>;

  constructor(
    hotkeys: HotkeyConfig,
    actions: Record<string, (e?: KeyboardEvent, isInputFocus?: boolean) => void | boolean>,
  ) {
    this.actions = actions;
    this.hotkeys = Object.fromEntries(
      Object.entries(hotkeys).filter(([, v]) => typeof v === 'string') as [string, string][],
    );
  }

  handle(e: KeyboardEvent, isInputFocus: boolean): boolean {
    if (!e.key) return false;

    const modifier = e.altKey ? 'alt' : e.ctrlKey ? 'ctrl' : e.metaKey ? 'meta' : undefined;
    const key =
      modifier && e.code?.startsWith('Key')
        ? `${modifier}+${e.code.replace('Key', '').toLowerCase()}`
        : modifier
          ? `${modifier}+${e.key.toLowerCase()}`
          : e.key.toLowerCase();

    const action = this.actions[this.hotkeys[key]];
    if (action && action(e, isInputFocus) !== true) {
      return true;
    }
    switch (key) {
      case 'tab':
        if (e.shiftKey) {
          this.actions['select_previous_result'](e, isInputFocus);
        } else {
          this.actions['select_next_result'](e, isInputFocus);
        }
        break;
      case 'arrowdown':
        this.actions['select_next_result'](e, isInputFocus);
        break;
      case 'arrowup':
        this.actions['select_previous_result'](e, isInputFocus);
        break;
      default:
        break;
    }
    return true;
  }
}

export const RESULTS_PER_PAGE = 20;

export interface SearchQueryOptions {
  sort?: string;
  semantic?: { enabled: boolean; threshold: number };
  pageKey?: string;
  limit?: number;
  facets?: boolean;
}

interface QueryParams {
  text: string;
  sort?: string;
  highlight?: string;
  semantic_enabled?: boolean;
  semantic_threshold?: number;
  limit?: number;
  page_key?: string;
  facets?: boolean;
}

export function buildSearchQuery(text: string, opts: SearchQueryOptions = {}): QueryParams {
  const { sort, semantic, pageKey, limit, facets } = opts;
  return {
    text,
    highlight: 'HTML',
    ...(sort && { sort }),
    ...(semantic && { semantic_enabled: semantic.enabled, semantic_threshold: semantic.threshold }),
    ...(limit && { limit }),
    ...(pageKey && { page_key: pageKey }),
    ...(facets && { facets: true }),
  };
}

export function parseSearchResults(data: string): SearchResults {
  const res = JSON.parse(data);
  return res;
}

export function openURL(url: string, newWindow: boolean = false): void {
  if (newWindow) {
    window.open(url, '_blank');
    window.focus();
    return;
  }
  window.location.href = url;
}
