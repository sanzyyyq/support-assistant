package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/scrypt"

	"support-assistant/internal/agent"
	"support-assistant/internal/model"
)

// Сотрудники в Postgres: пароли — scrypt с солью, сессии — случайный токен в HttpOnly-cookie,
// в базе хранится только его хеш. Выход, смена пароля и отключение сотрудника сразу закрывают сессии.

const (
	sessionCookie = "sa_operator"
	sessionTTL    = 7 * 24 * time.Hour
	minPassword   = 8
)

// AuthError — ошибка входа или управления сотрудниками с HTTP-кодом.
type AuthError struct {
	Status int
	Msg    string
}

func (e AuthError) Error() string { return e.Msg }

// Auth — сотрудники и сессии.
type Auth struct {
	db *pgxpool.Pool
	// Хеш-заглушка: проверяем пароль даже для несуществующего логина, чтобы время ответа не выдавало логины
	dummyHash string
	limiter   *loginLimiter
}

// ── База ──────────────────────────────────────────────────────────────

var migrations = []string{
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
}

// NewAuth — подключиться к Postgres (с повторами, пока база поднимается) и создать таблицы.
func NewAuth(ctx context.Context, databaseURL string) (*Auth, error) {
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
			return nil, fmt.Errorf("Postgres недоступен: %w", err)
		}
		slog.Warn("Postgres недоступен, жду", "попытка", attempt, "ошибка", err)
		time.Sleep(1500 * time.Millisecond)
	}
	for _, sql := range migrations {
		if _, err := pool.Exec(ctx, sql); err != nil {
			return nil, fmt.Errorf("создание таблиц: %w", err)
		}
	}
	dummy, err := hashPassword(randomString(12))
	if err != nil {
		return nil, err
	}
	return &Auth{db: pool, dummyHash: dummy, limiter: newLoginLimiter()}, nil
}

func (a *Auth) Close() { a.db.Close() }

// ── Пароли (формат совместим с прежней версией сервера на Node) ───────

const scryptN, scryptR, scryptP, scryptKeyLen = 16384, 8, 1, 64

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := scrypt.Key([]byte(password), salt, scryptN, scryptR, scryptP, scryptKeyLen)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("scrypt$%d$%d$%d$%s$%s", scryptN, scryptR, scryptP,
		base64.StdEncoding.EncodeToString(salt), base64.StdEncoding.EncodeToString(key)), nil
}

func verifyPassword(password, stored string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 6 || parts[0] != "scrypt" {
		return false
	}
	n, _ := strconv.Atoi(parts[1])
	r, _ := strconv.Atoi(parts[2])
	p, _ := strconv.Atoi(parts[3])
	salt, err1 := base64.StdEncoding.DecodeString(parts[4])
	want, err2 := base64.StdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil || n == 0 {
		return false
	}
	key, err := scrypt.Key([]byte(password), salt, n, r, p, len(want))
	return err == nil && subtle.ConstantTimeCompare(key, want) == 1
}

func checkPasswordPolicy(p string) error {
	if len([]rune(p)) < minPassword {
		return AuthError{400, fmt.Sprintf("Пароль — не короче %d символов", minPassword)}
	}
	if len(p) > 200 {
		return AuthError{400, "Пароль слишком длинный"}
	}
	return nil
}

func randomString(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func sha256hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// ── Сотрудники ────────────────────────────────────────────────────────

const userCols = `id, login, name, role, active, created_at, last_login_at, password_hash`

type userRow struct {
	model.StaffUser
	hash string
}

func scanUser(row pgx.Row) (*userRow, error) {
	var u userRow
	var created time.Time
	var last *time.Time
	err := row.Scan(&u.ID, &u.Login, &u.Name, &u.Role, &u.Active, &created, &last, &u.hash)
	if err != nil {
		return nil, err
	}
	u.CreatedAt = agent.ISO(created)
	if last != nil {
		u.LastLoginAt = agent.ISO(*last)
	}
	return &u, nil
}

func (a *Auth) ListUsers(ctx context.Context) ([]model.StaffUser, error) {
	rows, err := a.db.Query(ctx, `SELECT `+userCols+` FROM users ORDER BY active DESC, role, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.StaffUser{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u.StaffUser)
	}
	return out, rows.Err()
}

var reLogin = regexp.MustCompile(`^[a-zA-Z0-9._@-]{3,64}$`)

func cleanLogin(v string) (string, error) {
	v = strings.TrimSpace(v)
	if !reLogin.MatchString(v) {
		return "", AuthError{400, "Логин: 3–64 символа, латиница, цифры и . _ @ -"}
	}
	return v, nil
}

func cleanName(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" || len([]rune(v)) > 100 {
		return "", AuthError{400, "Укажите имя (до 100 символов)"}
	}
	return v, nil
}

func cleanRole(v string) (string, error) {
	if v != "admin" && v != "manager" {
		return "", AuthError{400, "Роль: admin или manager"}
	}
	return v, nil
}

// NewUser — тело запроса «добавить сотрудника».
type NewUser struct {
	Login    string `json:"login"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	Password string `json:"password"`
}

func (a *Auth) CreateUser(ctx context.Context, in NewUser) (model.StaffUser, error) {
	login, err := cleanLogin(in.Login)
	if err != nil {
		return model.StaffUser{}, err
	}
	name, err := cleanName(in.Name)
	if err != nil {
		return model.StaffUser{}, err
	}
	if in.Role == "" {
		in.Role = "manager"
	}
	role, err := cleanRole(in.Role)
	if err != nil {
		return model.StaffUser{}, err
	}
	if err := checkPasswordPolicy(in.Password); err != nil {
		return model.StaffUser{}, err
	}
	hash, err := hashPassword(in.Password)
	if err != nil {
		return model.StaffUser{}, err
	}
	u, err := scanUser(a.db.QueryRow(ctx,
		`INSERT INTO users (login, name, role, password_hash) VALUES ($1, $2, $3, $4) RETURNING `+userCols, login, name, role, hash))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return model.StaffUser{}, AuthError{409, "Логин «" + login + "» уже занят"}
	}
	if err != nil {
		return model.StaffUser{}, err
	}
	return u.StaffUser, nil
}

// UserPatch — что администратор меняет у сотрудника (nil — не менять).
type UserPatch struct {
	Name     *string `json:"name"`
	Role     *string `json:"role"`
	Active   *bool   `json:"active"`
	Password *string `json:"password"`
}

// UpdateUser — изменить сотрудника. В системе всегда остаётся хотя бы один активный администратор.
func (a *Auth) UpdateUser(ctx context.Context, actor model.Operator, id int, p UserPatch) (model.StaffUser, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return model.StaffUser{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // после Commit откат ничего не делает

	// Блокируем строки админов, чтобы два параллельных запроса не оставили систему без админа
	if _, err := tx.Exec(ctx, `SELECT id FROM users WHERE role = 'admin' FOR UPDATE`); err != nil {
		return model.StaffUser{}, err
	}
	u, err := scanUser(tx.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE id = $1 FOR UPDATE`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return model.StaffUser{}, AuthError{404, "Сотрудник не найден"}
	}
	if err != nil {
		return model.StaffUser{}, err
	}

	name, role, active := u.Name, u.Role, u.Active
	if p.Name != nil {
		if name, err = cleanName(*p.Name); err != nil {
			return model.StaffUser{}, err
		}
	}
	if p.Role != nil {
		if role, err = cleanRole(*p.Role); err != nil {
			return model.StaffUser{}, err
		}
	}
	if p.Active != nil {
		active = *p.Active
	}
	newPassword := p.Password != nil && *p.Password != ""
	if newPassword {
		if err := checkPasswordPolicy(*p.Password); err != nil {
			return model.StaffUser{}, err
		}
	}

	if u.ID == actor.ID && (!active || role != "admin") {
		return model.StaffUser{}, AuthError{400, "Нельзя отключить себя или снять с себя роль администратора"}
	}
	if u.Role == "admin" && u.Active && (role != "admin" || !active) {
		var others int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE role = 'admin' AND active AND id <> $1`, id).Scan(&others); err != nil {
			return model.StaffUser{}, err
		}
		if others == 0 {
			return model.StaffUser{}, AuthError{400, "Должен остаться хотя бы один активный администратор"}
		}
	}

	hash := u.hash
	if newPassword {
		if hash, err = hashPassword(*p.Password); err != nil {
			return model.StaffUser{}, err
		}
	}
	updated, err := scanUser(tx.QueryRow(ctx,
		`UPDATE users SET name = $2, role = $3, active = $4, password_hash = $5 WHERE id = $1 RETURNING `+userCols,
		id, name, role, active, hash))
	if err != nil {
		return model.StaffUser{}, err
	}
	// Отключили или сменили пароль — выкидываем из всех сессий
	if !active || newPassword {
		if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, id); err != nil {
			return model.StaffUser{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return model.StaffUser{}, err
	}
	return updated.StaffUser, nil
}

// ChangeOwnPassword — сотрудник меняет свой пароль; остальные его сессии закрываются.
func (a *Auth) ChangeOwnPassword(ctx context.Context, r *http.Request, me model.Operator, current, next string) error {
	if err := checkPasswordPolicy(next); err != nil {
		return err
	}
	u, err := scanUser(a.db.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE id = $1`, me.ID))
	if err != nil || !verifyPassword(current, u.hash) {
		return AuthError{400, "Текущий пароль неверен"}
	}
	hash, err := hashPassword(next)
	if err != nil {
		return err
	}
	if _, err := a.db.Exec(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1`, me.ID, hash); err != nil {
		return err
	}
	keep := ""
	if c, err := r.Cookie(sessionCookie); err == nil {
		keep = sha256hex(c.Value)
	}
	_, err = a.db.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1 AND token_hash <> $2`, me.ID, keep)
	return err
}

// EnsureFirstAdmin — первый запуск: таблица пустая — создаём администратора из ADMIN_* или со случайным паролем.
func (a *Auth) EnsureFirstAdmin(ctx context.Context) error {
	var n int
	if err := a.db.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	login := strings.TrimSpace(os.Getenv("ADMIN_LOGIN"))
	if login == "" {
		login = "admin"
	}
	name := strings.TrimSpace(os.Getenv("ADMIN_NAME"))
	if name == "" {
		name = "Администратор"
	}
	password := os.Getenv("ADMIN_PASSWORD")
	fromEnv := len([]rune(password)) >= minPassword
	if !fromEnv {
		password = randomString(12)
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	if _, err := a.db.Exec(ctx, `INSERT INTO users (login, name, role, password_hash) VALUES ($1, $2, 'admin', $3) ON CONFLICT DO NOTHING`,
		login, name, hash); err != nil {
		return err
	}
	if fromEnv {
		slog.Info("создан администратор (пароль из ADMIN_PASSWORD)", "логин", login)
	} else {
		slog.Warn("создан администратор со случайным паролем — смените его в кабинете (Профиль)", "логин", login, "пароль", password)
	}
	return nil
}

// ── Сессии ────────────────────────────────────────────────────────────

func cookieFor(r *http.Request, value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name: sessionCookie, Value: value, Path: "/", MaxAge: maxAge, HttpOnly: true, SameSite: http.SameSiteLaxMode,
		// За прокси Railway запрос приходит по http, исходный протокол — в X-Forwarded-Proto
		Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
	}
}

// OperatorOf — сотрудник по cookie сессии или nil.
func (a *Auth) OperatorOf(r *http.Request) *model.Operator {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" || len(c.Value) > 100 {
		return nil
	}
	var op model.Operator
	err = a.db.QueryRow(r.Context(),
		`SELECT u.id, u.login, u.name, u.role FROM sessions s JOIN users u ON u.id = s.user_id
		 WHERE s.token_hash = $1 AND s.expires_at > now() AND u.active`, sha256hex(c.Value)).
		Scan(&op.ID, &op.Login, &op.Name, &op.Role)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("проверка сессии", "ошибка", err)
		}
		return nil
	}
	return &op
}

func (a *Auth) Login(w http.ResponseWriter, r *http.Request, login, password string) (model.Operator, error) {
	u, err := scanUser(a.db.QueryRow(r.Context(), `SELECT `+userCols+` FROM users WHERE lower(login) = lower($1)`, strings.TrimSpace(login)))
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return model.Operator{}, err
	}
	hash := a.dummyHash
	if u != nil {
		hash = u.hash
	}
	ok := verifyPassword(password, hash)
	if u == nil || !ok {
		return model.Operator{}, AuthError{401, "Неверный логин или пароль"}
	}
	if !u.Active {
		return model.Operator{}, AuthError{401, "Учётная запись отключена"}
	}
	token := randomString(32)
	ctx := r.Context()
	if _, err := a.db.Exec(ctx, `DELETE FROM sessions WHERE expires_at < now()`); err != nil {
		return model.Operator{}, err
	}
	if _, err := a.db.Exec(ctx, `INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ($1, $2, $3)`,
		sha256hex(token), u.ID, time.Now().Add(sessionTTL)); err != nil {
		return model.Operator{}, err
	}
	if _, err := a.db.Exec(ctx, `UPDATE users SET last_login_at = now() WHERE id = $1`, u.ID); err != nil {
		return model.Operator{}, err
	}
	http.SetCookie(w, cookieFor(r, token, int(sessionTTL.Seconds())))
	return u.Operator, nil
}

func (a *Auth) Logout(w http.ResponseWriter, r *http.Request) error {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if _, err := a.db.Exec(r.Context(), `DELETE FROM sessions WHERE token_hash = $1`, sha256hex(c.Value)); err != nil {
			return err
		}
	}
	http.SetCookie(w, cookieFor(r, "", -1))
	return nil
}

// ── Ограничение попыток входа: 10 за 15 минут с одного адреса ─────────

type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string]*attempt
}

type attempt struct {
	count int
	until time.Time
}

func newLoginLimiter() *loginLimiter { return &loginLimiter{attempts: map[string]*attempt{}} }

// Allow — попытка разрешена (и учтена).
func (l *loginLimiter) Allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	a := l.attempts[ip]
	if a == nil || a.until.Before(now) {
		l.attempts[ip] = &attempt{count: 1, until: now.Add(15 * time.Minute)}
		if len(l.attempts) > 10_000 {
			for k, v := range l.attempts {
				if v.until.Before(now) {
					delete(l.attempts, k)
				}
			}
		}
		return true
	}
	a.count++
	return a.count <= 10
}

func (l *loginLimiter) Reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, ip)
}
