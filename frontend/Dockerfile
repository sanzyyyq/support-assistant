# syntax=docker/dockerfile:1

# ── 1. Сборка фронтенда ───────────────────────────────────────────────
FROM node:20-alpine AS build
WORKDIR /app

COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund

COPY . .

# true — заглушка в браузере (по умолчанию), false — настоящий бэкенд через /api
ARG VITE_USE_MOCK=true
ARG VITE_API_URL=/api
ENV VITE_USE_MOCK=$VITE_USE_MOCK \
    VITE_API_URL=$VITE_API_URL

RUN npm run build

# ── 2. Раздача статики через nginx ────────────────────────────────────
FROM nginx:1.27-alpine AS web

# Куда проксировать /api, когда появится бэкенд (подставляется при старте контейнера)
ENV BACKEND_URL=http://backend:8000

COPY nginx.conf.template /etc/nginx/templates/default.conf.template
COPY --from=build /app/dist /usr/share/nginx/html

EXPOSE 80
HEALTHCHECK --interval=30s --timeout=3s --retries=3 CMD wget -qO- http://127.0.0.1/healthz || exit 1
