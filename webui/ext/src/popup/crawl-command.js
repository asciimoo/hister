// Quote every argument for POSIX shells, including apostrophes in URL paths.
function shellQuote(value) {
  return "'" + String(value).replaceAll("'", "'\"'\"'") + "'";
}

function httpURL(value, label) {
  let url;
  try {
    url = new URL(value);
  } catch {
    throw new Error(`${label} must be a valid HTTP(S) URL.`);
  }
  if (!['http:', 'https:'].includes(url.protocol)) {
    throw new Error(`${label} must be an HTTP(S) URL.`);
  }
  if (url.username || url.password) {
    throw new Error(`${label} must not contain credentials.`);
  }
  return url;
}

export function buildCrawlCommand(serverURL, tabURL, maxPages, delay) {
  const server = httpURL(serverURL, 'Server URL');
  const site = httpURL(tabURL, 'Current page');
  if (server.search || server.hash) {
    throw new Error('Server URL must not contain a query or fragment.');
  }
  if (!Number.isInteger(maxPages) || maxPages < 1 || maxPages > 1000) {
    throw new Error('Choose a page limit between 1 and 1000.');
  }
  if (!Number.isInteger(delay) || delay < 1 || delay > 60) {
    throw new Error('Choose a request delay between 1 and 60 seconds.');
  }
  const siteURL = site.origin + '/';
  // --allowed-domain also permits subdomains; use a literal origin pattern.
  const pattern = '^' + siteURL.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const args = [
    '--server-url',
    server.href,
    'index',
    '--recursive',
    '--max-links',
    maxPages,
    '--delay',
    delay,
    '--allowed-pattern',
    pattern,
    '--',
    siteURL,
  ];
  return { siteURL, command: 'hister ' + args.map(shellQuote).join(' ') };
}
