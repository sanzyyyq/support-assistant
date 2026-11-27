import { useEffect, useState } from 'react';
import { Bot, Clock, FileText, Headset, ThumbsUp, Timer } from 'lucide-react';
import { api } from '../../api/client';
import type { Stats } from '../../api/types';
import { formatNumber, formatPrice } from '../../lib/format';

/** Метрики под цель бизнеса: меньше ручной работы, быстрее ответ клиенту */
export function AnalyticsPage() {
  const [stats, setStats] = useState<Stats | null>(null);

  useEffect(() => {
    const load = () => api.getStats().then(setStats).catch(() => undefined);
    void load();
    return api.subscribe((e) => {
      if (e.type === 'ticket_updated' || e.type === 'quote_updated' || e.type === 'message_updated' || e.type === 'reset') void load();
    });
  }, []);

  if (!stats) return <div className="op__empty">Загружаю метрики…</div>;

  const maxIntent = Math.max(...stats.topIntents.map((i) => i.count));
  const cards = [
    { icon: <Bot size={18} />, label: 'Закрыто ИИ без менеджера', value: `${stats.aiResolvedPct}%`, hint: 'цель — 70%+' },
    { icon: <Timer size={18} />, label: 'Сэкономлено времени менеджеров', value: `${formatNumber(stats.hoursSavedPerDay)} ч/день`, hint: `≈ ${Math.round(stats.hoursSavedPerDay / 8)} ставок` },
    { icon: <Clock size={18} />, label: 'Первый ответ', value: `${stats.firstResponseSec.toLocaleString('ru-RU')} с`, hint: 'раньше — около 6 минут' },
    { icon: <Headset size={18} />, label: 'Передано менеджеру', value: `${stats.handoffPct}%`, hint: 'скидки, закупки, сложные случаи' },
    { icon: <FileText size={18} />, label: 'КП сформировано ИИ', value: formatNumber(stats.quotes), hint: `на ${formatPrice(stats.quotesAmount)}` },
    { icon: <ThumbsUp size={18} />, label: 'Полезные ответы', value: `${stats.csatPct}%`, hint: `${formatNumber(stats.ratedCount)} оценок` },
  ];

  return (
    <div className="analytics">
      <header className="analytics__head">
        <h1 className="tickets__title">Аналитика</h1>
        <span className="caption muted">Сегодня · {formatNumber(stats.ticketsToday)} обращений · демо-данные</span>
      </header>

      <div className="metrics">
        {cards.map((c) => (
          <div key={c.label} className="metric">
            <span className="metric__label">
              {c.icon}
              {c.label}
            </span>
            <b className="metric__value">{c.value}</b>
            <span className="metric__hint">{c.hint}</span>
          </div>
        ))}
      </div>

      <section className="panel">
        <h2 className="panel__title">С чем приходят клиенты</h2>
        <ul className="bars">
          {stats.topIntents.map((i) => (
            <li key={i.label}>
              <span className="bars__label">{i.label}</span>
              <span className="bars__track">
                <span className="bars__fill" style={{ width: `${(i.count / maxIntent) * 100}%` }} />
              </span>
              <span className="bars__value">{formatNumber(i.count)}</span>
            </li>
          ))}
        </ul>
      </section>
    </div>
  );
}
