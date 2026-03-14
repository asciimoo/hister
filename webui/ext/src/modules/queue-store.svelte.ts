import type { ConnectionStatus } from './status';

export interface QueueItem {
  id: number;
  title: string;
  url: string;
  createdAt: number;
}

export interface Result {
  message: string;
  type: 'success' | 'error';
}

const SECONDS_PER_MINUTE = 60;
const MINUTES_PER_HOUR = 60;
const HOURS_PER_DAY = 24;

export function timeAgo(ts: number): string {
  const seconds = Math.floor((Date.now() - ts) / 1000);
  if (seconds < SECONDS_PER_MINUTE) return 'just now';
  const minutes = Math.floor(seconds / SECONDS_PER_MINUTE);
  if (minutes < MINUTES_PER_HOUR) return `${minutes}m ago`;
  const hours = Math.floor(minutes / MINUTES_PER_HOUR);
  if (hours < HOURS_PER_DAY) return `${hours}h ago`;
  return `${Math.floor(hours / HOURS_PER_DAY)}d ago`;
}

export function queueStore() {
  let status = $state<ConnectionStatus>('checking');
  let count = $state(0);
  let expanded = $state(false);
  let items = $state<QueueItem[]>([]);

  function sendAction(
    action: string,
    extra?: Record<string, unknown>,
  ): Promise<Record<string, unknown> | undefined> {
    return new Promise((resolve) => {
      chrome.runtime.sendMessage({ action, ...extra }, (resp) => {
        if (resp) {
          status = resp.connectionStatus || 'connected';
          count = resp.queueCount || 0;
        }
        resolve(resp);
      });
    });
  }

  async function refresh() {
    const resp = await chrome.runtime.sendMessage({ action: 'getStatus' });
    if (resp) {
      status = resp.connectionStatus || 'checking';
      count = resp.queueCount || 0;
    }
  }

  async function load() {
    const resp = await chrome.runtime.sendMessage({ action: 'getQueueItems' });
    if (resp?.items) items = resp.items;
  }

  async function retry(): Promise<Result> {
    const resp = await sendAction('retryQueue');
    if (resp?.drained) {
      if (expanded) load();
      return {
        message:
          resp.queueCount === 0
            ? 'Queue drained successfully'
            : `${resp.queueCount} item${resp.queueCount !== 1 ? 's' : ''} remaining`,
        type: 'success',
      };
    }
    return {
      message: 'Retry failed, server may be unreachable',
      type: 'error',
    };
  }

  async function clear(): Promise<Result> {
    await sendAction('clear');
    items = [];
    expanded = false;
    return { message: 'Queue cleared', type: 'success' };
  }

  function toggle() {
    expanded = !expanded;
    if (expanded) load();
  }

  async function remove(id: number) {
    const resp = await sendAction('removeQueueItem', { id });
    if (resp) {
      items = items.filter((item) => item.id !== id);
      if (count === 0) expanded = false;
    }
  }

  return {
    get status() {
      return status;
    },
    get count() {
      return count;
    },
    get expanded() {
      return expanded;
    },
    get items() {
      return items;
    },
    refresh,
    load,
    retry,
    clear,
    toggle,
    remove,
  };
}
