export type ExtensionBrowser = 'firefox' | 'chrome';

export const extensionStores: Record<ExtensionBrowser, { label: string; href: string }> = {
  firefox: {
    label: 'Firefox extension',
    href: 'https://addons.mozilla.org/en-US/firefox/addon/hister/',
  },
  chrome: {
    label: 'Chrome extension',
    href: 'https://chromewebstore.google.com/detail/hister/cciilamhchpmbdnniabclekddabkifhb',
  },
};

export function detectExtensionBrowser(userAgent: string): ExtensionBrowser | null {
  if (/firefox\//i.test(userAgent)) return 'firefox';
  if (/chrome\/|chromium\//i.test(userAgent)) return 'chrome';
  return null;
}
