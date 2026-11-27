package agent

import (
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"support-assistant/internal/model"
)

// Варианты решения: подбор под задачу, сверка редакций, российские аналоги, сравнение и КП.

var offerSeq atomic.Int64

// NewOfferID — уникальный id варианта (подменяется в тестах).
var NewOfferID = func() string {
	return "o-" + strconv.FormatInt(time.Now().UnixMilli(), 36) + "-" + strconv.FormatInt(offerSeq.Add(1), 10)
}

func itemFrom(p *Priced) model.OfferItem {
	return model.OfferItem{Product: ToProduct(p.Family, p.Variant), Qty: p.Qty, Packs: p.Packs, Total: p.Total}
}

func makeOffer(title string, items []model.OfferItem, reasons, notes []string) model.Offer {
	total := 0.0
	for _, i := range items {
		total += i.Total
	}
	if reasons == nil {
		reasons = []string{}
	}
	if len(notes) == 0 {
		notes = nil
	}
	return model.Offer{ID: NewOfferID(), Title: title, Items: items, Reasons: reasons, Notes: notes, Total: total}
}

var needText = map[model.Need]string{
	"mail": "защиту почты", "endpoint": "защиту ПК и файлов", "backup": "резервное копирование",
	"office": "офисные задачи", "os": "операционную систему",
}

func needTexts(ns []model.Need) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = needText[n]
	}
	return out
}

// reasonsFor — «почему подходит» и «на что обратить внимание» для набора позиций.
func reasonsFor(items []model.OfferItem, r model.Requirements, priced []*Priced, covered []model.Need, coveredSet bool) (reasons, notes []string) {
	reasons, notes = []string{}, []string{}
	total := 0.0
	for _, i := range items {
		total += i.Total
	}
	done := r.Needs
	if coveredSet {
		done = covered
	}
	if len(done) > 0 {
		reasons = append(reasons, "Закрывает "+joinAnd(needTexts(done)))
	}
	var left []model.Need
	for _, n := range r.Needs {
		if !contains(done, n) {
			left = append(left, n)
		}
	}
	if len(left) > 0 {
		notes = append(notes, "Не закрывает "+joinAnd(needTexts(left))+" — в каталоге нет подходящей позиции")
	}
	var reg []model.OfferItem
	allRU := true
	for _, i := range items {
		if i.Product.RegistryNumber != "" {
			reg = append(reg, i)
		}
		if !i.Product.RussianVendor {
			allRU = false
		}
	}
	switch {
	case len(reg) == len(items):
		reasons = append(reasons, "Всё ПО в реестре российского ПО — без НДС")
	case allRU:
		reasons = append(reasons, "Все продукты — российских разработчиков")
	default:
		var foreign []string
		for _, i := range items {
			if !i.Product.RussianVendor {
				foreign = append(foreign, i.Product.Vendor)
			}
		}
		if len(foreign) > 0 {
			notes = append(notes, strings.Join(uniqStrings(foreign), ", ")+" — зарубежный разработчик")
		}
	}
	for _, i := range reg {
		reasons = append(reasons, i.Product.Name+": в реестре, № "+i.Product.RegistryNumber)
	}
	vendors := map[string]bool{}
	for _, i := range items {
		vendors[i.Product.Vendor] = true
	}
	if len(vendors) == 1 && len(items) > 1 {
		reasons = append(reasons, "Один вендор — единая консоль и поддержка")
	}
	if r.Budget != nil && !r.Budget.Unlimited {
		if total <= r.Budget.Amount {
			reasons = append(reasons, "Укладывается в бюджет: "+FormatPrice(total)+" из "+FormatPrice(r.Budget.Amount))
		} else {
			notes = append(notes, "Выше бюджета на "+FormatPrice(total-r.Budget.Amount))
		}
	}
	for _, p := range priced {
		if p.Adjusted {
			notes = append(notes, p.Family.Title+": минимум "+UnitCount(p.Qty, p.Family.Unit)+" для этой цены")
		}
		if p.TermFallback {
			notes = append(notes, p.Family.Title+": срок "+TermText(p.Variant.Term)+" — другого в каталоге нет")
		}
	}
	return reasons, notes
}

// markRecommended — пометить лучший вариант: в бюджете, российский, с меньшим числом оговорок, дешевле.
func markRecommended(offers []model.Offer, r model.Requirements) []model.Offer {
	if len(offers) == 0 {
		return offers
	}
	budget := math.Inf(1)
	if r.Budget != nil && !r.Budget.Unlimited {
		budget = r.Budget.Amount
	}
	score := func(o model.Offer) float64 {
		s := 0.0
		if o.Total > budget {
			s += 2
		}
		for _, i := range o.Items {
			if !i.Product.RussianVendor {
				s++
				break
			}
		}
		return s + float64(len(o.Notes))*0.2 + o.Total/1e9
	}
	best := 0
	for i := range offers {
		if score(offers[i]) < score(offers[best]) {
			best = i
		}
	}
	out := make([]model.Offer, len(offers))
	for i, o := range offers {
		o.Recommended = boolPtr(i == best)
		out[i] = o
	}
	return out
}

// filtersFor — какие разделы каталога нужны под задачу.
func filtersFor(r model.Requirements) []string {
	var out []string
	for _, n := range r.Needs {
		if n == "mail" {
			if r.Infrastructure == "none" {
				out = append(out, "mail_server")
			}
			if r.Infrastructure != "cloud" {
				out = append(out, "mail")
			}
		} else {
			out = append(out, n)
		}
	}
	return out
}

// candidates — позиции под задачу: профильные первыми, внутри — дешевле.
func candidates(cat *Catalog, key string, qty, years int) []*Priced {
	pref := preferred[key]
	var list []*Priced
	for _, f := range cat.Families {
		if !f.hasNew || !needFilter(key, f) || looksLikeUnparsedPack(f) {
			continue
		}
		if p := PriceFor(f, qty, years, "new", "com"); usable(p) {
			list = append(list, p)
		}
	}
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		pa, pb := b2i(pref.MatchString(a.Family.Title)), b2i(pref.MatchString(b.Family.Title))
		if pa != pb {
			return pa > pb
		}
		return a.Total < b.Total
	})
	return list
}

// discoverOffers — сценарий «не знаю, что нужно»: два варианта под требования.
func discoverOffers(cat *Catalog, r model.Requirements) (offers []model.Offer, missing []string) {
	qty := r.Users
	if qty == 0 {
		qty = 1
	}
	y := r.Years
	if y == 0 {
		y = 1
	}
	var picksA, picksB []*Priced
	needOf := map[*Priced]model.Need{}
	toNeed := func(k string) model.Need {
		if k == "mail_server" {
			return "mail"
		}
		return k
	}
	for _, key := range filtersFor(r) {
		all := candidates(cat, key, qty, y)
		// Отсекаем позиции на порядок дороже самой дешёвой — обычно это серверные лицензии, посчитанные «за пользователя»
		floor := math.Inf(1)
		for _, x := range all {
			floor = math.Min(floor, x.Total)
		}
		var list []*Priced
		for _, x := range all {
			if x.Total <= floor*5 {
				list = append(list, x)
			}
		}
		if len(list) == 0 {
			missing = append(missing, key)
			continue
		}
		// Вариант 1 — российский разработчик (по возможности того же вендора, что уже выбран), вариант 2 — другой вендор
		find := func(ok func(*Priced) bool) *Priced {
			for _, x := range list {
				if ok(x) {
					return x
				}
			}
			return nil
		}
		vendorA := ""
		if len(picksA) > 0 {
			vendorA = picksA[0].Family.Vendor
		}
		a := find(func(x *Priced) bool { return x.Family.RU && x.Family.Vendor == vendorA && vendorA != "" })
		if a == nil {
			a = find(func(x *Priced) bool { return x.Family.RU })
		}
		if a == nil {
			a = list[0]
		}
		vendorB := ""
		if len(picksB) > 0 {
			vendorB = picksB[0].Family.Vendor
		}
		b := find(func(x *Priced) bool {
			return vendorB != "" && x.Family.Vendor == vendorB && x.Family.Vendor != a.Family.Vendor
		})
		if b == nil {
			b = find(func(x *Priced) bool { return x.Family.Vendor != a.Family.Vendor })
		}
		if b == nil {
			b = find(func(x *Priced) bool { return x.Family.ID != a.Family.ID })
		}
		picksA = append(picksA, a)
		needOf[a] = toNeed(key)
		if b != nil {
			picksB = append(picksB, b)
			needOf[b] = toNeed(key)
		}
	}
	for idx, picks := range [][]*Priced{picksA, picksB} {
		if len(picks) == 0 {
			continue
		}
		items := make([]model.OfferItem, len(picks))
		var coveredAll []string
		for i, p := range picks {
			items[i] = itemFrom(p)
			coveredAll = append(coveredAll, needOf[p])
		}
		covered := []model.Need{}
		for _, n := range uniqStrings(coveredAll) {
			if contains(r.Needs, n) {
				covered = append(covered, n)
			}
		}
		reasons, notes := reasonsFor(items, r, picks, covered, true)
		var vendors []string
		for _, i := range items {
			vendors = append(vendors, i.Product.Vendor)
		}
		title := "Вариант " + strconv.Itoa(idx+1) + " · " + strings.Join(uniqStrings(vendors), " + ")
		offers = append(offers, makeOffer(title, items, reasons, notes))
	}
	return markRecommended(offers, r), missing
}

// knownOffers — сценарий «знаю продукт»: сверяем редакции и считаем под количество и срок.
func knownOffers(cat *Catalog, f *Family, r model.Requirements) []model.Offer {
	qty := r.Users
	if qty == 0 {
		qty = 1
	}
	y := r.Years
	if y == 0 {
		y = 1
	}
	var priced []*Priced
	for _, e := range editionsOf(cat, f) {
		if p := PriceFor(e, qty, y, "new", "com"); p != nil {
			priced = append(priced, p)
		}
	}
	var offers []model.Offer
	for i, p := range priced {
		item := itemFrom(p)
		reasons := []string{CalcLabel(item) + ", " + TermText(p.Variant.Term)}
		if p.Family.Registry != nil {
			reasons = append(reasons, "В реестре российского ПО, № "+*p.Family.Registry)
		} else if p.Family.RU {
			reasons = append(reasons, "Российский разработчик")
		}
		if p.Family.Platforms != nil && *p.Family.Platforms != "" {
			reasons = append(reasons, "Платформы: "+*p.Family.Platforms)
		}
		var notes []string
		if !p.Family.RU {
			notes = append(notes, "Зарубежный разработчик")
		}
		if p.Adjusted {
			notes = append(notes, "Минимум "+UnitCount(p.Qty, p.Family.Unit)+" для этой цены")
		}
		if p.TermFallback {
			notes = append(notes, "Срок "+TermText(p.Variant.Term)+" — другого в каталоге нет")
		}
		if base := priced[0]; i > 0 && p.Total > base.Total {
			lastWord := base.Family.Title[strings.LastIndex(base.Family.Title, " ")+1:]
			notes = append(notes, "Дороже "+lastWord+" на "+FormatPrice(p.Total-base.Total))
		}
		o := makeOffer(p.Family.Title, []model.OfferItem{item}, reasons, notes)
		o.Recommended = boolPtr(i == 0)
		offers = append(offers, o)
	}
	return offers
}

var reTitleSuffix = regexp.MustCompile(` · .*`)

// shortTitle — название варианта без «· вендоры».
func shortTitle(o model.Offer) string { return reTitleSuffix.ReplaceAllString(o.Title, "") }

// replaceWithAnalogs — заменить зарубежное ПО на российские аналоги из каталога и пересчитать.
func replaceWithAnalogs(cat *Catalog, offers []model.Offer, r model.Requirements) []model.Offer {
	y := r.Years
	if y == 0 {
		y = 1
	}
	next := make([]model.Offer, len(offers))
	for k, o := range offers {
		changed := false
		var priced []*Priced
		items := make([]model.OfferItem, len(o.Items))
		for j, i := range o.Items {
			items[j] = i
			if i.Product.RussianVendor {
				continue
			}
			f := cat.ByID[i.Product.FamilyID]
			if f == nil {
				continue
			}
			analogs := RussianAnalogs(cat, f, i.Qty, y)
			if len(analogs) == 0 {
				continue
			}
			changed = true
			priced = append(priced, analogs[0])
			items[j] = itemFrom(analogs[0])
		}
		if !changed {
			next[k] = o
			continue
		}
		reasons, notes := reasonsFor(items, r, priced, nil, false)
		n := makeOffer(shortTitle(o)+" · с аналогами", items, reasons, notes)
		n.ID = o.ID
		next[k] = n
	}
	return markRecommended(next, r)
}

func compareOffers(offers []model.Offer) *model.CompareTable {
	items := make([]model.OfferItem, len(offers))
	for i, o := range offers {
		items[i] = o.Items[0]
	}
	row := func(label string, f func(int) string) model.CompareRow {
		vals := make([]string, len(items))
		for i := range items {
			vals[i] = f(i)
		}
		return model.CompareRow{Label: label, Values: vals}
	}
	cols := make([]string, len(items))
	for i, it := range items {
		cols[i] = it.Product.Name
	}
	return &model.CompareTable{
		Columns: cols,
		Rows: []model.CompareRow{
			row("Расчёт", func(i int) string { return CalcLabel(items[i]) }),
			row("Итого", func(i int) string { return FormatPrice(offers[i].Total) }),
			row("Срок", func(i int) string { return TermText(items[i].Product.TermMonths) }),
			row("Платформы", func(i int) string {
				p := items[i].Product.Platforms
				s := strings.Join(p, ", ")
				if len(p) > 3 {
					s = strings.Join(p[:3], ", ") + " и др."
				}
				if s == "" {
					return "—"
				}
				return s
			}),
			row("Реестр российского ПО", func(i int) string {
				if n := items[i].Product.RegistryNumber; n != "" {
					return "№ " + n
				}
				return "—"
			}),
			row("Разработчик", func(i int) string {
				if items[i].Product.RussianVendor {
					return "Россия"
				}
				return "Зарубежный"
			}),
		},
	}
}

var bundleRole = map[string]string{
	"Антивирусы для организаций":                         "Защита",
	"Программное обеспечение для резервного копирования": "Бэкап",
	"Почтовые утилиты":                                   "Почта",
	"Серверное ПО":                                       "Почтовый сервер",
}

func compareBundles(offers []model.Offer) *model.CompareTable {
	rowsCount := 0
	for _, o := range offers {
		rowsCount = max(rowsCount, len(o.Items))
	}
	t := &model.CompareTable{}
	for _, o := range offers {
		t.Columns = append(t.Columns, shortTitle(o))
	}
	for k := 0; k < rowsCount; k++ {
		label := "Позиция " + strconv.Itoa(k+1)
		if k < len(offers[0].Items) {
			cat := offers[0].Items[k].Product.Category
			if role, ok := bundleRole[cat]; ok {
				label = role
			} else {
				label = cat
			}
		}
		vals := make([]string, len(offers))
		for i, o := range offers {
			vals[i] = "—"
			if k < len(o.Items) {
				vals[i] = o.Items[k].Product.Name
			}
		}
		t.Rows = append(t.Rows, model.CompareRow{Label: label, Values: vals})
	}
	totals := make([]string, len(offers))
	ru := make([]string, len(offers))
	for i, o := range offers {
		totals[i] = FormatPrice(o.Total)
		ru[i] = "✓"
		for _, it := range o.Items {
			if !it.Product.RussianVendor {
				ru[i] = "—"
				break
			}
		}
	}
	t.Rows = append(t.Rows, model.CompareRow{Label: "Итого", Values: totals}, model.CompareRow{Label: "Российские разработчики", Values: ru})
	return t
}

// ── КП ────────────────────────────────────────────────────────────────

const (
	quoteValidDays    = 14
	quoteDeliveryTerm = "электронная поставка на e-mail, срок — согласно карточке товара"
)

// QuoteItemsFromOffer — позиции КП. Без НДС — только если номер в реестре есть в фиде;
// иначе ставку подтверждает менеджер (null).
func QuoteItemsFromOffer(offer model.Offer) []model.QuoteItem {
	out := make([]model.QuoteItem, len(offer.Items))
	for k, i := range offer.Items {
		qty := i.Qty
		if i.Product.Pricing == "pack" {
			qty = i.Packs
		}
		var vat *float64
		if i.Product.RegistryNumber != "" {
			zero := 0.0
			vat = &zero
		}
		out[k] = model.QuoteItem{SKU: i.Product.SKU, Name: i.Product.OfferName, Qty: qty, Price: i.Product.Price, VatRate: vat}
	}
	return out
}

// BuildQuote — черновик КП по шаблону Softline.
func BuildQuote(t *model.Ticket, offer model.Offer, req model.QuoteRequest, seq int, now time.Time) model.Quote {
	n := strconv.Itoa(t.Number) + "-" + strconv.Itoa(seq)
	return model.Quote{
		ID: "q-" + n, Number: "КП-" + n, TicketID: t.ID, Status: "review",
		Company: req.Company, INN: req.INN, Email: req.Email, PaymentTerms: req.PaymentTerms,
		DeliveryTerm: quoteDeliveryTerm,
		ValidUntil:   ISO(now.Add(quoteValidDays * 24 * time.Hour)),
		CreatedAt:    ISO(now),
		Items:        QuoteItemsFromOffer(offer),
	}
}
