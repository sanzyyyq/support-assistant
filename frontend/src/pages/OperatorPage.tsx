import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { AlertCircle, Inbox } from 'lucide-react';
import { api } from '../api/client';
import type { Message, Operator, Quote, Ticket, TicketStatus } from '../api/types';
import { useOperator } from './operator/operatorContext';
import { Conversation } from './operator/Conversation';
import { CustomerPanel } from './operator/CustomerPanel';
import { TicketList, type Filter } from './operator/TicketList';

const PRIORITY: Record<TicketStatus, number> = { waiting: 0, operator: 1, ai: 2, closed: 3 };

const sortTickets = (list: Ticket[]) =>
  [...list].sort((a, b) => PRIORITY[a.status] - PRIORITY[b.status] || +new Date(b.updatedAt) - +new Date(a.updatedAt));

const matchesFilter = (t: Ticket, f: Filter, me: Operator) => {
  switch (f) {
    case 'all':
      return t.status !== 'closed';
    case 'mine':
      return t.status === 'operator' && t.assignee === me.name;
    default:
      return t.status === f;
  }
};

const upsert = <T extends { id: string }>(list: T[], x: T) =>
  list.some((y) => y.id === x.id) ? list.map((y) => (y.id === x.id ? x : y)) : [...list, x];

/** Обращения: список + диалог + карточка клиента */
export function OperatorPage() {
  const me = useOperator();
  const [params, setParams] = useSearchParams();
  const navigate = useNavigate();
  const [tickets, setTickets] = useState<Ticket[]>([]);
  const [loading, setLoading] = useState(true);
  const [messages, setMessages] = useState<Message[]>([]);
  const [quotes, setQuotes] = useState<Record<string, Quote>>({});
  const [filter, setFilter] = useState<Filter>('all');
  const [query, setQuery] = useState('');
  const [botTyping, setBotTyping] = useState(false);
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [, tick] = useState(0);

  const selectedId = params.get('ticket');
  const setSelectedId = useCallback((id: string | null) => setParams(id ? { ticket: id } : {}, { replace: true }), [setParams]);
  const selectedRef = useRef<string | null>(null);
  selectedRef.current = selectedId;

  useEffect(() => {
    const id = window.setInterval(() => tick((x) => x + 1), 30_000);
    return () => window.clearInterval(id);
  }, []);

  const loadTickets = useCallback(async () => {
    try {
      const [list, qs] = await Promise.all([api.listTickets(), api.listQuotes()]);
      setTickets(list);
      setQuotes(Object.fromEntries(qs.map((q) => [q.id, q])));
      if (!selectedRef.current || !list.some((t) => t.id === selectedRef.current)) setSelectedId(sortTickets(list)[0]?.id ?? null);
    } catch {
      setError('Не удалось загрузить обращения.');
    } finally {
      setLoading(false);
    }
  }, [setSelectedId]);

  useEffect(() => {
    void loadTickets();
  }, [loadTickets]);

  useEffect(() => {
    setBotTyping(false);
    if (!selectedId) {
      setMessages([]);
      return;
    }
    let cancelled = false;
    api
      .getMessages(selectedId)
      .then((ms) => !cancelled && setMessages(ms))
      .catch(() => !cancelled && setError('Не удалось загрузить переписку.'));
    return () => {
      cancelled = true;
    };
  }, [selectedId]);

  useEffect(
    () =>
      api.subscribe((e) => {
        switch (e.type) {
          case 'ticket_updated':
            setTickets((ts) => upsert(ts, e.ticket));
            break;
          case 'quote_updated':
            setQuotes((x) => ({ ...x, [e.quote.id]: e.quote }));
            break;
          case 'message':
          case 'message_updated':
            if (e.message.ticketId === selectedRef.current) setMessages((ms) => upsert(ms, e.message));
            break;
          case 'typing':
            if (e.ticketId === selectedRef.current && e.sender === 'bot') setBotTyping(e.active);
            break;
          case 'reset':
            setSelectedId(null);
            void loadTickets();
            break;
        }
      }),
    [loadTickets, setSelectedId],
  );

  const counts = useMemo(() => {
    const c: Record<Filter, number> = { all: 0, waiting: 0, ai: 0, mine: 0, closed: 0 };
    for (const t of tickets) (Object.keys(c) as Filter[]).forEach((f) => matchesFilter(t, f, me) && c[f]++);
    return c;
  }, [tickets, me]);

  const visible = useMemo(() => {
    const q = query.trim().toLowerCase();
    return sortTickets(tickets).filter(
      (t) =>
        matchesFilter(t, filter, me) &&
        (!q ||
          [t.customer.name, t.customer.company ?? '', String(t.number), t.lastMessage ?? '', t.customer.email ?? ''].some((s) =>
            s.toLowerCase().includes(q),
          )),
    );
  }, [tickets, filter, query, me]);

  const selected = tickets.find((t) => t.id === selectedId) ?? null;
  const draft = selectedId ? drafts[selectedId] ?? '' : '';
  const setDraft = (v: string) => selectedId && setDrafts((d) => ({ ...d, [selectedId]: v }));

  const run = async (fn: () => Promise<unknown>, failText: string) => {
    setBusy(true);
    setError(null);
    try {
      await fn();
    } catch {
      setError(failText);
    } finally {
      setBusy(false);
    }
  };

  const join = () => selected && run(() => api.joinTicket(selected.id), 'Не удалось подключиться к чату.');
  const release = () => selected && run(() => api.releaseToAi(selected.id), 'Не удалось вернуть диалог ИИ.');
  const close = () => selected && run(() => api.closeTicket(selected.id), 'Не удалось закрыть обращение.');
  const send = (text: string) => {
    if (!selected) return;
    const id = selected.id;
    setDrafts((d) => ({ ...d, [id]: '' }));
    void run(() => api.sendOperatorMessage(id, text), 'Сообщение не отправлено.');
  };
  const quickAction = async (text: string) => {
    if (!selected) return;
    const mine = selected.status === 'operator' && selected.assignee === me.name;
    if (!mine) await run(() => api.joinTicket(selected.id), 'Не удалось подключиться к чату.');
    setDrafts((d) => ({ ...d, [selected.id]: text }));
  };
  const approve = (q: Quote) => run(() => api.approveQuote(q.id), 'Не удалось согласовать КП.');

  return (
    <>
      <TicketList
        tickets={visible}
        counts={counts}
        filter={filter}
        onFilter={setFilter}
        query={query}
        onQuery={setQuery}
        selectedId={selectedId}
        onSelect={setSelectedId}
        loading={loading}
      />

      {selected ? (
        <>
          <div className="op__center">
            {error && (
              <div className="error-banner op__error" role="alert">
                <AlertCircle size={16} />
                {error}
              </div>
            )}
            <Conversation
              ticket={selected}
              messages={messages}
              botTyping={botTyping}
              draft={draft}
              onDraft={setDraft}
              onJoin={join}
              onRelease={release}
              onClose={close}
              onSend={send}
              busy={busy}
              quotes={quotes}
              onOpenQuote={(q) => navigate(`/operator/quotes?id=${q.id}`)}
            />
          </div>
          <CustomerPanel
            ticket={selected}
            quote={selected.quoteId ? quotes[selected.quoteId] : undefined}
            onQuickAction={quickAction}
            onApproveQuote={approve}
            busy={busy}
          />
        </>
      ) : (
        <div className="op__empty">
          <Inbox size={40} strokeWidth={1.5} />
          <p>Выберите обращение слева</p>
        </div>
      )}
    </>
  );
}
