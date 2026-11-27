"""Сборка компактного каталога для ИИ-консультанта из товарного фида Store (YML).

Запуск (из папки support-assistant):
    python3 data/build_catalog.py Mindbox.yml.gz.xml backend/data/catalog.json.gz

Что делает:
  * берёт доступные позиции с ценой, только ПО, без предложений для физлиц и учебных заведений;
  * группирует позиции в «семейства» — вендор + название без количества, срока и типа лицензии
    (например, «Доктор Веб · Комплексная защита + Центр управления»);
  * для каждой позиции (варианта) разбирает срок, тип лицензии (новая / продление / миграция / поддержка),
    количество и способ расчёта цены:
        per_unit — цена за единицу, иногда в диапазоне количества (Kaspersky: «от 10 до 14 узлов»);
        pack     — цена за пакет на N единиц (MDaemon «10 пользователей», Dr.Web «на 248 ПК»).
"""
import gzip, json, os, re, sys, xml.etree.ElementTree as ET
from collections import defaultdict

src, dst = sys.argv[1], sys.argv[2]

# Российские разработчики — по списку вендоров (проверить перед питчем).
RU_VENDORS = ['Доктор Веб', 'Лаборатория Касперского', 'Группа Астра', 'Нанософт', 'НТП Трубопровод', 'CSoft',
    'АСКОН', 'Киберпротект', 'КриптоПро', 'Панорама', 'Эшелон', 'Код безопасности', 'РОСА', 'BaseALT', 'Базальт',
    'Яндекс', 'Смарт-Cофт', 'Смарт-Софт', '1С', 'Интеграл', 'РЕД СОФТ', 'Стахановец', 'Инсайдер', 'RuDesktop',
    'Инфотекс', 'ИнфоТеКС', 'Positive Technologies', 'Алтэкс', 'АЛТЭКС', 'Гарда', 'Индид', 'Аладдин', 'СёрчИнформ',
    'SearchInform', 'Р7', 'МойОфис', 'Новые облачные технологии', 'Postgres Professional', 'ГРАФТЕХ', 'НППКТ',
    'Ассистент', 'Инвест Лайн', 'Контур', 'Content AI', 'WebSoft', 'Вебсофт', 'Avanpost', 'Аванпост', 'VK Tech',
    'Солар', 'Softline', 'Софтлайн', 'UserGate', 'Юзергейт', 'Газинформсервис', 'Атом Безопасность', 'StaffCop', 'Лаборатория МБК', 'Хайстекс']

UNIT_WORDS = [
    (r'ПК|узл\w*|улов|устройств\w*|рабоч\w* мест\w*|компьютер\w*|хост\w*|актив\w*', 'device'),
    (r'пользовател\w*|учетн\w* запис\w*|сотрудник\w*', 'user'),
    (r'почтов\w* ящик\w*|ящик\w*', 'mailbox'),
    (r'сервер\w*|ядер|ядр\w*|CPU|сокет\w*|процессор\w*', 'server'),
]
QTY_RE = re.compile(r'(?:^|[\s,(])(?:на\s+|и\s+|до\s+)?(\d{1,6})\s*(' + '|'.join(w for w, _ in UNIT_WORDS) + r')', re.I)
TIER_RE = re.compile(r'К-во (?:узлов|пользователей|устройств)[^\d]*от (\d+) до (\d+)')
TERM_RE = re.compile(r'(\d{1,2})\s*(год|года|лет|мес\w*)', re.I)

KIND_RULES = [
    (r'продлен', 'renewal'),
    (r'миграц|cross-?grade|переход\s+с|перекрестн', 'migration'),
    (r'техподдерж|техническ\w* поддерж|сопровожден|сертификат на', 'support'),
    (r'обновлен|upgrade|\bupg\b', 'upgrade'),
    (r'дистрибутив|носител', 'media'),
]


PARAM_KIND = {'Продление': 'renewal', 'Техподдержка': 'support', 'Обновление': 'upgrade', 'Upg': 'upgrade',
              'Перекрестное обновление': 'migration', 'Миграция': 'migration', 'Дистрибутив': 'media',
              'Полная версия': 'new', 'Подписка': 'new', 'Базовая': 'new', 'Full': 'new', 'Base': 'new'}


def kind_of(name, ptype):
    n = name.lower()
    # Явные слова в названии важнее параметра: «продление лицензии», «Cross-grade»
    if re.search(r'продлен', n): return 'renewal'
    if re.search(r'миграц|cross-?grade|переход\s+с|перекрестн', n): return 'migration'
    if ptype in PARAM_KIND: return PARAM_KIND[ptype]
    # «лицензия с подпиской на обновления» — это новая лицензия, не обновление
    n = re.sub(r'подписк\w* на обновлени\w*|с обновлени\w*', ' ', n)
    for pat, k in KIND_RULES:
        if re.search(pat, n):
            return k
    return 'new'


def unit_of(word):
    for pat, u in UNIT_WORDS:
        if re.fullmatch(pat, word, re.I):
            return u
    return 'license'


def term_of(name, param):
    if param and 'бессроч' in param: return 0
    if 'бессроч' in name.lower(): return 0
    m = re.search(r'\d+', param or '')
    if m: return int(m.group())
    m = TERM_RE.search(name)
    if m:
        n = int(m.group(1))
        return n if m.group(2).lower().startswith('мес') else n * 12
    return None


def clean_vendor(v):
    v = re.sub(r'^(ООО|АО|ПАО|ЗАО|НПО)\s+', '', v.strip())
    v = re.sub(r',?\s*(Ltd\.?|Inc\.?|LLC|GmbH)$', '', v)
    return v.strip(' «»"')


NOISE = [
    r'\([^)]*\)?',                                   # всё в скобках (обычно тип лицензии и срок)
    r'\bпо количеству\b.*$',
    r'\b(?:на\s+)?\d{1,2}\s*(?:год|года|лет|мес\w*)\b',
    r'\bбессрочн\w*\b',
    r'\bпродлени\w*(?:\s+лицензи\w*)?\b',
    r'\b(?:базов\w*\s+)?лицензи\w*(?:\s+Base)?\b',
    r'\bCross-?grade\s+на\s+лицензию\b',
    r'\bмиграци\w*\b', r'\bверсия\b',
    r'\bBase\b',
    r'\bCross-?grade(?:\s+на)?\b',
    r'\bперекрестн\w*\s+обновлени\w*\b',
    r'\bновая продажа\b',
    r'\s*\.\s*Количество.*$',
    r',?\s*Сертификат на сопровождение ПО.*$',
    r'\s+на\s*$',
]


def family_title(name, vendor):
    t = name
    for prefix in (vendor, clean_vendor(vendor)):
        if prefix and t.lower().startswith(prefix.lower()):
            t = t[len(prefix):]
            break
    t = QTY_RE.sub(' ', t)
    for pat in NOISE:
        t = re.sub(pat, ' ', t, flags=re.I)
    t = re.sub(r'\s+и\s*$', '', t)
    t = re.sub(r'\s{2,}', ' ', t).strip(' ,.-–«»"')
    # Длинный хвост через запятую — это описание позиции, а не название продукта
    if len(t) > 48 and ', ' in t:
        t = t.split(', ')[0]
    t = re.sub(r'\s+,', ',', t).strip(' ,.-–«»"')
    return t[:90] or name[:90]


cats = {}
fams = {}
for ev, el in ET.iterparse(src, events=('end',)):
    if el.tag == 'category':
        cats[el.get('id')] = (el.text or '', el.get('parentId'))
        continue
    if el.tag != 'offer':
        continue
    try:
        if el.get('available') != 'true':
            continue
        price = float(el.findtext('price') or 0)
        if price <= 0:
            continue
        ps = {}
        for pr in el.findall('param'):
            ps.setdefault(pr.get('name'), pr.text or '')
        if ps.get('Тип продукта') != 'софт':
            continue
        if ps.get('Пользователь') == 'Физлицо' or ps.get('Тип организации') in ('Для дома', 'Академическая'):
            continue
        name = re.sub(r'\s+', ' ', (el.findtext('name') or '')).strip()
        desc = el.findtext('description') or ''
        vendor_raw = (el.findtext('vendor') or '').strip()
        vendor = clean_vendor(vendor_raw)
        title = family_title(name, vendor_raw)
        key = (vendor.lower(), title.lower())

        tier = TIER_RE.search(desc)
        q = QTY_RE.search(name)
        if tier:
            lo, hi, mode, unit = int(tier.group(1)), int(tier.group(2)), 'per_unit', 'device'
        elif q:
            lo = hi = int(q.group(1)); mode = 'pack'; unit = unit_of(q.group(2))
        else:
            lo, hi, mode, unit = 1, None, 'per_unit', 'license'
        if 'по количеству' in name:
            mode = 'per_unit'
            m = re.search(r'по количеству\s+(\w+)', name)
            if m and unit == 'license':
                unit = unit_of(m.group(1)) if unit_of(m.group(1)) != 'license' else 'device'

        reg = ps.get('Номер записи в реестре отечественного ПО', '')
        reg = reg if reg and 'другое' not in reg else None
        plat = re.search(r'Платформа: ([^•]+)', desc)
        org = ps.get('Тип организации') or None

        f = fams.get(key)
        if not f:
            f = fams[key] = {
                'id': f'f{len(fams) + 1}',
                'vendor': vendor,
                'title': title,
                'cat': el.findtext('categoryId'),
                'registry': reg,
                'platforms': plat.group(1).strip().replace('/', ', ') if plat else None,
                'picture': el.findtext('picture'),
                'unit': unit,
                'variants': [],
            }
        if reg and not f['registry']:
            f['registry'] = reg
        if f['unit'] == 'license' and unit != 'license':
            f['unit'] = unit
        f['variants'].append({
            'id': el.get('id'),
            'name': name,
            'price': round(price, 2),
            'term': term_of(name, ps.get('Срок действия')),
            'kind': kind_of(name, ps.get('Тип лицензии')),
            'min': lo, 'max': hi, 'mode': mode,
            'org': {'Коммерческая': 'com', 'Государственная': 'gov', 'Некоммерческая': 'nko'}.get(org),
            'url': el.findtext('url'),
        })
    finally:
        el.clear()


def path(cid):
    out = []
    while cid and cid in cats and len(out) < 6:
        n, p = cats[cid]
        out.append(n)
        cid = p
    return list(reversed(out))


out = []
for f in fams.values():
    f['path'] = path(f.pop('cat'))
    f['ru'] = bool(f['registry']) or any('Импортозамещ' in x for x in f['path']) or any(
        k.lower() in f['vendor'].lower() for k in RU_VENDORS)
    f['variants'].sort(key=lambda x: (x['kind'] != 'new', x['term'] or 0, x['min'] or 0, x['price']))
    out.append(f)

out.sort(key=lambda f: (-len(f['variants']), f['title']))
data = json.dumps({'generatedFrom': os.path.basename(src), 'families': out}, ensure_ascii=False,
                  separators=(',', ':')).encode()
os.makedirs(os.path.dirname(dst) or '.', exist_ok=True)
with gzip.open(dst, 'wb', 9) as fh:
    fh.write(data)
nv = sum(len(f['variants']) for f in out)
print(f'families={len(out)} variants={nv} registry={sum(1 for f in out if f["registry"])} '
      f'ru={sum(1 for f in out if f["ru"])} size={len(data) // 1024}KB gz={os.path.getsize(dst) // 1024}KB')
