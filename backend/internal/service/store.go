package service

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"support-assistant/internal/agent"
)

// Store — обращения, сообщения и КП в одном JSON-файле (db.json на томе /data).
// Запись — с задержкой 300 мс (несколько изменений подряд пишутся одним разом) и атомарно: через временный файл.
// Данные читаются и меняются только под мьютексом mu (он общий с Service).
type Store struct {
	mu    *sync.Mutex
	file  string
	seed  func() *agent.DB
	db    *agent.DB
	timer *time.Timer
}

func NewStore(mu *sync.Mutex, file string, seed func() *agent.DB) *Store {
	return &Store{mu: mu, file: file, seed: seed}
}

// Load — прочитать файл; если его нет или он испорчен — создать демо-данные. Вызывать под mu.
func (s *Store) Load() {
	data, err := os.ReadFile(s.file)
	if err == nil {
		var db agent.DB
		if err = json.Unmarshal(data, &db); err == nil {
			s.db = &db
			slog.Info("данные загружены", "файл", s.file, "обращений", len(db.Tickets))
			return
		}
		slog.Warn("файл данных испорчен — создаю демо-данные", "файл", s.file, "ошибка", err)
	} else if !errors.Is(err, os.ErrNotExist) {
		slog.Warn("не удалось прочитать данные — создаю демо-данные", "ошибка", err)
	}
	s.db = s.seed()
	s.write()
}

// DB — текущие данные. Вызывать под mu.
func (s *Store) DB() *agent.DB { return s.db }

// Changed — данные изменились: записать на диск через 300 мс. Вызывать под mu.
func (s *Store) Changed() {
	if s.timer != nil {
		return
	}
	s.timer = time.AfterFunc(300*time.Millisecond, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.timer = nil
		s.write()
	})
}

// Reset — пересоздать демо-данные. Вызывать под mu.
func (s *Store) Reset() {
	s.db = s.seed()
	s.write()
}

// Flush — записать несохранённое сразу (при остановке сервера).
func (s *Store) Flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	if s.db != nil {
		s.write()
	}
}

func (s *Store) write() {
	data, err := json.Marshal(s.db)
	if err != nil {
		slog.Error("не удалось сериализовать данные", "ошибка", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.file), 0o755); err != nil {
		slog.Error("не удалось создать папку данных", "ошибка", err)
		return
	}
	tmp := s.file + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		slog.Error("не удалось записать данные", "ошибка", err)
		return
	}
	if err := os.Rename(tmp, s.file); err != nil {
		slog.Error("не удалось записать данные", "ошибка", err)
	}
}
