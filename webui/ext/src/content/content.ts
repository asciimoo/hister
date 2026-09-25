import {
  type PageData,
  type PageState,
  extractEmbeddedContent,
  extractPageData,
  extractPageState,
  getPageURL,
  isEmbeddedContentFrame,
  registerResultExtractor,
  setEmbeddedContent,
} from '../modules/extract';

const minimumUpdateInterval = 30 * 1000;
const maximumPollInterval = 5 * 60 * 1000;
const previewCheckInterval = 5 * 60 * 1000;
const navigationDebounce = 1000;
const embeddedContentThrottle = 2000;
const supportedContentTypes = new Set(['text/html', 'application/xhtml+xml', 'text/plain']);

type PageSnapshot = { state: PageState; data: PageData };
type SubmissionResponse = { status?: string; status_code?: number; error?: string };

let previous: PageSnapshot | null = null;
let pendingHiddenSnapshot: PageSnapshot | null = null;
let lastSubmissionAt = -Infinity;
let lastCaptureAt = -Infinity;
let lastPreviewCheckAt = -Infinity;
let pollInterval = minimumUpdateInterval;
let updateTimer: ReturnType<typeof setTimeout> | null = null;
let skippedUrl: string | null = null;
let started = false;
let pageLeaving = false;
let submissionNumber = 0;
const embeddedFrame = isEmbeddedContentFrame();

function isContextValid(): boolean {
  try {
    return !!chrome.runtime.id;
  } catch (_) {
    return false;
  }
}

function isSupportedPage(): boolean {
  return supportedContentTypes.has(document.contentType.split(';', 1)[0].trim().toLowerCase());
}

function clearUpdateTimer() {
  if (updateTimer !== null) {
    clearTimeout(updateTimer);
    updateTimer = null;
  }
}

function cooldown(): number {
  return Math.max(0, lastSubmissionAt + minimumUpdateInterval - Date.now());
}

function scheduleUpdate(delay: number) {
  clearUpdateTimer();
  if (pageLeaving || document.hidden || !isContextValid()) return;
  updateTimer = setTimeout(
    update,
    Math.max(delay, cooldown(), lastCaptureAt + minimumUpdateInterval - Date.now()),
  );
}

function stateChanged(state: PageState): boolean {
  return (
    !previous ||
    state.url !== previous.state.url ||
    state.text !== previous.state.text ||
    state.title !== previous.state.title ||
    state.faviconURL !== previous.state.faviconURL ||
    state.metadata !== previous.state.metadata
  );
}

function capture(checkPreview = false): PageSnapshot | null {
  if (!isSupportedPage()) return null;
  const url = getPageURL();
  if (url === skippedUrl) return null;
  skippedUrl = null;

  lastCaptureAt = Date.now();
  const state = extractPageState();
  const changed = stateChanged(state);
  if (!changed && !checkPreview && Date.now() - lastPreviewCheckAt < previewCheckInterval) {
    return null;
  }
  const data = extractPageData(state);
  lastPreviewCheckAt = Date.now();
  return changed || data.html !== previous?.data.html ? { state, data } : null;
}

function submit(
  snapshot: PageSnapshot,
  manual = false,
  sendResponse?: (response: SubmissionResponse) => void,
) {
  previous = snapshot;
  lastSubmissionAt = Date.now();
  pollInterval = minimumUpdateInterval;
  const number = ++submissionNumber;
  const message = manual
    ? { pageData: snapshot.data, action: 'reindex' }
    : { pageData: snapshot.data };
  chrome.runtime.sendMessage(message, (response: SubmissionResponse | undefined) => {
    // A delayed reply must not mark a newer page or manual submission as skipped.
    if (number === submissionNumber) {
      if (response?.status_code === 406) {
        skippedUrl = snapshot.state.url;
      } else if (
        !response ||
        response.error ||
        response.status_code === 429 ||
        (response.status_code !== undefined && response.status_code >= 500)
      ) {
        // Retry from a fresh snapshot at the next scheduled check.
        previous = null;
      }
    }
    sendResponse?.(response ?? { error: 'No response from background' });
  });
}

function update() {
  updateTimer = null;
  if (pageLeaving || document.hidden || !isContextValid()) return;
  try {
    const snapshot = capture();
    if (snapshot) {
      submit(snapshot);
    } else {
      pollInterval = Math.min(pollInterval * 2, maximumPollInterval);
    }
  } catch (error) {
    console.log('failed to extract page data:', error);
  }
  const previewDelay = Math.max(
    minimumUpdateInterval,
    lastPreviewCheckAt + previewCheckInterval - Date.now(),
  );
  scheduleUpdate(Math.min(pollInterval, previewDelay));
}

function enableMonitoring() {
  if (started) return;
  started = true;
  registerResultExtractor(window, (result) => {
    if (isContextValid()) chrome.runtime.sendMessage({ resultData: result });
  });
}

function start() {
  if (started || !isContextValid() || !isSupportedPage()) return;
  const navEntry = window.performance.getEntries().find((e) => e.entryType === 'navigation') as
    PerformanceNavigationTiming | undefined;
  if (navEntry && navEntry.responseStatus > 299) return;
  enableMonitoring();
  scheduleUpdate(0);
}

// Runs inside an iframe that holds the page content (see embeddedContentSources)
// and relays that content to the top frame instead of indexing the iframe URL.
function startEmbeddedContentRelay() {
  let lastHTML: string | null = null;
  let timer: ReturnType<typeof setTimeout> | null = null;
  const relay = () => {
    timer = null;
    if (!isContextValid()) {
      observer.disconnect();
      return;
    }
    const content = extractEmbeddedContent();
    if (!content || content.html === lastHTML) return;
    lastHTML = content.html;
    chrome.runtime.sendMessage({ embeddedContent: content });
  };
  const observer = new MutationObserver(() => {
    timer ??= setTimeout(relay, embeddedContentThrottle);
  });
  observer.observe(document, { subtree: true, childList: true, characterData: true });
  relay();
}

if (embeddedFrame) {
  startEmbeddedContentRelay();
} else if (document.readyState === 'complete') {
  start();
} else {
  window.addEventListener('load', start, { once: true });
}

if (typeof window.navigation !== 'undefined') {
  window.navigation.addEventListener('navigatesuccess', () => {
    if (!started || pageLeaving || document.hidden) return;
    pollInterval = minimumUpdateInterval;
    scheduleUpdate(navigationDebounce);
  });
}

document.addEventListener('visibilitychange', () => {
  if (!started || pageLeaving || !isContextValid()) return;
  clearUpdateTimer();
  pendingHiddenSnapshot = null;
  if (!document.hidden) {
    pollInterval = minimumUpdateInterval;
    scheduleUpdate(navigationDebounce);
    return;
  }
  // Capture once on hiding. A delayed send retains that snapshot without
  // polling or reading the DOM again while the tab is hidden.
  if (!previous) return;
  try {
    pendingHiddenSnapshot = capture(true);
  } catch (error) {
    console.log('failed to extract page data:', error);
    return;
  }
  if (!pendingHiddenSnapshot) return;
  const flush = () => {
    updateTimer = null;
    const snapshot = pendingHiddenSnapshot;
    pendingHiddenSnapshot = null;
    if (snapshot && snapshot.state.url !== skippedUrl && isContextValid()) submit(snapshot);
  };
  if (cooldown() === 0) flush();
  else updateTimer = setTimeout(flush, cooldown());
});

window.addEventListener('pagehide', () => {
  pageLeaving = true;
  clearUpdateTimer();
  let snapshot = pendingHiddenSnapshot;
  pendingHiddenSnapshot = null;
  if (!started || !isContextValid() || lastCaptureAt === -Infinity) return;
  try {
    // Read the latest state in case it changed after the tab became hidden.
    // If it reverted to the previous submission, discard the pending snapshot.
    snapshot = capture(true);
  } catch (error) {
    console.log('failed to extract final page data:', error);
  }
  // Hand the snapshot to the background process before this context disappears.
  // Closing or navigating away is an exception to the automatic update interval.
  if (snapshot && snapshot.state.url !== skippedUrl) submit(snapshot);
});

window.addEventListener('pageshow', (event) => {
  if (!event.persisted) return;
  pageLeaving = false;
  if (started) {
    pollInterval = minimumUpdateInterval;
    scheduleUpdate(navigationDebounce);
  }
});

chrome.runtime.onMessage.addListener((request, sender, sendResponse) => {
  if (!request || embeddedFrame) return;
  if (request.embeddedContent) {
    setEmbeddedContent(request.embeddedContent);
    if (started && !pageLeaving && !document.hidden) {
      pollInterval = minimumUpdateInterval;
      scheduleUpdate(navigationDebounce);
    }
    return;
  }
  if (request.error) {
    alert(request.error);
    return;
  }
  if (request.action !== 'reindex') return;
  if (!isContextValid()) return;
  if (!isSupportedPage()) {
    sendResponse({ status: 'unsupported_content_type', content_type: document.contentType });
    return;
  }
  clearUpdateTimer();
  pendingHiddenSnapshot = null;
  skippedUrl = null;
  try {
    lastCaptureAt = Date.now();
    const state = extractPageState();
    const data = extractPageData(state);
    lastPreviewCheckAt = Date.now();
    submit({ state, data }, true, sendResponse);
    enableMonitoring();
    scheduleUpdate(minimumUpdateInterval);
  } catch (error) {
    sendResponse({ error: error instanceof Error ? error.message : String(error) });
    scheduleUpdate(minimumUpdateInterval);
  }
  return true;
});
