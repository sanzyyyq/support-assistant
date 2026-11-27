// Package service — логика поддержки: диалоги с агентом, передача менеджеру, КП, статистика.
// Данные — в Postgres (repo.go). HTTP-обработчики (internal/server) только разбирают запрос и вызывают методы Service.
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	mrand "math/rand/v2"
	"os"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"support-assistant/internal/agent"
	"support-assistant/internal/db"
	"support-assistant/internal/model"
)

// NotFoundError — нет такого обращения, КП или сообщения (HTTP 404).
type NotFoundError struct{ What string }

func (e NotFoundError) Error() string { return e.What + " не найдено" }

// BadRequestError — неверный запрос (HTTP 400).
type BadRequestError struct{ Msg string }

func (e BadRequestError) Error() string { return e.Msg }

// IsNotFound / IsBadRequest — для HTTP-кодов ответа.
func IsNotFound(err error) bool   { return errors.As(err, &NotFoundError{}) }
func IsBadRequest(err error) bool { return errors.As(err, &BadRequestError{}) }

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

// Service — все операции с обращениями.
// Изменения обращения идут по очереди (мьютекс): агент читает диалог и дописывает ответ,
// не мешая менеджеру. Рассчитано на один экземпляр сервера.
type Service struct {
	mu    sync.Mutex
	pool  *pgxpool.Pool
	cat   *agent.Catalog
	chats ChatsSummary
	// emit — разослать событие подписчикам WebSocket (не блокируется)
	emit func(model.Event)
	// TypingDelay — пауза «агент печатает…» перед n-м сообщением ответа
	TypingDelay func(n int) time.Duration
}

func New(pool *pgxpool.Pool, cat *agent.Catalog, chats ChatsSummary, emit func(model.Event)) *Service {
	return &Service{
		pool: pool, cat: cat, chats: chats, emit: emit,
		TypingDelay: func(n int) time.Duration {
			if n == 0 {
				return time.Duration(700+mrand.IntN(700)) * time.Millisecond
			}
			return 500 * time.Millisecond
		},
	}
}

// Init — первый запуск: если в базе нет обращений, перенести их из прежнего db.json (если есть) или создать демо-данные.
func (s *Service) Init(ctx context.Context, legacyFile string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	empty, err := isEmpty(ctx, s.pool)
	if err != nil || !empty {
		return err
	}
	if legacyFile != "" {
		if raw, err := os.ReadFile(legacyFile); err == nil {
			var old agent.DB
			if err := json.Unmarshal(raw, &old); err != nil {
				return fmt.Errorf("%s: %w", legacyFile, err)
			}
			if err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error { return fill(ctx, tx, &old) }); err != nil {
				return fmt.Errorf("перенос %s в базу: %w", legacyFile, err)
			}
			// Данные уже в базе — файл больше не нужен
			if err := os.Remove(legacyFile); err != nil {
				slog.Warn("перенёс данные, но не удалил файл", "файл", legacyFile, "ошибка", err)
			}
			slog.Info("обращения перенесены из файла в базу", "файл", legacyFile, "обращений", len(old.Tickets), "сообщений", len(old.Messages), "КП", len(old.Quotes))
			return nil
		}
	}
	seed := agent.CreateSeed(s.cat, time.Now())
	if err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error { return fill(ctx, tx, seed) }); err != nil {
		return fmt.Errorf("демо-данные: %w", err)
	}
	slog.Info("созданы демо-обращения", "обращений", len(seed.Tickets))
	return nil
}

// ── Внутренние помощники (вызываются под s.mu) ────────────────────────

func now() string { return agent.ISO(time.Now()) }

func uid() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// change — прочитать обращение, изменить его в транзакции и разослать события после сохранения.
// fn получает транзакцию и возвращает новые сообщения (они добавляются в диалог).
type change struct {
	t        *model.Ticket
	messages []model.Message
	events   []model.Event
}

func (c *change) post(drafts ...agent.Draft) []model.Message {
	var out []model.Message
	for _, d := range drafts {
		m := d
		m.ID, m.TicketID, m.CreatedAt = uid(), c.t.ID, now()
		c.t.UpdatedAt = m.CreatedAt
		if m.Sender != "system" {
			c.t.LastMessage = m.Text
		}
		c.messages = append(c.messages, m)
		out = append(out, m)
	}
	return out
}

func (c *change) system(text string) { c.post(agent.Draft{Sender: "system", Text: text}) }

// history — сообщения диалога из базы плюс ещё не сохранённые из этого изменения.
func (s *Service) history(ctx context.Context, q db.Querier, c *change) ([]model.Message, error) {
	msgs, err := ticketMessages(ctx, q, c.t.ID)
	return append(msgs, c.messages...), err
}

// edit — изменить обращение: fn меняет c.t и добавляет сообщения через c.post.
// Всё сохраняется в одной транзакции, события рассылаются после записи.
func (s *Service) edit(ctx context.Context, ticketID string, fn func(tx pgx.Tx, c *change) error) (*change, error) {
	var c *change
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		t, err := getTicket(ctx, tx, ticketID)
		if err != nil {
			return err
		}
		c = &change{t: t}
		if err := fn(tx, c); err != nil {
			return err
		}
		if len(c.messages) == 0 {
			c.t.UpdatedAt = now()
		}
		for i := range c.messages {
			if err := insertMessage(ctx, tx, &c.messages[i]); err != nil {
				return err
			}
		}
		return saveTicket(ctx, tx, c.t)
	})
	if err != nil {
		return nil, err
	}
	for _, e := range c.events {
		s.emit(e)
	}
	for i := range c.messages {
		m := c.messages[i]
		s.emit(model.Event{Type: "message", Message: &m})
	}
	t := *c.t
	s.emit(model.Event{Type: "ticket_updated", Ticket: &t})
	return c, nil
}

// handoff — передать диалог менеджеру (внутри edit).
func (s *Service) handoff(ctx context.Context, tx pgx.Tx, c *change, reason string, confidence float64, botText string) error {
	if botText != "" {
		c.post(agent.Draft{Sender: "bot", Intent: "handoff", Text: botText})
	}
	t := c.t
	t.Status = "waiting"
	t.HandoffAt = now()
	t.HandoffReason = reason
	t.AIConfidence = &confidence
	t.PendingSlot = ""
	history, err := s.history(ctx, tx, c)
	if err != nil {
		return err
	}
	t.AISummary = agent.BuildSummary(t, history, reason)
	c.system("ИИ передал диалог оператору")
	return nil
}

func (s *Service) typing(ticketID string, active bool) {
	s.emit(model.Event{Type: "typing", TicketID: ticketID, Sender: "bot", Active: &active})
}

// runBot — агент «печатает» и отвечает (в отдельной горутине, чтобы клиент сразу получил своё сообщение).
func (s *Service) runBot(ticketID, text string) {
	ctx := context.Background()
	s.typing(ticketID, true)
	time.Sleep(s.TypingDelay(0))
	s.typing(ticketID, false)

	var reply agent.Reply
	silent := false
	s.mu.Lock()
	_, err := s.edit(ctx, ticketID, func(tx pgx.Tx, c *change) error {
		// Пока агент «думал», мог подключиться менеджер — тогда молчим
		if c.t.Status != "ai" {
			silent = true
			return errSkip
		}
		history, err := s.history(ctx, tx, c)
		if err != nil {
			return err
		}
		reply = agent.GenerateReply(s.cat, c.t, text, history)
		agent.ApplyReply(c.t, reply)
		return nil
	})
	s.mu.Unlock()
	if silent || errors.Is(err, errSkip) {
		return
	}
	if err != nil {
		slog.Error("ответ агента", "обращение", ticketID, "ошибка", err)
		return
	}

	for i, m := range reply.Messages {
		if i > 0 {
			s.typing(ticketID, true)
			time.Sleep(s.TypingDelay(i))
			s.typing(ticketID, false)
		}
		s.mu.Lock()
		_, err := s.edit(ctx, ticketID, func(_ pgx.Tx, c *change) error {
			c.post(m)
			return nil
		})
		s.mu.Unlock()
		if err != nil {
			slog.Error("сообщение агента", "обращение", ticketID, "ошибка", err)
			return
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	_, err = s.edit(ctx, ticketID, func(tx pgx.Tx, c *change) error {
		if reply.Handoff == nil {
			// Резюме обновляется по ходу диалога — менеджер видит суть, не читая переписку
			history, err := s.history(ctx, tx, c)
			if err != nil {
				return err
			}
			c.t.AISummary = agent.BuildSummary(c.t, history, "")
			return nil
		}
		botText := ""
		if len(reply.Messages) == 0 {
			botText = agent.HandoffText
		}
		return s.handoff(ctx, tx, c, reply.Handoff.Reason, reply.Handoff.Confidence, botText)
	})
	if err != nil {
		slog.Error("резюме для менеджера", "обращение", ticketID, "ошибка", err)
	}
}

// errSkip — изменение не нужно (транзакция откатывается без ошибки для пользователя).
var errSkip = errors.New("skip")

// ── Клиент ────────────────────────────────────────────────────────────

// StartSession — продолжить диалог клиента (ticketID из его браузера) или начать новый.
func (s *Service) StartSession(ctx context.Context, ticketID string, forceNew bool) (model.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !forceNew && ticketID != "" {
		t, err := getTicket(ctx, s.pool, ticketID)
		if err == nil {
			msgs, err := ticketMessages(ctx, s.pool, t.ID)
			return model.Session{Ticket: *t, Messages: msgs}, err
		}
		if !IsNotFound(err) {
			return model.Session{}, err
		}
	}
	var t *model.Ticket
	var greeting model.Message
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		number, err := nextNumber(ctx, tx, "ticket")
		if err != nil {
			return err
		}
		t = &model.Ticket{
			ID: fmt.Sprintf("t-%d", number), Number: number, Channel: "web", Status: "ai",
			CreatedAt: now(), UpdatedAt: now(), Tags: []string{}, Sentiment: "neutral",
			Customer: model.Customer{ID: uid(), Name: fmt.Sprintf("Посетитель #%d", number)},
		}
		c := &change{t: t}
		greeting = c.post(agent.Greeting())[0]
		if err := saveTicket(ctx, tx, t); err != nil {
			return err
		}
		return insertMessage(ctx, tx, &greeting)
	})
	if err != nil {
		return model.Session{}, err
	}
	tc := *t
	s.emit(model.Event{Type: "ticket_updated", Ticket: &tc})
	s.emit(model.Event{Type: "message", Message: &greeting})
	return model.Session{Ticket: *t, Messages: []model.Message{greeting}}, nil
}

func (s *Service) SendCustomerMessage(ctx context.Context, ticketID, text string) (model.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var msg model.Message
	startBot := false
	_, err := s.edit(ctx, ticketID, func(_ pgx.Tx, c *change) error {
		msg = c.post(agent.Draft{Sender: "user", Text: text})[0]
		switch c.t.Status {
		case "closed":
			c.t.Status = "ai"
			c.t.Assignee = ""
			c.system("Диалог возобновлён")
			startBot = true
		case "ai":
			startBot = true
		}
		return nil
	})
	if err != nil {
		return model.Message{}, err
	}
	if startBot {
		go s.runBot(ticketID, text)
	}
	return msg, nil
}

func (s *Service) RequestOperator(ctx context.Context, ticketID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.edit(ctx, ticketID, func(tx pgx.Tx, c *change) error {
		if c.t.Status == "waiting" || c.t.Status == "operator" {
			return errSkip
		}
		conf := 0.7
		if c.t.AIConfidence != nil {
			conf = *c.t.AIConfidence
		}
		return s.handoff(ctx, tx, c, "Клиент попросил менеджера", conf, agent.HandoffText)
	})
	if errors.Is(err, errSkip) {
		return nil
	}
	return err
}

func (s *Service) SubmitQuoteRequest(ctx context.Context, ticketID, messageID string, req model.QuoteRequest) (model.Quote, error) {
	var q model.Quote
	s.mu.Lock()
	_, err := s.edit(ctx, ticketID, func(tx pgx.Tx, c *change) error {
		offer := c.t.SelectedOffer()
		if offer == nil {
			return BadRequestError{"Нет выбранного решения"}
		}
		seq, err := nextNumber(ctx, tx, "quote")
		if err != nil {
			return err
		}
		q = agent.BuildQuote(c.t, *offer, req, seq, time.Now())
		if err := saveQuote(ctx, tx, &q); err != nil {
			return err
		}
		qc := q
		c.events = append(c.events, model.Event{Type: "quote_updated", Quote: &qc})
		// Форма в чате заполнена
		if m, err := getMessage(ctx, tx, messageID); err == nil && m.TicketID == c.t.ID {
			m.FormDone = true
			if err := updateMessage(ctx, tx, m); err != nil {
				return err
			}
			c.events = append(c.events, model.Event{Type: "message_updated", Message: m})
		}
		c.t.QuoteID = q.ID
		c.t.Customer.Company, c.t.Customer.INN, c.t.Customer.Email = req.Company, req.INN, req.Email
		c.t.AddTag("КП")
		return nil
	})
	s.mu.Unlock()
	if err != nil {
		return model.Quote{}, err
	}

	s.typing(ticketID, true)
	time.Sleep(s.TypingDelay(1) + 100*time.Millisecond)
	s.typing(ticketID, false)

	s.mu.Lock()
	defer s.mu.Unlock()
	_, err = s.edit(ctx, ticketID, func(_ pgx.Tx, c *change) error {
		c.post(agent.Draft{
			Sender: "bot", Intent: "quote_created", QuoteID: q.ID,
			Text:         "Готово! Черновик " + q.Number + " на " + agent.FormatPrice(agent.QuoteSum(q)) + " сформирован по шаблону и передан менеджеру на проверку. После согласования он придёт на " + q.Email + ".",
			QuickReplies: []string{"Как происходит поставка?", "Позвать менеджера"},
		})
		return nil
	})
	return q, err
}

// RateMessage — 👍 / 👎 ответу агента; value "" снимает оценку.
func (s *Service) RateMessage(ctx context.Context, messageID, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := getMessage(ctx, s.pool, messageID)
	if err != nil {
		return err
	}
	m.Feedback = value
	if err := updateMessage(ctx, s.pool, m); err != nil {
		return err
	}
	s.emit(model.Event{Type: "message_updated", Message: m})
	return nil
}

// CustomerQuote — КП своего обращения (клиент видит только его).
func (s *Service) CustomerQuote(ctx context.Context, ticketID, quoteID string) (model.Quote, error) {
	q, err := getQuote(ctx, s.pool, quoteID)
	if err != nil {
		return model.Quote{}, err
	}
	if q.TicketID != ticketID {
		return model.Quote{}, NotFoundError{"КП " + quoteID}
	}
	return *q, nil
}

// ── Менеджер ──────────────────────────────────────────────────────────

func (s *Service) ListTickets(ctx context.Context) ([]model.Ticket, error) {
	return listTickets(ctx, s.pool)
}

func (s *Service) Messages(ctx context.Context, ticketID string) ([]model.Message, error) {
	return ticketMessages(ctx, s.pool, ticketID)
}

func (s *Service) JoinTicket(ctx context.Context, ticketID, operatorName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.edit(ctx, ticketID, func(_ pgx.Tx, c *change) error {
		c.t.Status = "operator"
		c.t.Assignee = operatorName
		c.t.PendingSlot = ""
		c.system("В чате оператор: " + operatorName)
		return nil
	})
	return err
}

func (s *Service) ReleaseToAI(ctx context.Context, ticketID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.edit(ctx, ticketID, func(_ pgx.Tx, c *change) error {
		c.t.Status = "ai"
		c.t.Assignee = ""
		c.system("Диалог снова ведёт ИИ-консультант")
		return nil
	})
	return err
}

func (s *Service) CloseTicket(ctx context.Context, ticketID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.edit(ctx, ticketID, func(_ pgx.Tx, c *change) error {
		c.t.Status = "closed"
		c.system("Обращение закрыто")
		return nil
	})
	return err
}

func (s *Service) SendOperatorMessage(ctx context.Context, ticketID, text, operatorName string) (model.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var msg model.Message
	_, err := s.edit(ctx, ticketID, func(_ pgx.Tx, c *change) error {
		msg = c.post(agent.Draft{Sender: "operator", Text: text, AuthorName: operatorName})[0]
		return nil
	})
	return msg, err
}

func (s *Service) Suggestion(ctx context.Context, ticketID, operatorName string) (string, error) {
	t, err := getTicket(ctx, s.pool, ticketID)
	if err != nil {
		return "", err
	}
	return agent.SuggestReply(t, operatorName), nil
}

// ── КП ────────────────────────────────────────────────────────────────

func (s *Service) ListQuotes(ctx context.Context) ([]model.Quote, error) {
	return listQuotes(ctx, s.pool)
}

func (s *Service) Quote(ctx context.Context, id string) (model.Quote, error) {
	q, err := getQuote(ctx, s.pool, id)
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

func (s *Service) UpdateQuote(ctx context.Context, id string, p QuotePatch) (model.Quote, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q, err := getQuote(ctx, s.pool, id)
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
	if err := saveQuote(ctx, s.pool, q); err != nil {
		return model.Quote{}, err
	}
	s.emit(model.Event{Type: "quote_updated", Quote: q})
	return *q, nil
}

func (s *Service) ApproveQuote(ctx context.Context, id, reviewer string) (model.Quote, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q, err := getQuote(ctx, s.pool, id)
	if err != nil {
		return model.Quote{}, err
	}
	_, err = s.edit(ctx, q.TicketID, func(tx pgx.Tx, c *change) error {
		q.Status = "sent"
		q.Reviewer = reviewer
		if err := saveQuote(ctx, tx, q); err != nil {
			return err
		}
		qc := *q
		c.events = append(c.events, model.Event{Type: "quote_updated", Quote: &qc})
		c.system(q.Number + " согласовано и отправлено на " + q.Email)
		return nil
	})
	if err != nil {
		return model.Quote{}, err
	}
	return *q, nil
}

// ── Аналитика и демо ──────────────────────────────────────────────────

func jsRound(x float64) float64 { return math.Floor(x + 0.5) }

// Stats — база: реальные переписки поддержки (≈500 обращений, как в презентации) + живые данные из Postgres.
func (s *Service) Stats(ctx context.Context) (model.Stats, error) {
	var live, handoffs, up, down int
	var quotes int
	err := s.pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM tickets),
			(SELECT count(*) FROM tickets WHERE data->>'handoffAt' IS NOT NULL OR assignee IS NOT NULL),
			(SELECT count(*) FROM messages WHERE data->>'feedback' = 'up'),
			(SELECT count(*) FROM messages WHERE data->>'feedback' = 'down'),
			(SELECT count(*) FROM quotes)`).Scan(&live, &handoffs, &up, &down, &quotes)
	if err != nil {
		return model.Stats{}, err
	}
	all, err := listQuotes(ctx, s.pool)
	if err != nil {
		return model.Stats{}, err
	}
	amount := 11_870_000.0
	for _, q := range all {
		amount += agent.QuoteSum(q)
	}
	total := float64(s.chats.Total + live)
	baseAI, baseHandoffs := jsRound(float64(s.chats.Total)*0.71), jsRound(float64(s.chats.Total)*0.22)
	aiResolved := float64(live - handoffs)
	ups, downs := float64(214+up), float64(23+down)
	intents := s.chats.Categories
	if intents == nil {
		intents = []model.IntentCount{}
	}
	return model.Stats{
		TicketsToday:     int(total),
		AIResolvedPct:    int(jsRound((baseAI + aiResolved) / total * 100)),
		HandoffPct:       int(jsRound((baseHandoffs + float64(handoffs)) / total * 100)),
		FirstResponseSec: 1.8,
		Quotes:           quotes + 41,
		QuotesAmount:     amount,
		CsatPct:          int(jsRound(ups / (ups + downs) * 100)),
		RatedCount:       int(ups + downs),
		TopIntents:       intents,
		// ~8 минут ручной работы на обращение, которое закрыл ИИ
		HoursSavedPerDay: jsRound((baseAI+aiResolved)*8/60*10) / 10,
	}, nil
}

// ResetDemo — удалить все обращения и КП и создать демо-данные заново.
func (s *Service) ResetDemo(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	seed := agent.CreateSeed(s.cat, time.Now())
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := clearAll(ctx, tx); err != nil {
			return err
		}
		return fill(ctx, tx, seed)
	})
	if err != nil {
		return err
	}
	s.emit(model.Event{Type: "reset"})
	return nil
}
