export type ConnectionStatus = 'connected' | 'offline' | 'error' | 'checking';

const BADGE_CONFIG = {
  checking: { text: '', color: '' },
  offline: { text: 'OFF', color: '#E11D48' },
  error: { text: 'ERR', color: '#FF6B6B' },
} as const satisfies Partial<Record<ConnectionStatus, { text: string; color: string }>>;

let status: ConnectionStatus = 'checking';

export function getStatus(): ConnectionStatus {
  return status;
}

export function setStatus(s: ConnectionStatus) {
  status = s;
}

export async function badge(queueCount: number) {
  const config = BADGE_CONFIG[status as keyof typeof BADGE_CONFIG];
  if (config) {
    await chrome.action.setBadgeText({ text: config.text });
    if (config.color) await chrome.action.setBadgeBackgroundColor({ color: config.color });
  } else if (queueCount > 0) {
    await chrome.action.setBadgeText({ text: String(queueCount) });
    await chrome.action.setBadgeBackgroundColor({ color: '#5D5FEF' });
  } else {
    await chrome.action.setBadgeText({ text: '' });
  }
}
