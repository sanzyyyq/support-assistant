package agent

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"sort"
	"strings"

	"support-assistant/internal/model"
)

// Каталог — выжимка из товарного фида Store (data/build_catalog.py → catalog.json.gz).
// Здесь поиск продукта по фразе клиента, расчёт цены под количество и срок,
// редакции и российские аналоги.

// RawVariant — позиция фида внутри семейства.
type RawVariant struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Price float64 `json:"price"`
	// Месяцы; 0 — бессрочно; nil — не указан
	Term *int   `json:"term"`
	Kind string `json:"kind"`
	Min  *int   `json:"min"`
	Max  *int   `json:"max"`
	// per_unit | pack
	Mode string `json:"mode"`
	// com | gov | nko | "" (для всех)
	Org string `json:"org"`
	URL string `json:"url"`
}

// Family — продукт (семейство позиций одного вендора).
type Family struct {
	ID        string       `json:"id"`
	Vendor    string       `json:"vendor"`
	Title     string       `json:"title"`
	Path      []string     `json:"path"`
	Registry  *string      `json:"registry"`
	Platforms *string      `json:"platforms"`
	Picture   *string      `json:"picture"`
	Unit      string       `json:"unit"`
	RU        bool         `json:"ru"`
	Variants  []RawVariant `json:"variants"`

	// Вычисляется при загрузке
	titleWords  []string
	vendorWords []string
	pathText    string
	hasNew      bool
}

// Catalog — все продукты и быстрый доступ по id.
type Catalog struct {
	Families []*Family
	ByID     map[string]*Family
	Source   string
}

// LoadCatalog читает catalog.json или catalog.json.gz.
func LoadCatalog(path string) (*Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) > 2 && data[0] == 0x1f && data[1] == 0x8b {
		zr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		if data, err = io.ReadAll(zr); err != nil {
			return nil, err
		}
	}
	var raw struct {
		GeneratedFrom string    `json:"generatedFrom"`
		Families      []*Family `json:"families"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("каталог %s: %w", path, err)
	}
	cat := &Catalog{Families: raw.Families, ByID: make(map[string]*Family, len(raw.Families)), Source: raw.GeneratedFrom}
	for _, f := range raw.Families {
		f.titleWords = words(f.Title)
		f.vendorWords = words(expand(f.Vendor))
		f.pathText = strings.Join(f.Path, " > ")
		for _, v := range f.Variants {
			if v.Kind == "new" {
				f.hasNew = true
				break
			}
		}
		cat.ByID[f.ID] = f
	}
	return cat, nil
}

// ── Нормализация текста ───────────────────────────────────────────────

// Синонимы: «касперского» → kaspersky, «доктор веб» → dr.web и т.п.
var aliases = []struct {
	re *regexp.Regexp
	to string
}{
	{regexp.MustCompile(`касперск\w*`), "kaspersky"},
	{regexp.MustCompile(`доктор\s*веб|др\.?\s*веб|drweb|dr\s+web`), "dr.web доктор веб"},
	{regexp.MustCompile(`мдемон|мдэймон|эмдемон`), "mdaemon"},
	{regexp.MustCompile(`астра\b|астру|астры`), "astra"},
	{regexp.MustCompile(`архикад|архмкад|архикада`), "archicad"},
	{regexp.MustCompile(`фотошоп\w*`), "photoshop"},
	{regexp.MustCompile(`анидеск|энидеск|эни деск`), "anydesk"},
	{regexp.MustCompile(`крипто\s*про`), "криптопро"},
	{regexp.MustCompile(`ворд|ворда`), "word"},
	{regexp.MustCompile(`эксел\w*`), "excel"},
	{regexp.MustCompile(`офис\s*365|o365`), "office 365"},
	{regexp.MustCompile(`1с\b|1c\b`), "1с"},
	{regexp.MustCompile(`кибербэкап`), "кибер бэкап"},
	{regexp.MustCompile(`ред\s*ос`), "ред ос"},
}

// Слова, которые не помогают найти продукт
var stopWords = toSet(
	"для", "на", "и", "с", "по", "от", "до", "в", "из", "или", "без", "при", "под", "нам", "мне", "нужен", "нужна", "нужно",
	"лицензия", "лицензии", "лицензий", "лицензию", "версия", "редакция", "год", "года", "лет", "шт", "штук",
	"компонентов", "количество", "количеству", "узлов", "пользователей", "business", "edition", "цена", "есть",
	"россия", "российский", "российская", "аналог", "компьютеров", "компьютер", "файлов", "почту", "почты", "ключи", "ключом",
	"сколько", "стоит", "стоимость", "купить", "приобрести", "хотим", "хочу", "добрый", "день", "здравствуйте",
)

var normPunct = strings.NewReplacer(
	"«", " ", "»", " ", `"`, " ", "(", " ", ")", " ", "[", " ", "]", " ", ",", " ", ";", " ", ":", " ",
	"!", " ", "?", " ", "/", " ", `\`, " ", "+", " ", " ", " ",
)

// Norm — нижний регистр, ё → е, без скобок и знаков препинания, одиночные пробелы.
func Norm(s string) string {
	s = strings.ReplaceAll(strings.ToLower(s), "ё", "е")
	return strings.Join(strings.Fields(normPunct.Replace(s)), " ")
}

// expand — нормализованный текст плюс синонимы в конце.
func expand(text string) string {
	t := Norm(text)
	for _, a := range aliases {
		if a.re.MatchString(t) {
			t += " " + a.to
		}
	}
	return t
}

var (
	reDigitsOnly = regexp.MustCompile(`^\d+$`)
	reLatin      = regexp.MustCompile(`[a-z]`)
	shortWords   = toSet("ос", "1с", "ии")
)

// words — значимые слова; двухбуквенные — только устойчивые («ос», «1с»).
func words(s string) []string {
	var out []string
	for _, w := range strings.Split(Norm(s), " ") {
		if (runeLen(w) >= 3 || shortWords[w]) && !stopWords[w] && !reDigitsOnly.MatchString(w) {
			out = append(out, w)
		}
	}
	return out
}

// stemMatch — слово запроса совпадает со словом каталога с точностью до окончания.
func stemMatch(q, w string) bool {
	if q == w {
		return true
	}
	// Латинские названия — строго (power ≠ powerdvd), допускаем только множественное число
	if reLatin.MatchString(q) || reLatin.MatchString(w) {
		return q+"s" == w || w+"s" == q
	}
	qr, wr := []rune(q), []rune(w)
	if len(qr) < 4 || len(wr) < 4 {
		return false
	}
	k := max(4, min(len(qr), len(wr))-2)
	if k > len(qr) || k > len(wr) {
		return false
	}
	return string(qr[:k]) == string(wr[:k])
}

// ── Поиск ─────────────────────────────────────────────────────────────

// Mention — что клиент назвал в сообщении: продукт или только вендора.
type Mention struct {
	Family *Family
	// Назван только вендор («касперского»), продукт не ясен
	Vendor         string
	VendorFamilies []*Family
}

// FindMentioned — найти продукт, который клиент назвал в сообщении.
func FindMentioned(cat *Catalog, text string) Mention {
	q := words(expand(text))
	if len(q) == 0 {
		return Mention{}
	}
	matchesQuery := func(w string) bool {
		for _, x := range q {
			if stemMatch(x, w) {
				return true
			}
		}
		return false
	}
	var best *Family
	bestScore := 0.0
	vendorHits := map[string][]*Family{}
	var vendorOrder []string
	for _, f := range cat.Families {
		if !f.hasNew {
			continue
		}
		vendorHit := false
		for _, w := range f.vendorWords {
			if matchesQuery(w) {
				vendorHit = true
				break
			}
		}
		var titleHit []string
		for _, w := range f.titleWords {
			if matchesQuery(w) {
				titleHit = append(titleHit, w)
			}
		}
		if vendorHit {
			if _, ok := vendorHits[f.Vendor]; !ok {
				vendorOrder = append(vendorOrder, f.Vendor)
			}
			vendorHits[f.Vendor] = append(vendorHits[f.Vendor], f)
		}
		// Слова названия, которые не повторяют имя вендора: «endpoint», «стандартный», «ос»
		var own []string
		for _, w := range titleHit {
			if !contains(f.vendorWords, w) {
				own = append(own, w)
			}
		}
		if len(own) == 0 || len(f.titleWords) == 0 {
			continue
		}
		ratio := float64(len(titleHit)) / float64(len(f.titleWords))
		latin := false
		for _, w := range own {
			if reLatin.MatchString(w) {
				latin = true
				break
			}
		}
		// Принимаем: всё название целиком; или латинский бренд + половина названия; или вендор + половина названия
		var ok bool
		if ratio == 1 {
			ok = len(own) >= 1 && (latin || vendorHit || len(titleHit) >= 2)
		} else {
			ok = ratio >= 0.5 && (vendorHit || (latin && len(own) >= 2))
		}
		if !ok {
			continue
		}
		// Полное совпадение названия важнее числа совпавших слов: «РЕД ОС» лучше, чем «Инсталляционный комплект … РЕД ОС»
		score := float64(len(titleHit)) + ratio*5 + b2f(vendorHit) + float64(min(len(f.Variants), 50))/100
		if best == nil || score > bestScore {
			best, bestScore = f, score
		}
	}
	if best != nil && bestScore >= 3 {
		return Mention{Family: best}
	}
	// Назван только вендор: «85 лицензий касперского» — берём вендора с наибольшим числом совпадений
	sort.SliceStable(vendorOrder, func(i, j int) bool { return len(vendorHits[vendorOrder[i]]) > len(vendorHits[vendorOrder[j]]) })
	if len(vendorOrder) > 0 {
		vendor := vendorOrder[0]
		var top []*Family
		for _, f := range vendorHits[vendor] {
			if f.hasNew {
				top = append(top, f)
			}
		}
		sort.SliceStable(top, func(i, j int) bool { return len(top[i].Variants) > len(top[j].Variants) })
		if len(top) > 4 {
			top = top[:4]
		}
		return Mention{Vendor: vendor, VendorFamilies: top}
	}
	return Mention{}
}

// ── Задачи клиента → разделы каталога ─────────────────────────────────

// needKeys — порядок важен: по нему ищется задача продукта для российских аналогов
var needKeys = []string{"mail", "mail_server", "endpoint", "backup", "office", "os"}

var (
	reSecurityPath = regexp.MustCompile(`(?i)Антивирус|безопасн`)
	reMailTitle    = regexp.MustCompile(`(?i)почт|mail`)
	reMailNot      = regexp.MustCompile(`(?i)office 365|connector`)
	reMailSrv1     = regexp.MustCompile(`(?i)почт|mail|messaging`)
	reMailSrv2     = regexp.MustCompile(`(?i)сервер|server|enterprise|messaging`)
	reMailSrvNot   = regexp.MustCompile(`(?i)антивирус|antivirus|security|защит|connector|activesync|outlook|клиент|the bat|spam`)
	reEndpoint     = regexp.MustCompile(`(?i)endpoint|комплексная защита|антивирус|security для бизнеса|small office|total security`)
	reEndpointNot  = regexp.MustCompile(`(?i)почт|шлюз|gateway|хранени|embedded|office 365|server security|curenet|виртуальн|фстэк|mail`)
	reBackupNot    = regexp.MustCompile(`(?i)mobiletrans|сопровожден|хранени|хранилищ|\bs3\b|дц\b`)
	reOSNot        = regexp.MustCompile(`(?i)сертификат|техподдерж|дистрибутив`)
)

// needFilter — какие семейства закрывают задачу клиента.
func needFilter(key string, f *Family) bool {
	p := f.pathText
	switch key {
	case "mail":
		return reSecurityPath.MatchString(p) && reMailTitle.MatchString(f.Title) && !reMailNot.MatchString(f.Title)
	case "mail_server":
		s := f.Title + " " + p
		return reMailSrv1.MatchString(s) && reMailSrv2.MatchString(s) && !reMailSrvNot.MatchString(f.Title)
	case "endpoint":
		return strings.Contains(p, "Антивирусы для организаций") && reEndpoint.MatchString(f.Title) && !reEndpointNot.MatchString(f.Title)
	case "backup":
		return strings.Contains(p, "резервного копирования") && !reBackupNot.MatchString(f.Title)
	case "office":
		return strings.Contains(p, "Офисные программы")
	case "os":
		return strings.Contains(p, "Операционные системы") && !reOSNot.MatchString(f.Title)
	}
	return false
}

// preferred — профильные продукты под задачу: их агент предлагает в первую очередь.
var preferred = map[string]*regexp.Regexp{
	"mail":        regexp.MustCompile(`(?i)security для почтовых серверов|mail security|почтовых серверов`),
	"mail_server": regexp.MustCompile(`(?i)messaging server|tegu|communigate|почтовый сервер`),
	"endpoint":    regexp.MustCompile(`(?i)endpoint security для бизнеса стандартный|комплексная защита|endpoint security standard`),
	"backup":      regexp.MustCompile(`(?i)кибер бэкап (стандартная|расширенная)|backup`),
	"office":      regexp.MustCompile(`(?i)360|р7|мойофис|workspad`),
	"os":          regexp.MustCompile(`(?i)^ред ос|astra linux|альт`),
}

var reUnparsedPack = regexp.MustCompile(`,\s*\d+\s*$`)

// looksLikeUnparsedPack — название вида «…, 5»: пакет, который фид не разметил количеством; в подборе не используем.
func looksLikeUnparsedPack(f *Family) bool { return reUnparsedPack.MatchString(f.Title) }

// ── Цена под требования ───────────────────────────────────────────────

// Priced — позиция семейства, посчитанная под количество и срок.
type Priced struct {
	Family  *Family
	Variant *RawVariant
	// Сколько единиц нужно клиенту (может быть увеличено до минимума тарифа)
	Qty   int
	Packs int
	Total float64
	// Количество подняли до минимального для этой цены
	Adjusted bool
	// Точного срока в каталоге нет — взяли ближайший
	TermFallback bool
}

func termEq(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func intOr(p *int, def int) int {
	if p == nil {
		return def
	}
	return *p
}

// PriceFor — подобрать позицию семейства под количество и срок.
// per_unit: цена × количество (с учётом диапазона количества);
// pack: наименьший пакет, который покрывает количество; если таких нет — несколько крупных пакетов.
func PriceFor(f *Family, qty, years int, kind, org string) *Priced {
	var vs []*RawVariant
	for i := range f.Variants {
		v := &f.Variants[i]
		if v.Kind == kind && (v.Org == "" || v.Org == org) {
			vs = append(vs, v)
		}
	}
	if len(vs) == 0 && kind == "new" {
		for i := range f.Variants {
			if f.Variants[i].Kind == "new" {
				vs = append(vs, &f.Variants[i])
			}
		}
	}
	if len(vs) == 0 {
		return nil
	}

	want := years * 12
	// Сроки в порядке появления (без повторов)
	var terms []*int
	for _, v := range vs {
		dup := false
		for _, t := range terms {
			if termEq(t, v.Term) {
				dup = true
				break
			}
		}
		if !dup {
			terms = append(terms, v.Term)
		}
	}
	has := func(m int) bool {
		for _, t := range terms {
			if t != nil && *t == m {
				return true
			}
		}
		return false
	}
	var term *int
	switch {
	case has(want):
		term = &want
	case has(0):
		zero := 0
		term = &zero
	default:
		sorted := append([]*int(nil), terms...)
		sort.SliceStable(sorted, func(i, j int) bool {
			return math.Abs(float64(intOr(sorted[i], 12)-want)) < math.Abs(float64(intOr(sorted[j], 12)-want))
		})
		term = sorted[0]
	}
	var sel []*RawVariant
	for _, v := range vs {
		if termEq(v.Term, term) {
			sel = append(sel, v)
		}
	}
	termFallback := term != nil && *term != want && *term != 0

	var packs, perUnit []*RawVariant
	for _, v := range sel {
		if v.Mode == "pack" {
			packs = append(packs, v)
		} else if v.Mode == "per_unit" {
			perUnit = append(perUnit, v)
		}
	}
	sort.SliceStable(packs, func(i, j int) bool {
		a, b := intOr(packs[i].Min, 1), intOr(packs[j].Min, 1)
		if a != b {
			return a < b
		}
		return packs[i].Price < packs[j].Price
	})

	var options []*Priced
	var fit *RawVariant
	for _, v := range packs {
		if intOr(v.Min, 1) >= qty {
			fit = v
			break
		}
	}
	if fit != nil {
		options = append(options, &Priced{Family: f, Variant: fit, Qty: qty, Packs: 1, Total: fit.Price, TermFallback: termFallback})
	} else if len(packs) > 0 {
		big := packs[len(packs)-1]
		n := int(math.Ceil(float64(qty) / float64(intOr(big.Min, 1))))
		options = append(options, &Priced{Family: f, Variant: big, Qty: qty, Packs: n, Total: big.Price * float64(n), TermFallback: termFallback})
	}
	for _, v := range perUnit {
		if (v.Min == nil || qty >= *v.Min) && (v.Max == nil || qty <= *v.Max) {
			options = append(options, &Priced{Family: f, Variant: v, Qty: qty, Packs: qty, Total: v.Price * float64(qty), TermFallback: termFallback})
		}
	}
	if len(options) == 0 && len(perUnit) > 0 {
		// Количество меньше минимального для тарифа — поднимаем до минимума и честно говорим об этом
		byMin := append([]*RawVariant(nil), perUnit...)
		sort.SliceStable(byMin, func(i, j int) bool { return intOr(byMin[i].Min, 1) < intOr(byMin[j].Min, 1) })
		v := byMin[0]
		q := max(qty, intOr(v.Min, 1))
		options = append(options, &Priced{Family: f, Variant: v, Qty: q, Packs: q, Total: v.Price * float64(q), Adjusted: q != qty, TermFallback: termFallback})
	}
	if len(options) == 0 {
		return nil
	}
	sort.SliceStable(options, func(i, j int) bool { return options[i].Total < options[j].Total })
	return options[0]
}

// ToProduct — карточка позиции для чата и КП.
func ToProduct(f *Family, v *RawVariant) model.Product {
	category := ""
	if len(f.Path) > 0 {
		category = f.Path[len(f.Path)-1]
	}
	platforms := []string{}
	if f.Platforms != nil {
		for _, p := range strings.Split(*f.Platforms, ", ") {
			if p != "" {
				platforms = append(platforms, p)
			}
		}
	}
	p := model.Product{
		ID: v.ID, FamilyID: f.ID, SKU: v.ID, Vendor: f.Vendor, Name: f.Title, OfferName: v.Name,
		Category: category, CategoryPath: f.Path, LicenseKind: v.Kind, TermMonths: v.Term, Unit: f.Unit,
		Price: v.Price, Pricing: v.Mode, TierMin: v.Min, TierMax: v.Max, Platforms: platforms,
		RussianVendor: f.RU, URL: v.URL,
	}
	if f.Registry != nil {
		p.RegistryNumber = *f.Registry
	}
	if f.Picture != nil {
		p.Picture = *f.Picture
	}
	return p
}

// editionsOf — редакции того же продукта: Kaspersky Endpoint Security для бизнеса Стандартный / Расширенный / Расширенный Плюс.
func editionsOf(cat *Catalog, f *Family) []*Family {
	// Редакция отличается последним словом при совпадающем начале; дополнения (ActiveSync, Connector) не редакции
	if len(f.titleWords) < 2 {
		return []*Family{f}
	}
	head := f.titleWords[:len(f.titleWords)-1]
	// В общем начале должно быть хотя бы одно слово, кроме имени вендора: «РЕД ОС» и «РЕД АДМ» — разные продукты
	ownWord := false
	for _, w := range head {
		if !contains(f.vendorWords, w) {
			ownWord = true
			break
		}
	}
	if !ownWord {
		return []*Family{f}
	}
	var sib []*Family
	for _, x := range cat.Families {
		if x.Vendor != f.Vendor || !x.hasNew || x.ID == f.ID || x.Unit != f.Unit ||
			len(x.titleWords) < len(f.titleWords) || len(x.titleWords) > len(f.titleWords)+1 {
			continue
		}
		same := true
		for i, w := range head {
			if x.titleWords[i] != w {
				same = false
				break
			}
		}
		if same {
			sib = append(sib, x)
		}
	}
	sort.SliceStable(sib, func(i, j int) bool {
		if len(sib[i].titleWords) != len(sib[j].titleWords) {
			return len(sib[i].titleWords) < len(sib[j].titleWords)
		}
		return len(sib[i].Variants) > len(sib[j].Variants)
	})
	if len(sib) > 2 {
		sib = sib[:2]
	}
	return append([]*Family{f}, sib...)
}

// usable — позицию можно предлагать: без «пакетов по 5 × 10» и подозрительно дешёвых (хранилище за гигабайт и т.п.)
func usable(x *Priced) bool {
	return x != nil && !x.Adjusted && x.Total >= 100 && !(x.Variant.Mode == "pack" && x.Packs > 1)
}

// RussianAnalogs — профильные продукты той же задачи (или того же раздела каталога) от российских разработчиков.
func RussianAnalogs(cat *Catalog, f *Family, qty, years int) []*Priced {
	leaf := ""
	if len(f.Path) > 0 {
		leaf = f.Path[len(f.Path)-1]
	}
	need := ""
	for _, k := range needKeys {
		if needFilter(k, f) {
			need = k
			break
		}
	}
	var pref *regexp.Regexp
	if need != "" {
		pref = preferred[need]
	}
	var list []*Priced
	for _, x := range cat.Families {
		if !x.RU || !x.hasNew || x.ID == f.ID || looksLikeUnparsedPack(x) {
			continue
		}
		if need != "" {
			if !needFilter(need, x) {
				continue
			}
		} else if len(x.Path) == 0 || x.Path[len(x.Path)-1] != leaf {
			continue
		}
		if p := PriceFor(x, qty, years, "new", "com"); usable(p) {
			list = append(list, p)
		}
	}
	isPref := func(p *Priced) int {
		if pref != nil && pref.MatchString(p.Family.Title) {
			return 1
		}
		return 0
	}
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if d := isPref(b) - isPref(a); d != 0 {
			return d < 0
		}
		if d := b2i(b.Family.Registry != nil) - b2i(a.Family.Registry != nil); d != 0 {
			return d < 0
		}
		return a.Total < b.Total
	})
	var out []*Priced
	seen := map[string]bool{}
	for _, x := range list {
		if seen[x.Family.Vendor] {
			continue
		}
		seen[x.Family.Vendor] = true
		out = append(out, x)
		if len(out) == 2 {
			break
		}
	}
	return out
}

// ── Мелочи ────────────────────────────────────────────────────────────

func toSet(xs ...string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func b2f(b bool) float64 { return float64(b2i(b)) }

// ── Витрина ───────────────────────────────────────────────────────────

// featuredTitles — известные продукты для демо-витрины
var featuredTitles = []string{
	"Kaspersky Endpoint Security для бизнеса Стандартный",
	"Комплексная защита + Центр управления",
	"MDaemon Messaging Server",
	"Кибер Бэкап Расширенная редакция",
	"Kaspersky Security для почтовых серверов",
	"РЕД ОС",
	"Secret Net Studio",
	"КОМПАС-3D",
}

// Featured — карточки витрины: самая дешёвая новая лицензия каждого продукта.
func Featured(cat *Catalog) []model.Product {
	var out []model.Product
	seen := map[string]bool{}
	for _, title := range featuredTitles {
		var f *Family
		for _, x := range cat.Families {
			if x.Title == title && x.hasNew {
				f = x
				break
			}
		}
		if f == nil {
			for _, x := range cat.Families {
				if x.hasNew && strings.HasPrefix(Norm(x.Title), Norm(title)) {
					f = x
					break
				}
			}
		}
		if f == nil || seen[f.ID] {
			continue
		}
		var cheapest *RawVariant
		for i := range f.Variants {
			v := &f.Variants[i]
			if v.Kind == "new" && (cheapest == nil || v.Price < cheapest.Price) {
				cheapest = v
			}
		}
		if cheapest != nil {
			seen[f.ID] = true
			out = append(out, ToProduct(f, cheapest))
		}
	}
	return out
}

// VariantCount — сколько позиций (offer) в каталоге.
func (c *Catalog) VariantCount() int {
	n := 0
	for _, f := range c.Families {
		n += len(f.Variants)
	}
	return n
}
