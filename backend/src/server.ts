import { existsSync } from 'node:fs';
import { readFile } from 'node:fs/promises';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import fastifyStatic from '@fastify/static';
import fastifyWebsocket from '@fastify/websocket';
import Fastify, { type FastifyReply } from 'fastify';
import type { WebSocket } from 'ws';
import { createSupportService, NotFoundError } from '../../frontend/src/api/core/service';
import { catalogSync, loadCatalog, setCatalogLoader } from '../../frontend/src/api/mock/catalog';
import { createSeed } from '../../frontend/src/api/mock/data';
import type { Quote, QuoteRequest, SupportEvent } from '../../frontend/src/api/types';
import { FileStore } from './store';

// ── Настройки (переменные окружения) ──────────────────────────────────

const here = dirname(fileURLToPath(import.meta.url));
const firstExisting = (...paths: string[]) => paths.find((p) => existsSync(p)) ?? paths[0];

const PORT = Number(process.env.PORT ?? 8080);
const HOST = process.env.HOST ?? '0.0.0.0';
/** Собранный фронтенд (в Docker — /app/public) */
const PUBLIC_DIR = process.env.PUBLIC_DIR ?? firstExisting(resolve(here, '../public'), resolve(here, '../../frontend/dist'));
/** Каталог Store, собранный из фида (data/build_catalog.py) */
const CATALOG_FILE =
  process.env.CATALOG_FILE ??
  firstExisting(join(PUBLIC_DIR, 'data/catalog.json.gz'), resolve(here, '../../frontend/public/data/catalog.json.gz'));
/** Где хранить обращения и КП. На Railway сюда монтируется том */
const DATA_DIR = process.env.DATA_DIR ?? resolve(here, '../.data');
/** Разрешить кнопку «Сбросить демо-данные» */
const ALLOW_RESET = process.env.ALLOW_RESET !== 'false';

// ── Данные и логика агента ────────────────────────────────────────────

setCatalogLoader(() => readFile(CATALOG_FILE));

const store = new FileStore(join(DATA_DIR, 'db.json'), () => createSeed(catalogSync()!));

let readyPromise: Promise<void> | null = null;
const ready = () =>
  (readyPromise ??= loadCatalog().then((cat) => {
    console.log(`Каталог: ${cat.families.length} продуктов из ${cat.source}`);
    store.load();
  }));

const sockets = new Set<WebSocket>();
const emit = (event: SupportEvent) => {
  const data = JSON.stringify(event);
  for (const ws of sockets) if (ws.readyState === ws.OPEN) ws.send(data);
};

const service = createSupportService({
  ready,
  getDb: () => store.get(),
  mutate: (fn) => store.mutate(fn),
  emit,
  catalog: () => catalogSync()!,
  reset: () => store.reset(),
});

// ── HTTP ──────────────────────────────────────────────────────────────

// Логи каждого запроса (в том числе проверок здоровья) — только по LOG_REQUESTS=true
const app = Fastify({
  logger: { level: process.env.LOG_LEVEL ?? 'info' },
  disableRequestLogging: process.env.LOG_REQUESTS !== 'true',
  bodyLimit: 256 * 1024,
});

app.setErrorHandler((err, req, reply) => {
  if (err instanceof NotFoundError) return reply.code(404).send({ error: err.message });
  if (err instanceof BadRequest) return reply.code(400).send({ error: err.message });
  req.log.error(err);
  return reply.code(500).send({ error: 'Внутренняя ошибка сервера' });
});

class BadRequest extends Error {}
const text = (v: unknown, field: string, max = 4000): string => {
  if (typeof v !== 'string' || !v.trim()) throw new BadRequest(`Поле ${field} обязательно`);
  if (v.length > max) throw new BadRequest(`Поле ${field} слишком длинное`);
  return v.trim();
};
const noContent = (reply: FastifyReply) => reply.code(204).send();

type P = { Params: { id: string } };
type B<T> = { Params: { id: string }; Body: T };

await app.register(fastifyWebsocket);

app.get('/healthz', async () => ({ ok: true }));

await app.register(
  async (api) => {
    // Клиент
    api.post<{ Body: { ticketId?: string; forceNew?: boolean } | undefined }>('/chat/session', async (req) =>
      service.startSession(req.body?.ticketId ?? null, !!req.body?.forceNew),
    );
    api.post<B<{ text: string }>>('/chat/:id/messages', async (req) => service.sendCustomerMessage(req.params.id, text(req.body?.text, 'text')));
    api.post<P>('/chat/:id/handoff', async (req, reply) => noContent(reply.send(await service.requestOperator(req.params.id))));
    api.post<B<QuoteRequest & { messageId: string }>>('/chat/:id/quote', async (req) => {
      const b = req.body ?? ({} as QuoteRequest & { messageId: string });
      return service.submitQuoteRequest(req.params.id, text(b.messageId, 'messageId', 100), {
        company: text(b.company, 'company', 300),
        inn: b.inn ? text(b.inn, 'inn', 12) : undefined,
        email: text(b.email, 'email', 200),
        paymentTerms: b.paymentTerms === 'Постоплата' ? 'Постоплата' : 'Предоплата',
      });
    });
    api.post<B<{ value: 'up' | 'down' | null }>>('/messages/:id/feedback', async (req, reply) => {
      const v = req.body?.value;
      await service.rateMessage(req.params.id, v === 'up' || v === 'down' ? v : null);
      return noContent(reply);
    });

    // Менеджер
    api.get('/tickets', async () => service.listTickets());
    api.get<P>('/tickets/:id/messages', async (req) => service.getMessages(req.params.id));
    api.post<B<{ operatorName: string }>>('/tickets/:id/join', async (req, reply) => {
      await service.joinTicket(req.params.id, text(req.body?.operatorName, 'operatorName', 100));
      return noContent(reply);
    });
    api.post<P>('/tickets/:id/release', async (req, reply) => {
      await service.releaseToAi(req.params.id);
      return noContent(reply);
    });
    api.post<P>('/tickets/:id/close', async (req, reply) => {
      await service.closeTicket(req.params.id);
      return noContent(reply);
    });
    api.post<B<{ text: string; operatorName: string }>>('/tickets/:id/messages', async (req) =>
      service.sendOperatorMessage(req.params.id, text(req.body?.text, 'text'), text(req.body?.operatorName, 'operatorName', 100)),
    );
    api.get<{ Params: { id: string }; Querystring: { operator?: string } }>('/tickets/:id/suggestion', async (req) => ({
      text: await service.getSuggestion(req.params.id, req.query.operator || 'Менеджер'),
    }));

    // КП
    api.get('/quotes', async () => service.listQuotes());
    api.get<P>('/quotes/:id', async (req) => service.getQuote(req.params.id));
    api.patch<B<Partial<Quote>>>('/quotes/:id', async (req) => {
      const b = req.body ?? {};
      // Менять можно только поля, которые правит менеджер
      const allowed = ['items', 'paymentTerms', 'deliveryTerm', 'validUntil', 'comment', 'company', 'inn', 'email'] as const;
      const patch = Object.fromEntries(allowed.filter((k) => k in b).map((k) => [k, b[k]]));
      return service.updateQuote(req.params.id, patch);
    });
    api.post<B<{ reviewer: string }>>('/quotes/:id/approve', async (req) => service.approveQuote(req.params.id, text(req.body?.reviewer, 'reviewer', 100)));

    // Аналитика и демо
    api.get('/stats', async () => service.getStats());
    api.post('/admin/reset', async (_req, reply) => {
      if (!ALLOW_RESET) return reply.code(403).send({ error: 'Сброс отключён (ALLOW_RESET=false)' });
      await service.resetDemo();
      return noContent(reply);
    });

    // События реального времени
    api.get('/ws', { websocket: true }, (socket) => {
      sockets.add(socket);
      socket.on('close', () => sockets.delete(socket));
      socket.on('error', () => sockets.delete(socket));
    });
  },
  { prefix: '/api' },
);

// ── Фронтенд ──────────────────────────────────────────────────────────

if (existsSync(join(PUBLIC_DIR, 'index.html'))) {
  await app.register(fastifyStatic, { root: PUBLIC_DIR, wildcard: false });
  // SPA: /operator, /chat и т.п. — отдаём index.html, маршруты разбирает React Router
  app.setNotFoundHandler((req, reply) => {
    if (req.url.startsWith('/api/') || req.method !== 'GET') return reply.code(404).send({ error: 'Не найдено' });
    return reply.sendFile('index.html');
  });
} else {
  app.log.warn(`Фронтенд не найден в ${PUBLIC_DIR} — работает только API`);
}

// ── Запуск ────────────────────────────────────────────────────────────

const shutdown = async () => {
  store.flush();
  await app.close();
  process.exit(0);
};
process.on('SIGTERM', shutdown);
process.on('SIGINT', shutdown);

await ready();
await app.listen({ port: PORT, host: HOST });
app.log.info(`Данные: ${DATA_DIR}, фронтенд: ${PUBLIC_DIR}, сброс демо: ${ALLOW_RESET ? 'включён' : 'выключен'}`);
