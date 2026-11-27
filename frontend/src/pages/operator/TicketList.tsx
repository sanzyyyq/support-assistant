import { Search } from 'lucide-react';
import type { Ticket } from '../../api/types';
import { StatusBadge } from '../../components/ui';
import { CHANNEL_LABEL, formatAgo, initials } from '../../lib/format';

export type Filter = 'all' | 'waiting' | 'ai' | 'mine' | 'closed';

export const FILTERS: Array<{ id: Filter; label: string }> = [
  { id: 'all', label: 'Все' },
  { id: 'waiting', label: 'Ждут' },
  { id: 'ai', label: 'ИИ' },
  { id: 'mine', label: 'Мои' },
  { id: 'closed', label: 'Архив' },
];

interface Props {
  tickets: Ticket[];
  counts: Record<Filter, number>;
  filter: Filter;
  onFilter: (f: Filter) => void;
  query: string;
  onQuery: (q: string) => void;
  selectedId: string | null;
  onSelect: (id: string) => void;
  loading: boolean;
}

export function TicketList({ tickets, counts, filter, onFilter, query, onQuery, selectedId, onSelect, loading }: Props) {
  return (
    <section className="tickets" aria-label="Обращения">
      <div className="tickets__head">
        <div className="tickets__title-row">
          <h1 className="tickets__title">Обращения</h1>
          {counts.waiting > 0 && <span className="tickets__alert">{counts.waiting} ждут</span>}
        </div>
        <label className="search">
          <Search size={16} aria-hidden />
          <span className="sr-only">Поиск</span>
          <input value={query} onChange={(e) => onQuery(e.target.value)} placeholder="Поиск по имени, номеру, тексту" />
        </label>
        <div className="tabs" role="tablist">
          {FILTERS.map((f) => (
            <button
              key={f.id}
              role="tab"
              aria-selected={filter === f.id}
              className={`tabs__tab${filter === f.id ? ' is-active' : ''}`}
              onClick={() => onFilter(f.id)}
            >
              {f.label}
              <span className="tabs__count">{counts[f.id]}</span>
            </button>
          ))}
        </div>
      </div>

      <div className="tickets__list">
        {loading && !tickets.length && <div className="tickets__empty">Загружаем обращения…</div>}
        {!loading && !tickets.length && (
          <div className="tickets__empty">{query ? 'По запросу ничего не нашлось' : 'Здесь пока пусто'}</div>
        )}
        {tickets.map((t) => (
          <button
            key={t.id}
            className={`ticket-row${t.id === selectedId ? ' is-selected' : ''}${t.status === 'waiting' ? ' is-waiting' : ''}`}
            onClick={() => onSelect(t.id)}
            aria-current={t.id === selectedId}
          >
            <span className="ticket-row__avatar">{initials(t.customer.name)}</span>
            <span className="ticket-row__body">
              <span className="ticket-row__top">
                <span className="ticket-row__name">{t.customer.name}</span>
                <span className="ticket-row__time">{formatAgo(t.updatedAt)}</span>
              </span>
              <span className="ticket-row__preview">{t.lastMessage ?? '—'}</span>
              <span className="ticket-row__meta">
                <StatusBadge status={t.status} />
                <span>
                  {CHANNEL_LABEL[t.channel]} · #{t.number}
                </span>
              </span>
            </span>
          </button>
        ))}
      </div>
    </section>
  );
}
