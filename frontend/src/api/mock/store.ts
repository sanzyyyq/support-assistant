import type { SupportEvent } from '../types';
import { catalogSync, loadCatalog } from './catalog';
import { createSeed, type DB } from './data';

// «База данных» заглушки живёт в localStorage, а события расходятся
// между вкладками через BroadcastChannel. Так чат клиента в одной вкладке
// и панель менеджера в другой видят друг друга в реальном времени.

const KEY = 'support-assistant.mock-db.v3';

function seed(): DB {
  const cat = catalogSync();
  if (!cat) throw new Error('Каталог ещё не загружен');
  const db = createSeed(cat);
  write(db);
  return db;
}

function read(): DB {
  try {
    const raw = localStorage.getItem(KEY);
    if (raw) return JSON.parse(raw) as DB;
  } catch {
    /* повреждённые данные — пересоздадим */
  }
  return seed();
}

function write(db: DB) {
  try {
    localStorage.setItem(KEY, JSON.stringify(db));
  } catch {
    /* приватный режим / переполнение — работаем без сохранения */
  }
}

let ready: Promise<void> | null = null;

/** Каталог загружен, база создана. Вызывать в начале каждого метода API. */
export function ensureDb(): Promise<void> {
  ready ??= loadCatalog().then(() => {
    read();
  });
  return ready;
}

export const getDb = read;

export function mutate<T>(fn: (db: DB) => T): T {
  const db = read();
  const result = fn(db);
  write(db);
  return result;
}

const listeners = new Set<(e: SupportEvent) => void>();
const channel = typeof BroadcastChannel !== 'undefined' ? new BroadcastChannel('support-assistant.mock') : null;

channel?.addEventListener('message', (ev: MessageEvent<SupportEvent>) => {
  listeners.forEach((l) => l(ev.data));
});

export function emit(event: SupportEvent) {
  listeners.forEach((l) => l(event));
  channel?.postMessage(event);
}

export function subscribe(listener: (e: SupportEvent) => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/** Сбросить демо-данные (кнопка в панели менеджера). */
export async function resetDb() {
  await ensureDb();
  seed();
  emit({ type: 'reset' });
}
