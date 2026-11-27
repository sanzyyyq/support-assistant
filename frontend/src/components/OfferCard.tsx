import { AlertTriangle, Check, ExternalLink, Star } from 'lucide-react';
import type { Offer } from '../api/types';
import { calcLabel } from '../lib/domain';
import { formatPrice, termText } from '../lib/format';
import { OriginBadge } from './ProductCard';
import { Button } from './ui';

interface Props {
  offer: Offer;
  selected?: boolean;
  onChoose?: (offer: Offer) => void;
}

/** Вариант решения с обоснованием — позиции и цены из каталога Store */
export function OfferCard({ offer, selected, onChoose }: Props) {
  const allRegistry = offer.items.every((i) => i.product.registryNumber);
  const allRu = offer.items.every((i) => i.product.russianVendor);
  const terms = [...new Set(offer.items.map((i) => termText(i.product.termMonths)))];
  return (
    <article className={`offer${offer.recommended ? ' is-recommended' : ''}${selected ? ' is-selected' : ''}`}>
      <header className="offer__head">
        <h4 className="offer__title">{offer.title.replace(/ · .*/, '')}</h4>
        {offer.recommended && (
          <span className="offer__badge">
            <Star size={12} fill="currentColor" aria-hidden /> Рекомендую
          </span>
        )}
        {selected && <span className="offer__badge offer__badge--selected">Выбрано</span>}
      </header>

      <ul className="offer__items">
        {offer.items.map((i) => (
          <li key={i.product.id}>
            <div className="offer__item-main">
              <span className="offer__vendor">{i.product.vendor}</span>
              <a className="offer__name" href={i.product.url} target="_blank" rel="noreferrer" title={i.product.offerName}>
                {i.product.name}
                <ExternalLink size={12} aria-hidden />
              </a>
              <span className="offer__calc">
                {calcLabel(i)} · {termText(i.product.termMonths)}
              </span>
            </div>
            <span className="offer__sum">{formatPrice(i.total)}</span>
          </li>
        ))}
      </ul>

      <ul className="offer__reasons" aria-label="Почему подходит">
        {offer.reasons.map((r) => (
          <li key={r}>
            <Check size={14} aria-hidden />
            {r}
          </li>
        ))}
        {offer.notes?.map((n) => (
          <li key={n} className="is-note">
            <AlertTriangle size={14} aria-hidden />
            {n}
          </li>
        ))}
      </ul>

      <footer className="offer__foot">
        <div className="offer__total">
          <span>Итого{terms.length === 1 ? ` · ${terms[0]}` : ''}</span>
          <b>{formatPrice(offer.total)}</b>
          <span className="offer__vat">{allRegistry ? 'НДС не облагается' : 'Цена с учётом налогов'}</span>
        </div>
        {allRu && <OriginBadge compact registryNumber={allRegistry ? offer.items[0].product.registryNumber : undefined} russianVendor />}
      </footer>
      {onChoose && (
        <Button variant={offer.recommended ? 'primary' : 'secondary'} block onClick={() => onChoose(offer)} disabled={selected}>
          {selected ? 'Вариант выбран' : 'Выбрать'}
        </Button>
      )}
    </article>
  );
}
