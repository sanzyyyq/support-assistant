import type { LicenseKind, Need, OfferItem, Product, Quote, QuoteItem, Requirements, Slot } from '../api/types';
import { formatPrice, termText, unitCount, unitShort, years } from './format';

export const NEED_LABEL: Record<Need, string> = {
  mail: 'Защита почты',
  endpoint: 'Защита ПК и файлов',
  backup: 'Резервное копирование',
  office: 'Офисный пакет',
  os: 'Операционная система',
};

/** «Нужно закрыть …» — винительный падеж */
export const NEED_ACC: Record<Need, string> = {
  mail: 'защиту почты',
  endpoint: 'защиту ПК и файлов',
  backup: 'резервное копирование',
  office: 'офисный пакет',
  os: 'операционную систему',
};

export const KIND_LABEL: Record<LicenseKind, string> = {
  new: 'Новая лицензия',
  renewal: 'Продление',
  migration: 'Переход с другого продукта',
  support: 'Техподдержка',
  upgrade: 'Обновление',
  media: 'Дистрибутив',
};

const INFRA_LABEL = { own_server: 'Свой почтовый сервер', cloud: 'Облачная почта', none: 'Почты пока нет' } as const;

export const productTitle = (p: Pick<Product, 'name'>) => p.name;

/** «4 814 ₽ / устройство · 1 год» или «61 698 ₽ за 10 пользователей · 1 год» */
export function priceLabel(p: Product): string {
  const term = termText(p.termMonths);
  if (p.pricing === 'pack' && p.tierMin) return `${formatPrice(p.price)} за ${unitCount(p.tierMin, p.unit)} · ${term}`;
  return `${formatPrice(p.price)} / ${unitShort(p.unit)} · ${term}`;
}

export function licenseLabel(p: Product): string {
  const tier =
    p.pricing === 'per_unit' && p.tierMin && p.tierMax ? `цена при ${p.tierMin}–${p.tierMax}` : p.pricing === 'pack' ? 'пакет' : `за ${unitShort(p.unit)}`;
  return [KIND_LABEL[p.licenseKind], termText(p.termMonths), tier].join(' · ');
}

/** «50 устройств × 4 814 ₽» или «пакет на 50 ПК» */
export function calcLabel(i: OfferItem): string {
  const p = i.product;
  if (p.pricing === 'pack') {
    const size = p.tierMin ?? 1;
    return i.packs > 1 ? `${i.packs} × пакет на ${unitCount(size, p.unit)}` : `пакет на ${unitCount(size, p.unit)}`;
  }
  return `${unitCount(i.packs, p.unit)} × ${formatPrice(p.price)}`;
}

export interface RequirementRow {
  slot: Slot;
  label: string;
  value?: string;
}

/** Строки «что агент уже знает» — общие для клиента и оператора */
export function requirementRows(r: Requirements, scenario?: 'known' | 'discover' | 'question'): RequirementRow[] {
  const rows: RequirementRow[] = [];
  if (scenario === 'known' || r.productName) {
    rows.push({ slot: 'product', label: 'Продукт', value: r.productName });
  } else {
    rows.push({ slot: 'needs', label: 'Задача', value: r.needs?.map((n) => NEED_LABEL[n]).join(', ') });
  }
  rows.push({ slot: 'users', label: 'Количество', value: r.users ? String(r.users) : undefined });
  if (r.needs?.includes('mail') || r.infrastructure) {
    rows.push({ slot: 'infrastructure', label: 'Почта', value: r.infrastructure && INFRA_LABEL[r.infrastructure] });
  }
  rows.push({ slot: 'years', label: 'Срок', value: r.years ? years(r.years) : undefined });
  if (scenario !== 'known') {
    rows.push({
      slot: 'budget',
      label: 'Бюджет',
      value: r.budget === 'unlimited' ? 'не ограничен' : r.budget ? `до ${formatPrice(r.budget)}` : undefined,
    });
  }
  return rows;
}

export const hasRequirements = (r: Requirements) => Object.values(r).some((v) => v !== undefined && !(Array.isArray(v) && !v.length));

// ── КП ────────────────────────────────────────────────────────────────

export const itemSum = (i: QuoteItem) => i.price * i.qty;
export const itemVat = (i: QuoteItem) => (i.vatRate ? (itemSum(i) * i.vatRate) / (100 + i.vatRate) : 0);

export function quoteTotals(q: Pick<Quote, 'items'>) {
  const sum = q.items.reduce((s, i) => s + itemSum(i), 0);
  const vat = q.items.reduce((s, i) => s + itemVat(i), 0);
  return {
    sum,
    vat: Math.round(vat * 100) / 100,
    qty: q.items.reduce((s, i) => s + i.qty, 0),
    /** Есть позиции, где НДС уточняет менеджер */
    vatUnknown: q.items.some((i) => i.vatRate === null),
  };
}

export const QUOTE_STATUS_LABEL: Record<Quote['status'], string> = {
  review: 'На проверке у менеджера',
  sent: 'Согласовано и отправлено',
};
