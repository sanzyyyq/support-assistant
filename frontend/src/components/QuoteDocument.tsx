import type { Quote } from '../api/types';
import { VENDOR } from '../config';
import { itemSum, itemVat, quoteTotals } from '../lib/domain';
import { formatDate, formatMoney } from '../lib/format';

/**
 * Документ КП по шаблону «Формат КП».
 * Логотип, подпись и печать в черновик не выводятся — их ставят в настоящем документе.
 */
export function QuoteDocument({ quote }: { quote: Quote }) {
  const t = quoteTotals(quote);
  const draft = quote.status === 'review';
  return (
    <div className="qdoc" data-draft={draft || undefined}>
      {draft && (
        <div className="qdoc__watermark" aria-hidden>
          ЧЕРНОВИК
        </div>
      )}
      <div className="qdoc__top">
        <div className="qdoc__vendor">
          <strong>{VENDOR.company}</strong>
          <span>{VENDOR.address}</span>
          <span>{VENDOR.contacts}</span>
          <span>{VENDOR.sites}</span>
        </div>
        <div className="qdoc__to">
          <span>Куда: {quote.company}</span>
          {quote.inn && <span>ИНН {quote.inn}</span>}
          <span>
            Исх. № бн от {formatDate(quote.createdAt)}
          </span>
          <span className="qdoc__num">{quote.number}</span>
        </div>
      </div>

      <p className="qdoc__greet">Уважаемый заказчик!</p>
      <p className="qdoc__lead">{VENDOR.company} предлагает Вашему вниманию коммерческое предложение на следующих условиях:</p>

      <div className="qdoc__table-wrap">
        <table className="qdoc__table">
          <thead>
            <tr>
              <th>Артикул</th>
              <th>Наименование</th>
              <th>Кол-во</th>
              <th>Цена, руб.</th>
              <th>Сумма, руб.</th>
              <th>В т.ч. НДС, руб.</th>
            </tr>
          </thead>
          <tbody>
            {quote.items.map((i) => (
              <tr key={i.sku}>
                <td>{i.sku}</td>
                <td className="qdoc__name">{i.name}</td>
                <td className="num">{i.qty}</td>
                <td className="num">{formatMoney(i.price)}</td>
                <td className="num">{formatMoney(itemSum(i))}</td>
                <td className="num">{i.vatRate === null ? 'уточняется' : i.vatRate ? formatMoney(itemVat(i)) : 'Без НДС'}</td>
              </tr>
            ))}
          </tbody>
          <tfoot>
            <tr>
              <td />
              <td className="qdoc__total-label">Итого</td>
              <td className="num">{t.qty}</td>
              <td />
              <td className="num">{formatMoney(t.sum)}</td>
              <td className="num">{t.vatUnknown ? (t.vat ? `${formatMoney(t.vat)} + уточн.` : 'уточняется') : t.vat ? formatMoney(t.vat) : 'Без НДС'}</td>
            </tr>
          </tfoot>
        </table>
      </div>

      <div className="qdoc__terms">
        <p>Цена указана с учетом всех налогов, сборов и других обязательных платежей.</p>
        {quote.items.some((i) => i.vatRate === 0) && <p>ПО из реестра российского ПО НДС не облагается (пп. 26 п. 2 ст. 149 НК РФ).</p>}
        {quote.items.some((i) => i.vatRate === null) && <p className="qdoc__todo">Сумму НДС по позициям «уточняется» подтверждает менеджер перед отправкой.</p>}
        <p>Условия оплаты: {quote.paymentTerms}.</p>
        <p>Срок поставки: {quote.deliveryTerm}.</p>
        <p>Срок действия коммерческого предложения — до {formatDate(quote.validUntil)}.</p>
      </div>

      <div className="qdoc__sign">
        <div>
          <span>{VENDOR.signer}</span>
          <span>{VENDOR.signerTitle}</span>
        </div>
        <div className="qdoc__stamp">
          {draft ? 'Подпись и печать — после проверки менеджером' : `Согласовано: ${quote.reviewer ?? 'менеджер'}`}
        </div>
      </div>
    </div>
  );
}
