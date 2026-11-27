import { useEffect, useState, type FormEvent, type ReactNode } from 'react';
import { Link } from 'react-router-dom';
import { LockKeyhole, MessageCircle, Sparkles } from 'lucide-react';
import { api } from '../../api/client';
import { AUTH_REQUIRED } from '../../api/http';
import type { Operator } from '../../api/types';
import { BRAND } from '../../config';
import '../../styles/login.css';
import { OperatorContext } from './operatorContext';

/** Кабинет менеджера открывается только после входа; иначе — форма входа на том же адресе */
export function RequireOperator({ children }: { children: ReactNode }) {
  const [operator, setOperator] = useState<Operator | null | undefined>(undefined);

  useEffect(() => {
    api.getOperator().then(setOperator, () => setOperator(null));
    // Сессия истекла во время работы — API ответил 401
    const onExpired = () => setOperator(null);
    window.addEventListener(AUTH_REQUIRED, onExpired);
    return () => window.removeEventListener(AUTH_REQUIRED, onExpired);
  }, []);

  if (operator === undefined) return <div className="login" aria-busy="true" />;
  if (!operator) return <LoginForm onLogin={setOperator} />;
  return <OperatorContext.Provider value={{ operator, setOperator }}>{children}</OperatorContext.Provider>;
}

function LoginForm({ onLogin }: { onLogin: (op: Operator) => void }) {
  const [login, setLogin] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      onLogin(await api.login(login.trim(), password));
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось войти');
      setBusy(false);
    }
  };

  return (
    <div className="login">
      <form className="login__card" onSubmit={submit}>
        <span className="login__logo" aria-hidden>
          <Sparkles size={22} />
        </span>
        <h1 className="login__title">Кабинет менеджера</h1>
        <p className="login__hint">
          {BRAND.assistant} {BRAND.store}: обращения, КП и аналитика
        </p>

        <label className="qform__field">
          <span>Логин</span>
          <input value={login} onChange={(e) => setLogin(e.target.value)} autoComplete="username" autoFocus required />
        </label>
        <label className="qform__field">
          <span>Пароль</span>
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
            aria-invalid={error ? 'true' : undefined}
            required
          />
        </label>
        {error && (
          <p className="qform__error" role="alert">
            {error}
          </p>
        )}

        <button className="btn btn--primary btn--block" type="submit" disabled={busy || !login.trim() || !password}>
          <LockKeyhole size={16} /> {busy ? 'Входим…' : 'Войти'}
        </button>

        <div className="login__client">
          Вы клиент?{' '}
          <Link to="/chat">
            <MessageCircle size={14} aria-hidden /> Открыть чат с консультантом
          </Link>
        </div>
      </form>
    </div>
  );
}
