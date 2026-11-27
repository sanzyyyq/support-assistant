package agent

import (
	"fmt"
	"time"

	"support-assistant/internal/model"
)

// DB — всё, что хранит сервер про обращения (файл db.json на томе /data).
type DB struct {
	Tickets  []*model.Ticket  `json:"tickets"`
	Messages []*model.Message `json:"messages"`
	Quotes   []*model.Quote   `json:"quotes"`
	// Следующий номер обращения и КП
	Seq      int `json:"seq"`
	QuoteSeq int `json:"quoteSeq"`
}

// TicketMessages — сообщения одного обращения (копии, в порядке добавления).
func (db *DB) TicketMessages(ticketID string) []model.Message {
	var out []model.Message
	for _, m := range db.Messages {
		if m.TicketID == ticketID {
			out = append(out, *m)
		}
	}
	return out
}

// ApplyReply — применить ответ агента к обращению: изменения, настроение, метки.
func ApplyReply(t *model.Ticket, reply Reply) {
	if reply.Patch != nil {
		reply.Patch(t)
	}
	if reply.Sentiment != "" {
		t.Sentiment = reply.Sentiment
	}
	for _, tag := range reply.AddTags {
		t.AddTag(tag)
	}
}

// CreateSeed — демо-обращения: реальные (обезличенные) реплики клиентов из переписок поддержки,
// прогнанные через того же агента, что отвечает в чате. Номера обращений — из переписок.
func CreateSeed(cat *Catalog, now time.Time) *DB {
	db := &DB{Seq: 103100, QuoteSeq: 1}
	mid := 0
	clock := 0.0
	ago := func(min float64) string { return ISO(now.Add(-time.Duration(min * float64(time.Minute)))) }

	add := func(t *model.Ticket, d Draft, min float64) *model.Message {
		mid++
		m := d
		m.ID = fmt.Sprintf("seed-%d", mid)
		m.TicketID = t.ID
		m.CreatedAt = ago(min)
		db.Messages = append(db.Messages, &m)
		t.UpdatedAt = m.CreatedAt
		if m.Sender != "system" {
			t.LastMessage = m.Text
		}
		return &m
	}

	ticket := func(num int, name, company string, startMin float64, channel string) *model.Ticket {
		t := &model.Ticket{
			ID: fmt.Sprintf("t-%d", num), Number: num, Channel: channel, Status: "ai", CreatedAt: ago(startMin), UpdatedAt: ago(startMin),
			Tags: []string{}, Sentiment: "neutral", Customer: model.Customer{ID: fmt.Sprintf("c-%d", num), Name: name, Company: company},
		}
		db.Tickets = append(db.Tickets, t)
		add(t, Greeting(), startMin)
		clock = startMin
		return t
	}

	// Сообщение клиента → ответ агента (как в живом чате)
	say := func(t *model.Ticket, text string, gap float64) {
		clock -= gap
		add(t, Draft{Sender: "user", Text: text}, clock)
		reply := GenerateReply(cat, t, text, db.TicketMessages(t.ID))
		ApplyReply(t, reply)
		clock -= 0.3
		for _, m := range reply.Messages {
			add(t, m, clock)
		}
		if reply.Handoff != nil {
			if len(reply.Messages) == 0 {
				add(t, Draft{Sender: "bot", Intent: "handoff", Text: HandoffText}, clock)
			}
			conf := reply.Handoff.Confidence
			t.Status = "waiting"
			t.HandoffAt = ago(clock)
			t.HandoffReason = reply.Handoff.Reason
			t.AIConfidence = &conf
			t.PendingSlot = ""
			add(t, Draft{Sender: "system", Text: "ИИ передал диалог оператору"}, clock)
		}
		t.AISummary = BuildSummary(t, db.TicketMessages(t.ID), t.HandoffReason)
	}

	operator := func(t *model.Ticket, text string, gap float64) {
		clock -= gap
		if t.Status != "operator" {
			t.Status = "operator"
			t.Assignee = "Анна К."
			add(t, Draft{Sender: "system", Text: "В чате оператор: Анна К."}, clock+0.5)
		}
		add(t, Draft{Sender: "operator", Text: text, AuthorName: "Анна К."}, clock)
	}

	quote := func(t *model.Ticket, company, email, status string, gap float64) {
		offer := t.SelectedOffer()
		if offer == nil {
			return
		}
		clock -= gap
		q := BuildQuote(t, *offer, model.QuoteRequest{Company: company, Email: email, PaymentTerms: "Постоплата"}, db.QuoteSeq, now)
		db.QuoteSeq++
		q.CreatedAt = ago(clock)
		q.Status = status
		if status == "sent" {
			q.Reviewer = "Анна К."
		}
		db.Quotes = append(db.Quotes, &q)
		t.QuoteID = q.ID
		t.Customer.Company, t.Customer.Email = company, email
		t.AddTag("КП")
		for i := len(db.Messages) - 1; i >= 0; i-- {
			if m := db.Messages[i]; m.TicketID == t.ID && m.Form != "" {
				m.FormDone = true
				break
			}
		}
		add(t, Draft{Sender: "bot", Intent: "quote_created", QuoteID: q.ID,
			Text: "Готово! Черновик " + q.Number + " сформирован по шаблону и передан менеджеру на проверку. После согласования он придёт на " + email + "."}, clock)
		if status == "sent" {
			add(t, Draft{Sender: "system", Text: q.Number + " согласовано и отправлено на " + email}, clock-20)
		}
	}

	// 1. Знает продукт, сравнивает редакции (#102788)
	t1 := ticket(102788, "Клиент сайта", "", 16, "web")
	say(t1, "Здравствуйте! Мы хотим купить на фирму 85 лицензий Kaspersky Endpoint Security для бизнеса Стандартный. Сколько это будет стоить? А какие есть и в чём разница?", 1)
	say(t1, "1 год", 1)

	// 2. Точный запрос → КП на проверке (#102917)
	t2 := ticket(102917, "Клиент сайта", "ООО «Интеллектуальные системы управления»", 60, "web")
	say(t2, "Добрый день, интересует возможность приобретения лицензии Kaspersky Endpoint Security для бизнеса Расширенный сроком на 3 года для 100 пользователей", 1)
	say(t2, "Сформировать КП", 6)
	quote(t2, "ООО «Интеллектуальные системы управления»", "zakupki@example.com", "review", 3)

	// 3. Скидка при переходе с другого антивируса → ждёт менеджера (#103024)
	t3 := ticket(103024, "Сергей", "", 12, "web")
	say(t3, "Как получить счёт на антивирус Касперского со скидкой в связи с переходом с Dr.Web? Требуется Kaspersky Endpoint Security, 20 шт.", 1)

	// 4. Госзакупка → ждёт менеджера (#102814)
	t4 := ticket(102814, "Ольга", "ГАУ АО «УИКТ»", 9, "email")
	say(t4, "Добрый день! Работаю в ГАУ, закупка ПО для органов власти. Поступила заявка на закупку 300 лицензий ContentReader PDF 16. Возможно ли закупить конкурентные лицензии в количестве больше 25 штук?", 1)

	// 5. Российская ОС на 500 устройств — ИИ уточняет (#103033)
	t5 := ticket(103033, "Клиент сайта", "", 7, "web")
	say(t5, "Какие лицензии на операционную систему РЕД ОС требуется закупить для её использования на планшетах в количестве 500 шт?", 1)

	// 6. Вопрос о покупке — ответ из базы знаний (#103018)
	t6 := ticket(103018, "Клиент сайта", "", 25, "web")
	say(t6, "Добрый день, подскажите, цены с НДС указаны?", 1)
	for i := len(db.Messages) - 1; i >= 0; i-- {
		if m := db.Messages[i]; m.TicketID == t6.ID && m.Sender == "bot" {
			m.Feedback = "up"
			break
		}
	}

	// 7. Продукта нет в каталоге → менеджер ответил, закрыто (#103028)
	t7 := ticket(103028, "Клиент сайта", "", 140, "web")
	say(t7, "Добрый день! У вас возможно приобрести лицензию MS Power BI?", 1)
	operator(t7, "Здравствуйте! Сейчас Power BI недоступен для заказа в России. Могу предложить российские BI-системы — подобрать варианты?", 5)
	add(t7, Draft{Sender: "system", Text: "Обращение закрыто"}, clock-30)
	t7.Status = "closed"

	// 8. Госзакупка с готовым КП, отправлено (#101638)
	t8 := ticket(101638, "Клиент сайта", "МБУ «Центр ИТ»", 300, "web")
	say(t8, "Прошу дать коммерческое предложение на RuDesktop, удалённый доступ, подписка 1 год, в количестве 7 штук. Закупка будет конкурентная.", 1)
	rdm := FindMentioned(cat, "RuDesktop")
	rd := rdm.Family
	if rd == nil && len(rdm.VendorFamilies) > 0 {
		rd = rdm.VendorFamilies[0]
	}
	if rd != nil {
		t8.Requirements = model.Requirements{ProductID: rd.ID, ProductName: rd.Title, Users: 7, Years: 1}
		t8.Scenario = "known"
		offers := knownOffers(cat, rd, t8.Requirements)
		if len(offers) > 1 {
			offers = offers[:1]
		}
		t8.LastOffers = offers
		if len(offers) > 0 {
			t8.SelectedOfferID = offers[0].ID
		}
		operator(t8, "Здравствуйте! Подготовила КП на RuDesktop, 7 подключений на 1 год — отправлю на почту после согласования.", 10)
		quote(t8, "МБУ «Центр ИТ»", "it-center@example.com", "sent", 5)
		add(t8, Draft{Sender: "system", Text: "Обращение закрыто"}, clock-40)
		t8.Status = "closed"
	}
	return db
}
