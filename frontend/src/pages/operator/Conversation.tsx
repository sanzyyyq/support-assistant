import { useEffect, useRef, useState } from 'react';
import { Bot, Check, Headset, Lock, RefreshCw, Sparkles, X } from 'lucide-react';
import { api } from '../../api/client';
import type { Message, Quote, Ticket } from '../../api/types';
import { Composer } from '../../components/Composer';
import { MessageItem, TypingIndicator } from '../../components/MessageItem';
import { Button, StatusBadge } from '../../components/ui';
import { OPERATOR } from '../../config';
import { CHANNEL_LABEL, formatAgo, formatTime, initials } from '../../lib/format';

interface Props {
  ticket: Ticket;
  messages: Message[];
  botTyping: boolean;
  draft: string;
  onDraft: (v: string) => void;
  onJoin: () => void;
  onRelease: () => void;
  onClose: () => void;
  onSend: (text: string) => void;
  busy: boolean;
  quotes: Record<string, Quote>;
  onOpenQuote: (q: Quote) => void;
}

export function Conversation({ ticket, messages, botTyping, draft, onDraft, onJoin, onRelease, onClose, onSend, busy, quotes, onOpenQuote }: Props) {
  const endRef = useRef<HTMLDivElement>(null);
  const mine = ticket.status === 'operator' && ticket.assignee === OPERATOR.name;
  const lastUserId = [...messages].reverse().find((m) => m.sender === 'user')?.id;

  const [suggestion, setSuggestion] = useState<string | null>(null);
  const [suggestLoading, setSuggestLoading] = useState(false);

  const loadSuggestion = () => {
    setSuggestLoading(true);
    api
      .getSuggestion(ticket.id, OPERATOR.name)
      .then(setSuggestion)
      .catch(() => setSuggestion(null))
      .finally(() => setSuggestLoading(false));
  };

  // Новая подсказка — при подключении и после каждого сообщения клиента
  useEffect(() => {
    setSuggestion(null);
    if (!mine) return;
    let cancelled = false;
    setSuggestLoading(true);
    api
      .getSuggestion(ticket.id, OPERATOR.name)
      .then((s) => !cancelled && setSuggestion(s))
      .catch(() => !cancelled && setSuggestion(null))
      .finally(() => !cancelled && setSuggestLoading(false));
    return () => {
      cancelled = true;
    };
  }, [ticket.id, mine, lastUserId]);

  useEffect(() => {
    endRef.current?.scrollIntoView({ block: 'end' });
  }, [ticket.id, messages.length, botTyping]);

  const insertSuggestion = () => {
    if (!suggestion) return;
    onDraft(suggestion);
    setSuggestion(null);
  };

  return (
    <section className="conv" aria-label={`Диалог с ${ticket.customer.name}`}>
      <header className="conv__header">
        <span className="avatar avatar--neutral conv__avatar">{initials(ticket.customer.name)}</span>
        <div className="conv__who">
          <div className="conv__name-row">
            <h2 className="conv__name">{ticket.customer.name}</h2>
            <StatusBadge status={ticket.status} />
          </div>
          <div className="caption muted">
            {CHANNEL_LABEL[ticket.channel]} · #{ticket.number} · с {formatTime(ticket.createdAt)}
            {ticket.status === 'operator' && ticket.assignee && !mine ? ` · ведёт ${ticket.assignee}` : ''}
          </div>
        </div>
        <div className="conv__actions">
          {mine && (
            <>
              <Button icon={<Bot size={16} />} onClick={onRelease} disabled={busy}>
                Вернуть ИИ
              </Button>
              <Button variant="soft" icon={<Check size={16} />} onClick={onClose} disabled={busy}>
                Закрыть обращение
              </Button>
            </>
          )}
          {ticket.status === 'operator' && !mine && (
            <Button icon={<Headset size={16} />} onClick={onJoin} disabled={busy}>
              Перехватить
            </Button>
          )}
        </div>
      </header>

      {ticket.status === 'waiting' && (
        <div className="handoff handoff--waiting" role="alert">
          <span className="handoff__icon">
            <Headset size={18} />
          </span>
          <div className="handoff__text">
            <strong>ИИ запросил помощь менеджера</strong>
            <span>
              {ticket.handoffReason ?? 'Нужна помощь человека'}
              {ticket.aiConfidence !== undefined && ` · уверенность ИИ ${Math.round(ticket.aiConfidence * 100)}%`}
              {ticket.handoffAt && ` · ждёт ${formatAgo(ticket.handoffAt)}`}
            </span>
          </div>
          <Button variant="primary" icon={<Headset size={16} />} onClick={onJoin} disabled={busy}>
            Подключиться к чату
          </Button>
        </div>
      )}
      {ticket.status === 'ai' && (
        <div className="handoff handoff--ai">
          <span className="handoff__icon">
            <Sparkles size={18} />
          </span>
          <div className="handoff__text">
            <strong>Диалог ведёт ИИ-консультант</strong>
            <span>Можно подключиться в любой момент — ИИ перейдёт в режим подсказок.</span>
          </div>
          <Button icon={<Headset size={16} />} onClick={onJoin} disabled={busy}>
            Подключиться
          </Button>
        </div>
      )}

      <div className="conv__messages">
        {messages.map((m) => (
          <MessageItem key={m.id} message={m} quotes={quotes} onOpenQuote={onOpenQuote} selectedOfferId={ticket.selectedOfferId} />
        ))}
        {botTyping && <TypingIndicator />}
        <div ref={endRef} />
      </div>

      <footer className="conv__footer">
        {mine ? (
          <>
            {(suggestion || suggestLoading) && (
              <div className="suggest">
                <div className="suggest__head">
                  <Sparkles size={16} aria-hidden />
                  <span className="suggest__title">Подсказка ИИ</span>
                  {suggestion && <span className="caption muted">Tab — вставить</span>}
                  <button className="suggest__icon" aria-label="Другая подсказка" title="Другая подсказка" onClick={loadSuggestion}>
                    <RefreshCw size={14} />
                  </button>
                  <button className="suggest__icon" aria-label="Скрыть подсказку" title="Скрыть" onClick={() => setSuggestion(null)}>
                    <X size={14} />
                  </button>
                </div>
                {suggestLoading && !suggestion ? (
                  <div className="suggest__text muted">Готовлю вариант ответа…</div>
                ) : (
                  <button className="suggest__text" onClick={insertSuggestion}>
                    {suggestion}
                  </button>
                )}
              </div>
            )}
            <Composer
              value={draft}
              onChange={onDraft}
              onSubmit={onSend}
              placeholder="Ответить клиенту…"
              autoFocus
              onKeyDown={(e) => {
                if (e.key === 'Tab' && !draft && suggestion) {
                  e.preventDefault();
                  insertSuggestion();
                }
              }}
            />
          </>
        ) : (
          <div className="conv__locked">
            <Lock size={16} aria-hidden />
            {ticket.status === 'closed'
              ? 'Обращение закрыто. Если клиент напишет снова, диалог возобновится.'
              : ticket.status === 'operator'
                ? `Диалог ведёт ${ticket.assignee}. Перехватите его, чтобы ответить.`
                : 'Подключитесь к чату, чтобы ответить клиенту. Пока диалог ведёт ИИ-консультант.'}
          </div>
        )}
      </footer>
    </section>
  );
}
