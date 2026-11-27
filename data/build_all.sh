#!/bin/sh
# Пересобрать данные сервера (backend/data). Запускается в контейнере из docker compose (сервис catalog),
# папка support-assistant смонтирована в /work.
set -eu

FEED="${FEED:-Mindbox.yml.gz.xml}"
CHATS="${CHATS:-чаты поддержки}"

if [ -f "$FEED" ]; then
  echo "→ Каталог из $FEED"
  python3 -I /scripts/build_catalog.py "$FEED" backend/data/catalog.json.gz
else
  echo "! Фид $FEED не найден — каталог не пересобран (положите файл в папку support-assistant)"
fi

if [ -d "$CHATS" ]; then
  echo "→ Выжимка из переписок «$CHATS»"
  python3 -I /scripts/build_chats.py "$CHATS" backend/data/chats.json
else
  echo "! Папка «$CHATS» не найдена — переписки не обработаны"
fi

echo "Готово. Пересоберите фронтенд: docker compose up --build web"
