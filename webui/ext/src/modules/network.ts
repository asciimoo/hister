
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

async function sendPageData(url, doc, tok) {
    try {
        doc['favicon'] = await fetchFavicon(doc.faviconURL);
    } catch(e) {
        doc['favicon'] = "";
    }
    return sendResult(url, doc, tok);
}

async function sendResult(url, res, tok) {
    return fetch(url, {
        method: "POST",
        body: JSON.stringify(res),
        headers: {
            "Content-type": "application/json; charset=UTF-8",
            "X-Access-Token": tok,
        },
    })
}

export {
    sendPageData,
    sendResult,
}
