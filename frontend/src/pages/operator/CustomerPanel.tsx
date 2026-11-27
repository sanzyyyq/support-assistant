import { Check, CircleDashed, ClipboardList, FileText, FlaskConical, Send, Sparkles, Star } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import type { Quote, Ticket } from '../../api/types';
import { Button } from '../../components/ui';
import { QUOTE_STATUS_LABEL, quoteTotals, requirementRows } from '../../lib/domain';
import { formatPrice, initials } from '../../lib/format';

interface Props {
  ticket: Ticket;
  quote?: Quote;
  /** Вставить шаблон ответа в поле ввода (подключит менеджера, если нужно) */
  onQuickAction: (text: string) => void;
  onApproveQuote: (q: Quote) => void;
  busy: boolean;
}

const SCENARIO = { known: 'Знает продукт', discover: 'Подбор решения', question: 'Вопрос о продукте' } as const;

export function CustomerPanel({ ticket, quote, onQuickAction, onApproveQuote, busy }: Props) {
  const navigate = useNavigate();
  const { customer } = ticket;
  const rows = requirementRows(ticket.requirements, ticket.scenario);
  const offers = ticket.lastOffers ?? [];

  const actions = [
    {
      label: 'Запросить реквизиты',
      icon: <ClipboardList size={16} />,
      text: 'Чтобы подготовить коммерческое предложение, пришлите, пожалуйста, название компании, ИНН и email для документов.',
    },
    {
      label: 'Предложить тест на 30 дней',
      icon: <FlaskConical size={16} />,
      text: 'Могу выдать тестовый ключ на 30 дней — сможете проверить продукт на своей инфраструктуре до покупки. Оформить?',
    },
  ];

  return (
    <aside className="cpanel" aria-label="Карточка клиента">
      <div className="cpanel__customer">
        <span className="avatar avatar--brand-soft" style={{ width: 48, height: 48, fontSize: 16 }}>
          {initials(customer.name)}
        </span>
        <div className="cpanel__who">
          <div className="cpanel__name">{customer.name}</div>
          {customer.company && <div className="caption">{customer.company}</div>}
          <div className="caption muted cpanel__contact">
            {[customer.email, customer.inn && `ИНН ${customer.inn}`].filter(Boolean).join(' · ') || 'Контакты не указаны'}
          </div>
        </div>
      </div>

      <section className="cpanel__section">
        <h3 className="cpanel__label cpanel__label--brand">
          <Sparkles size={16} aria-hidden /> ИИ-резюме
          {ticket.scenario && <span className="cpanel__scenario">{SCENARIO[ticket.scenario]}</span>}
        </h3>
        <div className="summary">
          <p>{ticket.aiSummary ?? 'Резюме появится, когда ИИ передаст диалог менеджеру.'}</p>
          {ticket.tags.length > 0 && (
            <div className="summary__tags">
              {ticket.tags.map((t) => (
                <span key={t} className={`tag${t === 'Негатив' ? ' tag--danger' : ''}`}>
                  {t}
                </span>
              ))}
            </div>
          )}
        </div>
      </section>

      {ticket.scenario !== 'question' && (
        <section className="cpanel__section">
          <h3 className="cpanel__label">Требования клиента</h3>
          <dl className="reqlist">
            {rows.map((r) => (
              <div key={r.slot} className={r.value ? 'is-done' : ''}>
                <dt>
                  {r.value ? <Check size={14} aria-hidden /> : <CircleDashed size={14} aria-hidden />}
                  {r.label}
                </dt>
                <dd>{r.value ?? (ticket.pendingSlot === r.slot ? 'ИИ уточняет…' : '—')}</dd>
              </div>
            ))}
          </dl>
        </section>
      )}

      {offers.length > 0 && (
        <section className="cpanel__section">
          <h3 className="cpanel__label">Предложено ИИ</h3>
          <ul className="minioffers">
            {offers.map((o) => (
              <li key={o.id} className={o.id === ticket.selectedOfferId ? 'is-selected' : ''}>
                <div>
                  <b>
                    {o.title.replace(/ · .*/, '')}
                    {o.recommended && <Star size={12} fill="currentColor" aria-label="Рекомендован" />}
                  </b>
                  <span>{o.items.map((i) => i.product.name).join(' + ')}</span>
                </div>
                <div className="minioffers__side">
                  <b>{formatPrice(o.total)}</b>
                  {o.id === ticket.selectedOfferId && <span className="tag tag--success">Выбран</span>}
                </div>
              </li>
            ))}
          </ul>
        </section>
      )}

      <section className="cpanel__section">
        <h3 className="cpanel__label">
          <FileText size={16} aria-hidden /> Коммерческое предложение
        </h3>
        {quote ? (
          <div className="cquote">
            <div className="cquote__head">
              <b>{quote.number}</b>
              <span className={`tag ${quote.status === 'sent' ? 'tag--success' : 'tag--warning'}`}>{QUOTE_STATUS_LABEL[quote.status]}</span>
            </div>
            <div className="cquote__sum">{formatPrice(quoteTotals(quote).sum)}</div>
            <div className="cquote__actions">
              <Button size="sm" onClick={() => navigate(`/operator/quotes?id=${quote.id}`)}>
                Открыть и проверить
              </Button>
              {quote.status === 'review' && (
                <Button size="sm" variant="primary" icon={<Send size={14} />} disabled={busy} onClick={() => onApproveQuote(quote)}>
                  Согласовать
                </Button>
              )}
            </div>
          </div>
        ) : (
          <p className="caption muted cpanel__hint">
            {offers.length ? 'Решение подобрано — ИИ предложит клиенту сформировать КП.' : 'КП появится, когда клиент выберет решение.'}
          </p>
        )}
      </section>

      <section className="cpanel__section">
        <h3 className="cpanel__label">Быстрые действия</h3>
        <div className="cpanel__actions">
          {actions.map((a) => (
            <Button key={a.label} block icon={a.icon} onClick={() => onQuickAction(a.text)}>
              {a.label}
            </Button>
          ))}
        </div>
        <p className="caption muted cpanel__hint">Текст подставится в поле ответа — его можно отредактировать.</p>
      </section>
    </aside>
  );
}
