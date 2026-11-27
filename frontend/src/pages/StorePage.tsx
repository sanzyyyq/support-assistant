import { useEffect, useState } from 'react';
import { MessageCircle, Search, Sparkles, X } from 'lucide-react';
import { featured, loadCatalog, type Family } from '../api/mock/catalog';
import type { Product } from '../api/types';
import { ChatWindow } from '../components/ChatWindow';
import { OriginBadge, iconFor } from '../components/ProductCard';
import { BRAND } from '../config';
import { formatNumber, formatPrice, termText, unitCount } from '../lib/format';
import '../styles/store.css';

const CATEGORIES = ['Антивирусы и безопасность', 'Операционные системы', 'Резервное копирование', 'Офисные программы', 'САПР и ГИС', 'Импортозамещение'];

/** Демо-витрина: показывает помощника в его настоящем формате — виджетом поверх сайта */
export function StorePage() {
  const [open, setOpen] = useState(false);
  const [teaser, setTeaser] = useState(true);
  const [prompt, setPrompt] = useState<{ text: string; key: number } | null>(null);
  const [tiles, setTiles] = useState<Array<{ family: Family; from: Product }>>([]);
  const [total, setTotal] = useState<number | null>(null);

  useEffect(() => {
    loadCatalog()
      .then((cat) => {
        setTiles(featured(cat));
        setTotal(cat.families.reduce((s, f) => s + f.variants.length, 0));
      })
      .catch(() => setTiles([]));
  }, []);

  const ask = (text: string) => {
    setPrompt({ text, key: Date.now() });
    setOpen(true);
    setTeaser(false);
  };

  return (
    <div className="store">
      <header className="store__header">
        <span className="store__logo">{BRAND.store}</span>
        <span className="store__demo">демо-витрина</span>
        <label className="store__search">
          <Search size={16} aria-hidden />
          <span className="sr-only">Поиск по каталогу</span>
          <input placeholder={total ? `Поиск по ${formatNumber(total)} позициям каталога` : 'Поиск по каталогу'} />
        </label>
      </header>

      <nav className="store__cats" aria-label="Категории">
        {CATEGORIES.map((c) => (
          <span key={c}>{c}</span>
        ))}
      </nav>

      <section className="store__hero">
        <div>
          <h1>Программное обеспечение для бизнеса</h1>
          <p>Не знаете, что выбрать? ИИ-консультант задаст пару вопросов, подберёт решение по каталогу Store под задачу и бюджет и сразу подготовит КП.</p>
          <div className="store__hero-actions">
            <button className="btn btn--primary" onClick={() => ask('Не знаю, что нужно, но хотим защитить почту и файлы на 50 сотрудников')}>
              <Sparkles size={16} /> Подобрать с ИИ
            </button>
            <button className="btn btn--secondary" onClick={() => ask('Хотим купить на фирму 85 лицензий Kaspersky Endpoint Security для бизнеса Стандартный. Какие есть и в чём разница?')}>
              85 лицензий Kaspersky — в чём разница?
            </button>
          </div>
        </div>
      </section>

      <section className="store__grid" aria-label="Популярные продукты">
        {!tiles.length && <p className="muted">Загружаю каталог…</p>}
        {tiles.map(({ family, from }) => {
          const Icon = iconFor(family.path);
          return (
            <article key={family.id} className="tile">
              <span className="tile__icon" aria-hidden>
                <Icon size={24} />
              </span>
              <span className="tile__vendor">{family.vendor}</span>
              <h3 className="tile__name">
                <a href={from.url} target="_blank" rel="noreferrer">
                  {family.title}
                </a>
              </h3>
              {family.ru ? <OriginBadge compact registryNumber={family.registry ?? undefined} russianVendor /> : <span className="tile__spacer" />}
              <b className="tile__price">
                от {formatPrice(from.price)}
                <span>
                  {from.pricing === 'pack' && from.tierMin ? ` за ${unitCount(from.tierMin, from.unit)}` : ''} · {termText(from.termMonths)}
                </span>
              </b>
              <button className="tile__ask" onClick={() => ask(`Расскажите про ${family.title}: что это и на каких платформах работает?`)}>
                <MessageCircle size={14} /> Спросить консультанта
              </button>
            </article>
          );
        })}
      </section>

      {open && (
        <div className="widget" role="dialog" aria-label="ИИ-консультант">
          <ChatWindow variant="widget" onClose={() => setOpen(false)} prompt={prompt} />
        </div>
      )}

      {!open && teaser && (
        <div className="launcher-teaser">
          <button className="launcher-teaser__close" aria-label="Скрыть подсказку" onClick={() => setTeaser(false)}>
            <X size={14} />
          </button>
          <button className="launcher-teaser__text" onClick={() => setOpen(true)}>
            Помочь подобрать ПО? Отвечу за секунды и подготовлю КП.
          </button>
        </div>
      )}
      {!open && (
        <button className="launcher" aria-label="Открыть ИИ-консультанта" onClick={() => setOpen(true)}>
          <Sparkles size={24} />
        </button>
      )}
    </div>
  );
}
