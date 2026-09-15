import { type PageData, extractPageData, registerResultExtractor } from '../modules/extract';

let d: PageData;
// ms
const defaultSleepTime = 10 * 1000;
let sleepTime = defaultSleepTime;
const sleepIncrementRatio = 2;
const supportedContentTypes = new Set(['text/html', 'application/xhtml+xml', 'text/plain']);
// URL that was rejected by the server with a 406 (skip rule match).
// Cleared when the page navigates to a different URL.
let skippedUrl: string | null = null;
let updateTimer: ReturnType<typeof setTimeout> | null = null;

// @ts-ignore
var isFirefox = typeof InstallTrigger !== 'undefined';

function isContextValid(): boolean {
  try {
    return !!chrome.runtime.id;
  } catch (_) {
    return false;
  }
}

if (isFirefox) {
  if (document.readyState === 'complete') {
    extract(null);
  } else {
    window.addEventListener('load', extract);
  }
} else {
  window.addEventListener('load', extract);
}

// Detect SPA navigations via the Navigation API (fires on window.navigation,
// not on window). Falls back to polling for browsers without it.
if (typeof window.navigation !== 'undefined') {
  window.navigation.addEventListener('navigatesuccess', update);
}

// Submit the latest page state when the tab is being hidden or closed.
document.addEventListener('visibilitychange', () => {
  if (!document.hidden || !d || !isContextValid()) return;
  let current;
  try {
    current = extractPageData();
  } catch (_) {
    return;
  }
  if (current.html != d.html || current.url != d.url || current.title != d.title) {
    d = current;
    chrome.runtime.sendMessage({ pageData: d }, (resp) => {
      if (resp?.status_code === 406) {
        skippedUrl = d.url;
      }
    });
  }
});

function scheduleUpdate() {
  if (updateTimer !== null) {
    clearTimeout(updateTimer);
  }
  updateTimer = setTimeout(update, sleepTime);
}

function normalizeContentType(contentType: string): string {
  return contentType.split(';', 1)[0].trim().toLowerCase();
}

function isSupportedContentType(contentType: string): boolean {
  return supportedContentTypes.has(normalizeContentType(contentType));
}

// Unsuccessful HTTP responses are filtered out by the background script, which
// tracks main frame response statuses via chrome.webRequest. The content script
// cannot do it itself: PerformanceNavigationTiming.responseStatus is only
// implemented by Chromium.
function extract(sendResponse?, actionType?) {
  if (!isContextValid()) return;
  if (!isSupportedContentType(document.contentType)) {
    if (typeof sendResponse === 'function') {
      sendResponse({ status: 'unsupported_content_type', content_type: document.contentType });
    }
    return;
  }
  registerResultExtractor(window, (r) => {
    if (isContextValid()) chrome.runtime.sendMessage({ resultData: r });
  });
  try {
    d = extractPageData();
  } catch (e) {
    console.log('failed to extract page data:', e);
    return;
  }
  let msg = { pageData: d };
  if (actionType) {
    msg['action'] = actionType;
  }
  chrome.runtime.sendMessage(msg, (resp) => {
    if (typeof sendResponse === 'function') {
      sendResponse(resp);
    }
    if (!resp || resp.error || resp.status_code != 201) {
      console.log('failed to submit page data', resp);
    }
    if (resp?.status_code === 406) {
      skippedUrl = d.url;
    }
    // Always start polling for URL/content changes, even if the initial
    // submission failed (e.g. skip rule). The page may navigate to a
    // non-skipped URL later (SPA).
    scheduleUpdate();
  });
}

function update() {
  if (!d || !isContextValid()) {
    return;
  }
  let d2;
  try {
    d2 = extractPageData();
  } catch (e) {
    console.log('failed to extract page data', e);
    return;
  }
  if (d2.html != d.html || d2.url != d.url || d2.title != d.title) {
    sleepTime = defaultSleepTime;
    d = d2;
    if (d2.url === skippedUrl) {
      // URL is still server-side skipped; don't resubmit.
      scheduleUpdate();
      return;
    }
    skippedUrl = null;
    chrome.runtime.sendMessage({ pageData: d }, (resp) => {
      if (resp?.status_code === 406) {
        skippedUrl = d.url;
      }
    });
  } else {
    sleepTime *= sleepIncrementRatio;
  }
  scheduleUpdate();
}

// Get message from background page
// TODO check sender
chrome.runtime.onMessage.addListener(function (request, sender, sendResponse) {
  if (!request) {
    return;
  }
  if (request.error) {
    alert(request.error);
    return;
  }
  if (request.action == 'reindex') {
    extract(sendResponse, 'reindex');
    return true;
  }
  console.log('message received', request);
});
