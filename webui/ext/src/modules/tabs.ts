async function getTabPageURL(tabId: number, fallbackURL: string): Promise<string> {
  try {
    const response = await chrome.tabs.sendMessage(tabId, { action: 'getPageURL' }, { frameId: 0 });
    return response?.url || fallbackURL;
  } catch {
    // PDFs and browser pages may not have a content script.
    return fallbackURL;
  }
}

export { getTabPageURL };
