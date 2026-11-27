// Package service — логика поддержки: диалоги с агентом, передача менеджеру, КП, статистика.
// HTTP-обработчики (internal/server) только разбирают запрос и вызывают методы Service.
package service

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	mrand "math/rand/v2"
	"os"
	"sort"
	"sync"
	"time"

	"support-assistant/internal/agent"
	"support-assistant/internal/model"
)

// NotFoundError — нет такого обращения, КП или сообщения (HTTP 404).
type NotFoundError struct{ What string }

func (e NotFoundError) Error() string { return e.What + " не найдено" }

// BadRequestError — неверный запрос (HTTP 400).
type BadRequestError struct{ Msg string }

func (e BadRequestError) Error() string { return e.Msg }

// ChatsSummary — выжимка из переписок поддержки (backend/data/chats.json) для аналитики.
type ChatsSummary struct {
	Total      int                 `json:"total"`
	Categories []model.IntentCount `json:"categories"`
}

func LoadChats(path string) (ChatsSummary, error) {
	var c ChatsSummary
	data, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	return c, json.Unmarshal(data, &c)
}

// Service — все операции с обращениями. Один мьютекс на всё: нагрузка демо небольшая, так проще и надёжнее.
type Service struct {
	mu    sync.Mutex
	cat   *agent.Catalog
	store *Store
	chats ChatsSummary
	// Emit — разослать событие подписчикам WebSocket (вызывается под мьютексом, не должен блокироваться)
	emit func(model.Event)
	// Задержки «агент печатает…»; в тестах можно обнулить
	TypingDelay func(n int) time.Duration
}

func New(cat *agent.Catalog, dataFile string, chats ChatsSummary, emit func(model.Event)) *Service {
	s := &Service{cat: cat, chats: chats, emit: emit}
	s.store = NewStore(&s.mu, dataFile, func() *agent.DB { return agent.CreateSeed(cat, time.Now()) })
	s.TypingDelay = func(n int) time.Duration {
		if n == 0 {
			return time.Duration(700+mrand.IntN(700)) * time.Millisecond
		}
		return 500 * time.Millisecond
	}
	s.mu.Lock()
	s.store.Load()
	s.mu.Unlock()
	return s
}

// Flush — сохранить данные перед остановкой.
func (s *Service) Flush() { s.store.Flush() }

// ── Внутренние помощники (вызываются под s.mu) ────────────────────────

func now() string { return agent.ISO(time.Now()) }

func uid() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Service) db() *agent.DB { return s.store.DB() }

func (s *Service) findTicket(id string) (*model.Ticket, error) {
	for _, t := range s.db().Tickets {
		if t.ID == id {
			return t, nil
		}
	}
	return nil, NotFoundError{"Обращение " + id}
}

func (s *Service) findQuote(id string) (*model.Quote, error) {
	for _, q := range s.db().Quotes {
		if q.ID == id {
			return q, nil
		}
	}
	return nil, NotFoundError{"КП " + id}
}

func (s *Service) emitTicket(t *model.Ticket) {
	c := *t
	s.emit(model.Event{Type: "ticket_updated", Ticket: &c})
}

func (s *Service) emitQuote(q *model.Quote) {
	c := *q
	s.emit(model.Event{Type: "quote_updated", Quote: &c})
}

// post — добавить сообщения в диалог и разослать события.
func (s *Service) post(t *model.Ticket, drafts ...agent.Draft) []model.Message {
	var out []model.Message
	for _, d := range drafts {
		m := d
		m.ID, m.TicketID, m.CreatedAt = uid(), t.ID, now()
		s.db().Messages = append(s.db().Messages, &m)
		t.UpdatedAt = m.CreatedAt
		if m.Sender != "system" {
			t.LastMessage = m.Text
		}
		out = append(out, m)
	}
	s.store.Changed()
	for i := range out {
		m := out[i]
		s.emit(model.Event{Type: "message", Message: &m})
	}
	s.emitTicket(t)
	return out
}

func (s *Service) system(t *model.Ticket, text string) {
	s.post(t, agent.Draft{Sender: "system", Text: text})
}

// touch — обращение изменилось: обновить время, сохранить, разослать.
func (s *Service) touch(t *model.Ticket) {
	t.UpdatedAt = now()
	s.store.Changed()
	s.emitTicket(t)
}

func (s *Service) handoff(t *model.Ticket, reason string, confidence float64, botText string) {
	if botText != "" {
		s.post(t, agent.Draft{Sender: "bot", Intent: "handoff", Text: botText})
	}
	t.Status = "waiting"
	t.HandoffAt = now()
	t.HandoffReason = reason
	t.AIConfidence = &confidence
	t.PendingSlot = ""
	t.AISummary = agent.BuildSummary(t, s.db().TicketMessages(t.ID), reason)
	s.touch(t)
	s.system(t, "ИИ передал диалог оператору")
}

func (s *Service) typing(ticketID string, active bool) {
	s.emit(model.Event{Type: "typing", TicketID: ticketID, Sender: "bot", Active: &active})
}

// runBot — агент «печатает» и отвечает (в отдельной горутине, чтобы клиент сразу получил своё сообщение).
func (s *Service) runBot(ticketID, text string) {
	s.typing(ticketID, true)
	time.Sleep(s.TypingDelay(0))
	s.typing(ticketID, false)

	s.mu.Lock()
	t, err := s.findTicket(ticketID)
	// Пока агент «думал», мог подключиться менеджер — тогда молчим
	if err != nil || t.Status != "ai" {
		s.mu.Unlock()
		return
	}
	reply := agent.GenerateReply(s.cat, t, text, s.db().TicketMessages(ticketID))
	agent.ApplyReply(t, reply)
	s.touch(t)
	s.mu.Unlock()

	for i, m := range reply.Messages {
		if i > 0 {
			s.typing(ticketID, true)
			time.Sleep(s.TypingDelay(i))
			s.typing(ticketID, false)
		}
		s.mu.Lock()
		if t, err := s.findTicket(ticketID); err == nil {
			s.post(t, m)
		}
		s.mu.Unlock()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	t, err = s.findTicket(ticketID)
	if err != nil {
		return
	}
	if reply.Handoff == nil {
		// Резюме обновляется по ходу диалога — менеджер видит суть, не читая переписку
		t.AISummary = agent.BuildSummary(t, s.db().TicketMessages(ticketID), "")
		s.touch(t)
		return
	}
	botText := ""
	if len(reply.Messages) == 0 {
		botText = agent.HandoffText
	}
	s.handoff(t, reply.Handoff.Reason, reply.Handoff.Confidence, botText)
}

// ── Клиент ────────────────────────────────────────────────────────────

// StartSession — продолжить диалог клиента (ticketID из его браузера) или начать новый.
func (s *Service) StartSession(ticketID string, forceNew bool) model.Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !forceNew && ticketID != "" {
		if t, err := s.findTicket(ticketID); err == nil {
			return model.Session{Ticket: *t, Messages: s.db().TicketMessages(t.ID)}
		}
	}
	db := s.db()
	number := db.Seq
	db.Seq++
	t := &model.Ticket{
		ID: fmt.Sprintf("t-%d", number), Number: number, Channel: "web", Status: "ai",
		CreatedAt: now(), UpdatedAt: now(), Tags: []string{}, Sentiment: "neutral",
		Customer: model.Customer{ID: uid(), Name: fmt.Sprintf("Посетитель #%d", number)},
	}
	db.Tickets = append(db.Tickets, t)
	s.emitTicket(t)
	msgs := s.post(t, agent.Greeting())
	return model.Session{Ticket: *t, Messages: msgs}
}

func (s *Service) SendCustomerMessage(ticketID, text string) (model.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.findTicket(ticketID)
	if err != nil {
		return model.Message{}, err
	}
	msg := s.post(t, agent.Draft{Sender: "user", Text: text})[0]
	switch t.Status {
	case "closed":
		t.Status = "ai"
		t.Assignee = ""
		s.touch(t)
		s.system(t, "Диалог возобновлён")
		go s.runBot(ticketID, text)
	case "ai":
		go s.runBot(ticketID, text)
	}
	return msg, nil
}

func (s *Service) RequestOperator(ticketID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.findTicket(ticketID)
	if err != nil {
		return err
	}
	if t.Status == "waiting" || t.Status == "operator" {
		return nil
	}
	conf := 0.7
	if t.AIConfidence != nil {
		conf = *t.AIConfidence
	}
	s.handoff(t, "Клиент попросил менеджера", conf, agent.HandoffText)
	return nil
}

func (s *Service) SubmitQuoteRequest(ticketID, messageID string, req model.QuoteRequest) (model.Quote, error) {
	s.mu.Lock()
	t, err := s.findTicket(ticketID)
	if err != nil {
		s.mu.Unlock()
		return model.Quote{}, err
	}
	offer := t.SelectedOffer()
	if offer == nil {
		s.mu.Unlock()
		return model.Quote{}, BadRequestError{"Нет выбранного решения"}
	}
	db := s.db()
	q := agent.BuildQuote(t, *offer, req, db.QuoteSeq, time.Now())
	db.QuoteSeq++
	db.Quotes = append(db.Quotes, &q)
	s.emitQuote(&q)
	for _, m := range db.Messages {
		if m.ID == messageID {
			m.FormDone = true
			c := *m
			s.emit(model.Event{Type: "message_updated", Message: &c})
			break
		}
	}
	t.QuoteID = q.ID
	t.Customer.Company, t.Customer.INN, t.Customer.Email = req.Company, req.INN, req.Email
	t.AddTag("КП")
	s.touch(t)
	s.mu.Unlock()

	s.typing(ticketID, true)
	time.Sleep(s.TypingDelay(1) + 100*time.Millisecond)
	s.typing(ticketID, false)

	s.mu.Lock()
	defer s.mu.Unlock()
	if t, err := s.findTicket(ticketID); err == nil {
		s.post(t, agent.Draft{
			Sender: "bot", Intent: "quote_created", QuoteID: q.ID,
			Text:         "Готово! Черновик " + q.Number + " на " + agent.FormatPrice(agent.QuoteSum(q)) + " сформирован по шаблону и передан менеджеру на проверку. После согласования он придёт на " + q.Email + ".",
			QuickReplies: []string{"Как происходит поставка?", "Позвать менеджера"},
		})
	}
	return q, nil
}

// RateMessage — 👍 / 👎 ответу агента; value "" снимает оценку.
func (s *Service) RateMessage(messageID, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.db().Messages {
		if m.ID == messageID {
			m.Feedback = value
			s.store.Changed()
			c := *m
			s.emit(model.Event{Type: "message_updated", Message: &c})
			return nil
		}
	}
	return NotFoundError{"Сообщение"}
}

// CustomerQuote — КП своего обращения (клиент видит только его).
func (s *Service) CustomerQuote(ticketID, quoteID string) (model.Quote, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q, err := s.findQuote(quoteID)
	if err != nil || q.TicketID != ticketID {
		return model.Quote{}, NotFoundError{"КП " + quoteID}
	}
	return *q, nil
}

// ── Менеджер ──────────────────────────────────────────────────────────

func byDate[T any](list []*T, key func(*T) string) []T {
	out := make([]T, len(list))
	for i, x := range list {
		out[i] = *x
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, _ := time.Parse(time.RFC3339Nano, key(&out[i]))
		b, _ := time.Parse(time.RFC3339Nano, key(&out[j]))
		return a.After(b)
	})
	return out
}

func (s *Service) ListTickets() []model.Ticket {
	s.mu.Lock()
	defer s.mu.Unlock()
	return byDate(s.db().Tickets, func(t *model.Ticket) string {
		if t.UpdatedAt != "" {
			return t.UpdatedAt
		}
		return t.CreatedAt
	})
}

func (s *Service) Messages(ticketID string) []model.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	msgs := s.db().TicketMessages(ticketID)
	if msgs == nil {
		msgs = []model.Message{}
	}
	return msgs
}

func (s *Service) JoinTicket(ticketID, operatorName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.findTicket(ticketID)
	if err != nil {
		return err
	}
	t.Status = "operator"
	t.Assignee = operatorName
	t.PendingSlot = ""
	s.touch(t)
	s.system(t, "В чате оператор: "+operatorName)
	return nil
}

func (s *Service) ReleaseToAI(ticketID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.findTicket(ticketID)
	if err != nil {
		return err
	}
	t.Status = "ai"
	t.Assignee = ""
	s.touch(t)
	s.system(t, "Диалог снова ведёт ИИ-консультант")
	return nil
}

func (s *Service) CloseTicket(ticketID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.findTicket(ticketID)
	if err != nil {
		return err
	}
	t.Status = "closed"
	s.touch(t)
	s.system(t, "Обращение закрыто")
	return nil
}

func (s *Service) SendOperatorMessage(ticketID, text, operatorName string) (model.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.findTicket(ticketID)
	if err != nil {
		return model.Message{}, err
	}
	return s.post(t, agent.Draft{Sender: "operator", Text: text, AuthorName: operatorName})[0], nil
}

func (s *Service) Suggestion(ticketID, operatorName string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.findTicket(ticketID)
	if err != nil {
		return "", err
	}
	return agent.SuggestReply(t, operatorName), nil
}

// ── КП ────────────────────────────────────────────────────────────────

func (s *Service) ListQuotes() []model.Quote {
	s.mu.Lock()
	defer s.mu.Unlock()
	return byDate(s.db().Quotes, func(q *model.Quote) string { return q.CreatedAt })
}

func (s *Service) Quote(id string) (model.Quote, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q, err := s.findQuote(id)
	if err != nil {
		return model.Quote{}, err
	}
	return *q, nil
}

// QuotePatch — что менеджер может править в КП (nil — не менять).
type QuotePatch struct {
	Items        *[]model.QuoteItem `json:"items"`
	PaymentTerms *string            `json:"paymentTerms"`
	DeliveryTerm *string            `json:"deliveryTerm"`
	ValidUntil   *string            `json:"validUntil"`
	Comment      *string            `json:"comment"`
	Company      *string            `json:"company"`
	INN          *string            `json:"inn"`
	Email        *string            `json:"email"`
}

func (s *Service) UpdateQuote(id string, p QuotePatch) (model.Quote, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q, err := s.findQuote(id)
	if err != nil {
		return model.Quote{}, err
	}
	set := func(dst *string, v *string) {
		if v != nil {
			*dst = *v
		}
	}
	if p.Items != nil {
		q.Items = *p.Items
	}
	set(&q.PaymentTerms, p.PaymentTerms)
	set(&q.DeliveryTerm, p.DeliveryTerm)
	set(&q.ValidUntil, p.ValidUntil)
	set(&q.Comment, p.Comment)
	set(&q.Company, p.Company)
	set(&q.INN, p.INN)
	set(&q.Email, p.Email)
	s.store.Changed()
	s.emitQuote(q)
	return *q, nil
}

func (s *Service) ApproveQuote(id, reviewer string) (model.Quote, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q, err := s.findQuote(id)
	if err != nil {
		return model.Quote{}, err
	}
	q.Status = "sent"
	q.Reviewer = reviewer
	s.store.Changed()
	s.emitQuote(q)
	if t, err := s.findTicket(q.TicketID); err == nil {
		s.system(t, q.Number+" согласовано и отправлено на "+q.Email)
	}
	return *q, nil
}

// ── Аналитика и демо ──────────────────────────────────────────────────

func jsRound(x float64) float64 { return math.Floor(x + 0.5) }

// Stats — база: реальные переписки поддержки (≈500 обращений, как в презентации) + живые данные.
func (s *Service) Stats() model.Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	db := s.db()
	base := struct {
		tickets, aiResolved, handoffs, quotes, up, down float64
		quotesAmount                                    float64
	}{
		tickets: float64(s.chats.Total), aiResolved: jsRound(float64(s.chats.Total) * 0.71), handoffs: jsRound(float64(s.chats.Total) * 0.22),
		quotes: 41, quotesAmount: 11_870_000, up: 214, down: 23,
	}
	handoffs, aiResolved, up, down := 0.0, 0.0, base.up, base.down
	for _, t := range db.Tickets {
		if t.HandoffAt != "" || t.Assignee != "" {
			handoffs++
		} else {
			aiResolved++
		}
	}
	for _, m := range db.Messages {
		switch m.Feedback {
		case "up":
			up++
		case "down":
			down++
		}
	}
	total := base.tickets + float64(len(db.Tickets))
	amount := base.quotesAmount
	for _, q := range db.Quotes {
		amount += agent.QuoteSum(*q)
	}
	intents := s.chats.Categories
	if intents == nil {
		intents = []model.IntentCount{}
	}
	return model.Stats{
		TicketsToday:     int(total),
		AIResolvedPct:    int(jsRound((base.aiResolved + aiResolved) / total * 100)),
		HandoffPct:       int(jsRound((base.handoffs + handoffs) / total * 100)),
		FirstResponseSec: 1.8,
		Quotes:           len(db.Quotes) + int(base.quotes),
		QuotesAmount:     amount,
		CsatPct:          int(jsRound(up / (up + down) * 100)),
		RatedCount:       int(up + down),
		TopIntents:       intents,
		// ~8 минут ручной работы на обращение, которое закрыл ИИ
		HoursSavedPerDay: jsRound((base.aiResolved+aiResolved)*8/60*10) / 10,
	}
}

// ResetDemo — вернуть демо-данные к исходным.
func (s *Service) ResetDemo() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.store.Reset()
	s.emit(model.Event{Type: "reset"})
}

// IsNotFound / IsBadRequest — для HTTP-кодов ответа.
func IsNotFound(err error) bool   { return errors.As(err, &NotFoundError{}) }
func IsBadRequest(err error) bool { return errors.As(err, &BadRequestError{}) }
