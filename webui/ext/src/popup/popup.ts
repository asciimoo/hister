const urlInput = <HTMLInputElement>document.querySelector("#url");
const tokenInput = <HTMLInputElement>document.querySelector("#token");
const msgBox = <HTMLElement>document.querySelector("#msg");

const defaultURL = "http://127.0.0.1:4433/";

chrome.runtime.onMessage.addListener(function(request, sender, sendResponse) {
    if(!request) {
        return;
    }
});

function saveForm() {
    const u = urlInput.value;
    const t = tokenInput.value;
	chrome.storage.local.set({
		histerURL: u,
		histerToken: t,
	}).then(() => {
        msgBox.innerText = "Settings saved";
    });
}

document.querySelector("form").addEventListener("submit", (e) => {
    saveForm();
    e.preventDefault();
});

chrome.storage.local.get(['histerURL'], (d) => {
    if(!d['histerURL']) {
        chrome.storage.local.set({
            histerURL: defaultURL,
        });
    }
    urlInput.setAttribute('value', d['histerURL'] || defaultURL);
});

document.querySelector("#reindex").addEventListener("click", (e) => {
	chrome.tabs.query({active: true, currentWindow: true}, function(tabs) {
		if(!tabs) return;
		chrome.tabs.sendMessage(tabs[0].id, {action: "reindex"}, (r) => {
            if(r && r.status == "ok" && r.status_code == 201) {
                msgBox.innerText = "Reindex successful";
                return;
            }
            msgBox.innerText = "Reindex failed";
            if(r && r.error) {
                msgBox.innerText += ": " + r.error;
            }
            if(r && r.status_code == 403) {
                msgBox.innerText += ": Unauthorized - invalid access token";
            }
        });
	});
});

