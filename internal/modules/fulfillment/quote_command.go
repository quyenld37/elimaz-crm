package fulfillment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/store"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Lệnh báo phí command.shipping.quote (kế hoạch M3, Task M3-13): một lệnh mỗi (lần báo phí, tài khoản). Execute đọc
// dòng pending, tài khoản (đã giải mã), đơn, cài đặt trong một transaction ngắn, gọi QuoteNow ngoài transaction. Mọi
// câu trả lời chắc chắn của hãng (có phí hay mã lỗi) là done kèm kết quả; OnDone thay dòng pending (một dòng ok mỗi
// dịch vụ, hay error/unavailable). Báo phí chỉ đọc: Looker luôn not_sent, lỗi đọc database là not_sent (thử lại tới
// hạn 2 phút); dead hay expired → OnTerminal ghi error timeout. Chỉ dòng còn pending bị đổi (câu có điều kiện) nên
// chạy lại, ghi hai lần không hại. Sau mỗi lần ghi: báo hiệu shipping_quote.ready (fulfillment.quote ở kho của đơn).

const (
	// QuoteKindName là tên loại lệnh báo phí.
	QuoteKindName       = "command.shipping.quote"
	quoteExpires        = 2 * time.Minute
	quoteCommandTimeout = 12 * time.Second
	// quoteRateWaitMax, quoteRateAttempts: 429 có Retry-After ≤ 20 s ở lần thử thứ 1, 2 thì chờ rồi hỏi lại; quá thì
	// ghi rate_limited (người dùng bấm báo phí lại).
	quoteRateWaitMax  = 20 * time.Second
	quoteRateAttempts = 3

	quoteStatusOK          = "ok"
	quoteStatusError       = "error"
	quoteStatusUnavailable = "unavailable"

	signalQuoteReady = "shipping_quote.ready"
	permQuote        = "fulfillment.quote" // identity.Permission; fulfillment không nhập identity
)

// QuoteKind là Kind của lệnh báo phí: hết hạn sau 2 phút chờ, mỗi lần chạy tối đa 12 s (một lần gọi hãng ≤ 10 s).
func QuoteKind() queue.Kind {
	return commands.CommandKind(QuoteKindName, quoteExpires, quoteCommandTimeout)
}

// quotePayload là payload của lệnh báo phí: chỉ mã, cân nặng, giá trị khai, tùy chọn không ghi chú (không tên, SĐT,
// địa chỉ: Execute đọc lại địa chỉ từ đơn).
type quotePayload struct {
	RequestID     uuid.UUID    `json:"requestId"`
	OrderID       uuid.UUID    `json:"orderId"`
	AccountID     uuid.UUID    `json:"accountId"`
	SellerID      uuid.UUID    `json:"sellerId"`
	WarehouseID   uuid.UUID    `json:"warehouseId"`
	WeightGrams   int          `json:"weightGrams"`
	DeclaredValue int64        `json:"declaredValue"`
	ItemCount     int          `json:"itemCount"`
	Options       quoteOptions `json:"options"`
}

// quoteOptions là Options không có ghi chú (ghi chú không ảnh hưởng phí, có thể chứa dữ liệu cá nhân).
type quoteOptions struct {
	Inspection  string `json:"inspection,omitempty"`
	FeeOnRefuse int64  `json:"feeOnRefuse,omitempty"`
	Payer       string `json:"payer,omitempty"`
	Pickup      bool   `json:"pickup,omitempty"`
}

func newQuoteOptions(o Options) quoteOptions {
	return quoteOptions{Inspection: o.Inspection, FeeOnRefuse: o.FeeOnRefuse, Payer: o.Payer, Pickup: o.Pickup}
}

func (o quoteOptions) options() Options {
	return Options{Inspection: o.Inspection, FeeOnRefuse: o.FeeOnRefuse, Payer: o.Payer, Pickup: o.Pickup}
}

func (p quotePayload) parcel() Parcel {
	return Parcel{WeightGrams: p.WeightGrams, DeclaredValue: p.DeclaredValue, ItemCount: p.ItemCount}
}

// quoteOutcome là Outcome.Result của lệnh báo phí: chỉ phí, dịch vụ, băm, mã lỗi.
type quoteOutcome struct {
	Status    string          `json:"status"`
	ErrorCode string          `json:"errorCode,omitempty"`
	AuthCode  string          `json:"authCode,omitempty"`
	Services  []quotedService `json:"services,omitempty"`
}

type quotedService struct {
	Service      string           `json:"service"`
	ServiceName  string           `json:"serviceName"`
	Total        int64            `json:"total"`
	Breakdown    map[string]int64 `json:"breakdown,omitempty"`
	ExpectedDays int              `json:"expectedDays"`
	ValidUntil   time.Time        `json:"validUntil"`
	InputHash    []byte           `json:"inputHash"`
}

// QuoteCommand là lệnh command.shipping.quote (commands.Handler, Looker, DoneHook, queue.TerminalHook) của Service s.
type QuoteCommand struct {
	s           *Service
	callTimeout time.Duration
}

// NewQuoteCommand tạo lệnh báo phí dùng adapter, khóa, đồng hồ, báo hiệu của s; mỗi lần gọi hãng tối đa 10 s.
func NewQuoteCommand(s *Service) *QuoteCommand { return &QuoteCommand{s: s, callTimeout: maxQuoteCall} }

// Kind trả QuoteKind().
func (*QuoteCommand) Kind() queue.Kind { return QuoteKind() }

// AutoRetryNotSent trả true: báo phí chỉ đọc, hỏi lại an toàn.
func (*QuoteCommand) AutoRetryNotSent() bool { return true }

// Lookup luôn not_sent: báo phí không đổi gì ở hãng (lệnh in_doubt về hàng chờ, chạy lại).
func (*QuoteCommand) Lookup(context.Context, queue.Job) (string, error) {
	return commands.StatusNotSent, nil
}

// quoteTarget là thứ Execute đọc trước khi gọi hãng; gone: dòng pending không còn (đã có kết quả), unavailable: tài
// khoản không dùng được (không có, không active, nhập tay, của người bán khác, khóa không giải mã được).
type quoteTarget struct {
	acc          Account
	in           QuoteInput
	validMinutes int
	gone         bool
	unavailable  bool
}

// Execute báo phí cho tài khoản của lệnh j. 429 có Retry-After ≤ 20 s ở lần thử 1, 2 → not_sent chờ theo hãng; lỗi
// đọc database → not_sent; còn lại → done kèm kết quả (ok hay mã lỗi).
func (c *QuoteCommand) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	p, ok := parseQuotePayload(j)
	if !ok {
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonBadPayload}, nil
	}
	t, err := c.target(ctx, *j.TenantID, p)
	switch {
	case err != nil:
		return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonReadFailed}, nil //nolint:nilerr // chưa gọi hãng: đọc lại sau
	case t.gone:
		return commands.Outcome{Status: commands.StatusDone}, nil
	case t.unavailable:
		return quoteOutcome{Status: quoteStatusUnavailable}.outcome(), nil
	}
	fq := c.s.QuoteNow(ctx, []Account{t.acc}, t.in, c.callTimeout)[0]
	t.acc.Creds = nil
	if fq.ErrorCode == QuoteErrRateLimited && fq.RetryAfter > 0 && fq.RetryAfter <= quoteRateWaitMax &&
		j.Attempts < quoteRateAttempts {
		return commands.Outcome{Status: commands.StatusNotSent, Reason: QuoteErrRateLimited, RetryAfter: fq.RetryAfter}, nil
	}
	if fq.ErrorCode != "" {
		return quoteOutcome{Status: quoteStatusError, ErrorCode: fq.ErrorCode, AuthCode: fq.AuthCode}.outcome(), nil
	}
	now := c.s.d.Now().UTC()
	limit := now.Add(time.Duration(t.validMinutes) * time.Minute)
	out := quoteOutcome{Status: quoteStatusOK, Services: make([]quotedService, 0, len(fq.Results))}
	for _, r := range fq.Results {
		until := limit
		if r.ValidUntil != nil && r.ValidUntil.Before(until) {
			until = r.ValidUntil.UTC()
		}
		out.Services = append(out.Services, quotedService{
			Service: r.Service, ServiceName: r.ServiceName, Total: r.Total, Breakdown: r.Breakdown,
			ExpectedDays: r.ExpectedDays, ValidUntil: until,
			InputHash: InputHash(t.in.To, t.in.Parcel, t.in.COD, r.Service, t.in.Options),
		})
	}
	return out.outcome(), nil
}

// outcome là lệnh done kèm o. o chỉ có chuỗi, số, map, thời gian nên Marshal không lỗi; nếu có thì not_sent (hỏi lại).
func (o quoteOutcome) outcome() commands.Outcome {
	b, err := json.Marshal(o)
	if err != nil {
		return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonBadPayload}
	}
	return commands.Outcome{Status: commands.StatusDone, Result: b}
}

// target đọc dòng pending, tài khoản (đã giải mã, kèm bảng mã địa giới khi adapter có Locations), đơn và cài đặt
// trong một transaction ngắn; lỗi chỉ là lỗi đọc.
func (c *QuoteCommand) target(ctx context.Context, tenant uuid.UUID, p quotePayload) (quoteTarget, error) {
	var t quoteTarget
	err := tenancy.WithTenant(ctx, c.s.d.DB, tenant, func(tx pgx.Tx) error {
		t = quoteTarget{} // RetryTx có thể chạy lại
		q := store.New(tx)
		_, err := q.PendingQuote(ctx, store.PendingQuoteParams{RequestID: p.RequestID, CarrierAccountID: p.AccountID})
		if errors.Is(err, pgx.ErrNoRows) {
			t.gone = true
			return nil
		}
		if err != nil {
			return fmt.Errorf("đọc báo phí đang chờ: %w", err)
		}
		row, err := getAccount(ctx, q, p.AccountID)
		switch {
		case errors.Is(err, ErrAccountNotFound):
			t.unavailable = true
			return nil
		case err != nil:
			return err
		case row.Status != AccountActive || row.Mode != ModeAPI || row.SellerID != p.SellerID:
			t.unavailable = true
			return nil
		}
		t.acc, err = c.s.account(ctx, q, row)
		switch {
		case errors.Is(err, ErrKeysMissing), errors.Is(err, ErrCredentialsUnreadable):
			t.unavailable = true
			return nil
		case err != nil:
			return err
		}
		if ad, ok := c.s.adapter(row.CarrierKey); ok && ad.Capabilities().Locations {
			if t.acc.Locations, err = c.s.LocationMap(ctx, tx, row.CarrierKey); err != nil {
				return err
			}
		}
		o, err := order.Get(ctx, tx, p.SellerID, p.OrderID)
		if err != nil {
			return fmt.Errorf("đọc đơn của báo phí: %w", err)
		}
		st, _, err := LoadSettings(ctx, tx)
		if err != nil {
			return err
		}
		t.validMinutes = st.QuoteValidMinutes
		t.in = QuoteInput{
			From: t.acc.Pickup, To: shipTo(o), Parcel: p.parcel(), COD: o.CODAmount, Options: p.Options.options(),
		}
		return nil
	}, db.TxWorker)
	if err != nil {
		return quoteTarget{}, fmt.Errorf("đọc dữ liệu báo phí: %w", err)
	}
	return t, nil
}

// shipTo là địa chỉ giao của đơn theo mã địa giới của CRM.
func shipTo(o order.Order) Address {
	a := o.ShipAddress
	return Address{Name: a.Name, Phone: a.Phone, Line: string(a.Line), ProvinceCode: a.ProvinceCode, WardCode: a.WardCode}
}

// parseQuotePayload đọc payload của lệnh j; false khi payload hỏng, thiếu mã hay lệnh không có tenant.
func parseQuotePayload(j queue.Job) (quotePayload, bool) {
	var p quotePayload
	ok := j.TenantID != nil && json.Unmarshal(j.Payload, &p) == nil &&
		p.RequestID != uuid.Nil && p.AccountID != uuid.Nil && p.OrderID != uuid.Nil
	return p, ok
}

// OnDone (trong transaction đổi lệnh sang done): hãng từ chối khóa → MarkAuthFailed trước (carrier_accounts trước
// shipping_quotes, ADR-46 nhóm 8); ok → xóa dòng pending, chèn một dòng ok mỗi dịch vụ; còn lại → dòng pending sang
// error/unavailable. Dòng pending không còn (đã ghi) → không đổi gì. Kết quả không đọc được → error rejected.
func (c *QuoteCommand) OnDone(ctx context.Context, j queue.Job, result json.RawMessage, tx pgx.Tx) error {
	if result == nil { // dòng pending đã có kết quả, hay done do tra lại: không có gì để ghi
		return nil
	}
	p, ok := parseQuotePayload(j)
	if !ok { // Execute đã từ chối payload hỏng
		return nil
	}
	var out quoteOutcome
	if err := json.Unmarshal(result, &out); err != nil {
		out = quoteOutcome{Status: quoteStatusError, ErrorCode: QuoteErrRejected}
	}
	if out.Status == quoteStatusError && out.ErrorCode == QuoteErrAuth {
		code := out.AuthCode
		if code == "" {
			code = QuoteErrAuth
		}
		if err := MarkAuthFailed(ctx, tx, p.AccountID, code); err != nil {
			return err
		}
	}
	var wrote bool
	var err error
	if out.Status == quoteStatusOK && len(out.Services) > 0 {
		wrote, err = writeQuotes(ctx, store.New(tx), p, out.Services)
	} else {
		if out.Status == quoteStatusOK { // không xảy ra: Execute chỉ trả ok khi có dịch vụ
			out = quoteOutcome{Status: quoteStatusError, ErrorCode: QuoteErrRejected}
		}
		wrote, err = failQuote(ctx, store.New(tx), p, out.Status, out.ErrorCode)
	}
	if err != nil || !wrote {
		return err
	}
	return c.s.publishQuote(ctx, tx, *j.TenantID, p)
}

// OnTerminal (queue.TerminalHook): lệnh dead hay expired khi dòng còn pending (lỗi đọc database tới hạn, payload
// hỏng không có dòng nào) → error timeout. in_doubt không đổi gì: Looker trả not_sent nên lệnh được chạy lại.
func (c *QuoteCommand) OnTerminal(ctx context.Context, j queue.Job, status string, tx pgx.Tx) error {
	if status != queue.StatusDead && status != queue.StatusExpired {
		return nil
	}
	p, ok := parseQuotePayload(j)
	if !ok { // payload hỏng: không có dòng để đổi, lệnh vẫn kết thúc
		return nil
	}
	wrote, err := failQuote(ctx, store.New(tx), p, quoteStatusError, QuoteErrTimeout)
	if err != nil || !wrote {
		return err
	}
	return c.s.publishQuote(ctx, tx, *j.TenantID, p)
}

// writeQuotes xóa dòng pending của (lần báo phí, tài khoản) rồi chèn một dòng ok mỗi dịch vụ; false khi không còn
// pending. Phí ngoài 1…2^53−1 bị bỏ lần nữa (CHECK của bảng); không còn dịch vụ nào → error rejected.
func writeQuotes(ctx context.Context, q *store.Queries, p quotePayload, svcs []quotedService) (bool, error) {
	ins := store.InsertOkQuotesParams{OrderID: p.OrderID, RequestID: p.RequestID, CarrierAccountID: p.AccountID}
	for _, s := range svcs {
		if s.Total <= 0 || s.Total > maxQuoteFee {
			continue
		}
		bd := s.Breakdown
		if bd == nil {
			bd = map[string]int64{}
		}
		b, err := json.Marshal(bd)
		if err != nil {
			return false, fmt.Errorf("mã hóa chi tiết phí: %w", err)
		}
		days := int32(-1) // -1 → NULL: hãng không báo số ngày
		if s.ExpectedDays >= 0 && s.ExpectedDays <= 1<<15 {
			days = int32(s.ExpectedDays)
		}
		ins.Services = append(ins.Services, s.Service)
		ins.ServiceNames = append(ins.ServiceNames, s.ServiceName)
		ins.FeeTotals = append(ins.FeeTotals, s.Total)
		ins.FeeBreakdowns = append(ins.FeeBreakdowns, b)
		ins.ExpectedDays = append(ins.ExpectedDays, days)
		ins.InputHashes = append(ins.InputHashes, s.InputHash)
		ins.ValidUntils = append(ins.ValidUntils, s.ValidUntil)
	}
	if len(ins.Services) == 0 {
		return failQuote(ctx, q, p, quoteStatusError, QuoteErrRejected)
	}
	row, err := q.DeletePendingQuote(ctx, store.DeletePendingQuoteParams{
		RequestID: p.RequestID, CarrierAccountID: p.AccountID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("xóa báo phí đang chờ: %w", err)
	}
	ins.CarrierKey = row.CarrierKey
	if err := q.InsertOkQuotes(ctx, ins); err != nil {
		return false, fmt.Errorf("ghi kết quả báo phí: %w", err)
	}
	return true, nil
}

// failQuote đổi dòng pending sang status (error kèm code, unavailable không mã); false khi không còn pending.
func failQuote(ctx context.Context, q *store.Queries, p quotePayload, status, code string) (bool, error) {
	ec := pgtype.Text{String: code, Valid: code != "" && status == quoteStatusError}
	_, err := q.FailPendingQuote(ctx, store.FailPendingQuoteParams{
		Status: status, ErrorCode: ec, RequestID: p.RequestID, CarrierAccountID: p.AccountID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("ghi lỗi báo phí: %w", err)
	}
	return true, nil
}

// publishQuote báo hiệu shipping_quote.ready (entity là đơn; fulfillment.quote ở kho của đơn); Bus nil → bỏ qua.
func (s *Service) publishQuote(ctx context.Context, tx pgx.Tx, tenant uuid.UUID, p quotePayload) error {
	if s.d.Bus == nil {
		return nil
	}
	if err := s.d.Bus.Publish(ctx, tx, realtime.Signal{
		TenantID: tenant, Topic: signalQuoteReady, Requires: permQuote,
		Scope: realtime.WarehouseScope(p.WarehouseID.String()), Entity: realtime.Entity{Type: "order", ID: p.OrderID.String()},
	}); err != nil {
		return fmt.Errorf("báo hiệu %s: %w", signalQuoteReady, err)
	}
	return nil
}
