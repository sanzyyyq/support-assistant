import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { AlertCircle, Headset, Maximize2, RotateCcw, Sparkles, X } from 'lucide-react';
import { api } from '../api/client';
import type { Message, Quote, QuoteRequest, Ticket } from '../api/types';
import { BRAND } from '../config';
import { Composer } from './Composer';
import { MessageItem, TypingIndicator, type ChatActions } from './MessageItem';
import { QuoteModal } from './QuoteBlocks';
import { RequirementsBar } from './RequirementsBar';
import { IconButton } from './ui';
import '../styles/chat.css';

const upsert = (list: Message[], m: Message) =>
  list.some((x) => x.id === m.id) ? list.map((x) => (x.id === m.id ? m : x)) : [...list, m];

function statusLine(ticket: Ticket | null): { text: string; tone: 'online' | 'wait' | 'operator' | 'off' } {
  switch (ticket?.status) {
    case 'waiting':
      return { text: 'Подключаем менеджера · обычно до 2 минут', tone: 'wait' };
    case 'operator':
      return { text: `В чате менеджер ${ticket.assignee ?? ''}`.trim(), tone: 'operator' };
    case 'closed':
      return { text: 'Диалог завершён · напишите, чтобы продолжить', tone: 'off' };
    default:
      return { text: 'Онлайн · отвечает за секунды', tone: 'online' };
  }
}

interface Props {
  variant: 'page' | 'widget';
  onClose?: () => void;
  /** Сообщение, которое нужно отправить сразу (кнопка «Спросить консультанта» на витрине) */
  prompt?: { text: string; key: number } | null;
}

export function ChatWindow({ variant, onClose, prompt }: Props) {
  const [ticket, setTicket] = useState<Ticket | null>(null);
  const [messages, setMessages] = useState<Message[]>([]);
  const [quotes, setQuotes] = useState<Record<string, Quote>>({});
  const [openQuote, setOpenQuote] = useState<string | null>(null);
  const [typing, setTyping] = useState(false);
  const [draft, setDraft] = useState('');
  const [error, setError] = useState<string | null>(null);
  const endRef = useRef<HTMLDivElement>(null);
  const sentPrompt = useRef<number | null>(null);
  const ticketId = ticket?.id;

  const start = useCallback(async (forceNew = false) => {
    try {
      setError(null);
      const s = await api.startSession(forceNew);
      setTicket(s.ticket);
      setMessages(s.messages);
      setTyping(false);
    } catch {
      setError('Не удалось подключиться к консультанту. Обновите страницу.');
    }
  }, []);

  useEffect(() => {
    void start();
  }, [start]);

  // Подгружаем КП, на которые ссылаются сообщения
  useEffect(() => {
    const missing = messages.map((m) => m.quoteId).filter((id): id is string => !!id && !quotes[id]);
    for (const id of new Set(missing)) {
      if (!ticketId) continue;
      api.getCustomerQuote(ticketId, id).then((q) => setQuotes((x) => ({ ...x, [q.id]: q }))).catch(() => undefined);
    }
  }, [messages, quotes, ticketId]);

  useEffect(() => {
    if (!ticketId) return;
    return api.subscribe((e) => {
      switch (e.type) {
        case 'message':
        case 'message_updated':
          if (e.message.ticketId === ticketId) setMessages((ms) => upsert(ms, e.message));
          break;
        case 'ticket_updated':
          if (e.ticket.id === ticketId) setTicket(e.ticket);
          break;
        case 'quote_updated':
          if (e.quote.ticketId === ticketId) setQuotes((x) => ({ ...x, [e.quote.id]: e.quote }));
          break;
        case 'typing':
          if (e.ticketId === ticketId) setTyping(e.active);
          break;
        case 'reset':
          void start();
          break;
      }
    }, ticketId);
  }, [ticketId, start]);

  useEffect(() => {
    endRef.current?.scrollIntoView({ behavior: 'smooth', block: 'end' });
  }, [messages.length, typing]);

  const send = useCallback(
    async (text: string) => {
      if (!ticketId) return;
      setDraft('');
      try {
        const m = await api.sendCustomerMessage(ticketId, text);
        setMessages((ms) => upsert(ms, m));
      } catch {
        setError('Сообщение не отправлено. Проверьте соединение и попробуйте ещё раз.');
        setDraft(text);
      }
    },
    [ticketId],
  );

  // Запрос с витрины: отправляем, как только есть диалог
  useEffect(() => {
    if (prompt && ticketId && sentPrompt.current !== prompt.key) {
      sentPrompt.current = prompt.key;
      void send(prompt.text);
    }
  }, [prompt, ticketId, send]);

  const actions: ChatActions = useMemo(
    () => ({
      onQuickReply: (t) => void send(t),
      onSubmitQuote: async (messageId: string, data: QuoteRequest) => {
        if (!ticketId) return;
        const q = await api.submitQuoteRequest(ticketId, messageId, data);
        setQuotes((x) => ({ ...x, [q.id]: q }));
      },
      onRate: (messageId, value) => {
        setMessages((ms) => ms.map((m) => (m.id === messageId ? { ...m, feedback: value ?? undefined } : m)));
        void api.rateMessage(messageId, value);
      },
    }),
    [send, ticketId],
  );

  const callOperator = async () => {
    if (!ticketId) return;
    try {
      await api.requestOperator(ticketId);
    } catch {
      setError('Не получилось позвать менеджера. Попробуйте ещё раз.');
    }
  };

  const quickFor = useMemo(() => {
    const last = messages[messages.length - 1];
    return last?.sender === 'bot' && last.quickReplies?.length && !typing ? last.id : null;
  }, [messages, typing]);

  const status = statusLine(ticket);
  const canCallOperator = ticket?.status === 'ai' || ticket?.status === 'closed';
  const quote = openQuote ? quotes[openQuote] : undefined;

  return (
    <div className={`chat chat--${variant}`}>
      <header className="chat-header">
        <span className="chat-logo" aria-hidden>
          <Sparkles size={18} />
        </span>
        <div className="chat-header__info">
          <div className="chat-header__title">
            {BRAND.assistant} {BRAND.store}
          </div>
          <div className={`chat-status chat-status--${status.tone}`}>
            <i aria-hidden />
            {status.text}
          </div>
        </div>
        <div className="chat-header__actions">
          {canCallOperator && (
            <IconButton variant="soft" label="Позвать менеджера" onClick={callOperator}>
              <Headset size={18} />
            </IconButton>
          )}
          <IconButton label="Начать новый диалог" variant="ghost" onClick={() => void start(true)}>
            <RotateCcw size={18} />
          </IconButton>
          {variant === 'widget' && (
            // Тот же диалог на весь экран: обращение хранится в браузере и подхватится на /chat
            <Link className="icon-btn icon-btn--ghost" to="/chat" aria-label="Открыть чат на весь экран" title="Открыть чат на весь экран">
              <Maximize2 size={17} />
            </Link>
          )}
          {onClose && (
            <IconButton label="Свернуть чат" variant="ghost" onClick={onClose}>
              <X size={20} />
            </IconButton>
          )}
        </div>
      </header>

      {ticket && <RequirementsBar ticket={ticket} />}

      <main className="chat-scroll" aria-live="polite">
        <div className="chat-inner">
          {ticket && (
            <div className="chat-date">
              Обращение #{ticket.number} · {new Date(ticket.createdAt).toLocaleDateString('ru-RU', { day: 'numeric', month: 'long' })}
            </div>
          )}
          {messages.map((m) => (
            <MessageItem
              key={m.id}
              message={m}
              showQuickReplies={m.id === quickFor}
              actions={actions}
              quotes={quotes}
              onOpenQuote={(q) => setOpenQuote(q.id)}
              selectedOfferId={ticket?.selectedOfferId}
            />
          ))}
          {typing && <TypingIndicator />}
          <div ref={endRef} />
        </div>
      </main>

      <footer className="chat-footer">
        <div className="chat-inner chat-inner--footer">
          {error && (
            <div className="error-banner" role="alert">
              <AlertCircle size={16} />
              {error}
            </div>
          )}
          <Composer
            value={draft}
            onChange={setDraft}
            onSubmit={send}
            disabled={!ticket}
            autoFocus
            placeholder={ticket?.status === 'operator' ? 'Сообщение менеджеру…' : 'Опишите задачу или назовите продукт…'}
          />
          <p className="chat-disclaimer">ИИ может ошибаться — сверяйтесь с карточкой продукта. Менеджер подключится по запросу.</p>
        </div>
      </footer>

      {quote && <QuoteModal quote={quote} onClose={() => setOpenQuote(null)} />}
    </div>
  );
}
