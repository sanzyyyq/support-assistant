# syntax=docker/dockerfile:1
# Единый образ: сервер на Go раздаёт API, WebSocket и собранный фронтенд.
# Railway и docker compose собирают именно его.

# ── 1. Фронтенд ───────────────────────────────────────────────────────
FROM node:20-alpine AS frontend
WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY frontend/ ./
RUN npm run build

# ── 2. Сервер на Go ───────────────────────────────────────────────────
FROM golang:1.23-alpine AS backend
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
# Статический бинарник без зависимостей от системы
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /server .

# ── 3. Запуск ─────────────────────────────────────────────────────────
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
ENV HOST=0.0.0.0 \
    PORT=8080 \
    PUBLIC_DIR=/app/public \
    DATA_DIR=/data

COPY --from=backend /server ./server
COPY --from=frontend /app/frontend/dist ./public
# Каталог Store из фида и выжимка из переписок поддержки (для аналитики)
COPY backend/data ./data

# Обращения и КП. Том на /data подключается снаружи: в docker-compose.yml и в настройках Railway
# (инструкцию VOLUME Railway не поддерживает)
RUN mkdir -p /data

EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=20s --retries=3 \
  CMD wget -qO- "http://127.0.0.1:${PORT}/healthz" || exit 1

CMD ["./server"]
