window.addEventListener("load", extractData, false);

async function extractData() {
    let d = {
        "text": document.body.innerText,
        "title": document.querySelector("title").innerText,
        "url": getURL(),
        "html": document.documentElement.innerHTML,
    };
	let fu = new URL("/favicon.ico", d.url).href;
	let link = document.querySelector("link[rel~='icon']");
	if (link && link.getAttribute("href")) {
        fu = new URL(link.getAttribute("href"), d.url).href;
	}
    d['faviconURL'] = fu;
    chrome.runtime.sendMessage({data:  d}, resp => {
    });
}

function getURL() {
	return window.location.href.replace(window.location.hash, "");
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
        extractData();
        sendResponse({"action": "reindex", "status": "ok"});
		return;
    }
    console.log("message received", request)
});
