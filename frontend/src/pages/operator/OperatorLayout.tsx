import { useCallback, useEffect, useState } from 'react';
import { NavLink, Outlet } from 'react-router-dom';
import { BarChart3, FileText, Inbox, RotateCcw, Sparkles, Store } from 'lucide-react';
import { api } from '../../api/client';
import { OPERATOR } from '../../config';
import { initials } from '../../lib/format';
import '../../styles/operator.css';

/** Каркас кабинета менеджера: левая навигация + раздел */
export function OperatorLayout() {
  const [waiting, setWaiting] = useState(0);
  const [toReview, setToReview] = useState(0);

  const refresh = useCallback(async () => {
    const [tickets, quotes] = await Promise.all([api.listTickets(), api.listQuotes()]);
    setWaiting(tickets.filter((t) => t.status === 'waiting').length);
    setToReview(quotes.filter((q) => q.status === 'review').length);
  }, []);

  useEffect(() => {
    void refresh();
    return api.subscribe((e) => {
      if (e.type === 'ticket_updated' || e.type === 'quote_updated' || e.type === 'reset') void refresh();
    });
  }, [refresh]);

  const item = (to: string, label: string, icon: JSX.Element, badge?: number, end = false) => (
    <NavLink to={to} end={end} className={({ isActive }) => `rail__item${isActive ? ' is-active' : ''}`} aria-label={label} title={label}>
      {icon}
      {badge ? <span className="rail__badge">{badge}</span> : null}
    </NavLink>
  );

  return (
    <div className="op">
      <nav className="rail" aria-label="Навигация">
        <span className="rail__logo" aria-hidden>
          <Sparkles size={20} />
        </span>
        {item('/operator', 'Обращения', <Inbox size={22} />, waiting, true)}
        {item('/operator/quotes', 'Коммерческие предложения', <FileText size={22} />, toReview)}
        {item('/operator/analytics', 'Аналитика', <BarChart3 size={22} />)}
        <a className="rail__item" href="/" target="_blank" rel="noreferrer" aria-label="Открыть витрину с виджетом" title="Открыть витрину с виджетом (демо)">
          <Store size={22} />
        </a>
        <span className="rail__spacer" />
        <button
          className="rail__item"
          aria-label="Сбросить демо-данные"
          title="Сбросить демо-данные"
          onClick={() => window.confirm('Сбросить все демо-обращения и КП к исходному состоянию?') && void api.resetDemo()}
        >
          <RotateCcw size={20} />
        </button>
        <span className="avatar avatar--success" style={{ width: 40, height: 40 }} title={OPERATOR.name}>
          {initials(OPERATOR.name)}
        </span>
      </nav>
      <Outlet />
    </div>
  );
}
