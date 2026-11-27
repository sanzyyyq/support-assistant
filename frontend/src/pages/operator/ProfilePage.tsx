import { useState, type FormEvent } from 'react';
import { api } from '../../api/client';
import '../../styles/staff.css';
import { useOperator } from './operatorContext';

const MIN_PASSWORD = 8;

/** Профиль вошедшего сотрудника: кто я и смена пароля */
export function ProfilePage() {
  const me = useOperator();
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [repeat, setRepeat] = useState('');
  const [status, setStatus] = useState<{ ok: boolean; text: string } | null>(null);
  const [busy, setBusy] = useState(false);

  const mismatch = repeat.length > 0 && next !== repeat;

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setStatus(null);
    try {
      await api.changePassword(current, next);
      setCurrent('');
      setNext('');
      setRepeat('');
      setStatus({ ok: true, text: 'Пароль изменён. На других устройствах нужно войти заново.' });
    } catch (err) {
      setStatus({ ok: false, text: (err as Error).message });
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="analytics staff">
      <header className="analytics__head">
        <h1 className="tickets__title">Профиль</h1>
      </header>

      <section className="panel staff__profile">
        <h2 className="panel__title">{me.name}</h2>
        <dl>
          <dt>Логин</dt>
          <dd>{me.login}</dd>
          <dt>Роль</dt>
          <dd>{me.role === 'admin' ? 'Администратор' : 'Менеджер'}</dd>
        </dl>
        <p className="caption muted">Имя и роль меняет администратор в разделе «Сотрудники».</p>
      </section>

      <form className="panel staff__profile" onSubmit={submit}>
        <h2 className="panel__title">Сменить пароль</h2>
        <label className="qform__field">
          <span>Текущий пароль</span>
          <input type="password" value={current} onChange={(e) => setCurrent(e.target.value)} autoComplete="current-password" />
        </label>
        <label className="qform__field">
          <span>Новый пароль (от {MIN_PASSWORD} символов)</span>
          <input type="password" value={next} onChange={(e) => setNext(e.target.value)} autoComplete="new-password" />
        </label>
        <label className="qform__field">
          <span>Повторите новый пароль</span>
          <input
            type="password"
            value={repeat}
            onChange={(e) => setRepeat(e.target.value)}
            autoComplete="new-password"
            aria-invalid={mismatch ? 'true' : undefined}
          />
          {mismatch && <em>Пароли не совпадают</em>}
        </label>
        {status && (
          <p className={status.ok ? 'staff__ok' : 'qform__error'} role="status">
            {status.text}
          </p>
        )}
        <button className="btn btn--primary" type="submit" disabled={busy || !current || next.length < MIN_PASSWORD || next !== repeat}>
          Сменить пароль
        </button>
      </form>
    </div>
  );
}
