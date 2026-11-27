import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { Download, FileText, MessageSquare, Save, Send } from 'lucide-react';
import { api } from '../../api/client';
import type { Quote } from '../../api/types';
import { printQuote } from '../../components/QuoteBlocks';
import { QuoteDocument } from '../../components/QuoteDocument';
import { Button } from '../../components/ui';
import { OPERATOR, VAT_RATE } from '../../config';
import { QUOTE_STATUS_LABEL, quoteTotals } from '../../lib/domain';
import { formatAgo, formatPrice } from '../../lib/format';

/** Очередь КП: проверка черновиков, сформированных ИИ, и отправка клиенту */
export function QuotesPage() {
  const [params, setParams] = useSearchParams();
  const [quotes, setQuotes] = useState<Quote[]>([]);
  const [draft, setDraft] = useState<Quote | null>(null);
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState(false);
  const selectedId = params.get('id');

  const load = useCallback(async () => {
    const list = await api.listQuotes();
    setQuotes(list);
    if (!params.get('id') && list.length) {
      const first = list.find((q) => q.status === 'review') ?? list[0];
      setParams({ id: first.id }, { replace: true });
    }
  }, [params, setParams]);

  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(
    () =>
      api.subscribe((e) => {
        if (e.type === 'quote_updated') setQuotes((qs) => (qs.some((q) => q.id === e.quote.id) ? qs.map((q) => (q.id === e.quote.id ? e.quote : q)) : [e.quote, ...qs]));
        if (e.type === 'reset') void load();
      }),
    [load],
  );

  const selected = quotes.find((q) => q.id === selectedId) ?? null;
  useEffect(() => {
    setDraft(selected ? structuredClone(selected) : null);
    setSaved(false);
    // Пересоздаём черновик только при смене КП или его статуса
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selected?.id, selected?.status]);

  const dirty = useMemo(() => !!draft && !!selected && JSON.stringify(draft) !== JSON.stringify(selected), [draft, selected]);
  const editable = draft?.status === 'review';

  const patch = (p: Partial<Quote>) => {
    setDraft((d) => (d ? { ...d, ...p } : d));
    setSaved(false);
  };
  const patchItem = (idx: number, p: Partial<Quote['items'][number]>) =>
    setDraft((d) => (d ? { ...d, items: d.items.map((it, i) => (i === idx ? { ...it, ...p } : it)) } : d));

  const save = async () => {
    if (!draft) return;
    setBusy(true);
    const { items, paymentTerms, deliveryTerm, validUntil, comment, company, inn, email } = draft;
    await api.updateQuote(draft.id, { items, paymentTerms, deliveryTerm, validUntil, comment, company, inn, email });
    setBusy(false);
    setSaved(true);
  };
  const approve = async () => {
    if (!draft) return;
    setBusy(true);
    if (dirty) await save();
    await api.approveQuote(draft.id, OPERATOR.name);
    setBusy(false);
  };

  return (
    <div className="quotes">
      <section className="quotes__list" aria-label="Коммерческие предложения">
        <div className="tickets__head">
          <h1 className="tickets__title">Коммерческие предложения</h1>
          <p className="caption muted">ИИ собирает черновик по шаблону, менеджер проверяет и отправляет клиенту.</p>
        </div>
        <div className="tickets__list">
          {!quotes.length && <div className="tickets__empty">КП пока нет</div>}
          {quotes.map((q) => (
            <button key={q.id} className={`quote-row${q.id === selectedId ? ' is-selected' : ''}`} onClick={() => setParams({ id: q.id })}>
              <span className="quote-row__icon">
                <FileText size={18} />
              </span>
              <span className="quote-row__body">
                <span className="quote-row__top">
                  <b>{q.number}</b>
                  <span className="ticket-row__time">{formatAgo(q.createdAt)}</span>
                </span>
                <span className="quote-row__company">{q.company}</span>
                <span className="quote-row__meta">
                  <span className={`tag ${q.status === 'sent' ? 'tag--success' : 'tag--warning'}`}>{QUOTE_STATUS_LABEL[q.status]}</span>
                  <b>{formatPrice(quoteTotals(q).sum)}</b>
                </span>
              </span>
            </button>
          ))}
        </div>
      </section>

      {draft ? (
        <section className="quotes__detail">
          <header className="conv__header">
            <div className="conv__who">
              <div className="conv__name-row">
                <h2 className="conv__name">{draft.number}</h2>
                <span className={`tag ${draft.status === 'sent' ? 'tag--success' : 'tag--warning'}`}>{QUOTE_STATUS_LABEL[draft.status]}</span>
              </div>
              <div className="caption muted">
                {draft.company} · {draft.email}
                {draft.reviewer ? ` · согласовал(а) ${draft.reviewer}` : ''}
              </div>
            </div>
            <div className="conv__actions">
              <Link className="btn btn--ghost btn--md" to={`/operator?ticket=${draft.ticketId}`}>
                <MessageSquare size={16} /> Диалог
              </Link>
              <Button icon={<Download size={16} />} onClick={printQuote}>
                PDF
              </Button>
              {editable && (
                <>
                  <Button icon={<Save size={16} />} onClick={save} disabled={!dirty || busy}>
                    {saved && !dirty ? 'Сохранено' : 'Сохранить'}
                  </Button>
                  <Button variant="primary" icon={<Send size={16} />} onClick={approve} disabled={busy}>
                    Согласовать и отправить
                  </Button>
                </>
              )}
            </div>
          </header>

          <div className="quotes__body">
            {editable && (
              <div className="qedit">
                <h3 className="cpanel__label">Проверка черновика</h3>
                <div className="qedit__grid">
                  <label className="qform__field">
                    <span>Заказчик</span>
                    <input value={draft.company} onChange={(e) => patch({ company: e.target.value })} />
                  </label>
                  <label className="qform__field">
                    <span>ИНН</span>
                    <input value={draft.inn ?? ''} onChange={(e) => patch({ inn: e.target.value || undefined })} />
                  </label>
                  <label className="qform__field">
                    <span>Оплата</span>
                    <select value={draft.paymentTerms} onChange={(e) => patch({ paymentTerms: e.target.value as Quote['paymentTerms'] })}>
                      <option>Предоплата</option>
                      <option>Постоплата</option>
                    </select>
                  </label>
                  <label className="qform__field">
                    <span>Действует до</span>
                    <input
                      type="date"
                      value={draft.validUntil.slice(0, 10)}
                      onChange={(e) => e.target.value && patch({ validUntil: new Date(e.target.value).toISOString() })}
                    />
                  </label>
                  <label className="qform__field qedit__wide">
                    <span>Срок поставки</span>
                    <input value={draft.deliveryTerm} onChange={(e) => patch({ deliveryTerm: e.target.value })} />
                  </label>
                </div>
                <table className="qedit__items">
                  <thead>
                    <tr>
                      <th>Позиция</th>
                      <th>Кол-во</th>
                      <th>Цена, ₽</th>
                      <th>НДС</th>
                    </tr>
                  </thead>
                  <tbody>
                    {draft.items.map((it, i) => (
                      <tr key={it.sku}>
                        <td>
                          <span className="caption muted">{it.sku}</span>
                          <div>{it.name}</div>
                        </td>
                        <td>
                          <input type="number" min={1} value={it.qty} onChange={(e) => patchItem(i, { qty: Math.max(1, Number(e.target.value) || 1) })} />
                        </td>
                        <td>
                          <input type="number" min={0} value={it.price} onChange={(e) => patchItem(i, { price: Math.max(0, Number(e.target.value) || 0) })} />
                        </td>
                        <td>
                          <select
                            className={it.vatRate === null ? 'is-todo' : ''}
                            value={it.vatRate === null ? 'todo' : String(it.vatRate)}
                            onChange={(e) => patchItem(i, { vatRate: e.target.value === 'todo' ? null : Number(e.target.value) })}
                            aria-label="Ставка НДС"
                          >
                            <option value="todo">уточняется</option>
                            <option value="0">Без НДС (реестр)</option>
                            <option value={String(VAT_RATE)}>{VAT_RATE}%</option>
                          </select>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
                <div className="qedit__total">
                  Итого: <b>{formatPrice(quoteTotals(draft).sum)}</b>
                </div>
              </div>
            )}
            <QuoteDocument quote={draft} />
          </div>
        </section>
      ) : (
        <div className="op__empty">
          <FileText size={40} strokeWidth={1.5} />
          <p>Выберите КП слева</p>
        </div>
      )}
    </div>
  );
}
