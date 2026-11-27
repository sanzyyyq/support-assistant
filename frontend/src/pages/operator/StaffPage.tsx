import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { KeyRound, UserPlus } from 'lucide-react';
import { api } from '../../api/client';
import type { Role, StaffUser } from '../../api/types';
import { formatAgo } from '../../lib/format';
import '../../styles/staff.css';
import { useOperator } from './operatorContext';

const ROLE_LABEL: Record<Role, string> = { admin: 'Администратор', manager: 'Менеджер' };
const MIN_PASSWORD = 8;

/** Сотрудники кабинета (только администратор): добавить, сменить роль, отключить, задать пароль */
export function StaffPage() {
  const me = useOperator();
  const [users, setUsers] = useState<StaffUser[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(() => api.listUsers().then(setUsers, (e: Error) => setError(e.message)), []);
  useEffect(() => {
    void load();
  }, [load]);

  const update = async (u: StaffUser, patch: Parameters<typeof api.updateUser>[1]) => {
    setError(null);
    try {
      const next = await api.updateUser(u.id, patch);
      setUsers((list) => list?.map((x) => (x.id === next.id ? next : x)) ?? null);
      return true;
    } catch (e) {
      setError((e as Error).message);
      return false;
    }
  };

  if (me.role !== 'admin') return <div className="op__empty">Раздел доступен только администратору</div>;

  return (
    <div className="analytics staff">
      <header className="analytics__head">
        <h1 className="tickets__title">Сотрудники</h1>
        <span className="caption muted">Кто может входить в кабинет. Имя сотрудника видит клиент в чате и в КП.</span>
      </header>

      <NewUserForm onCreated={(u) => setUsers((list) => [...(list ?? []), u])} />

      {error && (
        <p className="qform__error" role="alert">
          {error}
        </p>
      )}

      <section className="panel">
        <h2 className="panel__title">Учётные записи</h2>
        {!users ? (
          <p className="muted">Загружаю…</p>
        ) : (
          <div className="staff__table-wrap">
            <table className="staff__table">
              <thead>
                <tr>
                  <th>Сотрудник</th>
                  <th>Роль</th>
                  <th>Последний вход</th>
                  <th>Статус</th>
                  <th aria-label="Действия" />
                </tr>
              </thead>
              <tbody>
                {users.map((u) => (
                  <UserRow key={u.id} user={u} isMe={u.id === me.id} onUpdate={(patch) => update(u, patch)} />
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  );
}

function UserRow({ user, isMe, onUpdate }: { user: StaffUser; isMe: boolean; onUpdate: (patch: Parameters<typeof api.updateUser>[1]) => Promise<boolean> }) {
  const [resetting, setResetting] = useState(false);
  const [password, setPassword] = useState('');

  const savePassword = async (e: FormEvent) => {
    e.preventDefault();
    if (await onUpdate({ password })) {
      setPassword('');
      setResetting(false);
    }
  };

  return (
    <>
      <tr className={user.active ? '' : 'is-off'}>
        <td>
          <b>{user.name}</b>
          {isMe && <span className="muted"> · это вы</span>}
          <div className="caption muted">{user.login}</div>
        </td>
        <td>
          <select
            className="staff__select"
            value={user.role}
            disabled={isMe}
            onChange={(e) => void onUpdate({ role: e.target.value as Role })}
            aria-label={`Роль: ${user.name}`}
          >
            <option value="manager">{ROLE_LABEL.manager}</option>
            <option value="admin">{ROLE_LABEL.admin}</option>
          </select>
        </td>
        <td className="caption muted">{user.lastLoginAt ? formatAgo(user.lastLoginAt) : 'не входил'}</td>
        <td>
          <span className={`tag ${user.active ? 'tag--success' : ''}`}>{user.active ? 'Активен' : 'Отключён'}</span>
        </td>
        <td className="staff__actions">
          <button className="btn btn--ghost btn--sm" onClick={() => setResetting((v) => !v)}>
            <KeyRound size={14} /> Пароль
          </button>
          {!isMe && (
            <button className="btn btn--ghost btn--sm" onClick={() => void onUpdate({ active: !user.active })}>
              {user.active ? 'Отключить' : 'Включить'}
            </button>
          )}
        </td>
      </tr>
      {resetting && (
        <tr className="staff__reset">
          <td colSpan={5}>
            <form onSubmit={savePassword}>
              <label className="qform__field">
                <span>Новый пароль для {user.login} — сотрудник выйдет со всех устройств</span>
                <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" autoFocus />
              </label>
              <button className="btn btn--primary btn--sm" type="submit" disabled={password.length < MIN_PASSWORD}>
                Сохранить
              </button>
            </form>
          </td>
        </tr>
      )}
    </>
  );
}

function NewUserForm({ onCreated }: { onCreated: (u: StaffUser) => void }) {
  const empty = { login: '', name: '', role: 'manager' as Role, password: '' };
  const [form, setForm] = useState(empty);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const set = (k: keyof typeof empty) => (e: { target: { value: string } }) => setForm((f) => ({ ...f, [k]: e.target.value }));

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      onCreated(await api.createUser({ ...form, login: form.login.trim(), name: form.name.trim() }));
      setForm(empty);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const valid = form.login.trim().length >= 3 && form.name.trim() && form.password.length >= MIN_PASSWORD;

  return (
    <form className="panel staff__new" onSubmit={submit}>
      <h2 className="panel__title">Добавить сотрудника</h2>
      <div className="staff__grid">
        <label className="qform__field">
          <span>Имя (видит клиент)</span>
          <input value={form.name} onChange={set('name')} placeholder="Анна К." />
        </label>
        <label className="qform__field">
          <span>Логин</span>
          <input value={form.login} onChange={set('login')} placeholder="anna" autoComplete="off" />
        </label>
        <label className="qform__field">
          <span>Пароль (от {MIN_PASSWORD} символов)</span>
          <input type="password" value={form.password} onChange={set('password')} autoComplete="new-password" />
        </label>
        <label className="qform__field">
          <span>Роль</span>
          <select className="staff__select" value={form.role} onChange={set('role')}>
            <option value="manager">{ROLE_LABEL.manager}</option>
            <option value="admin">{ROLE_LABEL.admin}</option>
          </select>
        </label>
        <button className="btn btn--primary" type="submit" disabled={busy || !valid}>
          <UserPlus size={16} /> Добавить
        </button>
      </div>
      {error && (
        <p className="qform__error" role="alert">
          {error}
        </p>
      )}
    </form>
  );
}
