// Сервер ИИ-консультанта Store: API, WebSocket и собранный фронтенд на одном адресе.
//
// Запуск локально (нужен Postgres, например `docker compose up -d db`):
//
//	DATABASE_URL=postgres://app:app@localhost:5432/support go run .
//
// Настройки — переменные окружения, см. README.md.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"support-assistant/internal/agent"
	"support-assistant/internal/db"
	"support-assistant/internal/server"
	"support-assistant/internal/service"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// firstExisting — первый существующий путь (запуск из Docker или из папки backend).
func firstExisting(paths ...string) string {
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return paths[0]
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		slog.Error("сервер остановлен", "ошибка", err)
		os.Exit(1)
	}
}

func run() error {
	publicDir := env("PUBLIC_DIR", firstExisting("/app/public", "../frontend/dist"))
	cfg := server.Config{
		PublicDir:   publicDir,
		AllowReset:  os.Getenv("ALLOW_RESET") == "true",
		LogRequests: os.Getenv("LOG_REQUESTS") == "true",
	}
	// Каталог из фида и выжимка из переписок — data/build_catalog.py и data/build_chats.py
	catalogFile := env("CATALOG_FILE", firstExisting("/app/data/catalog.json.gz", "data/catalog.json.gz"))
	chatsFile := env("CHATS_FILE", firstExisting("/app/data/chats.json", "data/chats.json"))
	// Прежнее хранилище обращений (JSON-файл на томе). Если файл есть, при первом запуске его данные
	// переносятся в Postgres, а файл удаляется
	legacyFile := filepath.Join(env("DATA_DIR", "/data"), "db.json")
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return errors.New("не задан DATABASE_URL. Локально: docker compose up (Postgres поднимется сам). " +
			"На Railway: добавьте сервис Postgres и в переменных приложения DATABASE_URL=${{Postgres.DATABASE_URL}}")
	}

	cat, err := agent.LoadCatalog(catalogFile)
	if err != nil {
		return err
	}
	slog.Info("каталог загружен", "продуктов", len(cat.Families), "источник", cat.Source)
	chats, err := service.LoadChats(chatsFile)
	if err != nil {
		slog.Warn("нет выжимки переписок — аналитика без базы", "файл", chatsFile, "ошибка", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	auth, err := server.NewAuth(pool)
	if err != nil {
		return err
	}
	if err := auth.EnsureFirstAdmin(ctx); err != nil {
		return err
	}

	hub := server.NewHub()
	svc := service.New(pool, cat, chats, hub.Emit)
	if err := svc.Init(ctx, legacyFile); err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              env("HOST", "0.0.0.0") + ":" + env("PORT", "8080"),
		Handler:           server.New(cfg, cat, svc, auth, hub).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	slog.Info("сервер запущен", "адрес", srv.Addr, "фронтенд", publicDir, "сброс демо", cfg.AllowReset)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		slog.Info("останавливаюсь")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		server.Shutdown(shutdownCtx, srv)
		return nil
	}
}
