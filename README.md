# ИИ-консультант Store — хакатон ИИкузница

Агент поддержки для store.softline.ru: подбирает ПО по каталогу, сверяет лицензии,
отвечает по базе знаний, готовит КП. Фронтенд — в `frontend/`, подготовка данных — в `data/`.

## Запуск в Docker

Нужен только Docker (Desktop) с Compose.

```bash
docker compose up --build
```

Открыть:
- http://localhost:8080 — демо-витрина Store с виджетом консультанта
- http://localhost:8080/operator — кабинет менеджера (обращения, КП, аналитика)
- http://localhost:8080/chat — чат на весь экран

Остановить: `docker compose down`.

### Режим разработки (горячая перезагрузка)

```bash
docker compose --profile dev up dev
```

http://localhost:5173 — правки в `frontend/src` применяются сразу.

### Пересобрать каталог из нового фида

Положите `Mindbox.yml.gz.xml` и папку `чаты поддержки` в эту папку, затем:

```bash
docker compose --profile tools run --rm catalog
docker compose up --build web
```

### Настройки (переменные окружения или файл `.env` рядом с docker-compose.yml)

| Переменная | По умолчанию | Что делает |
| --- | --- | --- |
| `WEB_PORT` | `8080` | порт собранного приложения |
| `DEV_PORT` | `5173` | порт режима разработки |
| `VITE_USE_MOCK` | `true` | `false` — ходить в настоящий бэкенд вместо заглушки |
| `BACKEND_URL` | `http://backend:8000` | куда nginx проксирует `/api` |

Когда появится бэкенд, добавьте его сервисом `backend` в `docker-compose.yml` (порт 8000)
и запустите с `VITE_USE_MOCK=false docker compose up --build`. Контракт API — в `frontend/src/api/client.ts`.

Подробнее о фронтенде и данных — в [frontend/README.md](frontend/README.md).
