window.addEventListener("load", init, false);

async function init() {
    let d = {
        "text": document.body.innerText,
        "title": document.querySelector("title").innerText,
        "url": getURL(),
        "html": document.documentElement.innerHTML,
    };
	let fu = new URL("/favicon.ico", d.url).href;
	var link = document.querySelector("link[rel~='icon']");
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
    if(request && request.error) {
        alert(request.error);
        return;
    }
    console.log("message received", request)
});
