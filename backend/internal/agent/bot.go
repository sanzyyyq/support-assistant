package agent

import (
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"support-assistant/internal/model"
)

// ИИ-агент без языковой модели: правила и «слоты» (что уже знаем о задаче клиента)
// поверх настоящего каталога Store. Главная функция — GenerateReply.

// Draft — сообщение агента до сохранения (без id, обращения и времени).
type Draft = model.Message

// Handoff — агент передаёт диалог менеджеру.
type Handoff struct {
	Reason     string  `json:"reason"`
	Confidence float64 `json:"confidence"`
}

// Reply — ответ агента: сообщения и изменения обращения.
type Reply struct {
	Messages []Draft
	// Patch меняет обращение (сценарий, требования, варианты…); nil — ничего не меняем
	Patch     func(t *model.Ticket)
	Handoff   *Handoff
	Sentiment string
	AddTags   []string
}

var StartReplies = []string{"Знаю, какой продукт нужен", "Помогите подобрать", "Вопрос о покупке", "Позвать менеджера"}

// Greeting — первое сообщение в новом диалоге.
func Greeting() Draft {
	return Draft{
		Sender: "bot", Intent: "greeting",
		Text:         "Здравствуйте! Я ИИ-консультант Store. Подберу ПО под задачу, сверю версии и лицензии по каталогу, отвечу на вопросы о покупке и подготовлю коммерческое предложение.",
		QuickReplies: append([]string(nil), StartReplies...),
	}
}

// HandoffText — что пишет агент, когда передаёт диалог менеджеру без своего ответа.
const HandoffText = "Подключаю менеджера — обычно это занимает 1–2 минуты. Он увидит всю историю и ваши требования."

func botNorm(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.ToLower(s), "ё", "е"), " ", " ")
}

// Распознавание намерений по тексту клиента (текст уже в нижнем регистре).
// \b в Go, как и в JavaScript, понимает только латиницу: «\bпк\b» с кириллицей не срабатывает — оставлено как было.
var (
	reOperator        = regexp.MustCompile(`(оператор|менеджер|человек|живо[йм]|специалист|сотрудник поддержки)`)
	reNegative        = regexp.MustCompile(`(безобраз|ужас|отврат|возмут|жалоб|сколько можно|обман|кошмар|бесит|достали|не берут трубку|никто не отвечает)`)
	reDiscount        = regexp.MustCompile(`(скидк|индивидуальн.*цен|спецусло|акци[ия] для|партнер)`)
	reGov             = regexp.MustCompile(`(госзакуп|44-фз|223-фз|тендер|бюджетн\w* (организац|учрежд)|конкурентн|закупк\w* сред|учреждени|органов власти|\b(гау|гбу|мбу|мбоу|фгбу|гку)\b)`)
	reQuote           = regexp.MustCompile(`(кп\b|коммерческ|сформир|счет на оплату|выставить сч|пришлите сч|нужен сч|счет на )`)
	reChoose          = regexp.MustCompile(`(вариант\s*\d|беру|выбираю|остановимся)`)
	reAnalog          = regexp.MustCompile(`(российск|отечествен|аналог|импортозамещ|реестр)`)
	reReplace         = regexp.MustCompile(`заменить на аналог`)
	reKeep            = regexp.MustCompile(`оставить как есть`)
	reCompare         = regexp.MustCompile(`(сравн|отлич|разниц)`)
	reChangeQty       = regexp.MustCompile(`изменить количество`)
	reProductQuestion = regexp.MustCompile(`(поддерж|работает|есть ли (у него|в нем)|что входит|функци|умеет|чем хорош|подойдет ли|совместим|платформ|расскажите про|что это)`)
	reAvailability    = regexp.MustCompile(`((можно|возможно|получится)[^.?]{0,30}(купить|приобрести|заказать)|есть ли (у вас|возможность)|продаете ли|у вас есть|сколько стоит|стоимость|цена на|прайс)`)
	reIndividual      = regexp.MustCompile(`(физлиц|физ\.? ?лиц|физическ\w* лиц|самозанят|для себя|для дома|для учеб)`)
	reStartKnown      = regexp.MustCompile(`^знаю,? какой продукт`)
	reStartDiscover   = regexp.MustCompile(`^(помогите подобрать|подобрать решение)`)
	reStartQuestion   = regexp.MustCompile(`^вопрос о покупке`)
	reRenewal         = regexp.MustCompile(`(продлен|продлить|продлит)`)
	reGreet           = regexp.MustCompile(`^(привет|здравствуй|добрый|hello|hi)\b`)
	reThanks          = regexp.MustCompile(`^(спасибо|благодар|понятно|отлично)`)
	reCallManager     = regexp.MustCompile(`^позвать менеджера$`)
	reCalc            = regexp.MustCompile(`рассчитать`)
	reVat             = regexp.MustCompile(`ндс`)
	reDontKnow        = regexp.MustCompile(`не знаю`)
	reMail            = regexp.MustCompile(`почт`)
)

// ── Разбор ответов ────────────────────────────────────────────────────

var (
	reNeedMail     = regexp.MustCompile(`(почт|email|e-mail|ящик|спам|фишинг|exchange)`)
	reNeedEndpoint = regexp.MustCompile(`(файл|компьютер|\bпк\b|рабоч(их|ие) (станц|мест)|антивирус|вирус|шифровал|ноутбук|защит\w* пк)`)
	reNeedBackup   = regexp.MustCompile(`(файл|бэкап|резерв|backup|копирован)`)
	reNeedOffice   = regexp.MustCompile(`(офисн|офис\b|документ|word|excel|таблиц|редактор)`)
	reNeedOS       = regexp.MustCompile(`(операционн|\bос\b|linux|линукс|windows)`)
)

func parseNeeds(t string) []model.Need {
	var n []model.Need
	if reNeedMail.MatchString(t) {
		n = append(n, "mail")
	}
	if reNeedEndpoint.MatchString(t) {
		n = append(n, "endpoint")
	}
	if reNeedBackup.MatchString(t) {
		n = append(n, "backup")
	}
	if reNeedOffice.MatchString(t) {
		n = append(n, "office")
	}
	if reNeedOS.MatchString(t) {
		n = append(n, "os")
	}
	return n
}

var reSpaces = regexp.MustCompile(`\s`)

// toInt — «1 000» → 1000 (как parseInt в JavaScript: берёт цифры с начала строки).
func toInt(s string) int {
	s = reSpaces.ReplaceAllString(s, "")
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	n, _ := strconv.Atoi(s[:end])
	return n
}

var (
	reUsers       = regexp.MustCompile(`(\d[\d\s]{0,6})\s*(сотрудник|пользовател|человек|чел\b|пк\b|компьютер|ноутбук|устройств|рабоч|ящик|лиценз|мест|узл|шт|планшет|учетн|сервер)`)
	reUsersFor    = regexp.MustCompile(`(?:на|для)\s+(\d{1,5})\b`)
	reUsersForNot = regexp.MustCompile(`^\s*(год|лет|мес|тыс|₽|руб)`)
	reAnyNumber   = regexp.MustCompile(`\d[\d\s]*`)
)

func parseUsers(t string, pending bool) int {
	if m := reUsers.FindStringSubmatch(t); m != nil {
		return toInt(m[1])
	}
	// «на 50», «для 20» — но не «на 3 года», «на 300 тыс»
	for _, m := range reUsersFor.FindAllStringSubmatchIndex(t, -1) {
		if !reUsersForNot.MatchString(t[m[1]:]) {
			return toInt(t[m[2]:m[3]])
		}
	}
	if pending {
		if n := reAnyNumber.FindString(t); n != "" {
			return toInt(n)
		}
	}
	return 0
}

var (
	reYears       = regexp.MustCompile(`(\d)\s*(?:и\s*\d\s*)?(год|года|лет)`)
	rePerpetual   = regexp.MustCompile(`бессрочн`)
	reYearWord    = regexp.MustCompile(`(год|года)`)
	reNoLimit     = regexp.MustCompile(`(не огранич|без огранич|неважно|не важно)`)
	reBudget      = regexp.MustCompile(`(\d[\d\s]*)\s*(тыс|млн|k\b|к\b|₽|руб)`)
	reBudgetWord  = regexp.MustCompile(`бюджет`)
	reThousands   = regexp.MustCompile(`^(тыс|k|к)`)
	reInfraNone   = regexp.MustCompile(`(нет почт|почты нет|пока нет|почты пока нет)`)
	reInfraCloud  = regexp.MustCompile(`(облач|google|яндекс|mail\.ru|360)`)
	reInfraServer = regexp.MustCompile(`(свой|собствен|exchange|сервер|on-?prem|у себя)`)
)

func parseYears(t string, pending bool) int {
	if m := reYears.FindStringSubmatch(t); m != nil {
		n, _ := strconv.Atoi(m[1])
		return min(5, max(1, n))
	}
	if rePerpetual.MatchString(t) {
		return 1
	}
	if pending && reYearWord.MatchString(t) {
		return 1
	}
	return 0
}

func parseBudget(t string, pending bool) *model.Budget {
	if pending && reNoLimit.MatchString(t) {
		return &model.Budget{Unlimited: true}
	}
	m := reBudget.FindStringSubmatch(t)
	if m == nil || (!pending && !reBudgetWord.MatchString(t)) {
		return nil
	}
	n := float64(toInt(m[1]))
	switch {
	case strings.HasPrefix(m[2], "млн"):
		n *= 1_000_000
	case reThousands.MatchString(m[2]):
		n *= 1000
	}
	return &model.Budget{Amount: n}
}

func parseInfra(t string) string {
	switch {
	case reInfraNone.MatchString(t):
		return "none"
	case reInfraCloud.MatchString(t):
		return "cloud"
	case reInfraServer.MatchString(t):
		return "own_server"
	}
	return ""
}

// ── Вопросы ───────────────────────────────────────────────────────────

func question(slot model.Slot, r model.Requirements, cat *Catalog) Draft {
	unit := ""
	if f := cat.ByID[r.ProductID]; f != nil && r.ProductID != "" {
		unit = f.Unit
	}
	var text string
	var options []string
	switch slot {
	case "needs":
		text, options = "Какую задачу нужно решить?", []string{"Защита ПК и файлов", "Защита почты", "Резервное копирование", "Российская ОС"}
	case "product":
		text, options = "Какой продукт вас интересует? Напишите название и редакцию, если знаете.", []string{"Kaspersky Endpoint Security для бизнеса Стандартный", "Dr.Web Комплексная защита", "MDaemon Messaging Server"}
	case "users":
		switch unit {
		case "mailbox":
			text = "Сколько почтовых ящиков нужно?"
		case "device":
			text = "Сколько устройств нужно защитить?"
		case "server":
			text = "Сколько серверов (или ядер) нужно лицензировать?"
		default:
			text = "Сколько пользователей или рабочих мест нужно покрыть?"
		}
		options = []string{"10", "25", "50", "100"}
	case "infrastructure":
		text, options = "Где сейчас работает почта?", []string{"Свой почтовый сервер", "Облачная почта", "Почты пока нет"}
	case "years":
		text, options = "На какой срок нужна лицензия?", []string{"1 год", "2 года", "3 года"}
	case "budget":
		text, options = "Есть ориентир по бюджету?", []string{"до 300 тыс ₽", "до 500 тыс ₽", "Не ограничен"}
	}
	return Draft{Sender: "bot", Intent: "ask_" + slot, Text: text, QuickReplies: options}
}

// nextMissing — о чём ещё нужно спросить ("" — всё известно).
func nextMissing(scenario string, r model.Requirements, cat *Catalog) model.Slot {
	if scenario == "known" {
		if r.ProductID == "" {
			return "product"
		}
		if r.Users == 0 {
			return "users"
		}
		hasTerms := false
		if f := cat.ByID[r.ProductID]; f != nil {
			for _, v := range f.Variants {
				if v.Kind == "new" && v.Term != nil && *v.Term > 0 {
					hasTerms = true
					break
				}
			}
		}
		if r.Years == 0 && hasTerms {
			return "years"
		}
		return ""
	}
	if len(r.Needs) == 0 {
		return "needs"
	}
	if r.Users == 0 {
		return "users"
	}
	if contains(r.Needs, "mail") && r.Infrastructure == "" {
		return "infrastructure"
	}
	if r.Years == 0 {
		return "years"
	}
	if r.Budget == nil {
		return "budget"
	}
	return ""
}

// ── Ответы ────────────────────────────────────────────────────────────

func productSource(p model.Product) model.Source {
	return model.Source{Kind: "product", Title: "Каталог Store: " + p.Name, URL: p.URL}
}

func familySource(f *Family) model.Source {
	s := model.Source{Kind: "product", Title: "Каталог Store: " + f.Title}
	if len(f.Variants) > 0 {
		s.URL = f.Variants[0].URL
	}
	return s
}

func kbSource(id string) model.Source {
	a := kbByID(id)
	return model.Source{Kind: "kb", Title: "База знаний: " + a.Title, URL: a.URL}
}

// uniqProducts — по одному продукту на семейство, в порядке появления (последняя позиция семейства побеждает, как Map в JS).
func uniqProducts(offers []model.Offer) []model.Product {
	var order []string
	byFamily := map[string]model.Product{}
	for _, o := range offers {
		for _, i := range o.Items {
			if _, ok := byFamily[i.Product.FamilyID]; !ok {
				order = append(order, i.Product.FamilyID)
			}
			byFamily[i.Product.FamilyID] = i.Product
		}
	}
	out := make([]model.Product, len(order))
	for k, id := range order {
		out[k] = byFamily[id]
	}
	return out
}

func productSources(ps []model.Product) []model.Source {
	out := make([]model.Source, len(ps))
	for i, p := range ps {
		out[i] = productSource(p)
	}
	return out
}

func recommend(cat *Catalog, r model.Requirements, scenario string) Reply {
	if scenario == "known" {
		f := cat.ByID[r.ProductID]
		offers := knownOffers(cat, f, r)
		if len(offers) == 0 {
			return Reply{
				Handoff:  &Handoff{Reason: "Нет подходящей позиции " + f.Title + " в онлайн-каталоге", Confidence: 0.4},
				Messages: []Draft{{Sender: "bot", Intent: "handoff", Text: f.Title + " есть в каталоге, но не под эти параметры. Передаю менеджеру — он подготовит расчёт."}},
			}
		}
		multi := len(offers) > 1
		head := "Сверил по каталогу Store: " + f.Vendor + " · " + f.Title + ". Расчёт на " + UnitCount(r.Users, f.Unit)
		if r.Years > 0 {
			head += ", срок — " + Years(r.Years)
		}
		if multi {
			head += ", рядом — соседние редакции:"
		} else {
			head += ":"
		}
		sources := make([]model.Source, len(offers))
		for i, o := range offers {
			sources[i] = productSource(o.Items[0].Product)
		}
		var second Draft
		if multi {
			var qr []string
			for i := range offers {
				if i < 3 {
					qr = append(qr, "Выбираю вариант "+strconv.Itoa(i+1))
				}
			}
			second = Draft{Sender: "bot", Intent: "compare", Text: "Чем отличаются по каталогу:", Compare: compareOffers(offers),
				QuickReplies: append(qr, "Есть российский аналог?", "Сформировать КП")}
		} else {
			second = Draft{Sender: "bot", Intent: "next", Text: "Подготовить коммерческое предложение?", QuickReplies: []string{"Сформировать КП", "Есть российский аналог?", "Изменить количество"}}
		}
		return Reply{
			AddTags: []string{"Лицензирование"},
			Patch: func(t *model.Ticket) {
				t.LastOffers = offers
				t.SelectedOfferID = ""
			},
			Messages: []Draft{{Sender: "bot", Intent: "offers", Text: head, Offers: offers, Sources: sources}, second},
		}
	}
	offers, missing := discoverOffers(cat, r)
	if len(offers) == 0 {
		return Reply{
			Handoff:  &Handoff{Reason: "Под требования не нашлось позиций в онлайн-каталоге", Confidence: 0.35},
			Messages: []Draft{{Sender: "bot", Intent: "handoff", Text: "В онлайн-каталоге не нашлось готовых позиций под эти требования. Передаю менеджеру — он подберёт решение под заказ."}},
		}
	}
	var rec *model.Offer
	for i := range offers {
		if offers[i].IsRecommended() {
			rec = &offers[i]
			break
		}
	}
	miss := ""
	if len(missing) > 0 {
		miss = " По задаче «" + strings.Join(missing, ", ") + "» в каталоге ничего не нашлось — уточнит менеджер."
	}
	count := "вариант"
	if len(offers) > 1 {
		count = "2 варианта"
	}
	users, years := r.Users, r.Years
	if users == 0 {
		users = 1
	}
	if years == 0 {
		years = 1
	}
	text := "Подобрал " + count + " по каталогу Store на " + UnitCount(users, "user") + ", срок — " + Years(years) + "."
	if rec != nil && len(offers) > 1 {
		text += " Рекомендую " + strings.ToLower(shortTitle(*rec)) + " — обоснование в карточках."
	}
	text += miss
	var second Draft
	if len(offers) > 1 {
		second = Draft{Sender: "bot", Intent: "compare", Text: "Коротко в сравнении:", Compare: compareBundles(offers), QuickReplies: []string{"Выбираю вариант 1", "Выбираю вариант 2", "Есть российский аналог?", "Сформировать КП"}}
	} else {
		second = Draft{Sender: "bot", Intent: "next", Text: "Подготовить коммерческое предложение?", QuickReplies: []string{"Сформировать КП", "Изменить количество"}}
	}
	return Reply{
		AddTags: []string{"Подбор ПО"},
		Patch: func(t *model.Ticket) {
			t.LastOffers = offers
			t.SelectedOfferID = ""
		},
		Messages: []Draft{{Sender: "bot", Intent: "offers", Text: text, Offers: offers, Sources: productSources(uniqProducts(offers))}, second},
	}
}

func analogReply(cat *Catalog, ticket *model.Ticket, mention Mention) Reply {
	offers := ticket.LastOffers
	r := ticket.Requirements
	var base []*Family
	if mention.Family != nil {
		base = []*Family{mention.Family}
	} else {
		for _, p := range uniqProducts(offers) {
			if f := cat.ByID[p.FamilyID]; f != nil {
				base = append(base, f)
			}
		}
	}
	if len(base) == 0 {
		return Reply{
			Patch: func(t *model.Ticket) {
				t.Scenario = "known"
				t.PendingSlot = "product"
			},
			Messages: []Draft{{Sender: "bot", Intent: "ask_product", Text: "Для какого продукта ищем российский аналог?", QuickReplies: []string{"MDaemon", "Microsoft Office"}}},
		}
	}
	var foreign []*Family
	for _, f := range base {
		if !f.RU {
			foreign = append(foreign, f)
		}
	}
	if len(foreign) == 0 {
		var titles, regs []string
		for _, f := range base {
			titles = append(titles, f.Title)
			if f.Registry != nil && *f.Registry != "" {
				regs = append(regs, f.Title+" (№ "+*f.Registry+")")
			}
		}
		who := "продукт российского разработчика"
		if len(base) > 1 {
			who = "продукты российских разработчиков"
		}
		regText := "Номер записи в реестре в каталоге не указан — проверит менеджер, это влияет на НДС."
		if len(regs) > 0 {
			regText = "В реестре российского ПО по данным каталога: " + strings.Join(regs, ", ") + "."
		}
		var sources []model.Source
		for _, f := range base {
			sources = append(sources, familySource(f))
		}
		return Reply{Messages: []Draft{{
			Sender: "bot", Intent: "analog",
			Text:         joinAnd(titles) + " — " + who + ". " + regText,
			Sources:      append(sources, kbSource("kb-vat")),
			QuickReplies: []string{"Сформировать КП", "Позвать менеджера"},
		}}}
	}
	qty, y := r.Users, r.Years
	if qty == 0 {
		qty = 10
	}
	if y == 0 {
		y = 1
	}
	var lines []string
	var analogs []*Priced
	for _, f := range foreign {
		a := RussianAnalogs(cat, f, qty, y)
		if len(a) == 0 {
			lines = append(lines, "• "+f.Title+" — российского аналога в онлайн-каталоге нет")
			continue
		}
		var names []string
		for _, x := range a {
			s := x.Family.Title + " (" + x.Family.Vendor
			if x.Family.Registry != nil && *x.Family.Registry != "" {
				s += ", реестр № " + *x.Family.Registry
			}
			names = append(names, s+")")
		}
		lines = append(lines, "• "+f.Title+" → "+strings.Join(names, " или "))
		analogs = append(analogs, a...)
	}
	var fullRu []string
	for _, o := range offers {
		all := true
		for _, i := range o.Items {
			if !i.Product.RussianVendor {
				all = false
				break
			}
		}
		if all {
			fullRu = append(fullRu, shortTitle(o))
		}
	}
	var parts []string
	if len(fullRu) > 0 && mention.Family == nil {
		parts = append(parts, strings.Join(fullRu, ", ")+" уже полностью из российского ПО.")
	}
	parts = append(parts, "Российские аналоги из каталога:\n"+strings.Join(lines, "\n"))
	if len(analogs) > 0 {
		parts = append(parts, "Цены посчитал под ваше количество и срок.")
	}
	var products []model.Product
	var sources []model.Source
	for _, f := range foreign {
		sources = append(sources, familySource(f))
	}
	for _, a := range analogs {
		products = append(products, itemFrom(a).Product)
		sources = append(sources, familySource(a.Family))
	}
	qr := []string{"Помогите подобрать", "Позвать менеджера"}
	if len(offers) > 0 && len(analogs) > 0 {
		qr = []string{"Заменить на аналог", "Оставить как есть", "Сформировать КП"}
	}
	return Reply{Messages: []Draft{{
		Sender: "bot", Intent: "analog", Text: strings.Join(parts, "\n\n"),
		Products: products, Sources: sources, QuickReplies: qr,
	}}}
}

var reVariantN = regexp.MustCompile(`вариант\s*(\d)`)

func chooseOffer(t string, offers []model.Offer) *model.Offer {
	if m := reVariantN.FindStringSubmatch(t); m != nil {
		n, _ := strconv.Atoi(m[1])
		if n >= 1 && n <= len(offers) {
			return &offers[n-1]
		}
		return nil
	}
	// Сначала самые длинные названия: «Расширенный Плюс» не должен совпасть как «Расширенный»
	idx := make([]int, len(offers))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		return runeLen(offers[idx[a]].Items[0].Product.Name) > runeLen(offers[idx[b]].Items[0].Product.Name)
	})
	for _, i := range idx {
		if strings.Contains(t, botNorm(offers[i].Items[0].Product.Name)) {
			return &offers[i]
		}
	}
	for i := range offers {
		if offers[i].IsRecommended() {
			return &offers[i]
		}
	}
	return &offers[0]
}

func productAnswer(f *Family, t string) Draft {
	kb := findKb(t)
	var fresh []*RawVariant
	var kinds []string
	for i := range f.Variants {
		v := &f.Variants[i]
		kinds = append(kinds, kindLabel[v.Kind])
		if v.Kind == "new" {
			fresh = append(fresh, v)
		}
	}
	kinds = uniqStrings(kinds)
	var terms []string
	for _, v := range fresh {
		terms = append(terms, TermText(v.Term))
	}
	terms = uniqStrings(terms)
	pool := fresh
	if len(pool) == 0 {
		for i := range f.Variants {
			pool = append(pool, &f.Variants[i])
		}
	}
	pool = append([]*RawVariant(nil), pool...)
	sort.SliceStable(pool, func(i, j int) bool { return pool[i].Price < pool[j].Price })
	cheapest := pool[0]

	path := f.Path
	if len(path) > 2 {
		path = path[len(path)-2:]
	}
	lines := []string{f.Title + " (" + f.Vendor + ") — раздел каталога «" + strings.Join(path, " › ") + "»."}
	if f.Platforms != nil && *f.Platforms != "" {
		lines = append(lines, "Платформы: "+*f.Platforms+".")
	}
	inCat := "В каталоге: " + strings.ToLower(strings.Join(kinds, ", "))
	if len(terms) > 0 {
		inCat += "; сроки — " + strings.Join(terms, ", ")
	}
	lines = append(lines, inCat+".")
	price := "Цена — от " + FormatPrice(cheapest.Price)
	if cheapest.Mode == "pack" && cheapest.Min != nil && *cheapest.Min != 0 {
		price += " за " + UnitCount(*cheapest.Min, f.Unit)
	}
	lines = append(lines, price+".")
	switch {
	case f.Registry != nil && *f.Registry != "":
		lines = append(lines, "В реестре российского ПО, № "+*f.Registry+".")
	case f.RU:
		lines = append(lines, "Российский разработчик.")
	default:
		lines = append(lines, "Зарубежный разработчик.")
	}
	sources := []model.Source{familySource(f)}
	if kb != nil {
		sources = append(sources, kbSource(kb.ID))
	}
	return Draft{
		Sender: "bot", Intent: "product_info",
		Text:         strings.Join(lines, "\n"),
		Products:     []model.Product{ToProduct(f, cheapest)},
		Sources:      sources,
		QuickReplies: []string{runePrefix("Рассчитать "+f.Title, 60), "Есть российский аналог?", "Позвать менеджера"},
	}
}

func askWhichProduct(m Mention) Draft {
	qr := []string{}
	for _, f := range m.VendorFamilies {
		qr = append(qr, runePrefix(f.Title, 60))
	}
	return Draft{Sender: "bot", Intent: "ask_product", Text: "У " + m.Vendor + " в каталоге несколько продуктов. Какой нужен?", QuickReplies: qr}
}

func kbReply(id string, tags ...string) Reply {
	return Reply{
		AddTags:  tags,
		Messages: []Draft{{Sender: "bot", Intent: "kb", Text: kbByID(id).Answer, Sources: []model.Source{kbSource(id)}, QuickReplies: []string{"Помогите подобрать", "Позвать менеджера"}}},
	}
}

// ── Главная функция ───────────────────────────────────────────────────

// GenerateReply — ответ агента на сообщение клиента.
// ticket — обращение в текущем состоянии (не меняется), history — все сообщения диалога.
func GenerateReply(cat *Catalog, ticket *model.Ticket, rawText string, history []model.Message) Reply {
	t := botNorm(rawText)
	r := ticket.Requirements.Clone()
	pending := ticket.PendingSlot
	offers := ticket.LastOffers

	// «Менеджер, пришлите КП» обрабатываем как запрос КП ниже; просто «позовите менеджера» — передаём сразу
	if reOperator.MatchString(t) && !(!reCallManager.MatchString(t) && reQuote.MatchString(t)) {
		return Reply{Handoff: &Handoff{Reason: "Клиент попросил менеджера", Confidence: 0.7}}
	}
	if reNegative.MatchString(t) {
		return Reply{
			Sentiment: "negative", AddTags: []string{"Негатив"},
			Handoff:  &Handoff{Reason: "Клиент недоволен, нужна помощь менеджера", Confidence: 0.35},
			Messages: []Draft{{Sender: "bot", Intent: "handoff", Text: "Понимаю, приношу извинения. Подключаю менеджера — он поможет разобраться."}},
		}
	}
	if reGov.MatchString(t) {
		return Reply{
			AddTags:  []string{"Госзакупки"},
			Handoff:  &Handoff{Reason: "Закупка для госоргана — нужна проверка условий и документов", Confidence: 0.45},
			Messages: []Draft{{Sender: "bot", Intent: "handoff", Text: "Для государственных и бюджетных закупок условия и документы готовит менеджер. Передаю ему диалог вместе с вашими требованиями."}},
		}
	}
	if reDiscount.MatchString(t) {
		return Reply{
			AddTags:  []string{"Скидка"},
			Handoff:  &Handoff{Reason: "Запрос скидки или спецусловий — решает менеджер", Confidence: 0.45},
			Messages: []Draft{{Sender: "bot", Intent: "handoff", Text: "Скидки и специальные условия согласует менеджер. Передаю ему диалог вместе с вашими требованиями."}},
		}
	}

	// Стартовые кнопки
	if reStartKnown.MatchString(t) {
		return Reply{
			Patch:    func(t *model.Ticket) { t.Scenario, t.PendingSlot = "known", "product" },
			Messages: []Draft{question("product", r, cat)},
		}
	}
	if reStartQuestion.MatchString(t) {
		return Reply{
			Patch: func(t *model.Ticket) { t.Scenario, t.PendingSlot = "question", "" },
			Messages: []Draft{{Sender: "bot", Intent: "ask_question", Text: "Задайте вопрос — отвечу по каталогу и базе знаний Softline, со ссылкой на источник.",
				QuickReplies: []string{"Цены указаны с НДС?", "Как доставляются лицензии?", "Можно купить как физлицо?"}}},
		}
	}

	// КП
	if reQuote.MatchString(t) {
		if ticket.QuoteID != "" {
			return Reply{Messages: []Draft{{Sender: "bot", Intent: "quote_exists", Text: "КП по этому диалогу уже сформировано и ждёт проверки менеджером. Если нужно изменить состав — напишите, что поменять."}}}
		}
		if len(offers) == 0 {
			m := FindMentioned(cat, rawText)
			if m.Family == nil && m.Vendor == "" && len(parseNeeds(expand(t))) == 0 {
				return Reply{
					Messages: []Draft{{Sender: "bot", Intent: "quote_no_offer", Text: "Подготовлю КП, как только подберём позиции. Какой продукт нужен и в каком количестве?", QuickReplies: StartReplies[:2:2]}},
					Patch:    func(t *model.Ticket) { t.Scenario, t.PendingSlot = "known", "product" },
				}
			}
			// «Пришлите счёт на Касперского на 5 компьютеров» — сначала уточним продукт и посчитаем
		} else {
			sel := ticket.SelectedOffer()
			id := sel.ID
			return Reply{
				AddTags: []string{"КП"},
				Patch:   func(t *model.Ticket) { t.SelectedOfferID, t.PendingSlot = id, "" },
				Messages: []Draft{{
					Sender: "bot", Intent: "quote_form", Form: "quote_request",
					Text: "Подготовлю КП по шаблону Softline: " + shortTitle(*sel) + " — " + FormatPrice(sel.Total) + ". Заполните данные компании — черновик проверит менеджер и пришлёт на email.",
				}},
			}
		}
	}

	if reReplace.MatchString(t) && len(offers) > 0 {
		next := replaceWithAnalogs(cat, offers, r)
		return Reply{
			Patch: func(t *model.Ticket) { t.LastOffers = next },
			Messages: []Draft{{
				Sender: "bot", Intent: "offers_analog", Text: "Обновил варианты — зарубежное ПО заменено российскими аналогами из каталога:",
				Offers: next, Sources: productSources(uniqProducts(next)), QuickReplies: []string{"Выбираю вариант 1", "Выбираю вариант 2", "Сформировать КП"},
			}},
		}
	}

	if reKeep.MatchString(t) {
		return Reply{Messages: []Draft{{Sender: "bot", Intent: "keep", Text: "Хорошо, оставляем варианты без изменений. Какой выбираете?", QuickReplies: []string{"Выбираю вариант 1", "Выбираю вариант 2", "Сформировать КП"}}}}
	}

	if reChoose.MatchString(t) && len(offers) > 0 && !reCalc.MatchString(t) {
		if o := chooseOffer(t, offers); o != nil {
			id := o.ID
			return Reply{
				Patch: func(t *model.Ticket) { t.SelectedOfferID = id },
				Messages: []Draft{{
					Sender: "bot", Intent: "chosen",
					Text:         "Фиксирую: " + shortTitle(*o) + " — " + FormatPrice(o.Total) + ". Подготовить коммерческое предложение?",
					QuickReplies: []string{"Сформировать КП", "Изменить количество", "Позвать менеджера"},
				}},
			}
		}
	}

	mention := FindMentioned(cat, rawText)

	if reAnalog.MatchString(t) && !reVat.MatchString(t) {
		return analogReply(cat, ticket, mention)
	}

	if reCompare.MatchString(t) && len(offers) > 1 {
		same := true
		for _, o := range offers {
			if len(o.Items) != 1 {
				same = false
				break
			}
		}
		table := compareBundles(offers)
		if same {
			table = compareOffers(offers)
		}
		return Reply{Messages: []Draft{{Sender: "bot", Intent: "compare", Text: "Сравнение вариантов:", Compare: table, QuickReplies: []string{"Выбираю вариант 1", "Выбираю вариант 2", "Сформировать КП"}}}}
	}

	if reChangeQty.MatchString(t) {
		return Reply{Patch: func(t *model.Ticket) { t.PendingSlot = "users" }, Messages: []Draft{question("users", r, cat)}}
	}

	if reIndividual.MatchString(t) {
		return kbReply("kb-individual", "Физлицо")
	}

	// «Можно ли у вас купить Power BI?» — если в каталоге нет, честно говорим и зовём менеджера
	if reAvailability.MatchString(t) && mention.Family == nil && mention.Vendor == "" && pending == "" && len(parseNeeds(expand(t))) == 0 && len(ticket.LastOffers) == 0 {
		return Reply{
			AddTags:  []string{"Доступность вендора"},
			Handoff:  &Handoff{Reason: "Продукта нет в онлайн-каталоге — проверить возможность поставки", Confidence: 0.5},
			Messages: []Draft{{Sender: "bot", Intent: "availability", Text: "Сейчас этого продукта нет в онлайн-каталоге Store. Передаю менеджеру — он проверит, можно ли поставить его под заказ или предложить аналог."}},
		}
	}

	if mention.Family != nil && reProductQuestion.MatchString(t) && pending == "" {
		scenario := ticket.Scenario
		if scenario == "" {
			scenario = "question"
		}
		return Reply{
			AddTags:  []string{"Вопрос о продукте"},
			Patch:    func(t *model.Ticket) { t.Scenario = scenario },
			Messages: []Draft{productAnswer(mention.Family, t)},
		}
	}

	// ── Заполнение требований ──
	scenario := ticket.Scenario
	before := r.Clone()

	if pending == "infrastructure" {
		if v := parseInfra(t); v != "" {
			r.Infrastructure = v
		}
	}
	if mention.Family != nil {
		r.ProductID = mention.Family.ID
		r.ProductName = mention.Family.Title
		scenario = "known"
	}
	if scenario != "known" && pending != "infrastructure" {
		if needs := parseNeeds(expand(t)); len(needs) > 0 {
			r.Needs = uniqStrings(append(append([]string{}, r.Needs...), needs...))
			scenario = "discover"
		}
	}
	if (reStartDiscover.MatchString(t) || reDontKnow.MatchString(t)) && scenario != "known" {
		scenario = "discover"
	}
	if u := parseUsers(t, pending == "users"); u != 0 {
		r.Users = u
	}
	if y := parseYears(t, pending == "years"); y != 0 {
		r.Years = y
	}
	if b := parseBudget(t, pending == "budget"); b != nil {
		r.Budget = b
	}
	if pending != "infrastructure" && reMail.MatchString(t) {
		if v := parseInfra(t); v != "" {
			r.Infrastructure = v
		}
	}

	changed := !reflect.DeepEqual(normReq(r), normReq(before))
	var kbEarly *KbArticle
	if !changed {
		kbEarly = findKb(t)
	}

	// Назван вендор и задача («счёт на Касперского на 5 компьютеров») — берём профильный продукт этого вендора
	if mention.Family == nil && mention.Vendor != "" && r.ProductID == "" && len(r.Needs) > 0 && r.Users > 0 {
		y := r.Years
		if y == 0 {
			y = 1
		}
		for _, x := range candidates(cat, r.Needs[0], r.Users, y) {
			if x.Family.Vendor == mention.Vendor {
				r.ProductID = x.Family.ID
				r.ProductName = x.Family.Title
				scenario = "known"
				break
			}
		}
	}

	// Назван только вендор («85 лицензий касперского») — уточняем продукт
	if mention.Family == nil && mention.Vendor != "" && r.ProductID == "" && (scenario == "known" || len(r.Needs) == 0) {
		req := r
		return Reply{
			Patch:    func(t *model.Ticket) { t.Scenario, t.Requirements, t.PendingSlot = "known", req, "product" },
			Messages: []Draft{askWhichProduct(mention)},
		}
	}

	if (scenario == "known" || scenario == "discover") && kbEarly == nil && (changed || len(offers) == 0) {
		next := nextMissing(scenario, r, cat)
		tag := "Подбор ПО"
		if scenario == "known" {
			tag = "Лицензирование"
		}
		if next != "" && !changed && pending == next {
			q := question(next, r, cat)
			q.Text = "Не совсем понял ответ. " + q.Text
			return Reply{Messages: []Draft{q}}
		}
		req, sc := r, scenario
		if next != "" {
			q := question(next, r, cat)
			firstInfo := changed && pending == ""
			ack := ""
			if firstInfo && scenario == "discover" && len(r.Needs) > 0 {
				acc := make([]string, len(r.Needs))
				for i, n := range r.Needs {
					acc[i] = needAcc[n]
				}
				ack = "Понял: нужно закрыть " + joinAnd(acc)
				if r.Users > 0 {
					ack += " на " + UnitCount(r.Users, "user")
				}
				ack += ". Уточню пару деталей, чтобы подобрать точно.\n\n"
			} else if f := cat.ByID[r.ProductID]; firstInfo && scenario == "known" && f != nil && r.ProductID != "" {
				ack = f.Vendor + " · " + f.Title + " — есть в каталоге Store (" + strconv.Itoa(len(f.Variants)) + " позиций). Уточню параметры лицензии.\n\n"
			}
			q.Text = ack + q.Text
			return Reply{
				AddTags:  []string{tag},
				Patch:    func(t *model.Ticket) { t.Scenario, t.Requirements, t.PendingSlot = sc, req, next },
				Messages: []Draft{q},
			}
		}
		rec := recommend(cat, r, scenario)
		recPatch := rec.Patch
		rec.AddTags = append(rec.AddTags, tag)
		rec.Patch = func(t *model.Ticket) {
			if recPatch != nil {
				recPatch(t)
			}
			t.Scenario, t.Requirements, t.PendingSlot = sc, req, ""
		}
		return rec
	}

	// База знаний
	if kb := findKb(t); kb != nil {
		return kbReply(kb.ID, "Вопрос о покупке")
	}
	if mention.Family != nil {
		return Reply{Messages: []Draft{productAnswer(mention.Family, t)}}
	}
	if mention.Vendor != "" {
		return Reply{Patch: func(t *model.Ticket) { t.Scenario, t.PendingSlot = "known", "product" }, Messages: []Draft{askWhichProduct(mention)}}
	}

	if reRenewal.MatchString(t) {
		return Reply{
			Messages: []Draft{{Sender: "bot", Intent: "renewal", Text: "Помогу с продлением. Какой продукт продлеваем и на сколько устройств?", QuickReplies: []string{"Kaspersky Endpoint Security для бизнеса Стандартный", "Dr.Web Комплексная защита"}}},
			Patch:    func(t *model.Ticket) { t.Scenario, t.PendingSlot = "known", "product" },
		}
	}
	if reGreet.MatchString(t) {
		g := Greeting()
		g.Text = "Здравствуйте! Чем помочь?"
		return Reply{Messages: []Draft{g}}
	}
	if reThanks.MatchString(t) {
		return Reply{Messages: []Draft{{Sender: "bot", Intent: "thanks", Text: "Рад помочь! Если появятся вопросы — пишите.", QuickReplies: StartReplies[:3:3]}}}
	}

	// Второй раз подряд не поняли — зовём менеджера
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Sender == "bot" {
			if history[i].Intent == "fallback" {
				return Reply{
					Handoff:  &Handoff{Reason: "ИИ не смог распознать запрос", Confidence: 0.2},
					Messages: []Draft{{Sender: "bot", Intent: "handoff", Text: "Кажется, я не до конца понимаю вопрос. Подключаю менеджера."}},
				}
			}
			break
		}
	}
	return Reply{Messages: []Draft{{
		Sender: "bot", Intent: "fallback",
		Text:         "Не уверен, что правильно понял. Назовите продукт или опишите задачу — или выберите вариант:",
		QuickReplies: append([]string(nil), StartReplies...),
	}}}
}

// normReq — для сравнения «изменились ли требования»: пустой и nil-список одинаковы.
func normReq(r model.Requirements) model.Requirements {
	if len(r.Needs) == 0 {
		r.Needs = nil
	}
	return r
}

// BuildSummary — короткое резюме для менеджера: требования, что предложил ИИ, последнее сообщение.
func BuildSummary(ticket *model.Ticket, history []model.Message, reason string) string {
	var req []string
	for _, row := range requirementRows(ticket.Requirements, ticket.Scenario) {
		if row.value != "" {
			req = append(req, strings.ToLower(row.label)+": "+row.value)
		}
	}
	last := ""
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Sender == "user" {
			last = history[i].Text
			break
		}
	}
	quote := last
	if runeLen(last) > 110 {
		quote = runePrefix(last, 107) + "…"
	}
	var parts []string
	if len(req) > 0 {
		parts = append(parts, "Требования — "+strings.Join(req, "; ")+".")
	}
	if sel := selectedOrRecommended(ticket); sel != nil {
		var names []string
		for _, i := range sel.Items {
			names = append(names, i.Product.Name)
		}
		parts = append(parts, "ИИ предложил: "+shortTitle(*sel)+" ("+strings.Join(names, " + ")+") — "+FormatPrice(sel.Total)+".")
	}
	if quote != "" {
		parts = append(parts, "Последнее сообщение: «"+quote+"».")
	}
	if reason != "" {
		parts = append(parts, reason+".")
	}
	return strings.Join(parts, " ")
}

// selectedOrRecommended — выбранный вариант или рекомендуемый (без запасного «первого»).
func selectedOrRecommended(t *model.Ticket) *model.Offer {
	for i := range t.LastOffers {
		if t.SelectedOfferID != "" && t.LastOffers[i].ID == t.SelectedOfferID {
			return &t.LastOffers[i]
		}
	}
	for i := range t.LastOffers {
		if t.LastOffers[i].IsRecommended() {
			return &t.LastOffers[i]
		}
	}
	return nil
}

// SuggestReply — подсказка ответа менеджеру.
func SuggestReply(ticket *model.Ticket, operatorName string) string {
	hello := "Здравствуйте! Меня зовут " + operatorName
	if !strings.HasSuffix(operatorName, ".") {
		hello += "."
	}
	switch {
	case ticket.HasTag("Скидка"):
		return hello + " Запрошу у вендора условия для вашего объёма и вернусь с расчётом сегодня до 18:00. Подскажите, это новая покупка или переход с другого продукта?"
	case ticket.HasTag("Госзакупки"):
		return hello + " Подготовлю КП и пакет документов для закупки. Подскажите, по какому закону проводится закупка (44-ФЗ или 223-ФЗ) и нужен ли номер записи в реестре российского ПО?"
	case ticket.HasTag("Доступность вендора"):
		return hello + " Проверю возможность поставки у вендора и вернусь с ответом в течение рабочего дня. Если поставка невозможна, предложу аналог."
	}
	sel := selectedOrRecommended(ticket)
	if ticket.QuoteID != "" {
		return hello + " Проверила черновик КП — всё корректно, отправляю его на вашу почту. Если понадобится счёт или договор — подготовлю."
	}
	if sel != nil {
		return hello + " Посмотрела ваш запрос: " + shortTitle(*sel) + " на " + FormatPrice(sel.Total) + " подходит под ваши требования. Подготовить коммерческое предложение?"
	}
	return hello + " Я менеджер Store, помогу с выбором и оформлением. Расскажите, пожалуйста, подробнее о задаче."
}
