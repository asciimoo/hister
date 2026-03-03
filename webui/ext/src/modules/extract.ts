type PageData = {
    title: string;
    text: string;
    url: string;
    html: string;
    faviconURL: string;
}

type Result = {
    title: string;
    url: string;
    query: string;
}

type ExtractorCallback = (r: Result) => void;

interface ResultExtractor {
    isMatch(w: Window): boolean;
    setCallback(d: Document, cb: ExtractorCallback);
}

class GoogleExtractor implements ResultExtractor {
    isMatch(w) {
        return w.location.hostname == "www.google.com" && w.location.pathname == "/search";
    }
    setCallback(d, cb) {
        d.body.addEventListener("click", (e) => {
            let el = e.target;
            if(el.nodeName != "H3") {
                return;
            }
            let res = el.closest('a[jsname="UWckNb"]');
            if(!res) {
                return;
            }
            let result = {
                'url': res.getAttribute('href'),
                'title': el.innerText,
                'query': d.querySelector("textarea[name='q']").value,

            }
            cb(result);
        });
    }
}

class DuckDuckGoExtractor implements ResultExtractor {
    isMatch(w) {
        return w.location.hostname.match(/^(noai\.|www\.)?duckduckgo.com$/) && w.location.pathname == "/";
    }
    setCallback(d, cb) {
        d.body.addEventListener("click", (e) => {
            let el = e.target;
            if(el.nodeName != "SPAN") {
                return;
            }
            let res = el.closest('a[class="eVNpHGjtxRBq_gLOfGDr LQNqh2U1kzYxREs65IJu"]');
            if(!res) {
                return;
            }
            let result = {
                'url': res.getAttribute('href'),
                'title': el.innerText,
                'query': d.querySelector("input[name='q']").value,

            }
            cb(result);
        });
    }
}



let resultExtractors: ResultExtractor[] = [
    new GoogleExtractor(),
    new DuckDuckGoExtractor(),
];


function getURL() {
	return window.location.href.replace(window.location.hash, "");
}

function extractPageData() : PageData{
    let d : PageData = {
        text: document.body.innerText,
        title: document.querySelector("title").innerText,
        url: getURL(),
        html: document.documentElement.innerHTML,
        faviconURL: new URL("/favicon.ico", getURL()).href,
    };
	let link = document.querySelector("link[rel~='icon']");
	if (link && link.getAttribute("href")) {
        d.faviconURL = new URL(link.getAttribute("href"), d.url).href;
	}
    return d;
}

function registerResultExtractor(w: Window, cb: ExtractorCallback) {
    for(let ex of resultExtractors) {
        if(ex.isMatch(w)) {
            ex.setCallback(w.document, cb);
            return;
        }
    }
}

export {
    PageData,
    registerResultExtractor,
    extractPageData,
}
