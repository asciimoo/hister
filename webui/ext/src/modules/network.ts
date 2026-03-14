async function fetchFavicon(url: string): Promise<string> {
  const response = await fetch(url);
  const blob = await response.blob();
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onloadend = () => resolve(reader.result as string);
    reader.onerror = reject;
    reader.readAsDataURL(blob);
  });
}

function normalize(url: string): string {
  return url.endsWith('/') ? url : url + '/';
}

function headers(tok: string): HeadersInit {
  const h: HeadersInit = { 'Content-type': 'application/json; charset=UTF-8' };
  if (tok) h['X-Access-Token'] = tok;
  return h;
}

async function resolvePageData(doc: { favicon?: string; faviconURL?: string }) {
  try {
    doc.favicon = await fetchFavicon(doc.faviconURL!);
  } catch (e) {
    doc.favicon = '';
  }
  return doc;
}

async function sendResult(url: string, res: object, tok: string) {
  return fetch(url, {
    method: 'POST',
    body: JSON.stringify(res),
    headers: headers(tok),
  });
}

async function sendBatch(base: string, documents: object[], history: object[], tok: string) {
  const r = await fetch(normalize(base) + 'api/batch', {
    method: 'POST',
    body: JSON.stringify({ documents, history }),
    headers: headers(tok),
  });
  if (!r.ok) {
    throw new Error(`Batch request failed with status ${r.status}`);
  }
  return r.json();
}

export { sendResult as send, resolvePageData as resolve, sendBatch as batch, normalize, headers };
