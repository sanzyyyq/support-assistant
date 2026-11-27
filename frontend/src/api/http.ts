import type { SupportApi } from './client';
import type { Message, Quote, Session, Stats, SupportEvent, Ticket } from './types';

// Реализация под настоящий бэкенд (support-assistant/backend). Используется по умолчанию;
// VITE_USE_MOCK=true переключает на офлайн-заглушку в браузере.

const BASE = import.meta.env.VITE_API_URL ?? '/api';
const SESSION_KEY = 'support-assistant.session-ticket';

async function req<T>(path: string, init: RequestInit = {}): Promise<T> {
  const res = await fetch(BASE + path, {
    ...init,
    headers: init.body ? { 'Content-Type': 'application/json', ...(init.headers ?? {}) } : init.headers,
  });
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}: ${path}`);
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
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

  listQuotes: () => req<Quote[]>('/quotes'),
  getQuote: (id) => req<Quote>(`/quotes/${id}`),
  updateQuote: (id, patch) => req<Quote>(`/quotes/${id}`, { method: 'PATCH', body: JSON.stringify(patch) }),
  approveQuote: (id, reviewer) => post<Quote>(`/quotes/${id}/approve`, { reviewer }),
  getStats: () => req<Stats>('/stats'),
  resetDemo: () => post<void>('/admin/reset'),

  listTickets: () => req<Ticket[]>('/tickets'),
  getMessages: (ticketId) => req<Message[]>(`/tickets/${ticketId}/messages`),
  joinTicket: (ticketId, operatorName) => post<void>(`/tickets/${ticketId}/join`, { operatorName }),
  releaseToAi: (ticketId) => post<void>(`/tickets/${ticketId}/release`),
  closeTicket: (ticketId) => post<void>(`/tickets/${ticketId}/close`),
  sendOperatorMessage: (ticketId, text, operatorName) =>
    post<Message>(`/tickets/${ticketId}/messages`, { text, operatorName }),
  getSuggestion: async (ticketId, operatorName) =>
    (await req<{ text: string }>(
      `/tickets/${ticketId}/suggestion?operator=${encodeURIComponent(operatorName)}`,
    )).text,

  subscribe(listener) {
    let ws: WebSocket | null = null;
    let closed = false;
    let retry: number | undefined;
    const connect = () => {
      ws = new WebSocket(wsUrl());
      ws.onmessage = (e) => listener(JSON.parse(e.data) as SupportEvent);
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
