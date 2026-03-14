import Dexie, { type EntityTable } from 'dexie';

export interface PagePayload {
  title?: string;
  text?: string;
  url?: string;
  html?: string;
  faviconURL?: string;
  favicon?: string;
}

export interface HistoryPayload {
  title?: string;
  url?: string;
  query?: string;
}

export type QueuePayload = PagePayload | HistoryPayload;

export interface QueueItem {
  id?: number;
  type: 'pageData' | 'resultData';
  endpoint: string;
  payload: QueuePayload;
  createdAt: number;
  attempts: number;
  lastAttempt?: number;
  error?: string;
}

export const db = new Dexie('hister') as Dexie & {
  queue: EntityTable<QueueItem, 'id'>;
};

db.version(1).stores({
  queue: '++id, type, createdAt',
});
