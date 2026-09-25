type PageData = {
  title: string;
  text: string;
  url: string;
  html: string;
  faviconURL: string;
};

type PageState = Omit<PageData, 'html'> & { metadata: string };

type EmbeddedContent = { html: string; text: string };

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

// Some apps render the document body in a cross-origin iframe, so the top frame
// only sees the app shell. A content script in the iframe reads the content and
// relays it to the top frame, which merges it into the submitted page.
const embeddedContentSources = [
  // Proton Docs renders its Lexical editor on docs-editor.proton.me
  { hostname: 'docs-editor.proton.me', selector: '[data-lexical-editor="true"]' },
];

let embeddedContent: EmbeddedContent | null = null;

function isEmbeddedContentFrame(): boolean {
  return (
    window.top !== window &&
    embeddedContentSources.some((s) => s.hostname === window.location.hostname)
  );
}

function extractEmbeddedContent(): EmbeddedContent | null {
  const source = embeddedContentSources.find((s) => s.hostname === window.location.hostname);
  const el = source && (document.querySelector(source.selector) as HTMLElement | null);
  if (!el) return null;
  return { html: el.innerHTML, text: el.innerText };
}

function setEmbeddedContent(content: EmbeddedContent | null) {
  embeddedContent = content;
}

function getPageURL() {
  return window.location.href.replace(window.location.hash, '');
}

// Read the fields that can justify an automatic update without serializing
// the entire DOM. Other markup changes are captured by periodic preview checks.
function extractPageState(): PageState {
  const url = getPageURL();
  let faviconURL = '';
  try {
    const faviconHref = document.querySelector("link[rel~='icon']")?.getAttribute('href');
    faviconURL = new URL(faviconHref || '/favicon.ico', url).href;
  } catch {}

  return {
    text: [document.body?.innerText ?? '', embeddedContent?.text ?? '']
      .filter(Boolean)
      .join('\n\n'),
    title: document.querySelector('title')?.innerText ?? document.title,
    url,
    faviconURL,
    metadata: JSON.stringify(
      Array.from(
        document.querySelectorAll(
          'meta[name], meta[property], link[rel="canonical"], script[type="application/ld+json"]',
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
  let html = document.documentElement?.innerHTML ?? '';
  if (embeddedContent?.html) {
    const article = `<article>${embeddedContent.html}</article>`;
    const end = html.lastIndexOf('</body>');
    html = end === -1 ? html + article : html.slice(0, end) + article + html.slice(end);
  }
  return { ...data, html };
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
  isEmbeddedContentFrame,
  extractEmbeddedContent,
  setEmbeddedContent,
  getPageURL,
  extractPageState,
  extractPageData,
};
