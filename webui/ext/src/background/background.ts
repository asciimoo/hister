import { send, resolve, normalize, headers } from '../modules/network';
import { enqueue, drain, count, clear as clearAll, pages, remove } from '../modules/queue';
import { db, type PagePayload, type QueuePayload } from '../modules/db';
import { getStatus, setStatus, badge } from '../modules/status';
import { STORAGE_KEYS } from '../modules/constants';

const ALARM = 'histerQueueDrain';
const DRAIN_INTERVAL_MINUTES = 2;
const HTTP_FORBIDDEN = 403;

const missingURLMsg = {
  error: 'Missing or invalid Hister server URL. Configure it in the addon popup.',
};

interface ExtMessage {
  action?: string;
  query?: string;
  id?: number;
  pageData?: PagePayload;
  resultData?: Record<string, unknown>;
}

async function settings() {
  const data = await chrome.storage.local.get(Object.values(STORAGE_KEYS));
  return {
    url: (data[STORAGE_KEYS.url] || '') as string,
    token: (data[STORAGE_KEYS.token] || '') as string,
    indexingEnabled: data[STORAGE_KEYS.indexingEnabled] !== false,
  };
}

async function sync() {
  await badge(await count());
}

/** Try to send payload; on failure enqueue and update status. */
async function trySend(
  base: string,
  endpoint: string,
  payload: QueuePayload,
  token: string,
  type: 'pageData' | 'resultData',
  respond: (response?: unknown) => void,
) {
  try {
    const r = await send(base + endpoint, payload, token);
    if (r.ok) {
      setStatus('connected');
      drain(base, token).catch((e) => console.warn('drain failed', e));
      respond({ status: 'ok', status_code: r.status });
      return;
    }
    if (r.status === HTTP_FORBIDDEN) {
      setStatus('error');
      respond({ status: 'error', status_code: r.status });
      return;
    }
    await enqueue(type, endpoint, payload);
    setStatus('error');
    respond({ status: 'queued' });
  } catch (err) {
    console.warn('send failed, queuing', err);
    await enqueue(type, endpoint, payload);
    setStatus('offline');
    respond({ status: 'queued' });
  } finally {
    await sync();
  }
}

type Handler = (
  req: ExtMessage,
  sender: chrome.runtime.MessageSender,
  respond: (response?: unknown) => void,
) => Promise<void>;

const actions: Record<string, Handler> = {
  async getStatus(_req, _sender, respond) {
    respond({ connectionStatus: getStatus(), queueCount: await count() });
  },

  async retryQueue(_req, _sender, respond) {
    const before = await count();
    const s = await settings();
    if (s.url) await drain(s.url, s.token);
    const after = await count();
    respond({ connectionStatus: getStatus(), queueCount: after, drained: after < before });
  },

  async clear(_req, _sender, respond) {
    await clearAll();
    setStatus('connected');
    await sync();
    respond({ connectionStatus: getStatus(), queueCount: 0 });
  },

  async searchQueue(req, _sender, respond) {
    respond({ results: await pages(req.query) });
  },

  async getQueueItems(_req, _sender, respond) {
    const items = await db.queue.orderBy('createdAt').toArray();
    respond({
      items: items.map((item) => {
        const p = item.payload as PagePayload;
        return {
          id: item.id,
          title: p.title || '',
          url: p.url || '',
          createdAt: item.createdAt,
        };
      }),
    });
  },

  async removeQueueItem(req, _sender, respond) {
    await remove(req.id!);
    await sync();
    respond({ connectionStatus: getStatus(), queueCount: await count() });
  },
};

function tabId(sender: chrome.runtime.MessageSender): number | undefined {
  return sender.tab?.id;
}

function cjsMsgHandler(
  request: ExtMessage,
  sender: chrome.runtime.MessageSender,
  sendResponse: (response?: unknown) => void,
) {
  if (request.action && actions[request.action]) {
    actions[request.action](request, sender, sendResponse).catch((e) =>
      console.warn('action failed', e),
    );
    return true;
  }

  // Handle pageData / resultData, all async, must return true
  (async () => {
    const s = await settings();
    const tid = tabId(sender);

    if (!s.url) {
      if (tid) chrome.tabs.sendMessage(tid, missingURLMsg);
      return;
    }
    const u = normalize(s.url);
    if (request.pageData) {
      if (!s.indexingEnabled && request.action != 'reindex') {
        sendResponse({ status: 'disabled' });
        return;
      }

      // Resolve favicon before potential enqueue
      await resolve(request.pageData);

      await trySend(u, 'api/add', request.pageData, s.token, 'pageData', sendResponse);
      return;
    }
    if (request.resultData) {
      await trySend(u, 'api/history', request.resultData, s.token, 'resultData', sendResponse);
      return;
    }
  })().catch((e) => {
    console.warn('message handler failed', e);
    const tid = tabId(sender);
    if (tid) chrome.tabs.sendMessage(tid, missingURLMsg);
  });
  return true;
}

chrome.runtime.onMessage.addListener(cjsMsgHandler);

// Set up alarm for periodic queue drain
chrome.alarms.create(ALARM, { periodInMinutes: DRAIN_INTERVAL_MINUTES });

async function checkConnection(drainIfOnline = false) {
  const s = await settings();
  if (!s.url) return;
  try {
    const r = await fetch(normalize(s.url) + 'api/stats', { headers: headers(s.token) });
    if (r.ok) {
      setStatus('connected');
      if (drainIfOnline) await drain(s.url, s.token);
    } else {
      setStatus('error');
    }
  } catch {
    setStatus('offline');
  }
  await sync();
}

chrome.alarms.onAlarm.addListener(async (alarm) => {
  if (alarm.name !== ALARM) return;
  if ((await count()) === 0) return;
  await checkConnection(true);
});

checkConnection();
