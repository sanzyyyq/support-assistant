// Package db — подключение к Postgres и схема базы.
//
// Таблицы:
//   - users, sessions — сотрудники кабинета и их сессии входа;
//   - tickets, messages, quotes — обращения, сообщения чатов и КП;
//   - counters — номера следующего обращения и КП.
//
// У обращений, сообщений и КП основные поля лежат в отдельных колонках (по ним можно искать и строить отчёты),
// а полная запись — в колонке data (jsonb): там вложенные данные агента — варианты решения, карточки товаров и т.п.
package db

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var migrations = []string{
	// ── Сотрудники ──
	`CREATE TABLE IF NOT EXISTS users (
		id            serial PRIMARY KEY,
		login         text NOT NULL,
		name          text NOT NULL,
		role          text NOT NULL CHECK (role IN ('admin', 'manager')),
		password_hash text NOT NULL,
		active        boolean NOT NULL DEFAULT true,
		created_at    timestamptz NOT NULL DEFAULT now(),
		last_login_at timestamptz
	)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS users_login_key ON users (lower(login))`,
	`CREATE TABLE IF NOT EXISTS sessions (
		token_hash text PRIMARY KEY,
		user_id    integer NOT NULL REFERENCES users (id) ON DELETE CASCADE,
		created_at timestamptz NOT NULL DEFAULT now(),
		expires_at timestamptz NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS sessions_user_idx ON sessions (user_id)`,

	// ── Обращения и чаты ──
	`CREATE TABLE IF NOT EXISTS tickets (
		id            text PRIMARY KEY,
		number        integer NOT NULL UNIQUE,
		status        text NOT NULL,
		channel       text NOT NULL,
		customer_name text NOT NULL,
		assignee      text,
		created_at    timestamptz NOT NULL,
		updated_at    timestamptz NOT NULL,
		data          jsonb NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS tickets_updated_idx ON tickets (updated_at DESC)`,
	`CREATE TABLE IF NOT EXISTS messages (
		seq        bigserial PRIMARY KEY,
		id         text NOT NULL UNIQUE,
		ticket_id  text NOT NULL REFERENCES tickets (id) ON DELETE CASCADE,
		sender     text NOT NULL,
		text       text NOT NULL,
		created_at timestamptz NOT NULL,
		data       jsonb NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS messages_ticket_idx ON messages (ticket_id, seq)`,
	`CREATE TABLE IF NOT EXISTS quotes (
		id         text PRIMARY KEY,
		number     text NOT NULL,
		ticket_id  text NOT NULL REFERENCES tickets (id) ON DELETE CASCADE,
		status     text NOT NULL,
		company    text NOT NULL,
		email      text NOT NULL,
		created_at timestamptz NOT NULL,
		data       jsonb NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS quotes_created_idx ON quotes (created_at DESC)`,
	`CREATE TABLE IF NOT EXISTS counters (
		name  text PRIMARY KEY,
		value integer NOT NULL
	)`,
}

// Connect — подключиться к Postgres (с повторами, пока база поднимается) и создать таблицы.
func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("DATABASE_URL: %w", err)
	}
	for attempt := 1; ; attempt++ {
		err = pool.Ping(ctx)
		if err == nil {
			break
		}
		if attempt >= 20 {
			pool.Close()
			return nil, fmt.Errorf("Postgres недоступен: %w", err)
		}
		slog.Warn("Postgres недоступен, жду", "попытка", attempt, "ошибка", err)
		time.Sleep(1500 * time.Millisecond)
	}
	for _, sql := range migrations {
		if _, err := pool.Exec(ctx, sql); err != nil {
			pool.Close()
			return nil, fmt.Errorf("создание таблиц: %w", err)
		}
	}
	return pool, nil
}

// Querier — то, что умеют и пул соединений, и транзакция: функции хранилища работают с обоими.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// InTx — выполнить fn в транзакции: либо все изменения сохранятся, либо ни одно.
func InTx(ctx context.Context, pool *pgxpool.Pool, fn func(tx pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // после Commit откат ничего не делает
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
