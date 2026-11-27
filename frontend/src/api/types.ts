// Контракт данных между фронтендом и бэкендом. Даты — ISO 8601, деньги — рубли.

export type TicketStatus = 'ai' | 'waiting' | 'operator' | 'closed';
export type Sender = 'user' | 'bot' | 'operator' | 'system';
export type Channel = 'web' | 'telegram' | 'email';
export type Sentiment = 'positive' | 'neutral' | 'negative';

// ── Каталог (из товарного фида Store, см. data/build_catalog.py) ──────

export type LicenseUnit = 'user' | 'mailbox' | 'device' | 'server' | 'license';
/** new — новая лицензия, renewal — продление, migration — переход с другого продукта */
export type LicenseKind = 'new' | 'renewal' | 'migration' | 'support' | 'upgrade' | 'media';

/** Конкретная позиция каталога (offer из фида) */
export interface Product {
  /** id позиции в фиде */
  id: string;
  familyId: string;
  /** Артикул для КП */
  sku: string;
  vendor: string;
  /** Название продукта (семейства) */
  name: string;
  /** Полное наименование позиции из фида — идёт в КП */
  offerName: string;
  category: string;
  categoryPath: string[];
  licenseKind: LicenseKind;
  /** Срок в месяцах; 0 — бессрочно; null — не указан */
  termMonths: number | null;
  unit: LicenseUnit;
  /** Цена из фида: за единицу (per_unit) или за пакет (pack) */
  price: number;
  pricing: 'per_unit' | 'pack';
  /** Для per_unit — диапазон количества, на который действует цена; для pack — размер пакета */
  tierMin: number | null;
  tierMax: number | null;
  platforms: string[];
  /** Номер записи в реестре отечественного ПО — только если он есть в фиде */
  registryNumber?: string;
  /** Российский разработчик (по списку вендоров) */
  russianVendor: boolean;
  url: string;
  picture?: string;
}

/** Позиция в варианте решения */
export interface OfferItem {
  product: Product;
  /** Сколько единиц нужно клиенту */
  qty: number;
  /** Сколько позиций покупаем: единиц (per_unit) или пакетов (pack) */
  packs: number;
  total: number;
}

/** Вариант решения, который предлагает агент */
export interface Offer {
  id: string;
  title: string;
  recommended?: boolean;
  items: OfferItem[];
  /** Почему подходит */
  reasons: string[];
  /** На что обратить внимание */
  notes?: string[];
  total: number;
}

export interface Source {
  title: string;
  url?: string;
  kind: 'product' | 'kb';
}

export interface CompareTable {
  columns: string[];
  rows: Array<{ label: string; values: string[] }>;
}

// ── Требования, которые собирает агент ────────────────────────────────

export type Need = 'mail' | 'endpoint' | 'backup' | 'office' | 'os';
export type Slot = 'needs' | 'product' | 'users' | 'infrastructure' | 'years' | 'budget';

export interface Requirements {
  needs?: Need[];
  productId?: string;
  productName?: string;
  users?: number;
  infrastructure?: 'own_server' | 'cloud' | 'none';
  years?: number;
  /** Рубли или 'unlimited' */
  budget?: number | 'unlimited';
}

// ── Коммерческое предложение ──────────────────────────────────────────

export interface QuoteItem {
  sku: string;
  name: string;
  qty: number;
  /** Цена за единицу с учётом налогов */
  price: number;
  /** Ставка НДС, %; 0 — ПО из реестра (НДС не облагается); null — уточняет менеджер */
  vatRate: number | null;
}

export type QuoteStatus = 'review' | 'sent';

export interface Quote {
  id: string;
  number: string;
  ticketId: string;
  status: QuoteStatus;
  company: string;
  inn?: string;
  email: string;
  paymentTerms: 'Предоплата' | 'Постоплата';
  deliveryTerm: string;
  /** Дата окончания действия, ISO */
  validUntil: string;
  createdAt: string;
  items: QuoteItem[];
  reviewer?: string;
  comment?: string;
}

export interface QuoteRequest {
  company: string;
  inn?: string;
  email: string;
  paymentTerms: Quote['paymentTerms'];
}

// ── Диалоги ───────────────────────────────────────────────────────────

export interface Message {
  id: string;
  ticketId: string;
  sender: Sender;
  text: string;
  createdAt: string;
  authorName?: string;
  /** Кнопки-варианты ответа */
  quickReplies?: string[];
  /** Карточки отдельных продуктов */
  products?: Product[];
  /** Варианты решения с обоснованием */
  offers?: Offer[];
  compare?: CompareTable;
  /** На что опирается ответ */
  sources?: Source[];
  /** Встроенная форма (данные для КП) */
  form?: 'quote_request';
  formDone?: boolean;
  /** Ссылка на сформированное КП */
  quoteId?: string;
  /** Оценка ответа клиентом */
  feedback?: 'up' | 'down';
  intent?: string;
}

export interface Customer {
  id: string;
  name: string;
  email?: string;
  phone?: string;
  company?: string;
  inn?: string;
}

export interface Ticket {
  id: string;
  number: number;
  customer: Customer;
  channel: Channel;
  status: TicketStatus;
  createdAt: string;
  updatedAt: string;
  lastMessage?: string;
  assignee?: string;
  handoffAt?: string;
  handoffReason?: string;
  /** 0..1 */
  aiConfidence?: number;
  aiSummary?: string;
  sentiment?: Sentiment;
  tags: string[];
  /** Сценарий: клиент знает продукт / подбираем / вопрос о продукте */
  scenario?: 'known' | 'discover' | 'question';
  requirements: Requirements;
  /** Какой вопрос агент задал последним */
  pendingSlot?: Slot;
  lastOffers?: Offer[];
  selectedOfferId?: string;
  quoteId?: string;
}

export interface Session {
  ticket: Ticket;
  messages: Message[];
}

export interface Stats {
  ticketsToday: number;
  aiResolvedPct: number;
  handoffPct: number;
  firstResponseSec: number;
  quotes: number;
  quotesAmount: number;
  csatPct: number;
  ratedCount: number;
  topIntents: Array<{ label: string; count: number }>;
  /** Сэкономлено часов работы операторов в день */
  hoursSavedPerDay: number;
}

/** События реального времени (WebSocket у настоящего бэкенда). */
export type SupportEvent =
  | { type: 'message'; message: Message }
  | { type: 'message_updated'; message: Message }
  | { type: 'ticket_updated'; ticket: Ticket }
  | { type: 'quote_updated'; quote: Quote }
  | { type: 'typing'; ticketId: string; sender: 'bot' | 'operator'; active: boolean }
  | { type: 'reset' };
