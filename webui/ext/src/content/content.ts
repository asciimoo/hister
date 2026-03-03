import {
    PageData,
    extractPageData,
    registerResultExtractor,
} from '../modules/extract';

let d : PageData;
// ms
const defaultSleepTime = 10*1000;
let sleepTime = defaultSleepTime;
const sleepIncrementRatio = 2;

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
window.addEventListener("navigatesuccess", update)

function extract(sendResponse) {
    registerResultExtractor(window, r => chrome.runtime.sendMessage({resultData:  r}));
    try {
        d = extractPageData();
    } catch(e) {
        console.log("failed to extract page data:", e);
        return;
    }
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
            setTimeout(update, sleepTime);
        }
    );
}

function update() {
    if(!d) {
        return;
    }
    let d2;
    try {
        d2 = extractPageData();
    } catch(e) {
        console.log("failed to extract page data", e);
        return;
    }
    if(d2.html != d.html || d2.url != d.url) {
        sleepTime = defaultSleepTime;
        d = d2;
        chrome.runtime.sendMessage({pageData:  d}, resp => {});
    } else {
        sleepTime *= sleepIncrementRatio;
    }
    setTimeout(update, sleepTime);
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
