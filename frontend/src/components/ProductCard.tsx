import { Archive, ExternalLink, FileText, HardDrive, Mail, MonitorSmartphone, Package, ShieldCheck, type LucideIcon } from 'lucide-react';
import type { Product } from '../api/types';
import { licenseLabel, priceLabel } from '../lib/domain';

/** Иконка по разделу каталога */
export function iconFor(path: string[]): LucideIcon {
  const p = path.join(' ');
  if (/почт/i.test(p)) return Mail;
  if (/резервного|бэкап/i.test(p)) return Archive;
  if (/Антивирус|безопасн|защит/i.test(p)) return ShieldCheck;
  if (/Операционн/i.test(p)) return HardDrive;
  if (/Офисн|документ|текст/i.test(p)) return FileText;
  if (/САПР|графи|дизайн/i.test(p)) return MonitorSmartphone;
  return Package;
}

/** Происхождение ПО: номер в реестре — только из фида; «российский разработчик» — по списку вендоров */
export function OriginBadge({ registryNumber, russianVendor, compact }: { registryNumber?: string; russianVendor?: boolean; compact?: boolean }) {
  if (registryNumber) {
    return (
      <span className="registry" title="Номер записи в реестре российского ПО из каталога Store — лицензии не облагаются НДС">
        <i aria-hidden>RU</i>
        {compact ? `Реестр № ${registryNumber}` : `В реестре ПО, № ${registryNumber}`}
      </span>
    );
  }
  if (russianVendor) {
    return (
      <span className="registry registry--soft" title="Российский разработчик. Номер записи в реестре в каталоге не указан — проверит менеджер">
        <i aria-hidden>RU</i>
        {compact ? 'Российское ПО' : 'Российский разработчик'}
      </span>
    );
  }
  return null;
}

/** Карточка позиции каталога */
export function ProductCard({ product }: { product: Product }) {
  const Icon = iconFor(product.categoryPath);
  return (
    <article className="pcard">
      <header className="pcard__head">
        <span className="pcard__icon" aria-hidden>
          <Icon size={20} />
        </span>
        <div className="pcard__title">
          <span className="pcard__vendor">{product.vendor}</span>
          <h4 className="pcard__name">{product.name}</h4>
        </div>
      </header>
      <p className="pcard__desc">{product.categoryPath.slice(-2).join(' › ')}</p>
      <div className="pcard__meta">{licenseLabel(product)}</div>
      {product.platforms.length > 0 && <div className="pcard__meta">{product.platforms.join(', ')}</div>}
      <OriginBadge registryNumber={product.registryNumber} russianVendor={product.russianVendor} />
      <div className="pcard__foot">
        <b>{priceLabel(product)}</b>
        <a className="pcard__link" href={product.url} target="_blank" rel="noreferrer">
          На Store <ExternalLink size={14} aria-hidden />
        </a>
      </div>
    </article>
  );
}
