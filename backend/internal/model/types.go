// Package model — данные, которыми обмениваются фронтенд и сервер (JSON).
// Совпадает с контрактом во frontend/src/api/types.ts: даты — ISO 8601 (UTC), деньги — рубли.
// Необязательные поля — указатели или omitempty: в JSON их просто нет.
package model

import (
	"encoding/json"
	"fmt"
)

// ── Каталог ───────────────────────────────────────────────────────────

// Product — конкретная позиция каталога (offer из товарного фида).
type Product struct {
	ID       string `json:"id"`
	FamilyID string `json:"familyId"`
	// Артикул для КП
	SKU    string `json:"sku"`
	Vendor string `json:"vendor"`
	// Название продукта (семейства)
	Name string `json:"name"`
	// Полное наименование позиции из фида — идёт в КП
	OfferName    string   `json:"offerName"`
	Category     string   `json:"category"`
	CategoryPath []string `json:"categoryPath"`
	// new — новая лицензия, renewal — продление, migration — переход с другого продукта, …
	LicenseKind string `json:"licenseKind"`
	// Срок в месяцах; 0 — бессрочно; null — не указан
	TermMonths *int `json:"termMonths"`
	// user | mailbox | device | server | license
	Unit string `json:"unit"`
	// Цена из фида: за единицу (per_unit) или за пакет (pack)
	Price   float64 `json:"price"`
	Pricing string  `json:"pricing"`
	// per_unit — диапазон количества для цены; pack — размер пакета
	TierMin   *int     `json:"tierMin"`
	TierMax   *int     `json:"tierMax"`
	Platforms []string `json:"platforms"`
	// Номер в реестре отечественного ПО — только если он есть в фиде
	RegistryNumber string `json:"registryNumber,omitempty"`
	RussianVendor  bool   `json:"russianVendor"`
	URL            string `json:"url"`
	Picture        string `json:"picture,omitempty"`
}

// OfferItem — позиция в варианте решения.
type OfferItem struct {
	Product Product `json:"product"`
	// Сколько единиц нужно клиенту
	Qty int `json:"qty"`
	// Сколько позиций покупаем: единиц (per_unit) или пакетов (pack)
	Packs int     `json:"packs"`
	Total float64 `json:"total"`
}

// Offer — вариант решения, который предлагает агент.
type Offer struct {
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	Recommended *bool       `json:"recommended,omitempty"`
	Items       []OfferItem `json:"items"`
	// Почему подходит
	Reasons []string `json:"reasons"`
	// На что обратить внимание
	Notes []string `json:"notes,omitempty"`
	Total float64  `json:"total"`
}

// IsRecommended — вариант помечен как рекомендуемый.
func (o Offer) IsRecommended() bool { return o.Recommended != nil && *o.Recommended }

type Source struct {
	Title string `json:"title"`
	URL   string `json:"url,omitempty"`
	// product | kb
	Kind string `json:"kind"`
}

type CompareRow struct {
	Label  string   `json:"label"`
	Values []string `json:"values"`
}

type CompareTable struct {
	Columns []string     `json:"columns"`
	Rows    []CompareRow `json:"rows"`
}

// ── Требования, которые собирает агент ────────────────────────────────

// Need — задача клиента: mail | endpoint | backup | office | os
type Need = string

// Slot — о чём агент спрашивает: needs | product | users | infrastructure | years | budget
type Slot = string

// Budget — бюджет в рублях или «не ограничен» (в JSON: число или "unlimited").
type Budget struct {
	Unlimited bool
	Amount    float64
}

func (b Budget) MarshalJSON() ([]byte, error) {
	if b.Unlimited {
		return []byte(`"unlimited"`), nil
	}
	return json.Marshal(b.Amount)
}

func (b *Budget) UnmarshalJSON(data []byte) error {
	if string(data) == `"unlimited"` {
		*b = Budget{Unlimited: true}
		return nil
	}
	var n float64
	if err := json.Unmarshal(data, &n); err != nil {
		return fmt.Errorf("budget: %w", err)
	}
	*b = Budget{Amount: n}
	return nil
}

type Requirements struct {
	Needs       []Need `json:"needs,omitempty"`
	ProductID   string `json:"productId,omitempty"`
	ProductName string `json:"productName,omitempty"`
	Users       int    `json:"users,omitempty"`
	// own_server | cloud | none
	Infrastructure string  `json:"infrastructure,omitempty"`
	Years          int     `json:"years,omitempty"`
	Budget         *Budget `json:"budget,omitempty"`
}

// Clone — копия требований (срез needs не делится с оригиналом).
func (r Requirements) Clone() Requirements {
	c := r
	c.Needs = append([]Need(nil), r.Needs...)
	if r.Budget != nil {
		b := *r.Budget
		c.Budget = &b
	}
	return c
}

// ── Коммерческое предложение ──────────────────────────────────────────

type QuoteItem struct {
	SKU  string `json:"sku"`
	Name string `json:"name"`
	Qty  int    `json:"qty"`
	// Цена за единицу с учётом налогов
	Price float64 `json:"price"`
	// Ставка НДС, %; 0 — ПО из реестра (НДС не облагается); null — уточняет менеджер
	VatRate *float64 `json:"vatRate"`
}

type Quote struct {
	ID       string `json:"id"`
	Number   string `json:"number"`
	TicketID string `json:"ticketId"`
	// review | sent
	Status  string `json:"status"`
	Company string `json:"company"`
	INN     string `json:"inn,omitempty"`
	Email   string `json:"email"`
	// Предоплата | Постоплата
	PaymentTerms string `json:"paymentTerms"`
	DeliveryTerm string `json:"deliveryTerm"`
	// Дата окончания действия, ISO
	ValidUntil string      `json:"validUntil"`
	CreatedAt  string      `json:"createdAt"`
	Items      []QuoteItem `json:"items"`
	Reviewer   string      `json:"reviewer,omitempty"`
	Comment    string      `json:"comment,omitempty"`
}

type QuoteRequest struct {
	Company      string `json:"company"`
	INN          string `json:"inn,omitempty"`
	Email        string `json:"email"`
	PaymentTerms string `json:"paymentTerms"`
}

// ── Диалоги ───────────────────────────────────────────────────────────

type Message struct {
	ID       string `json:"id"`
	TicketID string `json:"ticketId"`
	// user | bot | operator | system
	Sender     string `json:"sender"`
	Text       string `json:"text"`
	CreatedAt  string `json:"createdAt"`
	AuthorName string `json:"authorName,omitempty"`
	// Кнопки-варианты ответа
	QuickReplies []string `json:"quickReplies,omitempty"`
	// Карточки отдельных продуктов
	Products []Product `json:"products,omitempty"`
	// Варианты решения с обоснованием
	Offers  []Offer       `json:"offers,omitempty"`
	Compare *CompareTable `json:"compare,omitempty"`
	// На что опирается ответ
	Sources []Source `json:"sources,omitempty"`
	// Встроенная форма: quote_request
	Form     string `json:"form,omitempty"`
	FormDone bool   `json:"formDone,omitempty"`
	// Ссылка на сформированное КП
	QuoteID string `json:"quoteId,omitempty"`
	// Оценка ответа клиентом: up | down
	Feedback string `json:"feedback,omitempty"`
	Intent   string `json:"intent,omitempty"`
}

type Customer struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Email   string `json:"email,omitempty"`
	Phone   string `json:"phone,omitempty"`
	Company string `json:"company,omitempty"`
	INN     string `json:"inn,omitempty"`
}

type Ticket struct {
	ID       string   `json:"id"`
	Number   int      `json:"number"`
	Customer Customer `json:"customer"`
	// web | telegram | email
	Channel string `json:"channel"`
	// ai | waiting | operator | closed
	Status        string `json:"status"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
	LastMessage   string `json:"lastMessage,omitempty"`
	Assignee      string `json:"assignee,omitempty"`
	HandoffAt     string `json:"handoffAt,omitempty"`
	HandoffReason string `json:"handoffReason,omitempty"`
	// 0..1
	AIConfidence *float64 `json:"aiConfidence,omitempty"`
	AISummary    string   `json:"aiSummary,omitempty"`
	// positive | neutral | negative
	Sentiment string   `json:"sentiment,omitempty"`
	Tags      []string `json:"tags"`
	// known — клиент знает продукт; discover — подбираем; question — вопрос
	Scenario     string       `json:"scenario,omitempty"`
	Requirements Requirements `json:"requirements"`
	// Какой вопрос агент задал последним
	PendingSlot     Slot    `json:"pendingSlot,omitempty"`
	LastOffers      []Offer `json:"lastOffers,omitempty"`
	SelectedOfferID string  `json:"selectedOfferId,omitempty"`
	QuoteID         string  `json:"quoteId,omitempty"`
}

// HasTag — у обращения есть метка.
func (t *Ticket) HasTag(tag string) bool {
	for _, x := range t.Tags {
		if x == tag {
			return true
		}
	}
	return false
}

// AddTag — добавить метку, если её ещё нет.
func (t *Ticket) AddTag(tag string) {
	if !t.HasTag(tag) {
		t.Tags = append(t.Tags, tag)
	}
}

// SelectedOffer — выбранный клиентом вариант, иначе рекомендуемый, иначе первый.
func (t *Ticket) SelectedOffer() *Offer {
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
	if len(t.LastOffers) > 0 {
		return &t.LastOffers[0]
	}
	return nil
}

type Session struct {
	Ticket   Ticket    `json:"ticket"`
	Messages []Message `json:"messages"`
}

type IntentCount struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

type Stats struct {
	TicketsToday     int           `json:"ticketsToday"`
	AIResolvedPct    int           `json:"aiResolvedPct"`
	HandoffPct       int           `json:"handoffPct"`
	FirstResponseSec float64       `json:"firstResponseSec"`
	Quotes           int           `json:"quotes"`
	QuotesAmount     float64       `json:"quotesAmount"`
	CsatPct          int           `json:"csatPct"`
	RatedCount       int           `json:"ratedCount"`
	TopIntents       []IntentCount `json:"topIntents"`
	// Сэкономлено часов работы операторов в день
	HoursSavedPerDay float64 `json:"hoursSavedPerDay"`
}

// ── Сотрудники ────────────────────────────────────────────────────────

// Operator — вошедший сотрудник. Role: admin (управляет сотрудниками) | manager.
type Operator struct {
	ID    int    `json:"id"`
	Login string `json:"login"`
	// Имя в чате с клиентом и в КП
	Name string `json:"name"`
	Role string `json:"role"`
}

type StaffUser struct {
	Operator
	Active      bool   `json:"active"`
	CreatedAt   string `json:"createdAt"`
	LastLoginAt string `json:"lastLoginAt,omitempty"`
}

// ── События реального времени (WebSocket) ─────────────────────────────

// Event — одно из: message, message_updated, ticket_updated, quote_updated, typing, reset.
type Event struct {
	Type     string   `json:"type"`
	Message  *Message `json:"message,omitempty"`
	Ticket   *Ticket  `json:"ticket,omitempty"`
	Quote    *Quote   `json:"quote,omitempty"`
	TicketID string   `json:"ticketId,omitempty"`
	Sender   string   `json:"sender,omitempty"`
	Active   *bool    `json:"active,omitempty"`
}

// EventTicketID — к какому обращению относится событие ("" — ко всем).
func (e Event) EventTicketID() string {
	switch {
	case e.Message != nil:
		return e.Message.TicketID
	case e.Ticket != nil:
		return e.Ticket.ID
	case e.Quote != nil:
		return e.Quote.TicketID
	default:
		return e.TicketID
	}
}
