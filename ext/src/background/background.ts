const missingURLMsg = {"error": "Missing or invalid Hister server URL. Configure it in the addon popup."};
// TODO check source
async function cjsMsgHandler(request, sender, sendResponse) {
    let d = request.data;
    try {
        d['favicon'] = await fetchFavicon(d.faviconURL);
    } catch(e) {
        d['favicon'] = "";
        console.log("Failed to fetch favicon", e);
    }
    chrome.storage.local.get(['histerURL']).then(data => {
        let u = data['histerURL'] || "";
        if(!u) {
            chrome.tabs.sendMessage(sender.tab.id, missingURLMsg);
            return;
        }
        if(!u.endsWith('/')) {
            u += '/';
        }
        fetch(u+`add`, {
            method: "POST",
            body: JSON.stringify(d),
            headers: {"Content-type": "application/json; charset=UTF-8"},
        }).then((r) => sendResponse({"msg": "ok"}));
    }).catch(error => {
        chrome.tabs.sendMessage(sender.tab.id, missingURLMsg);
    });
}

chrome.runtime.onMessage.addListener(cjsMsgHandler);

async function fetchFavicon(url) {
    const response = await fetch(url);
    let iconBytes = await response.blob();
    const reader = new FileReader();
    reader.readAsDataURL(iconBytes);
    //let icon = btoa(iconBytes.text());
    return new Promise((resolve, reject) => {
		const reader = new FileReader();
		reader.onloadend = () => {
		  resolve(reader.result);
		};
		reader.onerror = () => resolve('');
		reader.readAsDataURL(iconBytes);
  });
}
