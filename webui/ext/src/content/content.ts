import {
    PageData,
    extractPageData,
    registerResultExtractor,
} from '../modules/extract';

let d : PageData;
const debounceDelay = 2000;
const minSendInterval = 60*1000;
let lastSentTime = 0;
let debounceTimer: ReturnType<typeof setTimeout> | null = null;
let throttleTimer: ReturnType<typeof setTimeout> | null = null;

// @ts-ignore
var isFirefox = typeof InstallTrigger !== 'undefined';

if(isFirefox) {
    if (document.readyState === "complete") {
        extract(null);
    } else {
        window.addEventListener("load", extract);
    }
} else {
	window.addEventListener("load", extract);
}
window.addEventListener("navigatesuccess", () => {
    checkAndSend(true);
});

function extract(sendResponse) {
    registerResultExtractor(window, r => chrome.runtime.sendMessage({resultData:  r}));
    try {
        d = extractPageData();
    } catch(e) {
        console.log("failed to extract page data:", e);
        return;
    }
    lastSentTime = Date.now();
    chrome.runtime.sendMessage(
        {pageData:  d},
        resp => {
            if(typeof sendResponse === 'function') {
                sendResponse(resp);
            }
            if(!resp || (resp.error || resp.status_code != 201)) {
                console.log("failed to submit page data, stopping extraction", resp);
                return;
            }
            startObserving();
        }
    );
}

function checkAndSend(immediate = false) {
    let d2;
    try {
        d2 = extractPageData();
    } catch(e) {
        console.log("failed to extract page data", e);
        return;
    }
    if(d2.text === d.text && d2.url === d.url) {
        return;
    }
    d = d2;
    if(immediate) {
        lastSentTime = Date.now();
        chrome.runtime.sendMessage({pageData: d}, resp => {});
        return;
    }
    const elapsed = Date.now() - lastSentTime;
    if(elapsed >= minSendInterval) {
        lastSentTime = Date.now();
        chrome.runtime.sendMessage({pageData: d}, resp => {});
    } else if(!throttleTimer) {
        throttleTimer = setTimeout(() => {
            throttleTimer = null;
            lastSentTime = Date.now();
            chrome.runtime.sendMessage({pageData: d}, resp => {});
        }, minSendInterval - elapsed);
    }
}

let observer: MutationObserver | null = null;

function startObserving() {
    if(observer) return;
    observer = new MutationObserver(() => {
        if(debounceTimer) clearTimeout(debounceTimer);
        debounceTimer = setTimeout(() => {
            debounceTimer = null;
            checkAndSend();
        }, debounceDelay);
    });
    observer.observe(document.body, {
        childList: true,
        subtree: true,
        characterData: true,
    });
}

// Get message from background page
// TODO check sender
chrome.runtime.onMessage.addListener(function(request, sender, sendResponse) {
    if(!request) {
        return;
    }
    if(request.error) {
        alert(request.error);
        return;
    }
    if(request.action == "reindex") {
        extract(sendResponse);
		return true;
    }
    console.log("message received", request)
});
