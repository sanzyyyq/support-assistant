"""Выжимка из обезличенных переписок поддержки для фронтенда.

Запуск (из папки support-assistant):
    python3 data/build_chats.py "чаты поддержки" backend/data/chats.json

Формат строки в файлах:  #103033 [Категория] [страница сайта] текст обращения клиента
Результат: частоты категорий (для аналитики) и по несколько примеров на категорию
(для демо-обращений и проверки бота на реальных формулировках).
"""
import json, os, re, sys
from collections import Counter, defaultdict

src, dst = sys.argv[1], sys.argv[2]
LINE = re.compile(r'^#(\d+)\s+\[([^\]]+)\]\s+\[([^\]]*)\]\s*(.*)$')

items = []
for fn in sorted(os.listdir(src)):
    if not fn.endswith('.txt'):
        continue
    for line in open(os.path.join(src, fn), encoding='utf-8'):
        m = LINE.match(line.strip())
        if m:
            items.append({'id': int(m.group(1)), 'category': m.group(2), 'page': m.group(3), 'text': m.group(4).strip()})

counts = Counter(i['category'] for i in items)
by_cat = defaultdict(list)
for i in items:
    t = i['text']
    # короткие, но содержательные реплики без вложений — лучшие примеры
    if 40 <= len(t) <= 320 and '[отправил файл]' not in t:
        by_cat[i['category']].append(i)

samples = []
for cat, lst in by_cat.items():
    samples += sorted(lst, key=lambda x: -x['id'])[:6]

json.dump({
    'total': len(items),
    'categories': [{'label': k, 'count': v} for k, v in counts.most_common()],
    'samples': samples,
}, open(dst, 'w', encoding='utf-8'), ensure_ascii=False, indent=1)
print(f'items={len(items)} categories={len(counts)} samples={len(samples)}')
