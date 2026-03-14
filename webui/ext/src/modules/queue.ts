import PQueue from 'p-queue';
import { db, type QueueItem, type QueuePayload, type PagePayload, type HistoryPayload } from './db';
import { batch } from './network';
import { setStatus, badge } from './status';

const MAX_ATTEMPTS = 10;
const BATCH_CHUNK_SIZE = 100;
const TEXT_PREVIEW_LENGTH = 200;
const pq = new PQueue({ concurrency: 1, interval: 30_000, intervalCap: 1 });
let draining = false;

export async function enqueue(
  type: QueueItem['type'],
  endpoint: string,
  payload: QueuePayload,
): Promise<void> {
  await db.queue.add({
    type,
    endpoint,
    payload,
    createdAt: Date.now(),
    attempts: 0,
  });
}

export async function count(): Promise<number> {
  return db.queue.count();
}

interface QueuedPage {
  url: string;
  title: string;
  domain: string;
  text: string;
  favicon: string;
  added: number;
  queued: boolean;
}

export async function pages(query?: string): Promise<QueuedPage[]> {
  let items = await db.queue.where('type').equals('pageData').toArray();
  if (query) {
    const q = query.toLowerCase();
    items = items.filter((item) => {
      const p = item.payload as PagePayload;
      return (
        (p.title && p.title.toLowerCase().includes(q)) ||
        (p.url && p.url.toLowerCase().includes(q)) ||
        (p.text && p.text.toLowerCase().includes(q))
      );
    });
  }
  return items.map((item) => {
    const p = item.payload as PagePayload;
    return {
      url: p.url || '',
      title: p.title || '',
      domain: '',
      text: p.text ? p.text.substring(0, TEXT_PREVIEW_LENGTH) : '',
      favicon: p.favicon || '',
      added: Math.floor(item.createdAt / 1000),
      queued: true,
    };
  });
}

export async function clear(): Promise<void> {
  pq.clear();
  await db.queue.clear();
}

export async function remove(id: number): Promise<void> {
  await db.queue.delete(id);
}

export async function drain(base: string, token: string): Promise<void> {
  if (draining) return;
  draining = true;

  try {
    // Resume in case the queue was paused by a previous network error
    pq.start();

    const items = await db.queue.orderBy('createdAt').toArray();
    if (items.length === 0) return;

    // Partition into expired vs active in a single pass
    const expired: number[] = [];
    const active: QueueItem[] = [];
    for (const item of items) {
      if (item.attempts >= MAX_ATTEMPTS) {
        expired.push(item.id!);
      } else {
        active.push(item);
      }
    }
    if (expired.length > 0) {
      await db.queue.bulkDelete(expired);
    }
    if (active.length === 0) return;

    const chunks = Array.from({ length: Math.ceil(active.length / BATCH_CHUNK_SIZE) }, (_, i) =>
      active.slice(i * BATCH_CHUNK_SIZE, i * BATCH_CHUNK_SIZE + BATCH_CHUNK_SIZE),
    );

    for (const chunk of chunks) {
      pq.add(async () => {
        const docs: PagePayload[] = [];
        const hist: HistoryPayload[] = [];

        for (const item of chunk) {
          if (item.type === 'pageData') {
            docs.push(item.payload as PagePayload);
          } else {
            hist.push(item.payload as HistoryPayload);
          }
        }

        try {
          await batch(base, docs, hist, token);
          await db.queue.bulkDelete(chunk.map((i) => i.id!));
          setStatus('connected');
        } catch (err) {
          if (
            (err as Error)?.message === 'Failed to fetch' ||
            (err as Error)?.name === 'TypeError'
          ) {
            pq.pause();
            setStatus('offline');
            return;
          }
          // Server error, increment attempts, drop items over max
          for (const item of chunk) {
            if (item.attempts + 1 >= MAX_ATTEMPTS) {
              await db.queue.delete(item.id!);
            } else {
              await db.queue.update(item.id!, {
                attempts: item.attempts + 1,
                lastAttempt: Date.now(),
                error: (err as Error)?.message || 'Unknown error',
              });
            }
          }
          setStatus('error');
        }

        await badge(await count());
      });
    }
  } finally {
    draining = false;
  }
}
