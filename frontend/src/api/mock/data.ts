import type { Message, Quote, Ticket } from '../types';
import { buildSummary, generateReply, GREETING, type DraftMessage } from './bot';
import { findMentioned, type Catalog } from './catalog';
import { buildQuote, knownOffers } from './offers';

export interface DB {
  tickets: Ticket[];
  messages: Message[];
  quotes: Quote[];
  seq: number;
  quoteSeq: number;
}

const ago = (min: number) => new Date(Date.now() - min * 60_000).toISOString();

/**
 * Демо-обращения: реальные (обезличенные) реплики клиентов из переписок поддержки,
 * прогнанные через того же бота, что отвечает в чате. Номера обращений — из переписок.
 */
export function createSeed(cat: Catalog): DB {
  const db: DB = { tickets: [], messages: [], quotes: [], seq: 103100, quoteSeq: 1 };
  let mid = 0;
  let clock = 0;

  const add = (t: Ticket, d: DraftMessage, min: number) => {
    const m: Message = { ...d, id: `seed-${++mid}`, ticketId: t.id, createdAt: ago(min) };
    db.messages.push(m);
    t.updatedAt = m.createdAt;
    if (m.sender !== 'system') t.lastMessage = m.text;
    return m;
  };

  const ticket = (num: number, name: string, company: string | undefined, startMin: number, channel: Ticket['channel'] = 'web'): Ticket => {
    const t: Ticket = {
      id: `t-${num}`, number: num, channel, status: 'ai', createdAt: ago(startMin), updatedAt: ago(startMin),
      tags: [], sentiment: 'neutral', requirements: {}, customer: { id: `c-${num}`, name, company },
    };
    db.tickets.push(t);
    add(t, GREETING, startMin);
    clock = startMin;
    return t;
  };

  /** Сообщение клиента → ответ бота (как в живом чате) */
  const say = (t: Ticket, text: string, gap = 1) => {
    clock -= gap;
    add(t, { sender: 'user', text }, clock);
    const reply = generateReply(cat, t, text, db.messages.filter((m) => m.ticketId === t.id));
    Object.assign(t, reply.patch ?? {});
    if (reply.sentiment) t.sentiment = reply.sentiment;
    for (const tag of reply.addTags ?? []) if (!t.tags.includes(tag)) t.tags.push(tag);
    clock -= 0.3;
    for (const m of reply.messages) add(t, m, clock);
    if (reply.handoff) {
      if (!reply.messages.length) add(t, { sender: 'bot', intent: 'handoff', text: 'Подключаю менеджера — обычно это занимает 1–2 минуты. Он увидит всю историю и ваши требования.' }, clock);
      t.status = 'waiting';
      t.handoffAt = ago(clock);
      t.handoffReason = reply.handoff.reason;
      t.aiConfidence = reply.handoff.confidence;
      t.pendingSlot = undefined;
      add(t, { sender: 'system', text: 'ИИ передал диалог оператору' }, clock);
    }
    t.aiSummary = buildSummary(t, db.messages.filter((m) => m.ticketId === t.id), t.handoffReason);
  };

  const operator = (t: Ticket, text: string, gap = 2) => {
    clock -= gap;
    if (t.status !== 'operator') {
      t.status = 'operator';
      t.assignee = 'Анна К.';
      add(t, { sender: 'system', text: 'В чате оператор: Анна К.' }, clock + 0.5);
    }
    add(t, { sender: 'operator', text, authorName: 'Анна К.' }, clock);
  };

  const quote = (t: Ticket, company: string, email: string, status: Quote['status'], gap = 3) => {
    const offer = t.lastOffers?.find((o) => o.id === t.selectedOfferId) ?? t.lastOffers?.find((o) => o.recommended) ?? t.lastOffers?.[0];
    if (!offer) return;
    clock -= gap;
    const q = buildQuote(t, offer, { company, email, paymentTerms: 'Постоплата' }, db.quoteSeq++);
    q.createdAt = ago(clock);
    q.status = status;
    if (status === 'sent') q.reviewer = 'Анна К.';
    db.quotes.push(q);
    t.quoteId = q.id;
    t.customer = { ...t.customer, company, email };
    if (!t.tags.includes('КП')) t.tags.push('КП');
    const form = [...db.messages].reverse().find((m) => m.ticketId === t.id && m.form);
    if (form) form.formDone = true;
    add(t, { sender: 'bot', intent: 'quote_created', quoteId: q.id, text: `Готово! Черновик ${q.number} сформирован по шаблону и передан менеджеру на проверку. После согласования он придёт на ${email}.` }, clock);
    if (status === 'sent') add(t, { sender: 'system', text: `${q.number} согласовано и отправлено на ${email}` }, clock - 20);
  };

  // 1. Знает продукт, сравнивает редакции (#102788)
  const t1 = ticket(102788, 'Клиент сайта', undefined, 16);
  say(t1, 'Здравствуйте! Мы хотим купить на фирму 85 лицензий Kaspersky Endpoint Security для бизнеса Стандартный. Сколько это будет стоить? А какие есть и в чём разница?');
  say(t1, '1 год');

  // 2. Точный запрос → КП на проверке (#102917)
  const t2 = ticket(102917, 'Клиент сайта', 'ООО «Интеллектуальные системы управления»', 60);
  say(t2, 'Добрый день, интересует возможность приобретения лицензии Kaspersky Endpoint Security для бизнеса Расширенный сроком на 3 года для 100 пользователей');
  say(t2, 'Сформировать КП', 6);
  quote(t2, 'ООО «Интеллектуальные системы управления»', 'zakupki@example.com', 'review');

  // 3. Скидка при переходе с другого антивируса → ждёт менеджера (#103024)
  const t3 = ticket(103024, 'Сергей', undefined, 12);
  say(t3, 'Как получить счёт на антивирус Касперского со скидкой в связи с переходом с Dr.Web? Требуется Kaspersky Endpoint Security, 20 шт.');

  // 4. Госзакупка → ждёт менеджера (#102814)
  const t4 = ticket(102814, 'Ольга', 'ГАУ АО «УИКТ»', 9, 'email');
  say(t4, 'Добрый день! Работаю в ГАУ, закупка ПО для органов власти. Поступила заявка на закупку 300 лицензий ContentReader PDF 16. Возможно ли закупить конкурентные лицензии в количестве больше 25 штук?');

  // 5. Российская ОС на 500 устройств — ИИ уточняет (#103033)
  const t5 = ticket(103033, 'Клиент сайта', undefined, 7);
  say(t5, 'Какие лицензии на операционную систему РЕД ОС требуется закупить для её использования на планшетах в количестве 500 шт?');

  // 6. Вопрос о покупке — ответ из базы знаний (#103018)
  const t6 = ticket(103018, 'Клиент сайта', undefined, 25);
  say(t6, 'Добрый день, подскажите, цены с НДС указаны?');
  const kbMsg = [...db.messages].reverse().find((m) => m.ticketId === t6.id && m.sender === 'bot');
  if (kbMsg) kbMsg.feedback = 'up';

  // 7. Продукта нет в каталоге → менеджер ответил, закрыто (#103028)
  const t7 = ticket(103028, 'Клиент сайта', undefined, 140);
  say(t7, 'Добрый день! У вас возможно приобрести лицензию MS Power BI?');
  operator(t7, 'Здравствуйте! Сейчас Power BI недоступен для заказа в России. Могу предложить российские BI-системы — подобрать варианты?', 5);
  add(t7, { sender: 'system', text: 'Обращение закрыто' }, clock - 30);
  t7.status = 'closed';

  // 8. Госзакупка с готовым КП, отправлено (#101638)
  const t8 = ticket(101638, 'Клиент сайта', 'МБУ «Центр ИТ»', 300);
  say(t8, 'Прошу дать коммерческое предложение на RuDesktop, удалённый доступ, подписка 1 год, в количестве 7 штук. Закупка будет конкурентная.');
  const rdm = findMentioned(cat, 'RuDesktop');
  const rd = rdm.family ?? rdm.vendorFamilies?.[0];
  if (rd) {
    t8.requirements = { productId: rd.id, productName: rd.title, users: 7, years: 1 };
    t8.scenario = 'known';
    t8.lastOffers = knownOffers(cat, rd, t8.requirements).slice(0, 1);
    t8.selectedOfferId = t8.lastOffers[0]?.id;
    operator(t8, 'Здравствуйте! Подготовила КП на RuDesktop, 7 подключений на 1 год — отправлю на почту после согласования.', 10);
    quote(t8, 'МБУ «Центр ИТ»', 'it-center@example.com', 'sent', 5);
    add(t8, { sender: 'system', text: 'Обращение закрыто' }, clock - 40);
    t8.status = 'closed';
  }

  return db;
}
