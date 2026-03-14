import { PageData, extractPageData, registerResultExtractor } from '../modules/extract';
import { STORAGE_KEYS } from '../modules/constants';

let d: PageData;
// ms
const defaultSleepTime = 10 * 1000;
let sleepTime = defaultSleepTime;
const sleepIncrementRatio = 2;

// @ts-ignore
const isFirefox = typeof InstallTrigger !== 'undefined';

if (isFirefox) {
  if (document.readyState === 'complete') {
    extract(null);
  } else {
    window.addEventListener('load', extract);
  }
} else {
  window.addEventListener('load', extract);
}
window.addEventListener('navigatesuccess', update);

function extract(respond: ((response?: unknown) => void) | null, action?: string) {
  registerResultExtractor(window, (r) => chrome.runtime.sendMessage({ resultData: r }));
  try {
    d = extractPageData();
  } catch (e) {
    console.log('failed to extract page data:', e);
    return;
  }
  let msg: { pageData: PageData; action?: string } = { pageData: d };
  if (action) {
    msg.action = action;
  }
  chrome.runtime.sendMessage(msg, (resp) => {
    if (typeof respond === 'function') {
      respond(resp);
    }
    // Always schedule next update, even on error (queue handles offline)
    setTimeout(update, sleepTime);
  });
}

function update() {
  if (!d) {
    return;
  }
  let d2;
  try {
    d2 = extractPageData();
  } catch (e) {
    console.log('failed to extract page data', e);
    return;
  }
  if (d2.html != d.html || d2.url != d.url) {
    sleepTime = defaultSleepTime;
    d = d2;
    chrome.runtime.sendMessage({ pageData: d }, (resp) => {});
  } else {
    sleepTime *= sleepIncrementRatio;
  }
  setTimeout(update, sleepTime);
}

// Detect if we're on the Hister app page and set up search bridge
chrome.storage.local.get([STORAGE_KEYS.url], (data) => {
  const url = (data[STORAGE_KEYS.url] || '') as string;
  if (!url || !window.location.href.startsWith(url)) return;

  window.addEventListener('hister:search', ((e: CustomEvent<{ query: string }>) => {
    if (!e.detail?.query) return;
    chrome.runtime.sendMessage({ action: 'searchQueue', query: e.detail.query }, (resp) => {
      if (resp?.results?.length) {
        window.dispatchEvent(
          new CustomEvent('hister:queue-results', { detail: { results: resp.results } }),
        );
      }
    });
  }) as EventListener);
});

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
