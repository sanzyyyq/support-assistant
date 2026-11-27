package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"support-assistant/internal/agent"
	"support-assistant/internal/db"
	"support-assistant/internal/model"
)

// Хранение обращений, сообщений и КП в Postgres (схема — в internal/db).
// Основные поля пишутся в отдельные колонки, полная запись — в data (jsonb).

func parseTime(iso string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, iso)
	if err != nil {
		return time.Now()
	}
	return t
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ── Обращения ─────────────────────────────────────────────────────────

func saveTicket(ctx context.Context, q db.Querier, t *model.Ticket) error {
	data, err := json.Marshal(t)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `
		INSERT INTO tickets (id, number, status, channel, customer_name, assignee, created_at, updated_at, data)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO UPDATE SET
			status = EXCLUDED.status, channel = EXCLUDED.channel, customer_name = EXCLUDED.customer_name,
			assignee = EXCLUDED.assignee, updated_at = EXCLUDED.updated_at, data = EXCLUDED.data`,
		t.ID, t.Number, t.Status, t.Channel, t.Customer.Name, nullable(t.Assignee), parseTime(t.CreatedAt), parseTime(t.UpdatedAt), string(data))
	return err
}

func getTicket(ctx context.Context, q db.Querier, id string) (*model.Ticket, error) {
	var data []byte
	err := q.QueryRow(ctx, `SELECT data FROM tickets WHERE id = $1`, id).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, NotFoundError{"Обращение " + id}
	}
	if err != nil {
		return nil, err
	}
	var t model.Ticket
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, err
	}
	if t.Tags == nil {
		t.Tags = []string{}
	}
	return &t, nil
}

func listTickets(ctx context.Context, q db.Querier) ([]model.Ticket, error) {
	return queryJSON[model.Ticket](ctx, q, `SELECT data FROM tickets ORDER BY updated_at DESC, number DESC`)
}

// ── Сообщения ─────────────────────────────────────────────────────────

func insertMessage(ctx context.Context, q db.Querier, m *model.Message) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `INSERT INTO messages (id, ticket_id, sender, text, created_at, data) VALUES ($1, $2, $3, $4, $5, $6)`,
		m.ID, m.TicketID, m.Sender, m.Text, parseTime(m.CreatedAt), string(data))
	return err
}

func updateMessage(ctx context.Context, q db.Querier, m *model.Message) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `UPDATE messages SET text = $2, data = $3 WHERE id = $1`, m.ID, m.Text, string(data))
	return err
}

func getMessage(ctx context.Context, q db.Querier, id string) (*model.Message, error) {
	list, err := queryJSON[model.Message](ctx, q, `SELECT data FROM messages WHERE id = $1`, id)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, NotFoundError{"Сообщение"}
	}
	return &list[0], nil
}

// ticketMessages — сообщения обращения в порядке добавления.
func ticketMessages(ctx context.Context, q db.Querier, ticketID string) ([]model.Message, error) {
	return queryJSON[model.Message](ctx, q, `SELECT data FROM messages WHERE ticket_id = $1 ORDER BY seq`, ticketID)
}

// ── КП ────────────────────────────────────────────────────────────────

func saveQuote(ctx context.Context, q db.Querier, qt *model.Quote) error {
	data, err := json.Marshal(qt)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `
		INSERT INTO quotes (id, number, ticket_id, status, company, email, created_at, data)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO UPDATE SET
			status = EXCLUDED.status, company = EXCLUDED.company, email = EXCLUDED.email, data = EXCLUDED.data`,
		qt.ID, qt.Number, qt.TicketID, qt.Status, qt.Company, qt.Email, parseTime(qt.CreatedAt), string(data))
	return err
}

func getQuote(ctx context.Context, q db.Querier, id string) (*model.Quote, error) {
	list, err := queryJSON[model.Quote](ctx, q, `SELECT data FROM quotes WHERE id = $1`, id)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, NotFoundError{"КП " + id}
	}
	return &list[0], nil
}

func listQuotes(ctx context.Context, q db.Querier) ([]model.Quote, error) {
	return queryJSON[model.Quote](ctx, q, `SELECT data FROM quotes ORDER BY created_at DESC`)
}

// ── Счётчики номеров ──────────────────────────────────────────────────

// nextNumber — взять следующий номер обращения (ticket) или КП (quote).
func nextNumber(ctx context.Context, q db.Querier, name string) (int, error) {
	var n int
	err := q.QueryRow(ctx, `UPDATE counters SET value = value + 1 WHERE name = $1 RETURNING value - 1`, name).Scan(&n)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("нет счётчика %s — база не заполнена", name)
	}
	return n, err
}

func setCounter(ctx context.Context, q db.Querier, name string, value int) error {
	_, err := q.Exec(ctx, `INSERT INTO counters (name, value) VALUES ($1, $2) ON CONFLICT (name) DO UPDATE SET value = EXCLUDED.value`, name, value)
	return err
}

// ── Заполнение базы ───────────────────────────────────────────────────

// isEmpty — в базе ещё нет ни одного обращения.
func isEmpty(ctx context.Context, q db.Querier) (bool, error) {
	var exists bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM tickets)`).Scan(&exists)
	return !exists, err
}

// fill — записать набор данных целиком (демо-данные или перенос из прежнего db.json).
func fill(ctx context.Context, q db.Querier, data *agent.DB) error {
	for _, t := range data.Tickets {
		if t.Tags == nil {
			t.Tags = []string{}
		}
		if err := saveTicket(ctx, q, t); err != nil {
			return err
		}
	}
	for _, m := range data.Messages {
		if err := insertMessage(ctx, q, m); err != nil {
			return err
		}
	}
	for _, qt := range data.Quotes {
		if err := saveQuote(ctx, q, qt); err != nil {
			return err
		}
	}
	if err := setCounter(ctx, q, "ticket", data.Seq); err != nil {
		return err
	}
	return setCounter(ctx, q, "quote", data.QuoteSeq)
}

// clearAll — удалить все обращения, сообщения и КП (сотрудники остаются).
func clearAll(ctx context.Context, q db.Querier) error {
	_, err := q.Exec(ctx, `TRUNCATE tickets, messages, quotes RESTART IDENTITY CASCADE`)
	return err
}

// queryJSON — выбрать колонку data и разобрать каждую строку в T.
func queryJSON[T any](ctx context.Context, q db.Querier, sql string, args ...any) ([]T, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var v T
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
