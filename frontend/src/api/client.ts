import type { Message, Quote, QuoteRequest, Session, Stats, SupportEvent, Ticket } from './types';
import { httpApi } from './http';
import { mockApi } from './mock/mockApi';

/**
 * Единый интерфейс API. UI работает только через него,
 * поэтому замена заглушки на настоящий бэкенд не трогает компоненты.
 */
export interface SupportApi {
  // ── Клиент ──────────────────────────────────────────────
  /** POST /api/chat/session — получить текущий диалог клиента или начать новый */
  startSession(forceNew?: boolean): Promise<Session>;
  /** POST /api/chat/{ticketId}/messages — сообщение клиента (ответ ИИ придёт событием) */
  sendCustomerMessage(ticketId: string, text: string): Promise<Message>;
  /** POST /api/chat/{ticketId}/handoff — клиент просит менеджера */
  requestOperator(ticketId: string): Promise<void>;
  /** POST /api/chat/{ticketId}/quote — данные компании из формы → черновик КП */
  submitQuoteRequest(ticketId: string, messageId: string, data: QuoteRequest): Promise<Quote>;
  /** POST /api/messages/{messageId}/feedback — 👍 / 👎 ответу ИИ */
  rateMessage(messageId: string, value: 'up' | 'down' | null): Promise<void>;

  // ── Менеджер ────────────────────────────────────────────
  /** GET /api/tickets */
  listTickets(): Promise<Ticket[]>;
  /** GET /api/tickets/{ticketId}/messages */
  getMessages(ticketId: string): Promise<Message[]>;
  /** POST /api/tickets/{ticketId}/join — менеджер подключается, ИИ замолкает */
  joinTicket(ticketId: string, operatorName: string): Promise<void>;
  /** POST /api/tickets/{ticketId}/release — вернуть диалог ИИ */
  releaseToAi(ticketId: string): Promise<void>;
  /** POST /api/tickets/{ticketId}/close */
  closeTicket(ticketId: string): Promise<void>;
  /** POST /api/tickets/{ticketId}/messages */
  sendOperatorMessage(ticketId: string, text: string, operatorName: string): Promise<Message>;
  /** GET /api/tickets/{ticketId}/suggestion — подсказка ответа от ИИ */
  getSuggestion(ticketId: string, operatorName: string): Promise<string>;

  // ── КП ──────────────────────────────────────────────────
  /** GET /api/quotes */
  listQuotes(): Promise<Quote[]>;
  /** GET /api/quotes/{id} */
  getQuote(id: string): Promise<Quote>;
  /** PATCH /api/quotes/{id} — правки менеджера */
  updateQuote(id: string, patch: Partial<Pick<Quote, 'items' | 'paymentTerms' | 'deliveryTerm' | 'validUntil' | 'comment' | 'company' | 'inn' | 'email'>>): Promise<Quote>;
  /** POST /api/quotes/{id}/approve — согласовать и отправить клиенту */
  approveQuote(id: string, reviewer: string): Promise<Quote>;

  // ── Аналитика ───────────────────────────────────────────
  /** GET /api/stats */
  getStats(): Promise<Stats>;

  /** POST /api/admin/reset — вернуть демо-данные к исходному состоянию */
  resetDemo(): Promise<void>;

  // ── Реальное время ──────────────────────────────────────
  /** WS /api/ws — подписка на события; возвращает функцию отписки */
  subscribe(listener: (event: SupportEvent) => void): () => void;
}

/** По умолчанию — настоящий бэкенд; VITE_USE_MOCK=true включает офлайн-заглушку в браузере */
export const USE_MOCK = import.meta.env.VITE_USE_MOCK === 'true';

export const api: SupportApi = USE_MOCK ? mockApi : httpApi;
