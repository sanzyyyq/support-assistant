package agent

import (
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"support-assistant/internal/model"
)

// Форматирование чисел и подписей — как во фронтенде (frontend/src/lib/format.ts, domain.ts).

// nbsp — разделитель разрядов в русской локали («12 345»)
const nbsp = " "

// jsRound — округление как Math.round в JavaScript (.5 — вверх).
func jsRound(x float64) float64 { return math.Floor(x + 0.5) }

// FormatNumber — «12 345», дробная часть до 3 знаков через запятую.
func FormatNumber(n float64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	r := math.Round(n*1000) / 1000
	intPart := math.Floor(r)
	frac := r - intPart
	digits := strconv.FormatFloat(intPart, 'f', 0, 64)
	var b strings.Builder
	for i, c := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteString(nbsp)
		}
		b.WriteRune(c)
	}
	out := b.String()
	if frac > 0 {
		f := strings.TrimRight(strconv.FormatFloat(frac, 'f', 3, 64), "0")
		out += "," + strings.TrimPrefix(f, "0.")
	}
	if neg {
		out = "-" + out
	}
	return out
}

// FormatPrice — «12 345 ₽» (округление до рубля).
func FormatPrice(n float64) string { return FormatNumber(jsRound(n)) + " ₽" }

func plural(n int, one, few, many string) string {
	m10, m100 := n%10, n%100
	if m10 == 1 && m100 != 11 {
		return one
	}
	if m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14) {
		return few
	}
	return many
}

// Years — «1 год», «2 года», «5 лет».
func Years(n int) string { return strconv.Itoa(n) + " " + plural(n, "год", "года", "лет") }

// unit: один, несколько, много, короткая форма «за …»
var unitForms = map[string][4]string{
	"user":    {"пользователь", "пользователя", "пользователей", "пользователя"},
	"mailbox": {"почтовый ящик", "почтовых ящика", "почтовых ящиков", "ящик"},
	"device":  {"устройство", "устройства", "устройств", "устройство"},
	"server":  {"сервер", "сервера", "серверов", "сервер"},
	"license": {"лицензия", "лицензии", "лицензий", "лицензию"},
}

// UnitCount — «50 устройств».
func UnitCount(n int, unit string) string {
	f := unitForms[unit]
	return FormatNumber(float64(n)) + " " + plural(n, f[0], f[1], f[2])
}

// TermText — 12 → «1 год», 0 → «бессрочно», 3 → «3 мес.», nil → «срок не указан».
func TermText(months *int) string {
	if months == nil {
		return "срок не указан"
	}
	m := *months
	if m == 0 {
		return "бессрочно"
	}
	if m%12 == 0 {
		return Years(m / 12)
	}
	return strconv.Itoa(m) + " мес."
}

// ── Подписи предметной области ────────────────────────────────────────

var needLabel = map[model.Need]string{
	"mail": "Защита почты", "endpoint": "Защита ПК и файлов", "backup": "Резервное копирование",
	"office": "Офисный пакет", "os": "Операционная система",
}

// «Нужно закрыть …» — винительный падеж
var needAcc = map[model.Need]string{
	"mail": "защиту почты", "endpoint": "защиту ПК и файлов", "backup": "резервное копирование",
	"office": "офисный пакет", "os": "операционную систему",
}

var kindLabel = map[string]string{
	"new": "Новая лицензия", "renewal": "Продление", "migration": "Переход с другого продукта",
	"support": "Техподдержка", "upgrade": "Обновление", "media": "Дистрибутив",
}

var infraLabel = map[string]string{"own_server": "Свой почтовый сервер", "cloud": "Облачная почта", "none": "Почты пока нет"}

// CalcLabel — «50 устройств × 4 814 ₽» или «пакет на 50 устройств».
func CalcLabel(i model.OfferItem) string {
	p := i.Product
	if p.Pricing == "pack" {
		size := 1
		if p.TierMin != nil {
			size = *p.TierMin
		}
		if i.Packs > 1 {
			return strconv.Itoa(i.Packs) + " × пакет на " + UnitCount(size, p.Unit)
		}
		return "пакет на " + UnitCount(size, p.Unit)
	}
	return UnitCount(i.Packs, p.Unit) + " × " + FormatPrice(p.Price)
}

type requirementRow struct{ label, value string }

// requirementRows — строки «что агент уже знает» (пустое value — ещё не знает).
func requirementRows(r model.Requirements, scenario string) []requirementRow {
	var rows []requirementRow
	if scenario == "known" || r.ProductName != "" {
		rows = append(rows, requirementRow{"Продукт", r.ProductName})
	} else {
		labels := make([]string, len(r.Needs))
		for i, n := range r.Needs {
			labels[i] = needLabel[n]
		}
		rows = append(rows, requirementRow{"Задача", strings.Join(labels, ", ")})
	}
	users := ""
	if r.Users > 0 {
		users = strconv.Itoa(r.Users)
	}
	rows = append(rows, requirementRow{"Количество", users})
	if contains(r.Needs, "mail") || r.Infrastructure != "" {
		rows = append(rows, requirementRow{"Почта", infraLabel[r.Infrastructure]})
	}
	y := ""
	if r.Years > 0 {
		y = Years(r.Years)
	}
	rows = append(rows, requirementRow{"Срок", y})
	if scenario != "known" {
		b := ""
		if r.Budget != nil && r.Budget.Unlimited {
			b = "не ограничен"
		} else if r.Budget != nil && r.Budget.Amount != 0 {
			b = "до " + FormatPrice(r.Budget.Amount)
		}
		rows = append(rows, requirementRow{"Бюджет", b})
	}
	return rows
}

// QuoteSum — сумма КП (цена × количество по всем позициям).
func QuoteSum(q model.Quote) float64 {
	sum := 0.0
	for _, i := range q.Items {
		sum += i.Price * float64(i.Qty)
	}
	return sum
}

// ── Мелочи ────────────────────────────────────────────────────────────

// ISO — время в формате JavaScript toISOString(): 2026-01-02T15:04:05.000Z
func ISO(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// joinAnd — «a, b и c».
func joinAnd(xs []string) string {
	if len(xs) < 2 {
		return strings.Join(xs, "")
	}
	return strings.Join(xs[:len(xs)-1], ", ") + " и " + xs[len(xs)-1]
}

// runeLen и runePrefix — длина и начало строки в символах (как length/slice в JS для кириллицы).
func runeLen(s string) int { return utf8.RuneCountInString(s) }

func runePrefix(s string, n int) string {
	if n <= 0 {
		return ""
	}
	i := 0
	for pos := range s {
		if i == n {
			return s[:pos]
		}
		i++
	}
	return s
}

func uniqStrings(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

func boolPtr(b bool) *bool { return &b }
