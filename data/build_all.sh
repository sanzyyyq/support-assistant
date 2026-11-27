#!/bin/sh
# Пересобрать данные для фронтенда. Запускается в контейнере из docker compose (сервис catalog),
# папка support-assistant смонтирована в /work.
set -eu

FEED="${FEED:-Mindbox.yml.gz.xml}"
CHATS="${CHATS:-чаты поддержки}"

if [ -f "$FEED" ]; then
  echo "→ Каталог из $FEED"
  python3 -I /scripts/build_catalog.py "$FEED" frontend/public/data/catalog.json.gz
else
  echo "! Фид $FEED не найден — каталог не пересобран (положите файл в папку support-assistant)"
fi

if [ -d "$CHATS" ]; then
  echo "→ Выжимка из переписок «$CHATS»"
  python3 -I /scripts/build_chats.py "$CHATS" frontend/src/api/mock/chats.json
else
  echo "! Папка «$CHATS» не найдена — переписки не обработаны"
fi

echo "Готово. Пересоберите фронтенд: docker compose up --build web"
