import type { Message, NewStaffUser, Operator, Product, Quote, StaffUser, StaffUserPatch, QuoteRequest, Session, Stats, SupportEvent, Ticket } from './types';
import { httpApi } from './http';

/**
 * Интерфейс API сервера (backend/, Go). UI работает только через него;
 * реализация — http.ts (REST + WebSocket).
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
  /** GET /api/catalog/featured — карточки демо-витрины и число позиций в каталоге */
  getFeatured(): Promise<{ total: number; items: Product[] }>;
  /** GET /api/chat/{ticketId}/quotes/{quoteId} — КП своего обращения (без входа менеджера) */
  getCustomerQuote(ticketId: string, quoteId: string): Promise<Quote>;

  // ── Вход менеджера ──────────────────────────────────────
  /** GET /api/auth/me — кто вошёл; null — не вошёл */
  getOperator(): Promise<Operator | null>;
  /** POST /api/auth/login — ставит cookie сессии; ошибка с текстом при неверных данных */
  login(login: string, password: string): Promise<Operator>;
  /** POST /api/auth/logout */
  logout(): Promise<void>;
  /** POST /api/auth/password — сменить свой пароль (остальные сессии закрываются) */
  changePassword(current: string, next: string): Promise<void>;

  // ── Сотрудники (только администратор) ───────────────────
  /** GET /api/users */
  listUsers(): Promise<StaffUser[]>;
  /** POST /api/users */
  createUser(user: NewStaffUser): Promise<StaffUser>;
  /** PATCH /api/users/{id} — имя, роль, отключение, новый пароль */
  updateUser(id: number, patch: StaffUserPatch): Promise<StaffUser>;

  // ── Менеджер ────────────────────────────────────────────
  // Имя менеджера в чате и КП сервер берёт из учётной записи вошедшего сотрудника
  /** GET /api/tickets */
  listTickets(): Promise<Ticket[]>;
  /** GET /api/tickets/{ticketId}/messages */
  getMessages(ticketId: string): Promise<Message[]>;
  /** POST /api/tickets/{ticketId}/join — менеджер подключается, ИИ замолкает */
  joinTicket(ticketId: string): Promise<void>;
  /** POST /api/tickets/{ticketId}/release — вернуть диалог ИИ */
  releaseToAi(ticketId: string): Promise<void>;
  /** POST /api/tickets/{ticketId}/close */
  closeTicket(ticketId: string): Promise<void>;
  /** POST /api/tickets/{ticketId}/messages */
  sendOperatorMessage(ticketId: string, text: string): Promise<Message>;
  /** GET /api/tickets/{ticketId}/suggestion — подсказка ответа от ИИ */
  getSuggestion(ticketId: string): Promise<string>;

  // ── КП ──────────────────────────────────────────────────
  /** GET /api/quotes */
  listQuotes(): Promise<Quote[]>;
  /** GET /api/quotes/{id} */
  getQuote(id: string): Promise<Quote>;
  /** PATCH /api/quotes/{id} — правки менеджера */
  updateQuote(id: string, patch: Partial<Pick<Quote, 'items' | 'paymentTerms' | 'deliveryTerm' | 'validUntil' | 'comment' | 'company' | 'inn' | 'email'>>): Promise<Quote>;
  /** POST /api/quotes/{id}/approve — согласовать и отправить клиенту */
  approveQuote(id: string): Promise<Quote>;

  // ── Аналитика ───────────────────────────────────────────
  /** GET /api/stats */
  getStats(): Promise<Stats>;

  /** GET /api/config — разрешён ли сброс демо-данных (кнопка в кабинете) */
  canResetDemo(): Promise<boolean>;
  /** POST /api/admin/reset — вернуть демо-данные к исходному состоянию */
  resetDemo(): Promise<void>;

  // ── Реальное время ──────────────────────────────────────
  /**
   * WS /api/ws — подписка на события; возвращает функцию отписки.
   * Без входа менеджера сервер присылает только события обращения ticketId.
   */
  subscribe(listener: (event: SupportEvent) => void, ticketId?: string): () => void;
}

export const api: SupportApi = httpApi;
