// Package server — HTTP API, WebSocket и раздача собранного фронтенда.
package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"support-assistant/internal/agent"
	"support-assistant/internal/model"
	"support-assistant/internal/service"
)

// Config — настройки сервера (из переменных окружения, см. main.go).
type Config struct {
	// Собранный фронтенд (index.html, assets/)
	PublicDir string
	// Разрешить кнопку «Сбросить демо-данные»
	AllowReset bool
	// Писать в лог каждый запрос
	LogRequests bool
}

type Server struct {
	cfg  Config
	cat  *agent.Catalog
	svc  *service.Service
	auth *Auth
	hub  *Hub
}

func New(cfg Config, cat *agent.Catalog, svc *service.Service, auth *Auth, hub *Hub) *Server {
	return &Server{cfg: cfg, cat: cat, svc: svc, auth: auth, hub: hub}
}

// Handler — все маршруты.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]bool{"ok": true}) })

	// ── Клиент (без входа) ──
	mux.HandleFunc("POST /api/chat/session", s.startSession)
	mux.HandleFunc("POST /api/chat/{id}/messages", s.customerMessage)
	mux.HandleFunc("POST /api/chat/{id}/handoff", s.handoff)
	mux.HandleFunc("POST /api/chat/{id}/quote", s.quoteRequest)
	mux.HandleFunc("GET /api/chat/{id}/quotes/{quoteId}", s.customerQuote)
	mux.HandleFunc("POST /api/messages/{id}/feedback", s.feedback)

	// Демо-витрина: несколько известных продуктов и размер каталога
	mux.HandleFunc("GET /api/catalog/featured", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"total": s.cat.VariantCount(), "items": agent.Featured(s.cat)})
	})

	// ── Вход и настройки ──
	mux.HandleFunc("GET /api/auth/me", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]*model.Operator{"operator": s.auth.OperatorOf(r)})
	})
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if err := s.auth.Logout(w, r); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]bool{"allowReset": s.cfg.AllowReset})
	})
	mux.HandleFunc("GET /api/ws", func(w http.ResponseWriter, r *http.Request) {
		s.hub.Serve(w, r, s.auth.OperatorOf(r) != nil)
	})

	// ── Кабинет менеджера: только после входа ──
	op := func(pattern string, h func(http.ResponseWriter, *http.Request, model.Operator)) {
		mux.HandleFunc(pattern, s.requireOperator(h))
	}
	op("POST /api/auth/password", s.changePassword)
	op("GET /api/tickets", func(w http.ResponseWriter, r *http.Request, _ model.Operator) { writeJSON(w, 200, s.svc.ListTickets()) })
	op("GET /api/tickets/{id}/messages", func(w http.ResponseWriter, r *http.Request, _ model.Operator) {
		writeJSON(w, 200, s.svc.Messages(r.PathValue("id")))
	})
	// Имя менеджера в чате и КП берётся из его учётной записи, а не из запроса
	op("POST /api/tickets/{id}/join", func(w http.ResponseWriter, r *http.Request, me model.Operator) {
		noContent(w, s.svc.JoinTicket(r.PathValue("id"), me.Name))
	})
	op("POST /api/tickets/{id}/release", func(w http.ResponseWriter, r *http.Request, _ model.Operator) {
		noContent(w, s.svc.ReleaseToAI(r.PathValue("id")))
	})
	op("POST /api/tickets/{id}/close", func(w http.ResponseWriter, r *http.Request, _ model.Operator) {
		noContent(w, s.svc.CloseTicket(r.PathValue("id")))
	})
	op("POST /api/tickets/{id}/messages", s.operatorMessage)
	op("GET /api/tickets/{id}/suggestion", func(w http.ResponseWriter, r *http.Request, me model.Operator) {
		text, err := s.svc.Suggestion(r.PathValue("id"), me.Name)
		respond(w, map[string]string{"text": text}, err)
	})
	op("GET /api/quotes", func(w http.ResponseWriter, r *http.Request, _ model.Operator) { writeJSON(w, 200, s.svc.ListQuotes()) })
	op("GET /api/quotes/{id}", func(w http.ResponseWriter, r *http.Request, _ model.Operator) {
		q, err := s.svc.Quote(r.PathValue("id"))
		respond(w, q, err)
	})
	op("PATCH /api/quotes/{id}", s.updateQuote)
	op("POST /api/quotes/{id}/approve", func(w http.ResponseWriter, r *http.Request, me model.Operator) {
		q, err := s.svc.ApproveQuote(r.PathValue("id"), me.Name)
		respond(w, q, err)
	})
	op("GET /api/stats", func(w http.ResponseWriter, r *http.Request, _ model.Operator) { writeJSON(w, 200, s.svc.Stats()) })
	op("POST /api/admin/reset", func(w http.ResponseWriter, r *http.Request, _ model.Operator) {
		if !s.cfg.AllowReset {
			writeJSON(w, 403, errBody("Сброс отключён (ALLOW_RESET не true)"))
			return
		}
		s.svc.ResetDemo()
		w.WriteHeader(204)
	})

	// ── Сотрудники: только администратор ──
	op("GET /api/users", s.adminOnly(func(w http.ResponseWriter, r *http.Request, _ model.Operator) {
		users, err := s.auth.ListUsers(r.Context())
		respond(w, users, err)
	}))
	op("POST /api/users", s.adminOnly(s.createUser))
	op("PATCH /api/users/{id}", s.adminOnly(s.updateUser))

	// Неизвестные адреса API — 404 в JSON; остальное — фронтенд
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 404, errBody("Не найдено")) })
	mux.Handle("/", s.frontend())

	var h http.Handler = mux
	if s.cfg.LogRequests {
		h = logRequests(h)
	}
	return h
}

// ── Клиент ────────────────────────────────────────────────────────────

func (s *Server) startSession(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TicketID string `json:"ticketId"`
		ForceNew bool   `json:"forceNew"`
	}
	_ = readJSON(r, &body) // пустое тело — новый диалог
	writeJSON(w, 200, s.svc.StartSession(body.TicketID, body.ForceNew))
}

func (s *Server) customerMessage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text string `json:"text"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	text, err := required(body.Text, "text", 4000)
	if err != nil {
		writeError(w, err)
		return
	}
	msg, err := s.svc.SendCustomerMessage(r.PathValue("id"), text)
	respond(w, msg, err)
}

func (s *Server) handoff(w http.ResponseWriter, r *http.Request) {
	noContent(w, s.svc.RequestOperator(r.PathValue("id")))
}

func (s *Server) quoteRequest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		model.QuoteRequest
		MessageID string `json:"messageId"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	req := model.QuoteRequest{PaymentTerms: "Предоплата"}
	if body.PaymentTerms == "Постоплата" {
		req.PaymentTerms = "Постоплата"
	}
	var err error
	msgID, err := required(body.MessageID, "messageId", 100)
	if err == nil {
		req.Company, err = required(body.Company, "company", 300)
	}
	if err == nil {
		req.Email, err = required(body.Email, "email", 200)
	}
	if err == nil && body.INN != "" {
		req.INN, err = required(body.INN, "inn", 12)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	q, err := s.svc.SubmitQuoteRequest(r.PathValue("id"), msgID, req)
	respond(w, q, err)
}

func (s *Server) customerQuote(w http.ResponseWriter, r *http.Request) {
	q, err := s.svc.CustomerQuote(r.PathValue("id"), r.PathValue("quoteId"))
	respond(w, q, err)
}

func (s *Server) feedback(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Value *string `json:"value"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	v := ""
	if body.Value != nil && (*body.Value == "up" || *body.Value == "down") {
		v = *body.Value
	}
	noContent(w, s.svc.RateMessage(r.PathValue("id"), v))
}

// ── Вход и сотрудники ─────────────────────────────────────────────────

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !s.auth.limiter.Allow(ip) {
		writeJSON(w, 429, errBody("Слишком много попыток. Попробуйте через 15 минут"))
		return
	}
	var body struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	_ = readJSON(r, &body)
	op, err := s.auth.Login(w, r, body.Login, body.Password)
	if err != nil {
		writeError(w, err)
		return
	}
	s.auth.limiter.Reset(ip)
	writeJSON(w, 200, map[string]model.Operator{"operator": op})
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request, me model.Operator) {
	var body struct {
		Current  string `json:"current"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	noContent(w, s.auth.ChangeOwnPassword(r.Context(), r, me, body.Current, body.Password))
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request, _ model.Operator) {
	var body NewUser
	if err := readJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	u, err := s.auth.CreateUser(r.Context(), body)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 201, u)
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request, me model.Operator) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeJSON(w, 400, errBody("Неверный id"))
		return
	}
	var body UserPatch
	if err := readJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	u, err := s.auth.UpdateUser(r.Context(), me, id, body)
	respond(w, u, err)
}

// ── Менеджер ──────────────────────────────────────────────────────────

func (s *Server) operatorMessage(w http.ResponseWriter, r *http.Request, me model.Operator) {
	var body struct {
		Text string `json:"text"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	text, err := required(body.Text, "text", 4000)
	if err != nil {
		writeError(w, err)
		return
	}
	msg, err := s.svc.SendOperatorMessage(r.PathValue("id"), text, me.Name)
	respond(w, msg, err)
}

func (s *Server) updateQuote(w http.ResponseWriter, r *http.Request, _ model.Operator) {
	// Менять можно только поля, которые правит менеджер (остальные в теле игнорируются)
	var p service.QuotePatch
	if err := readJSON(r, &p); err != nil {
		writeError(w, err)
		return
	}
	q, err := s.svc.UpdateQuote(r.PathValue("id"), p)
	respond(w, q, err)
}

// ── Проверки доступа ──────────────────────────────────────────────────

func (s *Server) requireOperator(h func(http.ResponseWriter, *http.Request, model.Operator)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		op := s.auth.OperatorOf(r)
		if op == nil {
			writeJSON(w, 401, errBody("Нужно войти в кабинет менеджера"))
			return
		}
		h(w, r, *op)
	}
}

func (s *Server) adminOnly(h func(http.ResponseWriter, *http.Request, model.Operator)) func(http.ResponseWriter, *http.Request, model.Operator) {
	return func(w http.ResponseWriter, r *http.Request, me model.Operator) {
		if me.Role != "admin" {
			writeJSON(w, 403, errBody("Только для администратора"))
			return
		}
		h(w, r, me)
	}
}

// ── Фронтенд ──────────────────────────────────────────────────────────

// frontend — файлы сборки; для адресов приложения (/operator, /chat…) — index.html, маршруты разбирает React.
func (s *Server) frontend() http.Handler {
	index := filepath.Join(s.cfg.PublicDir, "index.html")
	if _, err := os.Stat(index); err != nil {
		slog.Warn("фронтенд не найден — работает только API", "папка", s.cfg.PublicDir)
		return http.NotFoundHandler()
	}
	files := http.FileServer(http.Dir(s.cfg.PublicDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeJSON(w, 404, errBody("Не найдено"))
			return
		}
		path := filepath.Join(s.cfg.PublicDir, filepath.FromSlash(filepath.Clean("/"+r.URL.Path)))
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			if strings.HasPrefix(r.URL.Path, "/assets/") {
				// В именах собранных файлов есть хеш — их можно кешировать надолго
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			files.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, index)
	})
}

// ── JSON и ошибки ─────────────────────────────────────────────────────

type errorBody struct {
	Error string `json:"error"`
}

func errBody(msg string) errorBody { return errorBody{msg} }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError — ошибка → HTTP-код: 404, 400, код ошибки входа или 500 (подробности только в лог).
func writeError(w http.ResponseWriter, err error) {
	var ae AuthError
	switch {
	case errors.As(err, &ae):
		writeJSON(w, ae.Status, errBody(ae.Msg))
	case service.IsNotFound(err):
		writeJSON(w, 404, errBody(err.Error()))
	case service.IsBadRequest(err):
		writeJSON(w, 400, errBody(err.Error()))
	default:
		slog.Error("ошибка запроса", "ошибка", err)
		writeJSON(w, 500, errBody("Внутренняя ошибка сервера"))
	}
}

func respond(w http.ResponseWriter, v any, err error) {
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, v)
}

func noContent(w http.ResponseWriter, err error) {
	if err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(204)
}

const maxBody = 256 * 1024

// readJSON — разобрать тело запроса (до 256 КБ). Пустое тело — не ошибка.
func readJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxBody))
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		return service.BadRequestError{Msg: "Неверный JSON в запросе"}
	}
	return nil
}

// required — обязательное текстовое поле: не пустое и не длиннее max символов.
func required(v, field string, max int) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", service.BadRequestError{Msg: "Поле " + field + " обязательно"}
	}
	if len([]rune(v)) > max {
		return "", service.BadRequestError{Msg: "Поле " + field + " слишком длинное"}
	}
	return v, nil
}

// clientIP — адрес клиента; за прокси Railway — первый адрес из X-Forwarded-For.
func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		return strings.TrimSpace(strings.Split(fwd, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Unwrap и Hijack — нужны, чтобы WebSocket мог перехватить соединение через обёртку
func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

func (s *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(s.ResponseWriter).Hijack()
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(sw, r)
		slog.Info("запрос", "метод", r.Method, "путь", r.URL.Path, "код", sw.status, "мс", time.Since(start).Milliseconds())
	})
}

// Shutdown — аккуратная остановка: дождаться текущих запросов и сохранить данные.
func Shutdown(ctx context.Context, srv *http.Server, svc *service.Service, auth *Auth) {
	_ = srv.Shutdown(ctx)
	svc.Flush()
	auth.Close()
}
