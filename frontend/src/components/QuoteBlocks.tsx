import { useState, type FormEvent } from 'react';
import { CheckCircle2, Clock, Download, FileText } from 'lucide-react';
import type { Quote, QuoteRequest } from '../api/types';
import { QUOTE_STATUS_LABEL, quoteTotals } from '../lib/domain';
import { formatDate, formatPrice } from '../lib/format';
import { Modal } from './Modal';
import { QuoteDocument } from './QuoteDocument';
import { Button } from './ui';

/** Форма данных компании прямо в чате */
export function QuoteRequestForm({ done, onSubmit }: { done?: boolean; onSubmit?: (data: QuoteRequest) => Promise<void> }) {
  const [company, setCompany] = useState('');
  const [inn, setInn] = useState('');
  const [email, setEmail] = useState('');
  const [paymentTerms, setPaymentTerms] = useState<QuoteRequest['paymentTerms']>('Предоплата');
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);

  if (done || !onSubmit) {
    return (
      <div className="qform qform--done">
        <CheckCircle2 size={16} aria-hidden /> Данные компании отправлены
      </div>
    );
  }

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const err: Record<string, string> = {};
    if (!company.trim()) err.company = 'Укажите название компании';
    if (inn && !/^\d{10}(\d{2})?$/.test(inn.trim())) err.inn = 'ИНН — 10 или 12 цифр';
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim())) err.email = 'Проверьте email';
    setErrors(err);
    if (Object.keys(err).length) return;
    setBusy(true);
    try {
      await onSubmit({ company: company.trim(), inn: inn.trim() || undefined, email: email.trim(), paymentTerms });
    } catch {
      setErrors({ form: 'Не получилось сформировать КП. Попробуйте ещё раз.' });
      setBusy(false);
    }
  };

  const field = (id: string, label: string, value: string, set: (v: string) => void, props: Record<string, string> = {}) => (
    <label className="qform__field">
      <span>{label}</span>
      <input
        value={value}
        onChange={(e) => {
          set(e.target.value);
          if (errors[id]) setErrors((x) => ({ ...x, [id]: '' }));
        }}
        aria-invalid={!!errors[id]}
        {...props}
      />
      {errors[id] && <em>{errors[id]}</em>}
    </label>
  );

  return (
    <form className="qform" onSubmit={submit} noValidate>
      {field('company', 'Компания *', company, setCompany, { placeholder: 'ООО «Ромашка»', autoComplete: 'organization' })}
      <div className="qform__row">
        {field('inn', 'ИНН', inn, setInn, { placeholder: '7701234567', inputMode: 'numeric' })}
        {field('email', 'Email для КП *', email, setEmail, { placeholder: 'buh@company.ru', type: 'email', autoComplete: 'email' })}
      </div>
      <fieldset className="qform__pay">
        <legend>Оплата</legend>
        {(['Предоплата', 'Постоплата'] as const).map((p) => (
          <label key={p} className={paymentTerms === p ? 'is-active' : ''}>
            <input type="radio" name="pay" checked={paymentTerms === p} onChange={() => setPaymentTerms(p)} />
            {p}
          </label>
        ))}
      </fieldset>
      {errors.form && <p className="qform__error">{errors.form}</p>}
      <Button type="submit" variant="primary" block disabled={busy} icon={<FileText size={16} />}>
        {busy ? 'Формирую КП…' : 'Сформировать КП'}
      </Button>
    </form>
  );
}

export function printQuote() {
  document.body.classList.add('printing-quote');
  window.print();
  window.setTimeout(() => document.body.classList.remove('printing-quote'), 500);
}

/** Карточка КП в ленте чата */
export function QuoteCard({ quote, onOpen }: { quote?: Quote; onOpen: (q: Quote) => void }) {
  if (!quote) return <div className="qcard qcard--loading">Загружаю КП…</div>;
  const t = quoteTotals(quote);
  const sent = quote.status === 'sent';
  return (
    <article className="qcard">
      <span className="qcard__icon" aria-hidden>
        <FileText size={22} />
      </span>
      <div className="qcard__body">
        <div className="qcard__title">Коммерческое предложение {quote.number}</div>
        <div className="qcard__meta">
          {quote.company} · {quote.items.length} поз. · до {formatDate(quote.validUntil)}
        </div>
        <div className={`qcard__status${sent ? ' is-sent' : ''}`}>
          {sent ? <CheckCircle2 size={14} aria-hidden /> : <Clock size={14} aria-hidden />}
          {sent ? `Отправлено на ${quote.email}` : QUOTE_STATUS_LABEL[quote.status]}
        </div>
      </div>
      <div className="qcard__side">
        <b>{formatPrice(t.sum)}</b>
        <Button size="sm" onClick={() => onOpen(quote)}>
          Открыть
        </Button>
      </div>
    </article>
  );
}

export function QuoteModal({ quote, onClose }: { quote: Quote; onClose: () => void }) {
  return (
    <Modal
      title={`${quote.number} · ${QUOTE_STATUS_LABEL[quote.status]}`}
      onClose={onClose}
      wide
      actions={
        <Button size="sm" icon={<Download size={16} />} onClick={printQuote}>
          Скачать PDF
        </Button>
      }
    >
      <QuoteDocument quote={quote} />
    </Modal>
  );
}
