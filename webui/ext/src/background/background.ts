import { fetchAPI, sendPageData, sendPDFData, sendResult } from '../modules/network';
import { ensureDefaultServerURL } from '../modules/settings';

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

type NavigationStatus = { url: string; statusCode: number };

// Maps tabId → status of the last main frame response of that tab. Mirrored in
// chrome.storage.session (when available) so the state survives a suspended
// MV3 background script.
const tabNavigationStatus = new Map<number, NavigationStatus>();

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

function recordNavigationStatus(tabId: number, url: string, statusCode: number) {
  const entry: NavigationStatus = { url: normalizeNavigationURL(url), statusCode };
  tabNavigationStatus.set(tabId, entry);
  if (sessionStore) {
    void sessionStore.set({ [navigationStatusKey(tabId)]: entry }).catch(() => {});
  }
}

function forgetNavigationStatus(tabId: number) {
  tabNavigationStatus.delete(tabId);
  if (sessionStore) {
    void sessionStore.remove(navigationStatusKey(tabId)).catch(() => {});
  }
}

async function readNavigationStatus(tabId: number): Promise<NavigationStatus | undefined> {
  const cached = tabNavigationStatus.get(tabId);
  if (cached || !sessionStore) {
    return cached;
  }
  try {
    const key = navigationStatusKey(tabId);
    return (await sessionStore.get(key))[key] as NavigationStatus | undefined;
  } catch (_) {
    return undefined;
  }
}

// Returns the HTTP status of the main frame response that produced `url` in
// `tabId`, or undefined when it is unknown (e.g. SPA history navigation, or a
// page loaded before the extension started).
async function getNavigationStatus(tabId: number, url: string): Promise<number | undefined> {
  const entry = await readNavigationStatus(tabId);
  if (!entry || entry.url !== normalizeNavigationURL(url)) {
    return undefined;
  }
  return entry.statusCode;
}

// `chrome.webRequest` is undefined when the permission is not granted; in that
// case the status stays unknown and documents are submitted as before.
chrome.webRequest?.onHeadersReceived.addListener(
  (details) => {
    if (details.tabId < 0) return;
    recordNavigationStatus(details.tabId, details.url, details.statusCode);
  },
  { urls: ['<all_urls>'], types: ['main_frame'] },
);

// --- Skip rules cache ---

interface SkipRulesCache {
  patterns: RegExp[];
  timestamp: number;
}

// TODO find better way to keep skip rules updated
// Perhaps a websocket connection to the server which pushes skip rule changes?
const SKIP_RULES_TTL = 60_000;
let skipRulesCache: SkipRulesCache | null = null;

async function getSkipPatterns(
  serverURL: string,
  customHeaders: CustomHeader[],
): Promise<RegExp[]> {
  const now = Date.now();
  if (skipRulesCache && now - skipRulesCache.timestamp < SKIP_RULES_TTL) {
    return skipRulesCache.patterns;
  }
  try {
    const u = serverURL.endsWith('/') ? serverURL : serverURL + '/';
    const r = await fetchAPI(u + 'api/rules', { customHeaders });
    if (!r.ok) return skipRulesCache?.patterns ?? [];
    const data = await r.json();
    const patterns: RegExp[] = ((data.skip as string[]) ?? [])
      .map((s) => {
        try {
          return new RegExp(s);
        } catch (_) {
          return null;
        }
      })
      .filter((p): p is RegExp => p !== null);
    skipRulesCache = { patterns, timestamp: now };
    return patterns;
  } catch (_) {
    return skipRulesCache?.patterns ?? [];
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
  skipRulesCache = null;
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

  if (data['indexingEnabled'] === false) {
    await setGreyIcon(tabId);
    if (showIndexedBadge && serverURL) {
      const indexed = await isUrlPreviouslyIndexed(url, serverURL, customHeaders);
      if (indexed) setPreviouslyIndexedBadge(tabId);
    }
    return;
  }

  if (!serverURL) {
    setNormalIcon(tabId);
    return;
  }

  const patterns = await getSkipPatterns(serverURL, customHeaders);
  if (patterns.some((re) => re.test(url))) {
    await setGreyIcon(tabId);
  } else {
    setNormalIcon(tabId);
    clearBadge(tabId);
    if (showIndexedBadge) {
      const indexed = await isUrlPreviouslyIndexed(url, serverURL, customHeaders);
      if (indexed) setPreviouslyIndexedBadge(tabId);
    }
  }
}

// --- PDF tab indexing ---

function isPDFUrl(url: string): boolean {
  try {
    const pathname = new URL(url).pathname.toLowerCase();
    return pathname.endsWith('.pdf');
  } catch (_) {
    return false;
  }
}

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

  const patterns = await getSkipPatterns(serverURL, customHeaders);
  if (!ignoreSkipRules && patterns.some((re) => re.test(tab.url!))) {
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
      skipRulesCache = null;
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
    if (isPDFUrl(tab.url)) {
      await indexPDFTab(tabId, tab);
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
  if (connectionChanged) skipRulesCache = null;
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

  if (tab.url && isPDFUrl(tab.url)) {
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
    const pattern = type === 'url' ? buildUrlSkipPattern(tab.url) : buildDomainSkipPattern(tab.url);
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
    // URL matched a server-side skip rule; invalidate cache and grey out
    skipRulesCache = null;
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
  if (!force && tabId !== undefined) {
    const statusCode = await getNavigationStatus(tabId, request.pageData.url);
    if (statusCode !== undefined && isErrorStatus(statusCode)) {
      return { status: 'http_error', status_code: statusCode };
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
        getSkipPatterns(baseURL, customHeaders).then((patterns) => {
          sendResponse({ isSkipped: patterns.some((re) => re.test(request.url)) });
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
