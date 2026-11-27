# Утилита пересборки данных: каталог из товарного фида и выжимка из переписок.
# Только стандартная библиотека Python — зависимости не нужны.
FROM python:3.12-slim

WORKDIR /work
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1

COPY build_catalog.py build_chats.py build_all.sh /scripts/
RUN chmod +x /scripts/build_all.sh

ENTRYPOINT ["/scripts/build_all.sh"]
