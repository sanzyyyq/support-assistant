# syntax=docker/dockerfile:1
# Единый образ: бэкенд (Node + Fastify) раздаёт API, WebSocket и собранный фронтенд.
# Railway и docker compose собирают именно его.

# ── 1. Фронтенд ───────────────────────────────────────────────────────
FROM node:20-alpine AS frontend
WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY frontend/ ./
# false — настоящий бэкенд (по умолчанию), true — офлайн-заглушка в браузере
ARG VITE_USE_MOCK=false
ENV VITE_USE_MOCK=$VITE_USE_MOCK
RUN npm run build

# ── 2. Бэкенд ─────────────────────────────────────────────────────────
FROM node:20-alpine AS backend
WORKDIR /app/backend
COPY backend/package.json backend/package-lock.json ./
RUN npm ci --no-audit --no-fund
# Логика агента, каталог и КП — общий код из frontend/src, вшивается в бандл сервера
COPY frontend/src /app/frontend/src
COPY backend/ ./
RUN npm run build && npm prune --omit=dev

# ── 3. Запуск ─────────────────────────────────────────────────────────
FROM node:20-alpine
WORKDIR /app
ENV NODE_ENV=production \
    HOST=0.0.0.0 \
    PORT=8080 \
    PUBLIC_DIR=/app/public \
    DATA_DIR=/data

COPY --from=backend /app/backend/node_modules ./node_modules
COPY --from=backend /app/backend/package.json ./package.json
COPY --from=backend /app/backend/dist ./dist
COPY --from=frontend /app/frontend/dist ./public

# Обращения и КП. На Railway сюда монтируется том, чтобы данные переживали перезапуск
RUN mkdir -p /data
VOLUME ["/data"]

EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=20s --retries=3 \
  CMD wget -qO- "http://127.0.0.1:${PORT}/healthz" || exit 1

CMD ["node", "dist/server.mjs"]
