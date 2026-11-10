import { fetchAPI, sendPageData, sendPDFData, sendResult } from '../modules/network';
import { ensureDefaultServerURL } from '../modules/settings';
import { getTabPageURL } from '../modules/tabs';

void ensureDefaultServerURL();

const missingURLMsg = {
  error: 'Missing or invalid Hister server URL. Configure it in the addon popup.',
};

type CustomHeader = { name: string; value: string };
type SkipRuleType = 'url' | 'domain';
type IndexingResult = { status?: string; status_code?: number; error?: string };

function escapeRegex(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

function buildUrlSkipPattern(url: string): string {
  return escapeRegex(url) + '$';
}

function buildDomainSkipPattern(url: string): string {
  try {
    return escapeRegex(new URL(url).origin);
  } catch (_) {
    return escapeRegex(url);
  }
}

// --- Badge helpers ---

function setErrorBadge(tabId: number) {
  chrome.action.setBadgeText({ text: '!', tabId }, () => void chrome.runtime.lastError);
  chrome.action.setBadgeBackgroundColor(
    { color: '#ff4444', tabId },
    () => void chrome.runtime.lastError,
  );
}

function setPreviouslyIndexedBadge(tabId: number) {
  chrome.action.setBadgeText({ text: '✓', tabId }, () => void chrome.runtime.lastError);
  chrome.action.setBadgeBackgroundColor(
    { color: '#44aa44', tabId },
    () => void chrome.runtime.lastError,
  );
  try {
    void chrome.action.setBadgeTextColor?.({ color: '#ffffff', tabId })?.catch(() => {});
  } catch (_) {}
}

function clearBadge(tabId: number) {
  chrome.action.setBadgeText({ text: '', tabId }, () => void chrome.runtime.lastError);
}

// --- Icon helpers ---

let greyIconCache: Record<number, ImageData> | null = null;
let normalIconCache: Record<number, ImageData> | null = null;

async function buildIcons(grey: boolean): Promise<Record<number, ImageData>> {
  const response = await fetch(chrome.runtime.getURL('assets/icons/icon128.png'));
  const blob = await response.blob();
  const bitmap = await createImageBitmap(blob);
  const result: Record<number, ImageData> = {};
  for (const size of [16, 32]) {
    const canvas = new OffscreenCanvas(size, size);
    const ctx = canvas.getContext('2d')!;
    ctx.drawImage(bitmap, 0, 0, size, size);
    const imageData = ctx.getImageData(0, 0, size, size);
    if (grey) {
      for (let i = 0; i < imageData.data.length; i += 4) {
        const lum =
          imageData.data[i] * 0.299 + imageData.data[i + 1] * 0.587 + imageData.data[i + 2] * 0.114;
        imageData.data[i] = lum;
        imageData.data[i + 1] = lum;
        imageData.data[i + 2] = lum;
        imageData.data[i + 3] = Math.round(imageData.data[i + 3] * 0.5);
      }
    }
    result[size] = imageData;
  }
  return result;
}

async function setGreyIcon(tabId: number): Promise<void> {
  clearBadge(tabId);
  try {
    if (!greyIconCache) greyIconCache = await buildIcons(true);
    chrome.action.setIcon({ imageData: greyIconCache, tabId }, () => void chrome.runtime.lastError);
  } catch (_) {}
}

async function setNormalIcon(tabId: number): Promise<void> {
  try {
    if (!normalIconCache) normalIconCache = await buildIcons(false);
    chrome.action.setIcon(
      { imageData: normalIconCache, tabId },
      () => void chrome.runtime.lastError,
    );
  } catch (_) {}
}

// --- Per-tab sensitive-content rejection state ---

// Maps tabId → URL that was last rejected due to sensitive content.
const tabSensitiveState = new Map<number, string>();

chrome.tabs.onRemoved.addListener((tabId) => {
  tabSensitiveState.delete(tabId);
  forgetNavigationStatus(tabId);
});

// --- Main frame HTTP status tracking ---

// `PerformanceNavigationTiming.responseStatus` is only implemented by Chromium,
// so the content script cannot tell an error page from a successful one on
// Firefox. Capture the status of main frame navigations here instead and gate
// document submission on it.

type NavigationStatus = { url: string; statusCode: number; isPDF?: boolean };

// Maps tabId → the most recent main frame responses of that tab, oldest first.
// A short history is needed because a page submits a final snapshot while it is
// being replaced, by which time the next navigation is already recorded.
// Mirrored in chrome.storage.session (when available) so the state survives a
// suspended MV3 background script.
const MAX_TRACKED_NAVIGATIONS = 5;
const tabNavigationStatus = new Map<number, NavigationStatus[]>();

// `chrome.storage.session` requires Chrome >= 102 / Firefox >= 115.
const sessionStore = chrome.storage.session;

function navigationStatusKey(tabId: number): string {
  return `navStatus:${tabId}`;
}

// The content script strips the fragment from the submitted URL, and
// webRequest URLs never carry one.
function normalizeNavigationURL(url: string): string {
  const i = url.indexOf('#');
  return i === -1 ? url : url.slice(0, i);
}

// Redirects (3xx) are followed by the browser and overwritten by the status of
// the redirect target, and 304 responses render a perfectly valid cached page,
// so only 4xx/5xx are treated as unsuccessful.
function isErrorStatus(statusCode: number): boolean {
  return statusCode >= 400;
}

function recordNavigationStatus(tabId: number, url: string, statusCode: number, isPDF: boolean) {
  const entry: NavigationStatus = { url: normalizeNavigationURL(url), statusCode, isPDF };
  const history = (tabNavigationStatus.get(tabId) ?? [])
    .filter((e) => e.url !== entry.url)
    .concat(entry)
    .slice(-MAX_TRACKED_NAVIGATIONS);
  tabNavigationStatus.set(tabId, history);
  if (sessionStore) {
    void sessionStore.set({ [navigationStatusKey(tabId)]: history }).catch(() => {});
  }
}

function forgetNavigationStatus(tabId: number) {
  tabNavigationStatus.delete(tabId);
  if (sessionStore) {
    void sessionStore.remove(navigationStatusKey(tabId)).catch(() => {});
  }
}

async function readNavigationStatus(tabId: number): Promise<NavigationStatus[]> {
  const cached = tabNavigationStatus.get(tabId);
  if (cached || !sessionStore) {
    return cached ?? [];
  }
  try {
    const key = navigationStatusKey(tabId);
    return ((await sessionStore.get(key))[key] as NavigationStatus[]) ?? [];
  } catch (_) {
    return [];
  }
}

// Returns the unsuccessful status `url` was served with in `tabId`, or
// undefined. An unknown URL is not an error: cached responses, non-HTTP pages
// and history navigations never reach the network.
async function isErrorPage(tabId: number, url: string): Promise<number | undefined> {
  const history = await readNavigationStatus(tabId);
  const wanted = normalizeNavigationURL(url);
  return history.find((e) => e.url === wanted && isErrorStatus(e.statusCode))?.statusCode;
}

// `chrome.webRequest` is undefined when the permission is not granted; in that
// case the status stays unknown and documents are submitted as before.
chrome.webRequest?.onHeadersReceived.addListener(
  (details) => {
    if (details.tabId < 0) return;
    // PDF detection shares this listener with navigation-status tracking. The
    // status is keyed by the URL navigated to, not the submitted URL, because
    // a page can declare a canonical URL; the PDF path uses the same request
    // URL (details.url), so both stay consistent.
    const contentType = details.responseHeaders?.find(
      (h) => h.name.toLowerCase() === 'content-type',
    )?.value;
    const isPdf = isPDFContentType(contentType);
    // Keep PDF detection with its URL in session storage. A download response
    // can belong to a tab that still displays the previous HTML page.
    recordNavigationStatus(details.tabId, details.url, details.statusCode, isPdf);
    if (details.statusCode !== 200 || !isPdf) return;
    // The response is a PDF, but the navigation has not committed yet (the tab
    // still points at the previous page) and the browser's PDF viewer derives
    // the document's real title only once the PDF has loaded. Defer the index
    // to tabs.onUpdated so we can use that real title; PDFs served as
    // downloads never commit, so schedulePdfIndex also arms a fallback that
    // indexes with the request URL as the title rather than miss them.
    schedulePdfIndex(details.tabId, details.url);
  },
  { urls: ['<all_urls>'], types: ['main_frame'] },
  ['responseHeaders'],
);

// --- Indexing rules cache ---

interface IndexingRules {
  allow: (RegExp | null)[];
  skip: (RegExp | null)[];
}

interface IndexingRulesCache {
  rules: IndexingRules;
  timestamp: number;
}

const INDEXING_RULES_TTL = 60_000;
let indexingRulesCache: IndexingRulesCache | null = null;

function compilePatterns(patterns: string[]): (RegExp | null)[] {
  return patterns.map((pattern) => {
    try {
      return new RegExp(pattern, 'u');
    } catch (_) {
      // Go patterns unsupported by JavaScript are evaluated by the server.
      return null;
    }
  });
}

function isURLSkipped(url: string, rules: IndexingRules): boolean {
  return (
    (rules.allow.length > 0 && !rules.allow.some((re) => re === null || re.test(url))) ||
    rules.skip.some((re) => re?.test(url))
  );
}

function isPageSkipped(url: string, sourceURL: string, rules: IndexingRules): boolean {
  return isURLSkipped(url, rules) || isURLSkipped(sourceURL.split('#', 1)[0], rules);
}

async function getIndexingRules(
  serverURL: string,
  customHeaders: CustomHeader[],
): Promise<IndexingRules> {
  const now = Date.now();
  if (indexingRulesCache && now - indexingRulesCache.timestamp < INDEXING_RULES_TTL) {
    return indexingRulesCache.rules;
  }
  try {
    const u = serverURL.endsWith('/') ? serverURL : serverURL + '/';
    const r = await fetchAPI(u + 'api/rules', { customHeaders });
    if (!r.ok) return indexingRulesCache?.rules ?? { allow: [], skip: [] };
    const data = await r.json();
    const rules = {
      allow: compilePatterns(data.allow ?? []),
      skip: compilePatterns(data.skip ?? []),
    };
    indexingRulesCache = { rules, timestamp: now };
    return rules;
  } catch (_) {
    return indexingRulesCache?.rules ?? { allow: [], skip: [] };
  }
}

function getCustomHeaders(data): CustomHeader[] {
  const customHeaders: CustomHeader[] = Array.isArray(data['histerCustomHeaders'])
    ? [...data['histerCustomHeaders']]
    : [];
  if (data['histerToken']) {
    customHeaders.push({ name: 'X-Access-Token', value: data['histerToken'] });
  }
  return customHeaders;
}

function getDocumentSubmissionHeaders(data): CustomHeader[] {
  const customHeaders = getCustomHeaders(data);
  if (data['submitPublicDocuments'] === true) {
    customHeaders.push({ name: 'X-Hister-Public', value: '1' });
  }
  return customHeaders;
}

async function saveSkipRule(
  baseURL: string,
  customHeaders: CustomHeader[],
  pattern: string,
  deleteQuery?: string,
): Promise<void> {
  const rulesResp = await fetchAPI(baseURL + 'api/rules', { customHeaders });
  if (!rulesResp.ok) {
    throw new Error(`Failed to fetch rules: ${rulesResp.status}`);
  }
  const rulesData = await rulesResp.json();
  const existingSkip: string[] = rulesData.skip ?? [];
  const existingPriority: string[] = rulesData.priority ?? [];
  const existingVersioning: string[] = rulesData.versioning ?? [];
  const newSkip = [...existingSkip, pattern];
  const saveResp = await fetchAPI(baseURL + 'api/rules', {
    formData: {
      skip: newSkip.join(' '),
      priority: existingPriority.join(' '),
      versioning: existingVersioning.join(' '),
    },
    customHeaders,
  });
  if (!saveResp.ok) {
    throw new Error(`Failed to save rule: ${saveResp.status}`);
  }
  if (deleteQuery) {
    const deleteResp = await fetchAPI(baseURL + 'api/delete', {
      body: { query: deleteQuery },
      customHeaders,
    });
    if (!deleteResp.ok) {
      throw new Error(`Failed to delete documents: ${deleteResp.status}`);
    }
  }
  indexingRulesCache = null;
}

// --- Tab icon state ---

async function updateTabIcon(tabId: number, url: string): Promise<void> {
  if (
    !url ||
    url.startsWith('chrome://') ||
    url.startsWith('about:') ||
    url.startsWith('moz-extension://') ||
    url.startsWith('chrome-extension://')
  ) {
    return;
  }
  const data = await chrome.storage.local.get([
    'histerURL',
    'histerToken',
    'indexingEnabled',
    'histerCustomHeaders',
    'showIndexedBadge',
  ]);

  const serverURL: string = data['histerURL'] || '';
  const showIndexedBadge: boolean = data['showIndexedBadge'] === true;
  const customHeaders: { name: string; value: string }[] = Array.isArray(
    data['histerCustomHeaders'],
  )
    ? data['histerCustomHeaders']
    : [];
  if (data['histerToken']) {
    customHeaders.push({ name: 'X-Access-Token', value: data['histerToken'] });
  }

  const pageURL = await getTabPageURL(tabId, url);
  if (data['indexingEnabled'] === false) {
    await setGreyIcon(tabId);
    if (showIndexedBadge && serverURL) {
      const indexed = await isUrlPreviouslyIndexed(pageURL, serverURL, customHeaders);
      if (indexed) setPreviouslyIndexedBadge(tabId);
    }
    return;
  }

  if (!serverURL) {
    setNormalIcon(tabId);
    return;
  }

  const rules = await getIndexingRules(serverURL, customHeaders);
  if (isPageSkipped(pageURL, url, rules)) {
    await setGreyIcon(tabId);
  } else {
    setNormalIcon(tabId);
    clearBadge(tabId);
    if (showIndexedBadge) {
      const indexed = await isUrlPreviouslyIndexed(pageURL, serverURL, customHeaders);
      if (indexed) setPreviouslyIndexedBadge(tabId);
    }
  }
}

// --- PDF tab indexing ---

// The primary signal for "this tab is a PDF" is the actual HTTP Content-Type
// header reported by the server (observed via webRequest.onHeadersReceived),
// which handles content-server PDFs whose path has no .pdf extension. isPDFUrl()
// is retained only as a conservative fallback for the manual "index current
// page" command, where no network response is being navigated at that moment:
// it just matches URLs whose path ends in .pdf.
function isPDFUrl(url: string): boolean {
  try {
    const pathname = new URL(url).pathname.toLowerCase();
    return pathname.endsWith('.pdf');
  } catch (_) {
    return false;
  }
}

// Content-Type value split on ';' so we match "application/pdf; charset=..." too.
function isPDFContentType(value: string | undefined): boolean {
  return value !== undefined && value.toLowerCase().split(';')[0].trim() === 'application/pdf';
}

// Non-pageload schemes we must never try to index.
function doNotIndexUrl(url: string): boolean {
  return (
    url.startsWith('chrome://') ||
    url.startsWith('about:') ||
    url.startsWith('moz-extension://') ||
    url.startsWith('chrome-extension://') ||
    url.startsWith('file://')
  );
}

// Guards against indexing the same PDF tab twice (webRequest fires per HTTP
// response and tabs.onUpdated may also fire after navigation completes).
const recentPdfIndex = new Map<string, number>();
const PDF_INDEX_DEDUPE_MS = 30_000;

function markPdfIndexed(tabId: number, url: string): boolean {
  const key = `${tabId}:${url}`;
  const last = recentPdfIndex.get(key);
  const now = Date.now();
  if (last !== undefined && now - last < PDF_INDEX_DEDUPE_MS) {
    return false;
  }
  recentPdfIndex.set(key, now);
  return true;
}

// A PDF was seen in onHeadersReceived, but the navigation has not committed
// and the PDF viewer has not derived the document title yet. The actual index
// is deferred to tabs.onUpdated (where tab.title is the real title); PDFs
// served as downloads never commit, so a fallback timer indexes them with the
// request URL as the title rather than miss them.
const pendingPdfIndex = new Map<number, Map<string, ReturnType<typeof setTimeout>>>();
const PDF_INDEX_FALLBACK_MS = 5000;

function schedulePdfIndex(tabId: number, url: string) {
  if (doNotIndexUrl(url)) return;
  url = normalizeNavigationURL(url);
  const pending = pendingPdfIndex.get(tabId) ?? new Map();
  if (pending.has(url)) return;
  const timer = setTimeout(() => {
    // No onUpdated 'complete' arrived (the server sent the PDF as an
    // attachment download, so there is no committed page). Index anyway,
    // using the request URL as the title.
    pending.delete(url);
    if (pending.size === 0) pendingPdfIndex.delete(tabId);
    void chrome.tabs.get(tabId).then(
      (tab) => {
        if (!tab?.id) return;
        if (!markPdfIndexed(tab.id, url)) return;
        void indexPDFTab(tab.id, { ...tab, url, title: url });
      },
      () => {}, // The tab may close after its fallback timer fires.
    );
  }, PDF_INDEX_FALLBACK_MS);
  pending.set(url, timer);
  pendingPdfIndex.set(tabId, pending);
}

// The navigation has committed to `url`, but the PDF viewer sets tab.title to
// the document's real embedded title only after it has loaded (a little later
// than the 'complete' event). Poll briefly for that real title, then index;
// fall back to the request URL as the title if it never appears.
async function indexCommittedPdf(tabId: number, url: string) {
  let title = '';
  for (let attempt = 0; attempt < 12; attempt++) {
    const tab = await chrome.tabs.get(tabId).catch(() => undefined);
    if (!tab) return;
    if (normalizeNavigationURL(tab.url ?? '') !== normalizeNavigationURL(url)) break;
    const t = tab.title ?? '';
    if (t && normalizeNavigationURL(t) !== normalizeNavigationURL(url)) {
      title = t;
      break;
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  if (!markPdfIndexed(tabId, url)) return;
  const tab = await chrome.tabs.get(tabId).catch(() => undefined);
  if (!tab?.id) return;
  void indexPDFTab(tab.id, { ...tab, url, title: title || url });
}

chrome.tabs.onRemoved.addListener((tabId) => {
  for (const key of recentPdfIndex.keys()) {
    if (key.startsWith(`${tabId}:`)) recentPdfIndex.delete(key);
  }
  const pending = pendingPdfIndex.get(tabId);
  for (const timer of pending?.values() ?? []) clearTimeout(timer);
  pendingPdfIndex.delete(tabId);
});

// Authoritative PDF detection lives in the navigation-status listener above;
// this section keeps the dedup bookkeeping it relies on.

async function indexPDFTab(
  tabId: number,
  tab: chrome.tabs.Tab,
  ignoreSkipRules = false,
): Promise<IndexingResult> {
  const data = await chrome.storage.local.get([
    'histerURL',
    'histerToken',
    'indexingEnabled',
    'histerCustomHeaders',
    'histerLabel',
    'showIndexedBadge',
    'submitPublicDocuments',
  ]);

  if (data['indexingEnabled'] === false && !ignoreSkipRules) return { status: 'disabled' };

  const serverURL: string = data['histerURL'] || '';
  if (!serverURL) return missingURLMsg;

  const showIndexedBadge: boolean = data['showIndexedBadge'] === true;

  const customHeaders = getDocumentSubmissionHeaders(data);

  const rules = await getIndexingRules(serverURL, customHeaders);
  if (!ignoreSkipRules && isURLSkipped(tab.url!, rules)) {
    await setGreyIcon(tabId);
    return { status: 'ok', status_code: 406 };
  }

  const u = serverURL.endsWith('/') ? serverURL : serverURL + '/';

  try {
    const response = await fetch(tab.url!, { credentials: 'include' });
    if (!response.ok) {
      setErrorBadge(tabId);
      return { error: `Failed to fetch PDF: ${response.status}` };
    }
    const buffer = await response.arrayBuffer();
    const bytes = new Uint8Array(buffer);
    let binary = '';
    for (let i = 0; i < bytes.length; i++) {
      binary += String.fromCharCode(bytes[i]);
    }
    const pdfBase64 = btoa(binary);

    const doc: Record<string, unknown> = {
      url: tab.url!,
      title: tab.title || tab.url!,
    };
    if (ignoreSkipRules) {
      doc.metadata = { ignore_skip_rules: true };
    }
    if (data['histerLabel']) {
      doc['label'] = data['histerLabel'];
    }

    const r = await sendPDFData(u + 'api/add_pdf', doc, pdfBase64, customHeaders);
    if (r.status === 201) {
      setNormalIcon(tabId);
      if (showIndexedBadge) {
        setPreviouslyIndexedBadge(tabId);
      } else {
        clearBadge(tabId);
      }
    } else if (r.status === 406) {
      indexingRulesCache = null;
      setGreyIcon(tabId);
    } else if (r.status === 422) {
      tabSensitiveState.set(tabId, tab.url!);
      setGreyIcon(tabId);
    } else {
      setErrorBadge(tabId);
    }
    return { status: 'ok', status_code: r.status };
  } catch (err) {
    setErrorBadge(tabId);
    return { error: err instanceof Error ? err.message : String(err) };
  }
}

// --- Tab listeners ---

chrome.tabs.onActivated.addListener(async ({ tabId }) => {
  try {
    const tab = await chrome.tabs.get(tabId);
    if (tab.url) await updateTabIcon(tabId, tab.url);
  } catch (_) {}
});

chrome.tabs.onUpdated.addListener(async (tabId, changeInfo, tab) => {
  if (changeInfo.status === 'complete' && tab.url) {
    await updateTabIcon(tabId, tab.url);
    // Deferred PDF index: the navigation has committed; indexCommittedPdf
    // waits for the PDF viewer to set tab.title to the real document title
    // before submitting. (Downloads never commit here; schedulePdfIndex's
    // fallback timer indexes them with the URL as title.)
    const url = normalizeNavigationURL(tab.url);
    const pending = pendingPdfIndex.get(tabId);
    const timer = pending?.get(url);
    if (pending && timer !== undefined) {
      pending.delete(url);
      if (pending.size === 0) pendingPdfIndex.delete(tabId);
      clearTimeout(timer);
      void indexCommittedPdf(tabId, url);
    }
  }
});

chrome.storage.onChanged.addListener(async (changes, area) => {
  if (area !== 'local') return;
  const connectionChanged =
    'histerURL' in changes ||
    'histerToken' in changes ||
    'histerCookies' in changes ||
    'histerCustomHeaders' in changes;
  if (!(connectionChanged || 'indexingEnabled' in changes || 'showIndexedBadge' in changes)) return;
  if (connectionChanged) indexingRulesCache = null;
  try {
    const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
    if (tab?.id && tab.url) await updateTabIcon(tab.id, tab.url);
  } catch (_) {}
});

async function isUrlPreviouslyIndexed(
  url: string,
  serverURL: string,
  customHeaders: CustomHeader[],
): Promise<boolean> {
  try {
    const base = serverURL.endsWith('/') ? serverURL : serverURL + '/';
    const r = await fetchAPI(`${base}api/document?url=${encodeURIComponent(url)}`, {
      method: 'HEAD',
      customHeaders,
    });
    return r.status === 200;
  } catch (_) {
    return false;
  }
}

async function indexCurrentTab(): Promise<IndexingResult> {
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  if (!tab?.id) return { error: 'No active tab found' };

  // Match the current URL, not just the tab: PDF downloads do not replace its
  // displayed page. Session storage preserves detection across worker restarts.
  const url = normalizeNavigationURL(tab.url ?? '');
  const isPdf =
    isPDFUrl(url) ||
    (await readNavigationStatus(tab.id)).some((entry) => entry.url === url && entry.isPDF);
  if (isPdf) {
    return indexPDFTab(tab.id, tab, true);
  }

  return new Promise((resolve) => {
    chrome.tabs.sendMessage(tab.id!, { action: 'reindex' }, (response) => {
      const error = chrome.runtime.lastError;
      if (error || response?.status_code !== 201) {
        setErrorBadge(tab.id!);
        resolve(
          error ? { error: error.message } : (response ?? { error: 'No response from page' }),
        );
        return;
      }
      setPreviouslyIndexedBadge(tab.id!);
      setTimeout(() => clearBadge(tab.id!), 2500);
      resolve(response);
    });
  });
}

async function disableIndexingForCurrentTab(type: SkipRuleType): Promise<void> {
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  if (!tab?.id || !tab.url) return;

  try {
    const data = await chrome.storage.local.get([
      'histerURL',
      'histerToken',
      'histerCustomHeaders',
    ]);
    let serverURL: string = data['histerURL'] || '';
    if (!serverURL) {
      setErrorBadge(tab.id);
      return;
    }
    if (!serverURL.endsWith('/')) {
      serverURL += '/';
    }
    const pageURL = await getTabPageURL(tab.id, tab.url);
    const pattern = type === 'url' ? buildUrlSkipPattern(pageURL) : buildDomainSkipPattern(pageURL);
    await saveSkipRule(serverURL, getCustomHeaders(data), pattern);
    await setGreyIcon(tab.id);
    setPreviouslyIndexedBadge(tab.id);
    setTimeout(() => clearBadge(tab.id!), 2500);
  } catch (_) {
    setErrorBadge(tab.id);
  }
}

chrome.commands?.onCommand?.addListener((command) => {
  if (command === 'index-current-page') {
    void indexCurrentTab();
  } else if (command === 'disable-indexing-current-page') {
    void disableIndexingForCurrentTab('url');
  } else if (command === 'disable-indexing-current-domain') {
    void disableIndexingForCurrentTab('domain');
  }
});

// --- Document submission ---

function applySubmissionStatus(status: number, sender, showIndexedBadge: boolean) {
  const tabId = sender.tab?.id;
  if (tabId === undefined) return;
  if (status === 201) {
    setNormalIcon(tabId);
    if (showIndexedBadge) {
      setPreviouslyIndexedBadge(tabId);
    } else {
      clearBadge(tabId);
    }
  } else if (status === 406) {
    // Server indexing rules rejected the URL; invalidate cache and grey out
    indexingRulesCache = null;
    setGreyIcon(tabId);
  } else if (status === 422) {
    // Document rejected due to sensitive content; not an error
    tabSensitiveState.set(tabId, sender.tab.url ?? '');
    setGreyIcon(tabId);
  } else {
    setErrorBadge(tabId);
  }
}

async function submitPageData(baseURL: string, request, sender, data): Promise<IndexingResult> {
  const force = request.action === 'reindex';
  const tabId = sender.tab?.id;
  if (!force) {
    // The status belongs to the URL that was navigated to, which is not
    // necessarily the submitted URL: a page can declare a canonical URL.
    const sourceURL = request.sourceURL ?? sender.url ?? sender.tab?.url ?? request.pageData.url;
    if (tabId !== undefined) {
      const statusCode = await isErrorPage(tabId, sourceURL);
      if (statusCode !== undefined) {
        return { status: 'http_error', status_code: statusCode };
      }
    }
    const rules = await getIndexingRules(baseURL, getCustomHeaders(data));
    if (isPageSkipped(request.pageData.url, sourceURL, rules)) {
      if (tabId !== undefined) await setGreyIcon(tabId);
      return { status: 'ok', status_code: 406 };
    }
  }
  const labelData = await chrome.storage.local.get(['histerLabel']);
  const pageData = { ...request.pageData };
  if (force) {
    pageData.metadata = { ...pageData.metadata, ignore_skip_rules: true };
  }
  if (labelData['histerLabel']) {
    pageData.label = labelData['histerLabel'];
  }
  const r = await sendPageData(baseURL + 'api/add', pageData, getDocumentSubmissionHeaders(data));
  applySubmissionStatus(r.status, sender, data['showIndexedBadge'] === true);
  return { status: 'ok', status_code: r.status };
}

// --- Message handler ---

// TODO check source
function cjsMsgHandler(request, sender, sendResponse) {
  if (request.action === 'indexCurrentPage') {
    indexCurrentTab()
      .then(sendResponse)
      .catch((error) => sendResponse({ error: error.message }));
    return true;
  }
  chrome.storage.local
    .get([
      'histerURL',
      'histerToken',
      'indexingEnabled',
      'histerCustomHeaders',
      'showIndexedBadge',
      'submitPublicDocuments',
    ])
    .then((data) => {
      let u = data['histerURL'] || '';
      const indexingEnabled = data['indexingEnabled'] !== false;
      const customHeaders = getCustomHeaders(data);

      if (request.action === 'getTabState') {
        const stored = tabSensitiveState.get(request.tabId as number);
        sendResponse({ isSensitive: stored !== undefined && stored === request.url });
        return;
      }

      if (request.action === 'addSkipRule') {
        if (!u) {
          sendResponse({ error: 'No server URL configured' });
          return;
        }
        const baseURL = u.endsWith('/') ? u : u + '/';
        (async () => {
          try {
            await saveSkipRule(baseURL, customHeaders, request.pattern, request.deleteQuery);
            sendResponse({ ok: true });
            // Grey out the icon on the active tab immediately
            const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
            if (tab?.id && tab.url) await updateTabIcon(tab.id, tab.url);
          } catch (e) {
            sendResponse({ error: e.message });
          }
        })();
        return;
      }

      if (request.action === 'checkSkipRule') {
        if (!u) {
          sendResponse({ isSkipped: false });
          return;
        }
        const baseURL = u.endsWith('/') ? u : u + '/';
        getIndexingRules(baseURL, customHeaders).then((rules) => {
          sendResponse({
            isSkipped: isPageSkipped(request.url, request.sourceURL ?? request.url, rules),
          });
        });
        return true;
      }

      if (!u) {
        chrome.tabs.sendMessage(sender.tab.id, missingURLMsg);
        setErrorBadge(sender.tab.id);
        return;
      }
      if (!u.endsWith('/')) {
        u += '/';
      }
      if (request.pageData) {
        if (!indexingEnabled && request.action != 'reindex') {
          sendResponse({ status: 'disabled' });
          return;
        }
        submitPageData(u, request, sender, data)
          .then(sendResponse)
          .catch((err) => {
            if (sender.tab?.id !== undefined) setErrorBadge(sender.tab.id);
            sendResponse({ error: err.message });
          });
        return true;
      }
      if (request.resultData) {
        sendResult(u + 'api/history', request.resultData, customHeaders)
          .then((r) => {
            if (r.status === 201) {
              clearBadge(sender.tab.id);
            } else if (r.status != 406) {
              setErrorBadge(sender.tab.id);
            }
            sendResponse({ status: 'ok', status_code: r.status });
          })
          .catch((err) => {
            setErrorBadge(sender.tab.id);
            sendResponse({ error: err.message });
          });
        return true;
      }
    })
    .catch((error) => {
      chrome.tabs.sendMessage(sender.tab.id, missingURLMsg);
      setErrorBadge(sender.tab.id);
    });
  return true;
}

chrome.runtime.onMessage.addListener(cjsMsgHandler);
