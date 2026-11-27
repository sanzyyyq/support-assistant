import { QUOTE_DEFAULTS, VAT_RATE } from '../../config';
import { calcLabel } from '../../lib/domain';
import { formatPrice, termText, unitCount } from '../../lib/format';
import type { CompareTable, Need, Offer, OfferItem, Quote, QuoteItem, QuoteRequest, Requirements, Ticket } from '../types';
import { NEED_FILTER, PREFERRED, editionsOf, looksLikeUnparsedPack, priceFor, russianAnalogs, toProduct, type Catalog, type Family, type Priced } from './catalog';

let offerSeq = 0;

export function itemFrom(p: Priced): OfferItem {
  return { product: toProduct(p.family, p.variant), qty: p.qty, packs: p.packs, total: p.total };
}

function makeOffer(title: string, items: OfferItem[], reasons: string[], notes: string[] = []): Offer {
  return {
    id: `o-${Date.now().toString(36)}-${++offerSeq}`,
    title,
    items,
    reasons,
    notes: notes.length ? notes : undefined,
    total: items.reduce((s, i) => s + i.total, 0),
  };
}

const NEED_TEXT: Record<Need, string> = {
  mail: 'защиту почты',
  endpoint: 'защиту ПК и файлов',
  backup: 'резервное копирование',
  office: 'офисные задачи',
  os: 'операционную систему',
};

const joinAnd = (xs: string[]) => (xs.length < 2 ? xs.join('') : `${xs.slice(0, -1).join(', ')} и ${xs[xs.length - 1]}`);

function reasonsFor(items: OfferItem[], r: Requirements, priced: Priced[], covered?: Need[]): { reasons: string[]; notes: string[] } {
  const reasons: string[] = [];
  const notes: string[] = [];
  const total = items.reduce((s, i) => s + i.total, 0);
  const done = covered ?? r.needs ?? [];
  if (done.length) reasons.push(`Закрывает ${joinAnd(done.map((n) => NEED_TEXT[n]))}`);
  const left = (r.needs ?? []).filter((n) => !done.includes(n));
  if (left.length) notes.push(`Не закрывает ${joinAnd(left.map((n) => NEED_TEXT[n]))} — в каталоге нет подходящей позиции`);
  const reg = items.filter((i) => i.product.registryNumber);
  if (reg.length === items.length) reasons.push('Всё ПО в реестре российского ПО — без НДС');
  else if (items.every((i) => i.product.russianVendor)) reasons.push('Все продукты — российских разработчиков');
  else {
    const foreign = items.filter((i) => !i.product.russianVendor).map((i) => i.product.vendor);
    if (foreign.length) notes.push(`${[...new Set(foreign)].join(', ')} — зарубежный разработчик`);
  }
  for (const i of reg) reasons.push(`${i.product.name}: в реестре, № ${i.product.registryNumber}`);
  const vendors = new Set(items.map((i) => i.product.vendor));
  if (vendors.size === 1 && items.length > 1) reasons.push('Один вендор — единая консоль и поддержка');
  if (typeof r.budget === 'number') {
    if (total <= r.budget) reasons.push(`Укладывается в бюджет: ${formatPrice(total)} из ${formatPrice(r.budget)}`);
    else notes.push(`Выше бюджета на ${formatPrice(total - r.budget)}`);
  }
  for (const p of priced) {
    if (p.adjusted) notes.push(`${p.family.title}: минимум ${unitCount(p.qty, p.family.unit)} для этой цены`);
    if (p.termFallback) notes.push(`${p.family.title}: срок ${termText(p.variant.term)} — другого в каталоге нет`);
  }
  return { reasons, notes };
}

function markRecommended(offers: Offer[], r: Requirements): Offer[] {
  if (!offers.length) return offers;
  const budget = typeof r.budget === 'number' ? r.budget : Infinity;
  const score = (o: Offer) =>
    (o.total <= budget ? 0 : 2) + (o.items.every((i) => i.product.russianVendor) ? 0 : 1) + (o.notes?.length ?? 0) * 0.2 + o.total / 1e9;
  const best = [...offers].sort((a, b) => score(a) - score(b))[0];
  return offers.map((o) => ({ ...o, recommended: o.id === best.id }));
}

/** Какие категории каталога нужны под задачу */
function filtersFor(r: Requirements): Array<keyof typeof NEED_FILTER> {
  const out: Array<keyof typeof NEED_FILTER> = [];
  for (const n of r.needs ?? []) {
    if (n === 'mail') {
      if (r.infrastructure === 'none') out.push('mail_server');
      if (r.infrastructure !== 'cloud') out.push('mail');
    } else out.push(n);
  }
  return out;
}

export function candidates(cat: Catalog, key: keyof typeof NEED_FILTER, qty: number, years: number): Priced[] {
  const pref = PREFERRED[key];
  return cat.families
    .filter((f) => f.hasNew && NEED_FILTER[key](f) && !looksLikeUnparsedPack(f))
    .map((f) => priceFor(f, qty, years))
    // Без «пакетов по 5 × 10» и подозрительно дешёвых позиций (хранилище за гигабайт и т.п.)
    .filter((x): x is Priced => !!x && !x.adjusted && x.total >= 100 && !(x.variant.mode === 'pack' && x.packs > 1))
    // Профильные продукты — первыми, внутри — дешевле
    .sort((a, b) => Number(pref.test(b.family.title)) - Number(pref.test(a.family.title)) || a.total - b.total);
}

/** Сценарий «не знаю, что нужно»: два варианта под требования */
export function discoverOffers(cat: Catalog, r: Requirements): { offers: Offer[]; missing: string[] } {
  const qty = r.users ?? 1;
  const y = r.years ?? 1;
  const picksA: Priced[] = [];
  const picksB: Priced[] = [];
  const needOf = new Map<Priced, Need>();
  const toNeed = (k: keyof typeof NEED_FILTER): Need => (k === 'mail_server' ? 'mail' : k);
  const missing: string[] = [];
  for (const key of filtersFor(r)) {
    const all = candidates(cat, key, qty, y);
    // Отсекаем позиции на порядок дороже самой дешёвой — обычно это серверные лицензии, посчитанные «за пользователя»
    const floor = Math.min(...all.map((x) => x.total));
    const list = all.filter((x) => x.total <= floor * 5);
    if (!list.length) {
      missing.push(key);
      continue;
    }
    // Вариант 1 — российский разработчик (по возможности того же вендора, что уже выбран), вариант 2 — другой вендор
    const vendorA = picksA[0]?.family.vendor;
    const a = list.find((x) => x.family.ru && x.family.vendor === vendorA) ?? list.find((x) => x.family.ru) ?? list[0];
    const vendorB = picksB[0]?.family.vendor;
    const b =
      list.find((x) => x.family.vendor === vendorB && x.family.vendor !== a.family.vendor) ??
      list.find((x) => x.family.vendor !== a.family.vendor) ??
      list.find((x) => x.family.id !== a.family.id);
    picksA.push(a);
    needOf.set(a, toNeed(key));
    if (b) {
      picksB.push(b);
      needOf.set(b, toNeed(key));
    }
  }
  const offers: Offer[] = [];
  for (const [idx, picks] of [picksA, picksB].entries()) {
    if (!picks.length) continue;
    const items = picks.map(itemFrom);
    const covered = [...new Set(picks.map((p) => needOf.get(p)!))].filter((n) => r.needs?.includes(n));
    const { reasons, notes } = reasonsFor(items, r, picks, covered);
    const vendors = [...new Set(items.map((i) => i.product.vendor))];
    offers.push(makeOffer(`Вариант ${idx + 1} · ${vendors.join(' + ')}`, items, reasons, notes));
  }
  return { offers: markRecommended(offers, r), missing };
}

/** Сценарий «знаю продукт»: сверяем редакции и считаем под количество и срок */
export function knownOffers(cat: Catalog, f: Family, r: Requirements): Offer[] {
  const qty = r.users ?? 1;
  const y = r.years ?? 1;
  const priced = editionsOf(cat, f)
    .map((e) => priceFor(e, qty, y))
    .filter((x): x is Priced => !!x);
  const base = priced[0];
  const offers = priced.map((p) => {
    const item = itemFrom(p);
    const reasons = [`${calcLabel(item)}, ${termText(p.variant.term)}`];
    if (p.family.registry) reasons.push(`В реестре российского ПО, № ${p.family.registry}`);
    else if (p.family.ru) reasons.push('Российский разработчик');
    if (p.family.platforms) reasons.push(`Платформы: ${p.family.platforms}`);
    const notes: string[] = [];
    if (!p.family.ru) notes.push('Зарубежный разработчик');
    if (p.adjusted) notes.push(`Минимум ${unitCount(p.qty, p.family.unit)} для этой цены`);
    if (p.termFallback) notes.push(`Срок ${termText(p.variant.term)} — другого в каталоге нет`);
    if (base && p !== base && p.total > base.total) notes.push(`Дороже ${base.family.title.split(' ').slice(-1)[0]} на ${formatPrice(p.total - base.total)}`);
    return makeOffer(p.family.title, [item], reasons, notes);
  });
  return offers.map((o, i) => ({ ...o, recommended: i === 0 }));
}

/** Заменить зарубежное ПО на российские аналоги из каталога и пересчитать */
export function replaceWithAnalogs(cat: Catalog, offers: Offer[], r: Requirements): Offer[] {
  const next = offers.map((o) => {
    let changed = false;
    const priced: Priced[] = [];
    const items = o.items.map((i) => {
      if (i.product.russianVendor) return i;
      const f = cat.byId.get(i.product.familyId);
      const analog = f && russianAnalogs(cat, f, i.qty, r.years ?? 1)[0];
      if (!analog) return i;
      changed = true;
      priced.push(analog);
      return itemFrom(analog);
    });
    if (!changed) return o;
    const { reasons, notes } = reasonsFor(items, r, priced);
    return { ...makeOffer(o.title.replace(/ · .*/, '') + ' · с аналогами', items, reasons, notes), id: o.id };
  });
  return markRecommended(next, r);
}

export function compareOffers(offers: Offer[]): CompareTable {
  const items = offers.map((o) => o.items[0]);
  return {
    columns: items.map((i) => i.product.name),
    rows: [
      { label: 'Расчёт', values: items.map((i) => calcLabel(i)) },
      { label: 'Итого', values: offers.map((o) => formatPrice(o.total)) },
      { label: 'Срок', values: items.map((i) => termText(i.product.termMonths)) },
      { label: 'Платформы', values: items.map((i) => (i.product.platforms.length > 3 ? `${i.product.platforms.slice(0, 3).join(', ')} и др.` : i.product.platforms.join(', ')) || '—') },
      { label: 'Реестр российского ПО', values: items.map((i) => (i.product.registryNumber ? `№ ${i.product.registryNumber}` : '—')) },
      { label: 'Разработчик', values: items.map((i) => (i.product.russianVendor ? 'Россия' : 'Зарубежный')) },
    ],
  };
}

const ROLE: Record<string, string> = {
  'Антивирусы для организаций': 'Защита',
  'Программное обеспечение для резервного копирования': 'Бэкап',
  'Почтовые утилиты': 'Почта',
  'Серверное ПО': 'Почтовый сервер',
};

export function compareBundles(offers: Offer[]): CompareTable {
  const rowsCount = Math.max(...offers.map((o) => o.items.length));
  return {
    columns: offers.map((o) => o.title.replace(/ · .*/, '')),
    rows: [
      ...Array.from({ length: rowsCount }, (_, k) => ({
        label: ROLE[offers[0].items[k]?.product.category ?? ''] ?? offers[0].items[k]?.product.category ?? `Позиция ${k + 1}`,
        values: offers.map((o) => o.items[k]?.product.name ?? '—'),
      })),
      { label: 'Итого', values: offers.map((o) => formatPrice(o.total)) },
      { label: 'Российские разработчики', values: offers.map((o) => (o.items.every((i) => i.product.russianVendor) ? '✓' : '—')) },
    ],
  };
}

export function quoteItemsFromOffer(offer: Offer): QuoteItem[] {
  return offer.items.map((i) => ({
    sku: i.product.sku,
    name: i.product.offerName,
    qty: i.product.pricing === 'pack' ? i.packs : i.qty,
    price: i.product.price,
    // Без НДС — только если номер в реестре есть в фиде; иначе ставку подтверждает менеджер
    vatRate: i.product.registryNumber ? 0 : null,
  }));
}

export function buildQuote(ticket: Ticket, offer: Offer, req: QuoteRequest, seq: number): Quote {
  const now = new Date();
  const valid = new Date(now.getTime() + QUOTE_DEFAULTS.validDays * 86_400_000);
  return {
    id: `q-${ticket.number}-${seq}`,
    number: `КП-${ticket.number}-${seq}`,
    ticketId: ticket.id,
    status: 'review',
    company: req.company,
    inn: req.inn,
    email: req.email,
    paymentTerms: req.paymentTerms,
    deliveryTerm: QUOTE_DEFAULTS.deliveryTerm,
    validUntil: valid.toISOString(),
    createdAt: now.toISOString(),
    items: quoteItemsFromOffer(offer),
  };
}

export { VAT_RATE };
