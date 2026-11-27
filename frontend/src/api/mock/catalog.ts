import type { LicenseKind, LicenseUnit, Need, Product } from '../types';

// Каталог — выжимка из товарного фида Store (data/build_catalog.py → public/data/catalog.json.gz).
// Заглушка ищет и считает цены прямо в браузере; настоящий бэкенд будет делать то же на сервере.

export interface RawVariant {
  id: string;
  name: string;
  price: number;
  /** Месяцы; 0 — бессрочно; null — не указан */
  term: number | null;
  kind: LicenseKind;
  min: number | null;
  max: number | null;
  mode: 'per_unit' | 'pack';
  org: 'com' | 'gov' | 'nko' | null;
  url: string;
}

export interface Family {
  id: string;
  vendor: string;
  title: string;
  path: string[];
  registry: string | null;
  platforms: string | null;
  picture: string | null;
  unit: LicenseUnit;
  ru: boolean;
  variants: RawVariant[];
  // вычисляется при загрузке
  titleWords: string[];
  vendorWords: string[];
  pathText: string;
  hasNew: boolean;
}

export interface Catalog {
  families: Family[];
  byId: Map<string, Family>;
  source: string;
}

// ── Нормализация текста ───────────────────────────────────────────────

const ALIASES: Array<[RegExp, string]> = [
  [/касперск\w*/g, 'kaspersky'],
  [/доктор\s*веб|др\.?\s*веб|drweb|dr\s+web/g, 'dr.web доктор веб'],
  [/мдемон|мдэймон|эмдемон/g, 'mdaemon'],
  [/астра\b|астру|астры/g, 'astra'],
  [/архикад|архмкад|архикада/g, 'archicad'],
  [/фотошоп\w*/g, 'photoshop'],
  [/анидеск|энидеск|эни деск/g, 'anydesk'],
  [/крипто\s*про/g, 'криптопро'],
  [/ворд|ворда/g, 'word'],
  [/эксел\w*/g, 'excel'],
  [/офис\s*365|o365/g, 'office 365'],
  [/1с\b|1c\b/g, '1с'],
  [/кибербэкап/g, 'кибер бэкап'],
  [/ред\s*ос/g, 'ред ос'],
];

const STOP = new Set([
  'для', 'на', 'и', 'с', 'по', 'от', 'до', 'в', 'из', 'или', 'без', 'при', 'под', 'нам', 'мне', 'нужен', 'нужна', 'нужно',
  'лицензия', 'лицензии', 'лицензий', 'лицензию', 'версия', 'редакция', 'год', 'года', 'лет', 'шт', 'штук',
  'компонентов', 'количество', 'количеству', 'узлов', 'пользователей', 'business', 'edition', 'цена', 'есть',
  'россия', 'российский', 'российская', 'аналог', 'компьютеров', 'компьютер', 'файлов', 'почту', 'почты', 'ключи', 'ключом',
  'сколько', 'стоит', 'стоимость', 'цена', 'купить', 'приобрести', 'хотим', 'хочу', 'добрый', 'день', 'здравствуйте',
]);

export const norm = (s: string) =>
  s
    .toLowerCase()
    .replace(/ё/g, 'е')
    .replace(/[«»"()\[\],;:!?/\\+]/g, ' ')
    .replace(/ /g, ' ')
    .replace(/\s+/g, ' ')
    .trim();

export function expand(text: string): string {
  let t = norm(text);
  for (const [re, to] of ALIASES) if (re.test(t)) t += ' ' + to;
  return t;
}

/** Значимые слова; двухбуквенные — только устойчивые («ос», «1с») */
const words = (s: string) =>
  norm(s)
    .split(' ')
    .filter((w) => (w.length >= 3 || /^(ос|1с|ии)$/.test(w)) && !STOP.has(w) && !/^\d+$/.test(w));

/** Слово запроса совпадает со словом каталога с точностью до окончания */
const stemMatch = (q: string, w: string) => {
  if (q === w) return true;
  // Латинские названия — строго (power ≠ powerdvd), допускаем только множественное число
  if (/[a-z]/.test(q) || /[a-z]/.test(w)) return q + 's' === w || w + 's' === q;
  const k = Math.max(4, Math.min(q.length, w.length) - 2);
  return q.length >= 4 && w.length >= 4 && q.slice(0, k) === w.slice(0, k);
};

// ── Загрузка ──────────────────────────────────────────────────────────

let loading: Promise<Catalog> | null = null;
let loaded: Catalog | null = null;

async function fetchCatalog(): Promise<{ families: Omit<Family, 'titleWords' | 'vendorWords' | 'pathText' | 'hasNew'>[]; generatedFrom: string }> {
  const res = await fetch(`${import.meta.env.BASE_URL}data/catalog.json.gz`);
  if (!res.ok) throw new Error(`Каталог не загрузился: ${res.status}`);
  const buf = new Uint8Array(await res.arrayBuffer());
  const gz = buf[0] === 0x1f && buf[1] === 0x8b;
  const text = gz
    ? await new Response(new Blob([buf]).stream().pipeThrough(new DecompressionStream('gzip'))).text()
    : new TextDecoder().decode(buf);
  return JSON.parse(text);
}

export function loadCatalog(): Promise<Catalog> {
  loading ??= fetchCatalog().then((raw) => {
    const families: Family[] = raw.families.map((f) => ({
      ...f,
      titleWords: words(f.title),
      vendorWords: words(expand(f.vendor)),
      pathText: f.path.join(' > '),
      hasNew: f.variants.some((v) => v.kind === 'new'),
    }));
    loaded = { families, byId: new Map(families.map((f) => [f.id, f])), source: raw.generatedFrom };
    return loaded;
  });
  return loading;
}

export const catalogSync = () => loaded;

// ── Поиск ─────────────────────────────────────────────────────────────

export interface Mention {
  family?: Family;
  /** Назван только вендор («касперского»), продукт не ясен */
  vendor?: string;
  vendorFamilies?: Family[];
}

/** Найти продукт, который клиент назвал в сообщении */
export function findMentioned(cat: Catalog, text: string): Mention {
  const q = words(expand(text));
  if (!q.length) return {};
  let best: { f: Family; score: number } | undefined;
  const vendorHits = new Map<string, Family[]>();
  for (const f of cat.families) {
    if (!f.hasNew) continue;
    const vendorHit = f.vendorWords.some((w) => q.some((x) => stemMatch(x, w)));
    const titleHit = f.titleWords.filter((w) => q.some((x) => stemMatch(x, w)));
    if (vendorHit) {
      const list = vendorHits.get(f.vendor) ?? [];
      list.push(f);
      vendorHits.set(f.vendor, list);
    }
    // Слова названия, которые не повторяют имя вендора: «endpoint», «стандартный», «ос»
    const own = titleHit.filter((w) => !f.vendorWords.includes(w));
    if (!own.length || !f.titleWords.length) continue;
    const ratio = titleHit.length / f.titleWords.length;
    const latin = own.some((w) => /[a-z]/.test(w));
    // Принимаем: всё название целиком; или латинский бренд + половина названия; или вендор + половина названия
    const ok = ratio === 1 ? own.length >= 1 && (latin || vendorHit || titleHit.length >= 2) : ratio >= 0.5 && (vendorHit || (latin && own.length >= 2));
    if (!ok) continue;
    // Полное совпадение названия важнее числа совпавших слов: «РЕД ОС» лучше, чем «Инсталляционный комплект … РЕД ОС»
    const score = titleHit.length + ratio * 5 + (vendorHit ? 1 : 0) + Math.min(f.variants.length, 50) / 100;
    if (!best || score > best.score) best = { f, score };
  }
  if (best && best.score >= 3) return { family: best.f };
  // Назван только вендор: «85 лицензий касперского»
  const [vendor, fams] = [...vendorHits.entries()].sort((a, b) => b[1].length - a[1].length)[0] ?? [];
  if (vendor && fams) {
    const top = fams.filter((f) => f.hasNew).sort((a, b) => b.variants.length - a.variants.length).slice(0, 4);
    return { vendor, vendorFamilies: top };
  }
  return {};
}

const P = (f: Family) => f.pathText;

/** Какие семейства закрывают задачу клиента */
export const NEED_FILTER: Record<Need | 'mail_server', (f: Family) => boolean> = {
  mail: (f) => /Антивирус|безопасн/i.test(P(f)) && /почт|mail/i.test(f.title) && !/office 365|connector/i.test(f.title),
  mail_server: (f) =>
    /почт|mail|messaging/i.test(f.title + ' ' + P(f)) &&
    /сервер|server|enterprise|messaging/i.test(f.title + ' ' + P(f)) &&
    !/антивирус|antivirus|security|защит|connector|activesync|outlook|клиент|the bat|spam/i.test(f.title),
  endpoint: (f) =>
    P(f).includes('Антивирусы для организаций') &&
    /endpoint|комплексная защита|антивирус|security для бизнеса|small office|total security/i.test(f.title) &&
    !/почт|шлюз|gateway|хранени|embedded|office 365|server security|curenet|виртуальн|фстэк|mail/i.test(f.title),
  backup: (f) => P(f).includes('резервного копирования') && !/mobiletrans|сопровожден|хранени|хранилищ|\bs3\b|дц\b/i.test(f.title),
  office: (f) => P(f).includes('Офисные программы'),
  os: (f) => P(f).includes('Операционные системы') && !/сертификат|техподдерж|дистрибутив/i.test(f.title),
};

/** Профильные продукты под задачу — их агент предлагает в первую очередь */
export const PREFERRED: Record<keyof typeof NEED_FILTER, RegExp> = {
  mail: /security для почтовых серверов|mail security|почтовых серверов/i,
  mail_server: /messaging server|tegu|communigate|почтовый сервер/i,
  endpoint: /endpoint security для бизнеса стандартный|комплексная защита|endpoint security standard/i,
  backup: /кибер бэкап (стандартная|расширенная)|backup/i,
  office: /360|р7|мойофис|workspad/i,
  os: /^ред ос|astra linux|альт/i,
};

/** Название вида «…, 5» — пакет, который фид не разметил количеством; в подборе такие не используем */
export const looksLikeUnparsedPack = (f: Family) => /,\s*\d+\s*$/.test(f.title);

// ── Цена под требования ───────────────────────────────────────────────

export interface Priced {
  family: Family;
  variant: RawVariant;
  /** Сколько единиц нужно клиенту (может быть увеличено до минимума тарифа) */
  qty: number;
  packs: number;
  total: number;
  /** Количество подняли до минимального для этой цены */
  adjusted?: boolean;
  /** Точного срока в каталоге нет — взяли ближайший */
  termFallback?: boolean;
}

/**
 * Подобрать позицию семейства под количество и срок.
 * per_unit: цена × количество (с учётом диапазона количества);
 * pack: наименьший пакет, который покрывает количество; если таких нет — несколько крупных пакетов.
 */
export function priceFor(f: Family, qty: number, years: number, kind: LicenseKind = 'new', org: 'com' | 'gov' = 'com'): Priced | undefined {
  let vs = f.variants.filter((v) => v.kind === kind && (!v.org || v.org === org));
  if (!vs.length && kind === 'new') vs = f.variants.filter((v) => v.kind === 'new');
  if (!vs.length) return undefined;

  const want = years * 12;
  const terms = [...new Set(vs.map((v) => v.term))];
  const term = terms.includes(want)
    ? want
    : terms.includes(0)
      ? 0
      : [...terms].sort((a, b) => Math.abs((a ?? 12) - want) - Math.abs((b ?? 12) - want))[0];
  vs = vs.filter((v) => v.term === term);
  const termFallback = term !== want && term !== 0 && term !== null;

  const packs = vs.filter((v) => v.mode === 'pack').sort((a, b) => (a.min ?? 1) - (b.min ?? 1) || a.price - b.price);
  const perUnit = vs.filter((v) => v.mode === 'per_unit');

  const options: Priced[] = [];
  const fitPack = packs.find((v) => (v.min ?? 1) >= qty);
  if (fitPack) options.push({ family: f, variant: fitPack, qty, packs: 1, total: fitPack.price, termFallback });
  else if (packs.length) {
    const big = packs[packs.length - 1];
    const n = Math.ceil(qty / (big.min ?? 1));
    options.push({ family: f, variant: big, qty, packs: n, total: big.price * n, termFallback });
  }
  const inTier = perUnit.filter((v) => (v.min === null || qty >= v.min) && (v.max === null || qty <= v.max));
  for (const v of inTier) options.push({ family: f, variant: v, qty, packs: qty, total: v.price * qty, termFallback });
  if (!options.length && perUnit.length) {
    // Количество меньше минимального для тарифа — поднимаем до минимума и честно говорим об этом
    const v = [...perUnit].sort((a, b) => (a.min ?? 1) - (b.min ?? 1))[0];
    const q = Math.max(qty, v.min ?? 1);
    options.push({ family: f, variant: v, qty: q, packs: q, total: v.price * q, adjusted: q !== qty, termFallback });
  }
  return options.sort((a, b) => a.total - b.total)[0];
}

export function toProduct(f: Family, v: RawVariant): Product {
  return {
    id: v.id,
    familyId: f.id,
    sku: v.id,
    vendor: f.vendor,
    name: f.title,
    offerName: v.name,
    category: f.path[f.path.length - 1] ?? '',
    categoryPath: f.path,
    licenseKind: v.kind,
    termMonths: v.term,
    unit: f.unit,
    price: v.price,
    pricing: v.mode,
    tierMin: v.min,
    tierMax: v.max,
    platforms: f.platforms ? f.platforms.split(', ').filter(Boolean) : [],
    registryNumber: f.registry ?? undefined,
    russianVendor: f.ru,
    url: v.url,
    picture: f.picture ?? undefined,
  };
}

/** Редакции того же продукта: Kaspersky Endpoint Security для бизнеса Стандартный / Расширенный / Расширенный Плюс */
export function editionsOf(cat: Catalog, f: Family): Family[] {
  // Редакция отличается последним словом при совпадающем начале; дополнения (ActiveSync, Connector) не редакции
  if (f.titleWords.length < 2) return [f];
  const head = f.titleWords.slice(0, -1);
  // В общем начале должно быть хотя бы одно слово, кроме имени вендора: «РЕД ОС» и «РЕД АДМ» — разные продукты
  if (!head.some((w) => !f.vendorWords.includes(w))) return [f];
  const sib = cat.families.filter(
    (x) =>
      x.vendor === f.vendor &&
      x.hasNew &&
      x.id !== f.id &&
      x.unit === f.unit &&
      x.titleWords.length >= f.titleWords.length &&
      x.titleWords.length <= f.titleWords.length + 1 &&
      head.every((w, i) => x.titleWords[i] === w),
  );
  return [f, ...sib.sort((a, b) => a.titleWords.length - b.titleWords.length || b.variants.length - a.variants.length).slice(0, 2)];
}

/** Российские аналоги: профильные продукты той же задачи (или того же раздела каталога) от российских разработчиков */
export function russianAnalogs(cat: Catalog, f: Family, qty: number, years: number): Priced[] {
  const leaf = f.path[f.path.length - 1];
  const need = (Object.keys(NEED_FILTER) as Array<keyof typeof NEED_FILTER>).find((k) => NEED_FILTER[k](f));
  const pref = need ? PREFERRED[need] : undefined;
  return cat.families
    .filter((x) => x.ru && x.hasNew && x.id !== f.id && !looksLikeUnparsedPack(x) && (need ? NEED_FILTER[need](x) : x.path[x.path.length - 1] === leaf))
    .map((x) => priceFor(x, qty, years))
    .filter((x): x is Priced => !!x && !x.adjusted && x.total >= 100 && !(x.variant.mode === 'pack' && x.packs > 1))
    .sort(
      (a, b) =>
        Number(!!pref?.test(b.family.title)) - Number(!!pref?.test(a.family.title)) ||
        Number(!!b.family.registry) - Number(!!a.family.registry) ||
        a.total - b.total,
    )
    .filter((x, i, arr) => arr.findIndex((y) => y.family.vendor === x.family.vendor) === i)
    .slice(0, 2);
}

/** Витрина: несколько известных продуктов из фида */
export function featured(cat: Catalog): Array<{ family: Family; from: Product }> {
  const want = [
    'Kaspersky Endpoint Security для бизнеса Стандартный',
    'Комплексная защита + Центр управления',
    'MDaemon Messaging Server',
    'Кибер Бэкап Расширенная редакция',
    'Kaspersky Security для почтовых серверов',
    'РЕД ОС',
    'Secret Net Studio',
    'КОМПАС-3D',
  ];
  const out: Array<{ family: Family; from: Product }> = [];
  for (const title of want) {
    const f =
      cat.families.find((x) => x.title === title && x.hasNew) ??
      cat.families.find((x) => x.hasNew && norm(x.title).startsWith(norm(title)));
    if (!f || out.some((o) => o.family.id === f.id)) continue;
    const v = [...f.variants].filter((x) => x.kind === 'new').sort((a, b) => a.price - b.price)[0];
    if (v) out.push({ family: f, from: toProduct(f, v) });
  }
  return out;
}
