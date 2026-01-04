const urlInput = <HTMLInputElement>document.querySelector("#url");
const msgBox = <HTMLElement>document.querySelector("#msg");

function saveURL() {
    let u = urlInput.value;
    console.log("YO", u);
	chrome.storage.local.set({
		histerURL: u,
	}).then(() => {
        msgBox.innerText = "Settings saved";
    });
}

document.querySelector("form").addEventListener("submit", (e) => {
    saveURL();
    e.preventDefault();
});

chrome.storage.local.get(['histerURL'], (d) => {
    urlInput.setAttribute('value', d['histerURL'] || "");
});
