import { ArrowLeftRight, BookOpen, Box, Sparkles, ThumbsDown, ThumbsUp } from 'lucide-react';
import type { Message, Offer, Quote, QuoteRequest } from '../api/types';
import { formatTime, initials } from '../lib/format';
import { OfferCard } from './OfferCard';
import { ProductCard } from './ProductCard';
import { QuoteCard, QuoteRequestForm } from './QuoteBlocks';
import { Avatar } from './ui';

/** Интерактивные действия клиента. В режиме менеджера не передаются — всё только для чтения. */
export interface ChatActions {
  onQuickReply: (text: string) => void;
  onSubmitQuote: (messageId: string, data: QuoteRequest) => Promise<void>;
  onRate: (messageId: string, value: 'up' | 'down' | null) => void;
}

interface Props {
  message: Message;
  showQuickReplies?: boolean;
  actions?: ChatActions;
  quotes?: Record<string, Quote>;
  onOpenQuote?: (q: Quote) => void;
  selectedOfferId?: string;
}

export function MessageItem({ message, showQuickReplies, actions, quotes, onOpenQuote, selectedOfferId }: Props) {
  const time = <time dateTime={message.createdAt}>{formatTime(message.createdAt)}</time>;

  if (message.sender === 'system') {
    return (
      <div className="msg msg--system" role="status">
        <span className="msg__pill">
          <ArrowLeftRight size={14} aria-hidden />
          {message.text} · {formatTime(message.createdAt)}
        </span>
      </div>
    );
  }

  if (message.sender === 'user') {
    return (
      <div className="msg msg--user">
        <div className="msg__bubble">{message.text}</div>
        <span className="msg__time">{time}</span>
      </div>
    );
  }

  const isBot = message.sender === 'bot';
  // Без дублей; база знаний — первой, она обычно важнее карточек
  const sources = [...new Map((message.sources ?? []).map((s) => [s.title, s])).values()].sort(
    (a, b) => (a.kind === 'kb' ? 0 : 1) - (b.kind === 'kb' ? 0 : 1),
  );
  const author = message.authorName ?? 'Менеджер';
  const choose = actions
    ? (o: Offer) => actions.onQuickReply(`Выбираю ${o.title.replace(/ · .*/, '')}`)
    : undefined;

  return (
    <div className={`msg msg--${message.sender}`}>
      <Avatar tone={isBot ? 'brand' : 'success'}>{isBot ? <Sparkles size={16} aria-hidden /> : initials(author)}</Avatar>
      <div className="msg__col">
        <div className="msg__author">
          {isBot ? 'ИИ-консультант' : `${author} · менеджер`}
          {time}
        </div>
        <div className="msg__bubble">{message.text}</div>

        {message.offers?.length ? (
          <div className="offers" role="list" aria-label="Варианты решения">
            {message.offers.map((o) => (
              <OfferCard key={o.id} offer={o} selected={o.id === selectedOfferId} onChoose={choose} />
            ))}
          </div>
        ) : null}

        {message.products?.length ? (
          <div className="carousel" role="list" aria-label="Продукты">
            {message.products.map((p) => (
              <ProductCard key={p.id} product={p} />
            ))}
          </div>
        ) : null}

        {message.compare && (
          <div className="compare">
            <table>
              <thead>
                <tr>
                  <th />
                  {message.compare.columns.map((c) => (
                    <th key={c}>{c}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {message.compare.rows.map((r) => (
                  <tr key={r.label}>
                    <th scope="row">{r.label}</th>
                    {r.values.map((v, i) => (
                      <td key={i} className={v === '✓' ? 'is-yes' : v === '—' ? 'is-no' : ''}>
                        {v}
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}

        {message.form === 'quote_request' && (
          <QuoteRequestForm done={message.formDone} onSubmit={actions && ((d) => actions.onSubmitQuote(message.id, d))} />
        )}

        {message.quoteId && onOpenQuote && <QuoteCard quote={quotes?.[message.quoteId]} onOpen={onOpenQuote} />}

        {sources.length ? (
          <div className="sources">
            <span>Источники:</span>
            {sources.slice(0, 3).map((s) =>
              s.url ? (
                <a key={s.title} href={s.url} target="_blank" rel="noreferrer" className="source">
                  <Box size={12} aria-hidden />
                  {s.title}
                </a>
              ) : (
                <span key={s.title} className="source">
                  <BookOpen size={12} aria-hidden />
                  {s.title}
                </span>
              ),
            )}
            {sources.length > 3 && (
              <span className="source" title={sources.slice(3).map((s) => s.title).join('\n')}>
                ещё {sources.length - 3}
              </span>
            )}
          </div>
        ) : null}

        {isBot && (actions || message.feedback) && message.intent !== 'greeting' && (
          <div className="feedback">
            {actions ? (
              <>
                <button
                  className={message.feedback === 'up' ? 'is-active' : ''}
                  aria-label="Полезный ответ"
                  aria-pressed={message.feedback === 'up'}
                  onClick={() => actions.onRate(message.id, message.feedback === 'up' ? null : 'up')}
                >
                  <ThumbsUp size={14} />
                </button>
                <button
                  className={message.feedback === 'down' ? 'is-active is-down' : ''}
                  aria-label="Бесполезный ответ"
                  aria-pressed={message.feedback === 'down'}
                  onClick={() => actions.onRate(message.id, message.feedback === 'down' ? null : 'down')}
                >
                  <ThumbsDown size={14} />
                </button>
              </>
            ) : (
              <span className={`feedback__mark${message.feedback === 'down' ? ' is-down' : ''}`}>
                {message.feedback === 'up' ? <ThumbsUp size={12} /> : <ThumbsDown size={12} />}
                Клиент оценил {message.feedback === 'up' ? 'как полезный' : 'как бесполезный'}
              </span>
            )}
          </div>
        )}

        {showQuickReplies && actions && message.quickReplies?.length ? (
          <div className="quick">
            {message.quickReplies.map((q) => (
              <button key={q} type="button" className="chip" onClick={() => actions.onQuickReply(q)}>
                {q}
              </button>
            ))}
          </div>
        ) : null}
      </div>
    </div>
  );
}

export function TypingIndicator() {
  return (
    <div className="msg msg--bot" aria-live="polite">
      <Avatar tone="brand">
        <Sparkles size={16} aria-hidden />
      </Avatar>
      <div className="msg__col">
        <div className="msg__author">ИИ-консультант ищет по каталогу…</div>
        <div className="typing" aria-hidden>
          <i />
          <i />
          <i />
        </div>
      </div>
    </div>
  );
}
