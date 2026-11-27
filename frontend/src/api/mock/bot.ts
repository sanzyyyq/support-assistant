import { KIND_LABEL, NEED_ACC, requirementRows } from '../../lib/domain';
import { formatPrice, termText, unitCount, years } from '../../lib/format';
import type { Message, Need, Offer, Product, Requirements, Sentiment, Slot, Source, Ticket } from '../types';
import { expand, findMentioned, priceFor, russianAnalogs, toProduct, type Catalog, type Family, type Mention } from './catalog';
import { findKb, kbById } from './kb';
import { candidates, compareBundles, compareOffers, discoverOffers, itemFrom, knownOffers, replaceWithAnalogs } from './offers';

// Заглушка ИИ-агента: правила + слоты поверх настоящего каталога Store.
// Настоящий бэкенд заменит это на LLM с поиском по фиду, базе знаний и перепискам (RAG).

export type DraftMessage = Omit<Message, 'id' | 'ticketId' | 'createdAt'>;

export interface BotReply {
  messages: DraftMessage[];
  patch?: Partial<Ticket>;
  handoff?: { reason: string; confidence: number };
  sentiment?: Sentiment;
  addTags?: string[];
}

export const START_REPLIES = ['Знаю, какой продукт нужен', 'Помогите подобрать', 'Вопрос о покупке', 'Позвать менеджера'];

export const GREETING: DraftMessage = {
  sender: 'bot',
  intent: 'greeting',
  text: 'Здравствуйте! Я ИИ-консультант Store. Подберу ПО под задачу, сверю версии и лицензии по каталогу, отвечу на вопросы о покупке и подготовлю коммерческое предложение.',
  quickReplies: START_REPLIES,
};

const norm = (s: string) => s.toLowerCase().replace(/ё/g, 'е').replace(/ /g, ' ');
const joinAnd = (xs: string[]) => (xs.length < 2 ? xs.join('') : `${xs.slice(0, -1).join(', ')} и ${xs[xs.length - 1]}`);

const RE = {
  operator: /(оператор|менеджер|человек|живо[йм]|специалист|сотрудник поддержки)/,
  negative: /(безобраз|ужас|отврат|возмут|жалоб|сколько можно|обман|кошмар|бесит|достали|не берут трубку|никто не отвечает)/,
  discount: /(скидк|индивидуальн.*цен|спецусло|акци[ия] для|партнер)/,
  gov: /(госзакуп|44-фз|223-фз|тендер|бюджетн\w* (организац|учрежд)|конкурентн|закупк\w* сред|учреждени|органов власти|\b(гау|гбу|мбу|мбоу|фгбу|гку)\b)/,
  quote: /(кп\b|коммерческ|сформир|счет на оплату|выставить сч|пришлите сч|нужен сч|счет на )/,
  choose: /(вариант\s*\d|беру|выбираю|остановимся)/,
  analog: /(российск|отечествен|аналог|импортозамещ|реестр)/,
  replace: /заменить на аналог/,
  keep: /оставить как есть/,
  compare: /(сравн|отлич|разниц)/,
  changeQty: /изменить количество/,
  productQuestion: /(поддерж|работает|есть ли (у него|в нем)|что входит|функци|умеет|чем хорош|подойдет ли|совместим|платформ|расскажите про|что это)/,
  availability: /((можно|возможно|получится)[^.?]{0,30}(купить|приобрести|заказать)|есть ли (у вас|возможность)|продаете ли|у вас есть|сколько стоит|стоимость|цена на|прайс)/,
  individual: /(физлиц|физ\.? ?лиц|физическ\w* лиц|самозанят|для себя|для дома|для учеб)/,
  startKnown: /^знаю,? какой продукт/,
  startDiscover: /^(помогите подобрать|подобрать решение)/,
  startQuestion: /^вопрос о покупке/,
  renewal: /(продлен|продлить|продлит)/,
  greet: /^(привет|здравствуй|добрый|hello|hi)\b/,
  thanks: /^(спасибо|благодар|понятно|отлично)/,
};

// ── Разбор ответов ────────────────────────────────────────────────────

function parseNeeds(t: string): Need[] {
  const n = new Set<Need>();
  if (/(почт|email|e-mail|ящик|спам|фишинг|exchange)/.test(t)) n.add('mail');
  if (/(файл|компьютер|\bпк\b|рабоч(их|ие) (станц|мест)|антивирус|вирус|шифровал|ноутбук|защит\w* пк)/.test(t)) n.add('endpoint');
  if (/(файл|бэкап|резерв|backup|копирован)/.test(t)) n.add('backup');
  if (/(офисн|офис\b|документ|word|excel|таблиц|редактор)/.test(t)) n.add('office');
  if (/(операционн|\bос\b|linux|линукс|windows)/.test(t)) n.add('os');
  return [...n];
}

const int = (s: string) => parseInt(s.replace(/\s/g, ''), 10);

function parseUsers(t: string, pending: boolean): number | undefined {
  const m = t.match(/(\d[\d\s]{0,6})\s*(сотрудник|пользовател|человек|чел\b|пк\b|компьютер|ноутбук|устройств|рабоч|ящик|лиценз|мест|узл|шт|планшет|учетн|сервер)/);
  if (m) return int(m[1]);
  const m2 = t.match(/(?:на|для)\s+(\d{1,5})\b(?!\s*(год|лет|мес|тыс|₽|руб))/);
  if (m2) return int(m2[1]);
  if (pending) {
    const n = t.match(/\d[\d\s]*/);
    if (n) return int(n[0]);
  }
  return undefined;
}

function parseYears(t: string, pending: boolean): number | undefined {
  const m = t.match(/(\d)\s*(?:и\s*\d\s*)?(год|года|лет)/);
  if (m) return Math.min(5, Math.max(1, Number(m[1])));
  if (/бессрочн/.test(t)) return 1;
  if (pending && /(год|года)/.test(t)) return 1;
  return undefined;
}

function parseBudget(t: string, pending: boolean): Requirements['budget'] {
  if (pending && /(не огранич|без огранич|неважно|не важно)/.test(t)) return 'unlimited';
  const m = t.match(/(\d[\d\s]*)\s*(тыс|млн|k\b|к\b|₽|руб)/);
  if (!m || (!pending && !/бюджет/.test(t))) return undefined;
  const n = int(m[1]);
  return m[2].startsWith('млн') ? n * 1_000_000 : /^(тыс|k|к)/.test(m[2]) ? n * 1000 : n;
}

function parseInfra(t: string): Requirements['infrastructure'] {
  if (/(нет почт|почты нет|пока нет|почты пока нет)/.test(t)) return 'none';
  if (/(облач|google|яндекс|mail\.ru|360)/.test(t)) return 'cloud';
  if (/(свой|собствен|exchange|сервер|on-?prem|у себя)/.test(t)) return 'own_server';
  return undefined;
}

// ── Вопросы ───────────────────────────────────────────────────────────

function question(slot: Slot, r: Requirements, cat: Catalog): DraftMessage {
  const f = r.productId ? cat.byId.get(r.productId) : undefined;
  const unit = f?.unit;
  const q: Record<Slot, { text: string; options?: string[] }> = {
    needs: { text: 'Какую задачу нужно решить?', options: ['Защита ПК и файлов', 'Защита почты', 'Резервное копирование', 'Российская ОС'] },
    product: { text: 'Какой продукт вас интересует? Напишите название и редакцию, если знаете.', options: ['Kaspersky Endpoint Security для бизнеса Стандартный', 'Dr.Web Комплексная защита', 'MDaemon Messaging Server'] },
    users: {
      text:
        unit === 'mailbox' ? 'Сколько почтовых ящиков нужно?'
        : unit === 'device' ? 'Сколько устройств нужно защитить?'
        : unit === 'server' ? 'Сколько серверов (или ядер) нужно лицензировать?'
        : 'Сколько пользователей или рабочих мест нужно покрыть?',
      options: ['10', '25', '50', '100'],
    },
    infrastructure: { text: 'Где сейчас работает почта?', options: ['Свой почтовый сервер', 'Облачная почта', 'Почты пока нет'] },
    years: { text: 'На какой срок нужна лицензия?', options: ['1 год', '2 года', '3 года'] },
    budget: { text: 'Есть ориентир по бюджету?', options: ['до 300 тыс ₽', 'до 500 тыс ₽', 'Не ограничен'] },
  };
  return { sender: 'bot', intent: `ask_${slot}`, text: q[slot].text, quickReplies: q[slot].options };
}

function nextMissing(scenario: Ticket['scenario'], r: Requirements, cat: Catalog): Slot | undefined {
  if (scenario === 'known') {
    if (!r.productId) return 'product';
    if (!r.users) return 'users';
    const f = cat.byId.get(r.productId);
    const hasTerms = f?.variants.some((v) => v.kind === 'new' && v.term && v.term > 0);
    if (!r.years && hasTerms) return 'years';
    return undefined;
  }
  if (!r.needs?.length) return 'needs';
  if (!r.users) return 'users';
  if (r.needs.includes('mail') && !r.infrastructure) return 'infrastructure';
  if (!r.years) return 'years';
  if (r.budget === undefined) return 'budget';
  return undefined;
}

// ── Ответы ────────────────────────────────────────────────────────────

const productSource = (p: Product): Source => ({ kind: 'product', title: `Каталог Store: ${p.name}`, url: p.url });
const familySource = (f: Family): Source => ({ kind: 'product', title: `Каталог Store: ${f.title}`, url: f.variants[0]?.url });
const kbSource = (id: string): Source => {
  const a = kbById(id);
  return { kind: 'kb', title: `База знаний: ${a.title}`, url: a.url };
};
const uniqProducts = (offers: Offer[]) => [...new Map(offers.flatMap((o) => o.items.map((i) => [i.product.familyId, i.product] as const))).values()];
const shortTitle = (o: Offer) => o.title.replace(/ · .*/, '');

function recommend(cat: Catalog, r: Requirements, scenario: Ticket['scenario']): BotReply {
  if (scenario === 'known') {
    const f = cat.byId.get(r.productId!)!;
    const offers = knownOffers(cat, f, r);
    if (!offers.length) {
      return {
        handoff: { reason: `Нет подходящей позиции ${f.title} в онлайн-каталоге`, confidence: 0.4 },
        messages: [{ sender: 'bot', intent: 'handoff', text: `${f.title} есть в каталоге, но не под эти параметры. Передаю менеджеру — он подготовит расчёт.` }],
      };
    }
    const multi = offers.length > 1;
    const head = `Сверил по каталогу Store: ${f.vendor} · ${f.title}. Расчёт на ${unitCount(r.users!, f.unit)}${r.years ? `, срок — ${years(r.years)}` : ''}${multi ? ', рядом — соседние редакции:' : ':'}`;
    return {
      addTags: ['Лицензирование'],
      patch: { lastOffers: offers, selectedOfferId: undefined },
      messages: [
        { sender: 'bot', intent: 'offers', text: head, offers, sources: offers.map((o) => productSource(o.items[0].product)) },
        multi
          ? {
              sender: 'bot', intent: 'compare',
              text: 'Чем отличаются по каталогу:',
              compare: compareOffers(offers),
              quickReplies: [...offers.map((_o, i) => `Выбираю вариант ${i + 1}`).slice(0, 3), 'Есть российский аналог?', 'Сформировать КП'],
            }
          : { sender: 'bot', intent: 'next', text: 'Подготовить коммерческое предложение?', quickReplies: ['Сформировать КП', 'Есть российский аналог?', 'Изменить количество'] },
      ],
    };
  }
  const { offers, missing } = discoverOffers(cat, r);
  if (!offers.length) {
    return {
      handoff: { reason: 'Под требования не нашлось позиций в онлайн-каталоге', confidence: 0.35 },
      messages: [{ sender: 'bot', intent: 'handoff', text: 'В онлайн-каталоге не нашлось готовых позиций под эти требования. Передаю менеджеру — он подберёт решение под заказ.' }],
    };
  }
  const rec = offers.find((o) => o.recommended);
  const miss = missing.length ? ` По задаче «${missing.join(', ')}» в каталоге ничего не нашлось — уточнит менеджер.` : '';
  return {
    addTags: ['Подбор ПО'],
    patch: { lastOffers: offers, selectedOfferId: undefined },
    messages: [
      {
        sender: 'bot', intent: 'offers',
        text: `Подобрал ${offers.length > 1 ? '2 варианта' : 'вариант'} по каталогу Store на ${unitCount(r.users ?? 1, 'user')}, срок — ${years(r.years ?? 1)}.${rec && offers.length > 1 ? ` Рекомендую ${shortTitle(rec).toLowerCase()} — обоснование в карточках.` : ''}${miss}`,
        offers,
        sources: uniqProducts(offers).map(productSource),
      },
      offers.length > 1
        ? { sender: 'bot', intent: 'compare', text: 'Коротко в сравнении:', compare: compareBundles(offers), quickReplies: ['Выбираю вариант 1', 'Выбираю вариант 2', 'Есть российский аналог?', 'Сформировать КП'] }
        : { sender: 'bot', intent: 'next', text: 'Подготовить коммерческое предложение?', quickReplies: ['Сформировать КП', 'Изменить количество'] },
    ],
  };
}

function analogReply(cat: Catalog, ticket: Ticket, mention: Mention): BotReply {
  const offers = ticket.lastOffers ?? [];
  const r = ticket.requirements;
  const base: Family[] = mention.family
    ? [mention.family]
    : uniqProducts(offers).map((p) => cat.byId.get(p.familyId)).filter((f): f is Family => !!f);
  if (!base.length) {
    return { patch: { scenario: 'known', pendingSlot: 'product' }, messages: [{ sender: 'bot', intent: 'ask_product', text: 'Для какого продукта ищем российский аналог?', quickReplies: ['MDaemon', 'Microsoft Office'] }] };
  }
  const foreign = base.filter((f) => !f.ru);
  if (!foreign.length) {
    const reg = base.filter((f) => f.registry);
    return {
      messages: [{
        sender: 'bot', intent: 'analog',
        text: [
          `${joinAnd(base.map((f) => f.title))} — ${base.length > 1 ? 'продукты российских разработчиков' : 'продукт российского разработчика'}.`,
          reg.length ? `В реестре российского ПО по данным каталога: ${reg.map((f) => `${f.title} (№ ${f.registry})`).join(', ')}.` : 'Номер записи в реестре в каталоге не указан — проверит менеджер, это влияет на НДС.',
        ].join(' '),
        sources: [...base.map(familySource), kbSource('kb-vat')],
        quickReplies: ['Сформировать КП', 'Позвать менеджера'],
      }],
    };
  }
  const qty = r.users ?? 10;
  const y = r.years ?? 1;
  const pairs = foreign.map((f) => [f, russianAnalogs(cat, f, qty, y)] as const);
  const lines = pairs.map(([f, a]) =>
    a.length ? `• ${f.title} → ${a.map((x) => `${x.family.title} (${x.family.vendor}${x.family.registry ? `, реестр № ${x.family.registry}` : ''})`).join(' или ')}` : `• ${f.title} — российского аналога в онлайн-каталоге нет`,
  );
  const analogs = pairs.flatMap(([, a]) => a);
  const fullRu = offers.filter((o) => o.items.every((i) => i.product.russianVendor)).map(shortTitle);
  return {
    messages: [{
      sender: 'bot', intent: 'analog',
      text: [
        fullRu.length && !mention.family ? `${fullRu.join(', ')} уже полностью из российского ПО.` : '',
        `Российские аналоги из каталога:\n${lines.join('\n')}`,
        analogs.length ? 'Цены посчитал под ваше количество и срок.' : '',
      ].filter(Boolean).join('\n\n'),
      products: analogs.map((a) => itemFrom(a).product),
      sources: [...foreign.map(familySource), ...analogs.map((a) => familySource(a.family))],
      quickReplies: offers.length && analogs.length ? ['Заменить на аналог', 'Оставить как есть', 'Сформировать КП'] : ['Помогите подобрать', 'Позвать менеджера'],
    }],
  };
}

function chooseOffer(t: string, offers: Offer[]): Offer | undefined {
  const n = t.match(/вариант\s*(\d)/);
  if (n) return offers[Number(n[1]) - 1];
  // Сначала самые длинные названия: «Расширенный Плюс» не должен совпасть как «Расширенный»
  const byName = [...offers].sort((a, b) => b.items[0].product.name.length - a.items[0].product.name.length);
  return byName.find((o) => t.includes(norm(o.items[0].product.name))) ?? offers.find((o) => o.recommended) ?? offers[0];
}

function productAnswer(f: Family, t: string): DraftMessage {
  const kb = findKb(t);
  const fresh = f.variants.filter((v) => v.kind === 'new');
  const kinds = [...new Set(f.variants.map((v) => KIND_LABEL[v.kind]))];
  const terms = [...new Set(fresh.map((v) => termText(v.term)))];
  const cheapest = [...(fresh.length ? fresh : f.variants)].sort((a, b) => a.price - b.price)[0];
  const p = toProduct(f, cheapest);
  return {
    sender: 'bot', intent: 'product_info',
    text: [
      `${f.title} (${f.vendor}) — раздел каталога «${f.path.slice(-2).join(' › ')}».`,
      f.platforms ? `Платформы: ${f.platforms}.` : '',
      `В каталоге: ${kinds.join(', ').toLowerCase()}${terms.length ? `; сроки — ${terms.join(', ')}` : ''}.`,
      `Цена — от ${formatPrice(cheapest.price)}${cheapest.mode === 'pack' && cheapest.min ? ` за ${unitCount(cheapest.min, f.unit)}` : ''}.`,
      f.registry ? `В реестре российского ПО, № ${f.registry}.` : f.ru ? 'Российский разработчик.' : 'Зарубежный разработчик.',
    ].filter(Boolean).join('\n'),
    products: [p],
    sources: [familySource(f), ...(kb ? [kbSource(kb.id)] : [])],
    quickReplies: [`Рассчитать ${f.title}`.slice(0, 60), 'Есть российский аналог?', 'Позвать менеджера'],
  };
}

function askWhichProduct(mention: Mention): DraftMessage {
  return {
    sender: 'bot', intent: 'ask_product',
    text: `У ${mention.vendor} в каталоге несколько продуктов. Какой нужен?`,
    quickReplies: (mention.vendorFamilies ?? []).map((f) => f.title.slice(0, 60)),
  };
}

// ── Главная функция ───────────────────────────────────────────────────

export function generateReply(cat: Catalog, ticket: Ticket, rawText: string, history: Message[]): BotReply {
  const t = norm(rawText);
  const r: Requirements = { ...ticket.requirements };
  const pending = ticket.pendingSlot;
  const offers = ticket.lastOffers ?? [];

  if (RE.operator.test(t) && !/^позвать менеджера$/.test(t) && RE.quote.test(t)) {
    // «менеджер, пришлите КП» — обрабатываем как запрос КП ниже
  } else if (RE.operator.test(t)) {
    return { handoff: { reason: 'Клиент попросил менеджера', confidence: 0.7 }, messages: [] };
  }
  if (RE.negative.test(t)) {
    return {
      sentiment: 'negative', addTags: ['Негатив'],
      handoff: { reason: 'Клиент недоволен, нужна помощь менеджера', confidence: 0.35 },
      messages: [{ sender: 'bot', intent: 'handoff', text: 'Понимаю, приношу извинения. Подключаю менеджера — он поможет разобраться.' }],
    };
  }
  if (RE.gov.test(t)) {
    return {
      addTags: ['Госзакупки'],
      handoff: { reason: 'Закупка для госоргана — нужна проверка условий и документов', confidence: 0.45 },
      messages: [{ sender: 'bot', intent: 'handoff', text: 'Для государственных и бюджетных закупок условия и документы готовит менеджер. Передаю ему диалог вместе с вашими требованиями.' }],
    };
  }
  if (RE.discount.test(t)) {
    return {
      addTags: ['Скидка'],
      handoff: { reason: 'Запрос скидки или спецусловий — решает менеджер', confidence: 0.45 },
      messages: [{ sender: 'bot', intent: 'handoff', text: 'Скидки и специальные условия согласует менеджер. Передаю ему диалог вместе с вашими требованиями.' }],
    };
  }

  // Стартовые кнопки
  if (RE.startKnown.test(t)) return { patch: { scenario: 'known', pendingSlot: 'product' }, messages: [question('product', r, cat)] };
  if (RE.startQuestion.test(t)) {
    return {
      patch: { scenario: 'question', pendingSlot: undefined },
      messages: [{ sender: 'bot', intent: 'ask_question', text: 'Задайте вопрос — отвечу по каталогу и базе знаний Softline, со ссылкой на источник.', quickReplies: ['Цены указаны с НДС?', 'Как доставляются лицензии?', 'Можно купить как физлицо?'] }],
    };
  }

  // КП
  if (RE.quote.test(t)) {
    if (ticket.quoteId) {
      return { messages: [{ sender: 'bot', intent: 'quote_exists', text: 'КП по этому диалогу уже сформировано и ждёт проверки менеджером. Если нужно изменить состав — напишите, что поменять.' }] };
    }
    if (!offers.length) {
      const m = findMentioned(cat, rawText);
      if (!m.family && !m.vendor && !parseNeeds(expand(t)).length) {
        return { messages: [{ sender: 'bot', intent: 'quote_no_offer', text: 'Подготовлю КП, как только подберём позиции. Какой продукт нужен и в каком количестве?', quickReplies: START_REPLIES.slice(0, 2) }], patch: { scenario: 'known', pendingSlot: 'product' } };
      }
      // «Пришлите счёт на Касперского на 5 компьютеров» — сначала уточним продукт и посчитаем
    } else {
      const sel = offers.find((o) => o.id === ticket.selectedOfferId) ?? offers.find((o) => o.recommended) ?? offers[0];
      return {
        addTags: ['КП'],
        patch: { selectedOfferId: sel.id, pendingSlot: undefined },
        messages: [{
          sender: 'bot', intent: 'quote_form',
          text: `Подготовлю КП по шаблону Softline: ${shortTitle(sel)} — ${formatPrice(sel.total)}. Заполните данные компании — черновик проверит менеджер и пришлёт на email.`,
          form: 'quote_request',
        }],
      };
    }
  }

  if (RE.replace.test(t) && offers.length) {
    const next = replaceWithAnalogs(cat, offers, r);
    return {
      patch: { lastOffers: next },
      messages: [{
        sender: 'bot', intent: 'offers_analog',
        text: 'Обновил варианты — зарубежное ПО заменено российскими аналогами из каталога:',
        offers: next,
        sources: uniqProducts(next).map(productSource),
        quickReplies: ['Выбираю вариант 1', 'Выбираю вариант 2', 'Сформировать КП'],
      }],
    };
  }

  if (RE.keep.test(t)) {
    return { messages: [{ sender: 'bot', intent: 'keep', text: 'Хорошо, оставляем варианты без изменений. Какой выбираете?', quickReplies: ['Выбираю вариант 1', 'Выбираю вариант 2', 'Сформировать КП'] }] };
  }

  if (RE.choose.test(t) && offers.length && !/рассчитать/.test(t)) {
    const o = chooseOffer(t, offers);
    if (o) {
      return {
        patch: { selectedOfferId: o.id },
        messages: [{
          sender: 'bot', intent: 'chosen',
          text: `Фиксирую: ${shortTitle(o)} — ${formatPrice(o.total)}. Подготовить коммерческое предложение?`,
          quickReplies: ['Сформировать КП', 'Изменить количество', 'Позвать менеджера'],
        }],
      };
    }
  }

  const mention = findMentioned(cat, rawText);

  if (RE.analog.test(t) && !/ндс/.test(t)) return analogReply(cat, ticket, mention);

  if (RE.compare.test(t) && offers.length > 1) {
    const sameProduct = offers.every((o) => o.items.length === 1);
    return {
      messages: [{ sender: 'bot', intent: 'compare', text: 'Сравнение вариантов:', compare: sameProduct ? compareOffers(offers) : compareBundles(offers), quickReplies: ['Выбираю вариант 1', 'Выбираю вариант 2', 'Сформировать КП'] }],
    };
  }

  if (RE.changeQty.test(t)) return { patch: { pendingSlot: 'users' }, messages: [question('users', r, cat)] };

  if (RE.individual.test(t)) {
    return { addTags: ['Физлицо'], messages: [{ sender: 'bot', intent: 'kb', text: kbById('kb-individual').answer, sources: [kbSource('kb-individual')], quickReplies: ['Помогите подобрать', 'Позвать менеджера'] }] };
  }

  // «Можно ли у вас купить Power BI?» — если в каталоге нет, честно говорим и зовём менеджера
  if (RE.availability.test(t) && !mention.family && !mention.vendor && !pending && !parseNeeds(expand(t)).length && !ticket.lastOffers?.length) {
    return {
      addTags: ['Доступность вендора'],
      handoff: { reason: 'Продукта нет в онлайн-каталоге — проверить возможность поставки', confidence: 0.5 },
      messages: [{ sender: 'bot', intent: 'availability', text: 'Сейчас этого продукта нет в онлайн-каталоге Store. Передаю менеджеру — он проверит, можно ли поставить его под заказ или предложить аналог.' }],
    };
  }

  if (mention.family && RE.productQuestion.test(t) && !pending) {
    return { addTags: ['Вопрос о продукте'], patch: { scenario: ticket.scenario ?? 'question' }, messages: [productAnswer(mention.family, t)] };
  }

  // ── Заполнение требований ──
  let scenario = ticket.scenario;
  const before = JSON.stringify(r);

  if (pending === 'infrastructure') r.infrastructure = parseInfra(t) ?? r.infrastructure;
  if (mention.family) {
    r.productId = mention.family.id;
    r.productName = `${mention.family.title}`;
    scenario = 'known';
  }
  if (scenario !== 'known' && pending !== 'infrastructure') {
    const needs = parseNeeds(expand(t));
    if (needs.length) {
      r.needs = [...new Set([...(r.needs ?? []), ...needs])];
      scenario = 'discover';
    }
  }
  if (RE.startDiscover.test(t) || /не знаю/.test(t)) scenario = scenario === 'known' ? scenario : 'discover';
  r.users = parseUsers(t, pending === 'users') ?? r.users;
  r.years = parseYears(t, pending === 'years') ?? r.years;
  const b = parseBudget(t, pending === 'budget');
  if (b !== undefined) r.budget = b;
  if (pending !== 'infrastructure' && /почт/.test(t)) r.infrastructure = parseInfra(t) ?? r.infrastructure;

  const changed = JSON.stringify(r) !== before;
  const kbEarly = changed ? undefined : findKb(t);

  // Назван вендор и задача («счёт на Касперского на 5 компьютеров») — берём профильный продукт этого вендора
  if (!mention.family && mention.vendor && !r.productId && r.needs?.length && r.users) {
    const need = r.needs[0];
    const pick = candidates(cat, need, r.users, r.years ?? 1).find((x) => x.family.vendor === mention.vendor);
    if (pick) {
      r.productId = pick.family.id;
      r.productName = pick.family.title;
      scenario = 'known';
    }
  }

  // Назван только вендор («85 лицензий касперского») — уточняем продукт
  if (!mention.family && mention.vendor && !r.productId && (scenario === 'known' || !r.needs?.length)) {
    return { patch: { scenario: 'known', requirements: r, pendingSlot: 'product' }, messages: [askWhichProduct(mention)] };
  }

  if ((scenario === 'known' || scenario === 'discover') && !kbEarly && (changed || !offers.length)) {
    const next = nextMissing(scenario, r, cat);
    const tags = scenario === 'known' ? ['Лицензирование'] : ['Подбор ПО'];
    if (next && !changed && pending === next) {
      const q = question(next, r, cat);
      return { messages: [{ ...q, text: `Не совсем понял ответ. ${q.text}` }] };
    }
    if (next) {
      const q = question(next, r, cat);
      const firstInfo = changed && !pending;
      const f = r.productId ? cat.byId.get(r.productId) : undefined;
      const ack =
        firstInfo && scenario === 'discover' && r.needs?.length
          ? `Понял: нужно закрыть ${joinAnd(r.needs.map((n) => NEED_ACC[n]))}${r.users ? ` на ${unitCount(r.users, 'user')}` : ''}. Уточню пару деталей, чтобы подобрать точно.\n\n`
          : firstInfo && scenario === 'known' && f
            ? `${f.vendor} · ${f.title} — есть в каталоге Store (${f.variants.length} позиций). Уточню параметры лицензии.\n\n`
            : '';
      return { addTags: tags, patch: { scenario, requirements: r, pendingSlot: next }, messages: [{ ...q, text: ack + q.text }] };
    }
    const rec = recommend(cat, r, scenario);
    return { ...rec, addTags: [...(rec.addTags ?? []), ...tags], patch: { ...rec.patch, scenario, requirements: r, pendingSlot: undefined } };
  }

  // База знаний
  const kb = findKb(t);
  if (kb) {
    return {
      addTags: ['Вопрос о покупке'],
      messages: [{ sender: 'bot', intent: 'kb', text: kb.answer, sources: [kbSource(kb.id)], quickReplies: ['Помогите подобрать', 'Позвать менеджера'] }],
    };
  }
  if (mention.family) return { messages: [productAnswer(mention.family, t)] };
  if (mention.vendor) return { patch: { scenario: 'known', pendingSlot: 'product' }, messages: [askWhichProduct(mention)] };

  if (RE.renewal.test(t)) {
    return { messages: [{ sender: 'bot', intent: 'renewal', text: 'Помогу с продлением. Какой продукт продлеваем и на сколько устройств?', quickReplies: ['Kaspersky Endpoint Security для бизнеса Стандартный', 'Dr.Web Комплексная защита'] }], patch: { scenario: 'known', pendingSlot: 'product' } };
  }
  if (RE.greet.test(t)) return { messages: [{ ...GREETING, text: 'Здравствуйте! Чем помочь?' }] };
  if (RE.thanks.test(t)) return { messages: [{ sender: 'bot', intent: 'thanks', text: 'Рад помочь! Если появятся вопросы — пишите.', quickReplies: START_REPLIES.slice(0, 3) }] };

  const lastBot = [...history].reverse().find((m) => m.sender === 'bot');
  if (lastBot?.intent === 'fallback') {
    return {
      handoff: { reason: 'ИИ не смог распознать запрос', confidence: 0.2 },
      messages: [{ sender: 'bot', intent: 'handoff', text: 'Кажется, я не до конца понимаю вопрос. Подключаю менеджера.' }],
    };
  }
  return {
    messages: [{
      sender: 'bot', intent: 'fallback',
      text: 'Не уверен, что правильно понял. Назовите продукт или опишите задачу — или выберите вариант:',
      quickReplies: START_REPLIES,
    }],
  };
}

export function buildSummary(ticket: Ticket, history: Message[], reason?: string): string {
  const req = requirementRows(ticket.requirements, ticket.scenario)
    .filter((x) => x.value)
    .map((x) => `${x.label.toLowerCase()}: ${x.value}`)
    .join('; ');
  const last = [...history].reverse().find((m) => m.sender === 'user')?.text ?? '';
  const quote = last.length > 110 ? last.slice(0, 107) + '…' : last;
  const sel = ticket.lastOffers?.find((o) => o.id === ticket.selectedOfferId) ?? ticket.lastOffers?.find((o) => o.recommended);
  return [
    req && `Требования — ${req}.`,
    sel && `ИИ предложил: ${shortTitle(sel)} (${sel.items.map((i) => i.product.name).join(' + ')}) — ${formatPrice(sel.total)}.`,
    quote && `Последнее сообщение: «${quote}».`,
    reason && `${reason}.`,
  ].filter(Boolean).join(' ');
}

export function suggestReply(ticket: Ticket, operatorName: string): string {
  const hello = `Здравствуйте! Меня зовут ${operatorName}${operatorName.endsWith('.') ? '' : '.'}`;
  if (ticket.tags.includes('Скидка')) {
    return `${hello} Запрошу у вендора условия для вашего объёма и вернусь с расчётом сегодня до 18:00. Подскажите, это новая покупка или переход с другого продукта?`;
  }
  if (ticket.tags.includes('Госзакупки')) {
    return `${hello} Подготовлю КП и пакет документов для закупки. Подскажите, по какому закону проводится закупка (44-ФЗ или 223-ФЗ) и нужен ли номер записи в реестре российского ПО?`;
  }
  if (ticket.tags.includes('Доступность вендора')) {
    return `${hello} Проверю возможность поставки у вендора и вернусь с ответом в течение рабочего дня. Если поставка невозможна, предложу аналог.`;
  }
  const sel = ticket.lastOffers?.find((o) => o.id === ticket.selectedOfferId) ?? ticket.lastOffers?.find((o) => o.recommended);
  if (ticket.quoteId) return `${hello} Проверила черновик КП — всё корректно, отправляю его на вашу почту. Если понадобится счёт или договор — подготовлю.`;
  if (sel) {
    return `${hello} Посмотрела ваш запрос: ${shortTitle(sel)} на ${formatPrice(sel.total)} подходит под ваши требования. Подготовить коммерческое предложение?`;
  }
  return `${hello} Я менеджер Store, помогу с выбором и оформлением. Расскажите, пожалуйста, подробнее о задаче.`;
}

export { priceFor };
