type PageData = {
  title: string;
  text: string;
  url: string;
  html: string;
  faviconURL: string;
};

type PageState = Omit<PageData, 'html'> & { metadata: string };

type Result = {
  title: string;
  url: string;
  query: string;
};

type ExtractorCallback = (r: Result) => void;

interface ResultExtractor {
  isMatch(w: Window): boolean;
  setCallback(d: Document, cb: ExtractorCallback);
}

class GoogleExtractor implements ResultExtractor {
  isMatch(w) {
    return w.location.hostname == 'www.google.com' && w.location.pathname == '/search';
  }
  setCallback(d, cb) {
    d.body.addEventListener('click', (e) => {
      let el = e.target;
      if (el.nodeName != 'H3') {
        return;
      }
      let res = el.closest('a[jsname="UWckNb"]');
      if (!res) {
        return;
      }
      let result = {
        url: res.getAttribute('href'),
        title: el.innerText,
        query: d.querySelector("textarea[name='q']").value,
      };
      cb(result);
    });
  }
}

class DuckDuckGoExtractor implements ResultExtractor {
  isMatch(w) {
    return (
      w.location.hostname.match(/^(noai\.|www\.)?duckduckgo.com$/) && w.location.pathname == '/'
    );
  }
  setCallback(d, cb) {
    d.body.addEventListener('click', (e) => {
      let el = e.target;
      if (el.nodeName != 'SPAN') {
        return;
      }
      let res = el.closest('a[class="eVNpHGjtxRBq_gLOfGDr LQNqh2U1kzYxREs65IJu"]');
      if (!res) {
        return;
      }
      let result = {
        url: res.getAttribute('href'),
        title: el.innerText,
        query: d.querySelector("input[name='q']").value,
      };
      cb(result);
    });
  }
}

let resultExtractors: ResultExtractor[] = [new GoogleExtractor(), new DuckDuckGoExtractor()];

function getPageURL() {
  let url = new URL(window.location.href);
  const canonicalHref = document
    .querySelector('link[rel~="canonical" i][href]')
    ?.getAttribute('href')
    ?.trim();
  if (canonicalHref) {
    try {
      const canonicalURL = new URL(canonicalHref, document.baseURI);
      // Compare parsed hostnames exactly so another site or subdomain cannot
      // use this page's content to overwrite a document in the index.
      if (
        (canonicalURL.protocol === 'http:' || canonicalURL.protocol === 'https:') &&
        canonicalURL.hostname === url.hostname &&
        !canonicalURL.username &&
        !canonicalURL.password
      ) {
        url = canonicalURL;
      }
    } catch {}
  }
  url.hash = '';
  return url.href;
}

// Read the fields that can justify an automatic update without serializing
// the entire DOM. Other markup changes are captured by periodic preview checks.
function extractPageState(): PageState {
  const url = getPageURL();
  let faviconURL = '';
  try {
    const faviconHref = document.querySelector("link[rel~='icon']")?.getAttribute('href');
    faviconURL = new URL(faviconHref || '/favicon.ico', document.baseURI).href;
  } catch {}

  return {
    text: document.body?.innerText ?? '',
    title: document.querySelector('title')?.innerText ?? document.title,
    url,
    faviconURL,
    metadata: JSON.stringify(
      Array.from(
        document.querySelectorAll(
          'meta[name], meta[property], link[rel~="canonical" i], script[type="application/ld+json"]',
        ),
        (el) => [
          el.tagName,
          el.getAttribute('name'),
          el.getAttribute('property'),
          el.getAttribute('content'),
          el.getAttribute('href'),
          el.tagName === 'SCRIPT' ? el.textContent : null,
        ],
      ),
    ),
  };
}

function extractPageData(state: PageState): PageData {
  const { metadata, ...data } = state;
  return { ...data, html: document.documentElement?.innerHTML ?? '' };
}

function registerResultExtractor(w: Window, cb: ExtractorCallback) {
  for (let ex of resultExtractors) {
    if (ex.isMatch(w)) {
      ex.setCallback(w.document, cb);
      return;
    }
  }
}

export {
  type PageData,
  type PageState,
  registerResultExtractor,
  getPageURL,
  extractPageState,
  extractPageData,
};
