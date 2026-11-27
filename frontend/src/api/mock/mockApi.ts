import type { SupportApi } from '../client';
import { createSupportService } from '../core/service';
import type { Session } from '../types';
import { catalogSync } from './catalog';
import { emit, ensureDb, getDb, mutate, reseed, subscribe } from './store';

// Офлайн-режим: та же логика, что на сервере, но данные — в localStorage этого браузера.
// Включается VITE_USE_MOCK=true (удобно для показа без сети и для разработки интерфейса).

const SESSION_KEY = 'support-assistant.session-ticket.v2';

const service = createSupportService({
  ready: ensureDb,
  getDb,
  mutate,
  emit,
  catalog: () => catalogSync()!,
  reset: reseed,
  latencyMs: () => 120 + Math.random() * 180,
});

// Одновременные вызовы startSession (StrictMode, две вкладки) не должны плодить диалоги
let pendingSession: Promise<Session> | null = null;

export const mockApi: SupportApi = {
  ...service,
  startSession(forceNew = false) {
    if (pendingSession && !forceNew) return pendingSession;
    const p = service
      .startSession(forceNew ? null : localStorage.getItem(SESSION_KEY), forceNew)
      .then((s) => {
        localStorage.setItem(SESSION_KEY, s.ticket.id);
        return s;
      })
      .finally(() => {
        if (pendingSession === p) pendingSession = null;
      });
    pendingSession = p;
    return p;
  },
  subscribe,
};
