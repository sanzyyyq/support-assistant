import type { SupportApi } from './client';
import type { Message, Operator, Product, Quote, StaffUser, Session, Stats, SupportEvent, Ticket } from './types';

// Клиент сервера (support-assistant/backend, Go): REST + WebSocket /api/ws.

const BASE = import.meta.env.VITE_API_URL ?? '/api';
const SESSION_KEY = 'support-assistant.session-ticket';

/** Событие окна: API ответил 401 — нужно снова войти */
export const AUTH_REQUIRED = 'support-assistant:auth-required';

export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
  }
}

async function req<T>(path: string, init: RequestInit = {}): Promise<T> {
  const res = await fetch(BASE + path, {
    ...init,
    headers: init.body ? { 'Content-Type': 'application/json', ...(init.headers ?? {}) } : init.headers,
  });
  if (!res.ok) {
    // Сессия менеджера истекла — экран входа (RequireOperator) слушает это событие
    if (res.status === 401 && !path.startsWith('/auth/')) window.dispatchEvent(new Event(AUTH_REQUIRED));
    const body = (await res.json().catch(() => null)) as { error?: string } | null;
    throw new ApiError(res.status, body?.error ?? `${res.status} ${res.statusText}: ${path}`);
  }
  // 204 или пустое тело — ответа нет (не пытаемся разобрать пустую строку как JSON)
  const body = await res.text();
  if (res.status === 204 || !body) return undefined as T;
  return JSON.parse(body) as T;
}

const post = <T>(path: string, body?: unknown) =>
  req<T>(path, { method: 'POST', body: body === undefined ? undefined : JSON.stringify(body) });

function wsUrl(): string {
  const url = new URL(BASE + '/ws', window.location.href);
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
  return url.toString();
}

export const httpApi: SupportApi = {
  async startSession(forceNew = false) {
    const ticketId = forceNew ? undefined : localStorage.getItem(SESSION_KEY) ?? undefined;
    const session = await post<Session>('/chat/session', { ticketId });
    localStorage.setItem(SESSION_KEY, session.ticket.id);
    return session;
  },
  sendCustomerMessage: (ticketId, text) => post<Message>(`/chat/${ticketId}/messages`, { text }),
  requestOperator: (ticketId) => post<void>(`/chat/${ticketId}/handoff`),
  submitQuoteRequest: (ticketId, messageId, data) => post<Quote>(`/chat/${ticketId}/quote`, { messageId, ...data }),
  rateMessage: (messageId, value) => post<void>(`/messages/${messageId}/feedback`, { value }),
  getFeatured: () => req<{ total: number; items: Product[] }>('/catalog/featured'),
  getCustomerQuote: (ticketId, quoteId) => req<Quote>(`/chat/${ticketId}/quotes/${encodeURIComponent(quoteId)}`),

  getOperator: async () => (await req<{ operator: Operator | null }>('/auth/me')).operator,
  login: async (login, password) => (await post<{ operator: Operator }>('/auth/login', { login, password })).operator,
  logout: () => post<void>('/auth/logout'),
  changePassword: (current, next) => post<void>('/auth/password', { current, password: next }),
  listUsers: () => req<StaffUser[]>('/users'),
  createUser: (user) => post<StaffUser>('/users', user),
  updateUser: (id, patch) => req<StaffUser>(`/users/${id}`, { method: 'PATCH', body: JSON.stringify(patch) }),

  listQuotes: () => req<Quote[]>('/quotes'),
  getQuote: (id) => req<Quote>(`/quotes/${id}`),
  updateQuote: (id, patch) => req<Quote>(`/quotes/${id}`, { method: 'PATCH', body: JSON.stringify(patch) }),
  approveQuote: (id) => post<Quote>(`/quotes/${id}/approve`),
  getStats: () => req<Stats>('/stats'),
  canResetDemo: async () => (await req<{ allowReset: boolean }>('/config')).allowReset,
  resetDemo: () => post<void>('/admin/reset'),

  listTickets: () => req<Ticket[]>('/tickets'),
  getMessages: (ticketId) => req<Message[]>(`/tickets/${ticketId}/messages`),
  joinTicket: (ticketId) => post<void>(`/tickets/${ticketId}/join`),
  releaseToAi: (ticketId) => post<void>(`/tickets/${ticketId}/release`),
  closeTicket: (ticketId) => post<void>(`/tickets/${ticketId}/close`),
  sendOperatorMessage: (ticketId, text) => post<Message>(`/tickets/${ticketId}/messages`, { text }),
  getSuggestion: async (ticketId) => (await req<{ text: string }>(`/tickets/${ticketId}/suggestion`)).text,

  subscribe(listener, ticketId) {
    let ws: WebSocket | null = null;
    let closed = false;
    let retry: number | undefined;
    const connect = () => {
      ws = new WebSocket(wsUrl());
      const socket = ws;
      // Клиентский чат сообщает своё обращение — сервер пришлёт только его события
      if (ticketId) socket.onopen = () => socket.send(JSON.stringify({ type: 'watch', ticketId }));
      socket.onmessage = (e) => listener(JSON.parse(e.data) as SupportEvent);
      ws.onclose = () => {
        if (!closed) retry = window.setTimeout(connect, 2000);
      };
    };
    connect();
    return () => {
      closed = true;
      window.clearTimeout(retry);
      ws?.close();
    };
  },
};
