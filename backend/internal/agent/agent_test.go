package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"support-assistant/internal/model"
)

// Эталонный тест: агент должен отвечать так же, как прежняя версия на TypeScript.
// testdata/scenarios.json — диалоги (реальные фразы клиентов из переписок и типовые сценарии),
// testdata/golden.json — ответы прежней версии на них.
//
//	go test ./internal/agent/
//
// Большой прогон на всех переписках (файлы не в репозитории):
//
//	GOLDEN_SCENARIOS=…/scenarios_full.json GOLDEN_FILE=…/golden_full.json go test ./internal/agent/ -run Golden

const catalogPath = "../../data/catalog.json.gz"

var testCat *Catalog

func catalog(t *testing.T) *Catalog {
	t.Helper()
	if testCat == nil {
		c, err := LoadCatalog(catalogPath)
		if err != nil {
			t.Fatalf("каталог: %v", err)
		}
		testCat = c
	}
	return testCat
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

// turn — один ход диалога; порядок полей как в эталоне (важен для нумерации вариантов o1, o2…)
type turn struct {
	Text       string            `json:"text"`
	Messages   []model.Message   `json:"messages"`
	Handoff    *Handoff          `json:"handoff"`
	Sentiment  *string           `json:"sentiment"`
	AddTags    []string          `json:"addTags"`
	Ticket     turnTicket        `json:"ticket"`
	Summary    string            `json:"summary"`
	Suggestion string            `json:"suggestion"`
	QuoteItems []model.QuoteItem `json:"quoteItems"`
}

type turnTicket struct {
	Status          string             `json:"status"`
	Scenario        *string            `json:"scenario"`
	PendingSlot     *string            `json:"pendingSlot"`
	Requirements    model.Requirements `json:"requirements"`
	Tags            []string           `json:"tags"`
	Sentiment       string             `json:"sentiment"`
	LastOffers      []model.Offer      `json:"lastOffers"`
	SelectedOfferID *string            `json:"selectedOfferId"`
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

var reOfferID = regexp.MustCompile(`"o-[0-9a-z]+-\d+"`)

// normalizeIDs — случайные id вариантов → o1, o2… в порядке появления в тексте (как в эталоне).
func normalizeIDs(js string, ids map[string]string) string {
	return reOfferID.ReplaceAllStringFunc(js, func(id string) string {
		if _, ok := ids[id]; !ok {
			ids[id] = fmt.Sprintf(`"o%d"`, len(ids)+1)
		}
		return ids[id]
	})
}

// clean — убрать пустые списки и null: в TypeScript «нет поля» и «пустой список» иногда различались, на смысл это не влияет
func clean(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, val := range x {
			c := clean(val)
			// У черновиков сообщений агента нет id и даты: в Go это пустые строки, в TypeScript — нет поля
			if c == nil || c == "" {
				continue
			}
			if arr, ok := c.([]any); ok && len(arr) == 0 && k != "tags" && k != "addTags" {
				continue
			}
			out[k] = c
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = clean(val)
		}
		return out
	}
	return v
}

func runScenario(cat *Catalog, messages []string) []string {
	ids := map[string]string{}
	t := &model.Ticket{ID: "t-1", Number: 1, Channel: "web", Status: "ai", Tags: []string{}, Sentiment: "neutral",
		CreatedAt: "2026-01-01T00:00:00.000Z", UpdatedAt: "2026-01-01T00:00:00.000Z", Customer: model.Customer{ID: "c-1", Name: "Тест"}}
	n := 0
	msg := func(d Draft) model.Message {
		n++
		d.ID, d.TicketID, d.CreatedAt = fmt.Sprintf("m%d", n), t.ID, t.CreatedAt
		return d
	}
	history := []model.Message{msg(Greeting())}
	var out []string
	for _, text := range messages {
		if t.Status != "ai" {
			break
		}
		history = append(history, msg(Draft{Sender: "user", Text: text}))
		reply := GenerateReply(cat, t, text, history)
		ApplyReply(t, reply)
		for _, m := range reply.Messages {
			history = append(history, msg(m))
		}
		if reply.Handoff != nil {
			conf := reply.Handoff.Confidence
			t.Status, t.HandoffReason, t.AIConfidence, t.PendingSlot = "waiting", reply.Handoff.Reason, &conf, ""
		}
		var qi []model.QuoteItem
		if sel := t.SelectedOffer(); sel != nil {
			qi = QuoteItemsFromOffer(*sel)
		}
		var sentiment *string
		if reply.Sentiment != "" {
			sentiment = &reply.Sentiment
		}
		tags := reply.AddTags
		if tags == nil {
			tags = []string{}
		}
		tr := turn{
			Text: text, Messages: reply.Messages, Handoff: reply.Handoff, Sentiment: sentiment, AddTags: tags,
			Ticket: turnTicket{
				Status: t.Status, Scenario: strPtr(t.Scenario), PendingSlot: strPtr(t.PendingSlot), Requirements: t.Requirements,
				Tags: t.Tags, Sentiment: t.Sentiment, LastOffers: t.LastOffers, SelectedOfferID: strPtr(t.SelectedOfferID),
			},
			Summary: BuildSummary(t, history, t.HandoffReason), Suggestion: SuggestReply(t, "Анна К."), QuoteItems: qi,
		}
		js, _ := json.Marshal(tr)
		out = append(out, normalizeIDs(string(js), ids))
	}
	return out
}

// firstDiff — путь к первому различию (для понятного сообщения об ошибке).
func firstDiff(path string, a, b any) string {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok {
			return fmt.Sprintf("%s: %v ≠ %v", path, short(a), short(b))
		}
		keys := map[string]bool{}
		for k := range x {
			keys[k] = true
		}
		for k := range y {
			keys[k] = true
		}
		sorted := make([]string, 0, len(keys))
		for k := range keys {
			sorted = append(sorted, k)
		}
		sort.Strings(sorted)
		for _, k := range sorted {
			if d := firstDiff(path+"."+k, x[k], y[k]); d != "" {
				return d
			}
		}
		return ""
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return fmt.Sprintf("%s: %v ≠ %v", path, short(a), short(b))
		}
		for i := range x {
			if d := firstDiff(fmt.Sprintf("%s[%d]", path, i), x[i], y[i]); d != "" {
				return d
			}
		}
		return ""
	}
	if !reflect.DeepEqual(a, b) {
		return fmt.Sprintf("%s: %v ≠ %v", path, short(a), short(b))
	}
	return ""
}

func short(v any) string {
	b, _ := json.Marshal(v)
	s := string(b)
	if len(s) > 400 {
		s = s[:400] + "…"
	}
	return s
}

func TestGolden(t *testing.T) {
	cat := catalog(t)
	var scenarios []struct {
		Name     string   `json:"name"`
		Messages []string `json:"messages"`
	}
	readJSON(t, envOr("GOLDEN_SCENARIOS", "../../testdata/scenarios.json"), &scenarios)
	var golden struct {
		Scenarios []struct {
			Name  string `json:"name"`
			Turns []any  `json:"turns"`
		} `json:"scenarios"`
	}
	readJSON(t, envOr("GOLDEN_FILE", "../../testdata/golden.json"), &golden)

	failed := 0
	for i, sc := range scenarios {
		want := golden.Scenarios[i]
		got := runScenario(cat, sc.Messages)
		if len(got) != len(want.Turns) {
			t.Errorf("%s: ходов %d, ожидалось %d", sc.Name, len(got), len(want.Turns))
			failed++
			continue
		}
		for k, js := range got {
			var g any
			_ = json.Unmarshal([]byte(js), &g)
			if d := firstDiff("", clean(want.Turns[k]), clean(g)); d != "" {
				t.Errorf("%s, ход %d «%s»:\n  %s", sc.Name, k+1, strings.TrimSpace(sc.Messages[k]), d)
				failed++
				break
			}
		}
		if failed > 15 {
			t.Fatal("слишком много расхождений")
		}
	}
}

func TestSeedMatchesGolden(t *testing.T) {
	cat := catalog(t)
	var golden struct {
		Seed any `json:"seed"`
	}
	readJSON(t, "../../testdata/golden.json", &golden)

	db := CreateSeed(cat, time.Now())
	type seedTicket struct {
		model.Ticket
		HasHandoffAt bool `json:"hasHandoffAt"`
	}
	var tickets []map[string]any
	for _, tk := range db.Tickets {
		b, _ := json.Marshal(seedTicket{Ticket: *tk, HasHandoffAt: tk.HandoffAt != ""})
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		delete(m, "createdAt")
		delete(m, "updatedAt")
		delete(m, "handoffAt")
		tickets = append(tickets, m)
	}
	strip := func(v any, keys ...string) []map[string]any {
		b, _ := json.Marshal(v)
		var list []map[string]any
		_ = json.Unmarshal(b, &list)
		for _, m := range list {
			for _, k := range keys {
				delete(m, k)
			}
		}
		return list
	}
	out := map[string]any{
		"seq": db.Seq, "quoteSeq": db.QuoteSeq, "tickets": tickets,
		"messages": strip(db.Messages, "createdAt"), "quotes": strip(db.Quotes, "createdAt", "validUntil"),
	}
	// Тот же порядок ключей, что в эталоне: seq, quoteSeq, tickets, messages, quotes — нумерация вариантов по порядку
	js, _ := json.Marshal(struct {
		Seq      any `json:"seq"`
		QuoteSeq any `json:"quoteSeq"`
		Tickets  any `json:"tickets"`
		Messages any `json:"messages"`
		Quotes   any `json:"quotes"`
	}{out["seq"], out["quoteSeq"], out["tickets"], out["messages"], out["quotes"]})
	var got any
	_ = json.Unmarshal([]byte(normalizeIDs(string(js), map[string]string{})), &got)
	if d := firstDiff("seed", clean(golden.Seed), clean(got)); d != "" {
		t.Error(d)
	}
}

func TestFormat(t *testing.T) {
	cases := map[float64]string{4346: "4 346", 166326: "166 326", 999: "999", 1234567: "1 234 567", 12.5: "12,5"}
	for n, want := range cases {
		if got := FormatNumber(n); got != want {
			t.Errorf("FormatNumber(%v) = %q, ожидалось %q", n, got, want)
		}
	}
	if got := FormatPrice(369441.45); got != "369 441 ₽" {
		t.Errorf("FormatPrice = %q", got)
	}
}
