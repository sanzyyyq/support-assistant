import { formatPrice } from '../../lib/format';
import { quoteTotals } from '../../lib/domain';
import type { SupportApi } from '../client';
import type { Message, Quote, Session, Stats, Ticket } from '../types';
import { buildSummary, generateReply, GREETING, suggestReply, type DraftMessage } from './bot';
import type { DB } from './data';
import { catalogSync } from './catalog';
import chats from './chats.json';
import { buildQuote } from './offers';
import { emit, ensureDb, getDb, mutate, subscribe } from './store';

const SESSION_KEY = 'support-assistant.session-ticket.v2';

const delay = (ms: number) => new Promise((r) => setTimeout(r, ms));
const latency = async () => {
  await ensureDb();
  await delay(120 + Math.random() * 180);
};
const uid = () => Math.random().toString(36).slice(2, 10) + Date.now().toString(36);
const now = () => new Date().toISOString();

function findTicket(db: DB, id: string): Ticket {
  const t = db.tickets.find((x) => x.id === id);
  if (!t) throw new Error(`Обращение ${id} не найдено`);
  return t;
}

function findQuote(db: DB, id: string): Quote {
  const q = db.quotes.find((x) => x.id === id);
  if (!q) throw new Error(`КП ${id} не найдено`);
  return q;
}

/** Добавить сообщения в диалог и разослать события. */
function post(ticketId: string, drafts: DraftMessage[]): Message[] {
  const { msgs, ticket } = mutate((db) => {
    const t = findTicket(db, ticketId);
    const msgs = drafts.map((d) => {
      const msg: Message = { ...d, id: uid(), ticketId, createdAt: now() };
      db.messages.push(msg);
      t.updatedAt = msg.createdAt;
      if (msg.sender !== 'system') t.lastMessage = msg.text;
      return msg;
    });
    return { msgs, ticket: t };
  });
  msgs.forEach((message) => emit({ type: 'message', message }));
  emit({ type: 'ticket_updated', ticket });
  return msgs;
}

const system = (ticketId: string, text: string) => post(ticketId, [{ sender: 'system', text }]);

function patch(ticketId: string, fn: (t: Ticket, db: DB) => void): Ticket {
  const ticket = mutate((db) => {
    const t = findTicket(db, ticketId);
    fn(t, db);
    t.updatedAt = now();
    return t;
  });
  emit({ type: 'ticket_updated', ticket });
  return ticket;
}

function updateMessage(id: string, fn: (m: Message) => void) {
  const message = mutate((db) => {
    const m = db.messages.find((x) => x.id === id);
    if (!m) throw new Error('Сообщение не найдено');
    fn(m);
    return m;
  });
  emit({ type: 'message_updated', message });
}

function handoff(ticketId: string, reason: string, confidence: number, botText?: string) {
  if (botText) post(ticketId, [{ sender: 'bot', intent: 'handoff', text: botText }]);
  patch(ticketId, (t, db) => {
    t.status = 'waiting';
    t.handoffAt = now();
    t.handoffReason = reason;
    t.aiConfidence = confidence;
    t.pendingSlot = undefined;
    t.aiSummary = buildSummary(t, db.messages.filter((m) => m.ticketId === t.id), reason);
  });
  system(ticketId, 'ИИ передал диалог оператору');
}

async function typing(ticketId: string, ms: number) {
  emit({ type: 'typing', ticketId, sender: 'bot', active: true });
  await delay(ms);
  emit({ type: 'typing', ticketId, sender: 'bot', active: false });
}

async function runBot(ticketId: string, text: string) {
  await typing(ticketId, 700 + Math.random() * 700);
  const db = getDb();
  const ticket = findTicket(db, ticketId);
  // Пока бот «думал», мог подключиться менеджер — тогда молчим.
  if (ticket.status !== 'ai') return;
  const reply = generateReply(catalogSync()!, ticket, text, db.messages.filter((m) => m.ticketId === ticketId));

  patch(ticketId, (t) => {
    Object.assign(t, reply.patch ?? {});
    if (reply.sentiment) t.sentiment = reply.sentiment;
    for (const tag of reply.addTags ?? []) if (!t.tags.includes(tag)) t.tags.push(tag);
  });

  for (const [i, m] of reply.messages.entries()) {
    if (i > 0) await typing(ticketId, 500);
    post(ticketId, [m]);
  }
  if (!reply.handoff) {
    // Резюме обновляется по ходу диалога — менеджер видит суть, не читая переписку
    patch(ticketId, (t, db) => {
      t.aiSummary = buildSummary(t, db.messages.filter((x) => x.ticketId === t.id));
    });
  }
  if (reply.handoff) {
    handoff(
      ticketId,
      reply.handoff.reason,
      reply.handoff.confidence,
      reply.messages.length ? undefined : 'Подключаю менеджера — обычно это занимает 1–2 минуты. Он увидит всю историю и ваши требования.',
    );
  }
}

const byDate = <T extends { updatedAt?: string; createdAt: string }>(list: T[]) =>
  [...list].sort((a, b) => +new Date(b.updatedAt ?? b.createdAt) - +new Date(a.updatedAt ?? a.createdAt));

async function createSession(forceNew: boolean): Promise<Session> {
  await latency();
  const existingId = forceNew ? null : localStorage.getItem(SESSION_KEY);
  const db = getDb();
  const existing = existingId ? db.tickets.find((t) => t.id === existingId) : undefined;
  if (existing) return { ticket: existing, messages: db.messages.filter((m) => m.ticketId === existing.id) };

  const ticket = mutate((db) => {
    const number = db.seq++;
    const t: Ticket = {
      id: `t-${number}`, number, channel: 'web', status: 'ai',
      createdAt: now(), updatedAt: now(), tags: [], sentiment: 'neutral', requirements: {},
      customer: { id: uid(), name: `Посетитель #${number}` },
    };
    db.tickets.push(t);
    return t;
  });
  localStorage.setItem(SESSION_KEY, ticket.id);
  emit({ type: 'ticket_updated', ticket });
  const messages = post(ticket.id, [GREETING]);
  return { ticket: findTicket(getDb(), ticket.id), messages };
}

// Одновременные вызовы startSession (StrictMode, две вкладки) не должны плодить диалоги
let pendingSession: Promise<Session> | null = null;

export const mockApi: SupportApi = {
  startSession(forceNew = false) {
    if (pendingSession && !forceNew) return pendingSession;
    const p = createSession(forceNew).finally(() => {
      if (pendingSession === p) pendingSession = null;
    });
    pendingSession = p;
    return p;
  },

  async sendCustomerMessage(ticketId, text) {
    await latency();
    const [msg] = post(ticketId, [{ sender: 'user', text }]);
    const ticket = findTicket(getDb(), ticketId);
    if (ticket.status === 'closed') {
      patch(ticketId, (t) => {
        t.status = 'ai';
        t.assignee = undefined;
      });
      system(ticketId, 'Диалог возобновлён');
      void runBot(ticketId, text);
    } else if (ticket.status === 'ai') {
      void runBot(ticketId, text);
    }
    return msg;
  },

  async requestOperator(ticketId) {
    await latency();
    const t = findTicket(getDb(), ticketId);
    if (t.status === 'waiting' || t.status === 'operator') return;
    handoff(ticketId, 'Клиент попросил менеджера', t.aiConfidence ?? 0.7, 'Подключаю менеджера — обычно это занимает 1–2 минуты. Он увидит всю историю и ваши требования.');
  },

  async submitQuoteRequest(ticketId, messageId, data) {
    await ensureDb();
    await delay(500);
    const db = getDb();
    const ticket = findTicket(db, ticketId);
    const offers = ticket.lastOffers ?? [];
    const offer = offers.find((o) => o.id === ticket.selectedOfferId) ?? offers.find((o) => o.recommended) ?? offers[0];
    if (!offer) throw new Error('Нет выбранного решения');

    const quote = mutate((db) => {
      const q = buildQuote(ticket, offer, data, db.quoteSeq++);
      db.quotes.push(q);
      return q;
    });
    emit({ type: 'quote_updated', quote });
    updateMessage(messageId, (m) => {
      m.formDone = true;
    });
    patch(ticketId, (t) => {
      t.quoteId = quote.id;
      t.customer = { ...t.customer, company: data.company, inn: data.inn, email: data.email };
      if (!t.tags.includes('КП')) t.tags.push('КП');
    });
    await typing(ticketId, 600);
    post(ticketId, [{
      sender: 'bot', intent: 'quote_created', quoteId: quote.id,
      text: `Готово! Черновик ${quote.number} на ${formatPrice(quoteTotals(quote).sum)} сформирован по шаблону и передан менеджеру на проверку. После согласования он придёт на ${quote.email}.`,
      quickReplies: ['Как происходит поставка?', 'Позвать менеджера'],
    }]);
    return quote;
  },

  async rateMessage(messageId, value) {
    await latency();
    updateMessage(messageId, (m) => {
      m.feedback = value ?? undefined;
    });
  },

  async listTickets() {
    await latency();
    return byDate(getDb().tickets);
  },

  async getMessages(ticketId) {
    await latency();
    return getDb().messages.filter((m) => m.ticketId === ticketId);
  },

  async joinTicket(ticketId, operatorName) {
    await latency();
    patch(ticketId, (t) => {
      t.status = 'operator';
      t.assignee = operatorName;
      t.pendingSlot = undefined;
    });
    system(ticketId, `В чате оператор: ${operatorName}`);
  },

  async releaseToAi(ticketId) {
    await latency();
    patch(ticketId, (t) => {
      t.status = 'ai';
      t.assignee = undefined;
    });
    system(ticketId, 'Диалог снова ведёт ИИ-консультант');
  },

  async closeTicket(ticketId) {
    await latency();
    patch(ticketId, (t) => {
      t.status = 'closed';
    });
    system(ticketId, 'Обращение закрыто');
  },

  async sendOperatorMessage(ticketId, text, operatorName) {
    await latency();
    const [msg] = post(ticketId, [{ sender: 'operator', text, authorName: operatorName }]);
    return msg;
  },

  async getSuggestion(ticketId, operatorName) {
    await ensureDb();
    await delay(400 + Math.random() * 400);
    return suggestReply(findTicket(getDb(), ticketId), operatorName);
  },

  async listQuotes() {
    await latency();
    return byDate(getDb().quotes);
  },

  async getQuote(id) {
    await latency();
    return findQuote(getDb(), id);
  },

  async updateQuote(id, changes) {
    await latency();
    const quote = mutate((db) => Object.assign(findQuote(db, id), changes));
    emit({ type: 'quote_updated', quote });
    return quote;
  },

  async approveQuote(id, reviewer) {
    await ensureDb();
    await delay(400);
    const quote = mutate((db) => {
      const q = findQuote(db, id);
      q.status = 'sent';
      q.reviewer = reviewer;
      return q;
    });
    emit({ type: 'quote_updated', quote });
    system(quote.ticketId, `${quote.number} согласовано и отправлено на ${quote.email}`);
    return quote;
  },

  async getStats(): Promise<Stats> {
    await latency();
    const db = getDb();
    // База — реальные переписки поддержки (≈500 обращений, как в презентации) + живые данные заглушки
    const BASE = { tickets: chats.total, aiResolved: Math.round(chats.total * 0.71), handoffs: Math.round(chats.total * 0.22), quotes: 41, quotesAmount: 11_870_000, up: 214, down: 23 };
    const live = db.tickets;
    const handoffs = live.filter((t) => t.handoffAt || t.assignee).length;
    const aiResolved = live.filter((t) => !t.handoffAt && !t.assignee).length;
    const rated = db.messages.filter((m) => m.feedback);
    const up = BASE.up + rated.filter((m) => m.feedback === 'up').length;
    const down = BASE.down + rated.filter((m) => m.feedback === 'down').length;
    const total = BASE.tickets + live.length;
    return {
      ticketsToday: total,
      aiResolvedPct: Math.round(((BASE.aiResolved + aiResolved) / total) * 100),
      handoffPct: Math.round(((BASE.handoffs + handoffs) / total) * 100),
      firstResponseSec: 1.8,
      quotes: db.quotes.length + BASE.quotes,
      quotesAmount: BASE.quotesAmount + db.quotes.reduce((s, q) => s + quoteTotals(q).sum, 0),
      csatPct: Math.round((up / (up + down)) * 100),
      ratedCount: up + down,
      // Реальные категории обращений из переписок поддержки
      topIntents: chats.categories.map((c) => ({ label: c.label, count: c.count })),
      // ~8 минут ручной работы на обращение, которое закрыл ИИ
      hoursSavedPerDay: Math.round((((BASE.aiResolved + aiResolved) * 8) / 60) * 10) / 10,
    };
  },

  subscribe,
};
