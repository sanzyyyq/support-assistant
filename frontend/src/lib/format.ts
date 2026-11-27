import type { LicenseUnit } from '../api/types';

const nf = new Intl.NumberFormat('ru-RU');
const nf2 = new Intl.NumberFormat('ru-RU', { minimumFractionDigits: 2, maximumFractionDigits: 2 });

export const formatNumber = (n: number) => nf.format(n);
export const formatPrice = (n: number) => `${nf.format(Math.round(n))} ₽`;
/** Для документа КП: 12 345,00 */
export const formatMoney = (n: number) => nf2.format(n);

export const formatTime = (iso: string) =>
  new Date(iso).toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' });

export const formatDate = (iso: string) =>
  new Date(iso).toLocaleDateString('ru-RU', { day: '2-digit', month: '2-digit', year: 'numeric' });

export function formatAgo(iso: string, now = Date.now()): string {
  const min = Math.floor((now - new Date(iso).getTime()) / 60000);
  if (min < 1) return 'сейчас';
  if (min < 60) return `${min} мин`;
  const h = Math.floor(min / 60);
  if (h < 24) return `${h} ч`;
  const d = Math.floor(h / 24);
  return d === 1 ? 'вчера' : `${d} дн`;
}

export function plural(n: number, one: string, few: string, many: string): string {
  const m10 = n % 10;
  const m100 = n % 100;
  if (m10 === 1 && m100 !== 11) return one;
  if (m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14)) return few;
  return many;
}

export const years = (n: number) => `${n} ${plural(n, 'год', 'года', 'лет')}`;

const UNIT: Record<LicenseUnit, [string, string, string, string]> = {
  // один, несколько, много, короткая форма «за …»
  user: ['пользователь', 'пользователя', 'пользователей', 'пользователя'],
  mailbox: ['почтовый ящик', 'почтовых ящика', 'почтовых ящиков', 'ящик'],
  device: ['устройство', 'устройства', 'устройств', 'устройство'],
  server: ['сервер', 'сервера', 'серверов', 'сервер'],
  license: ['лицензия', 'лицензии', 'лицензий', 'лицензию'],
};

export const unitCount = (n: number, unit: LicenseUnit) => `${formatNumber(n)} ${plural(n, UNIT[unit][0], UNIT[unit][1], UNIT[unit][2])}`;
export const unitShort = (unit: LicenseUnit) => UNIT[unit][3];

export const initials = (name: string) =>
  name
    .split(/\s+/)
    .filter((w) => /[\p{L}]/u.test(w[0] ?? ''))
    .slice(0, 2)
    .map((w) => w[0].toUpperCase())
    .join('') || '?';

/** 12 → «1 год», 0 → «бессрочно», 3 → «3 мес.» */
export function termText(months: number | null | undefined): string {
  if (months === null || months === undefined) return 'срок не указан';
  if (months === 0) return 'бессрочно';
  if (months % 12 === 0) return years(months / 12);
  return `${months} мес.`;
}

export const CHANNEL_LABEL = { web: 'Сайт', telegram: 'Telegram', email: 'Email' } as const;
