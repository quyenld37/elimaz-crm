package fulfillment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/store"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Yêu cầu so phí trên đơn và đọc kết quả (kế hoạch M3, Task M3-13): RequestQuotes ghi mỗi tài khoản một dòng pending và
// một lệnh báo phí trong tx của bên gọi; ListQuotes đọc kết quả một lần so phí. Dọn báo phí quá 7 ngày: quotesCleanups.

const (
	quoteRetention    = 7 * 24 * time.Hour
	quoteCleanupBatch = 1000
)

// QuoteRequest là yêu cầu so phí: cân nặng (≤ 0: DefaultWeightGramsPerItem × số món), giá trị khai (< 0 → 0), tùy chọn
// (nil: tùy chọn mặc định của từng tài khoản; ghi chú không dùng), tài khoản (rỗng: mọi tài khoản active báo phí được
// của người bán của đơn ở kho của đơn).
type QuoteRequest struct {
	WeightGrams   int
	DeclaredValue int64
	Options       *Options
	AccountIDs    []uuid.UUID
}

// QuoteAccount là một tài khoản được báo phí.
type QuoteAccount struct {
	ID               uuid.UUID
	Name, CarrierKey string
}

// QuoteAccepted là lần so phí vừa ghi.
type QuoteAccepted struct {
	RequestID uuid.UUID
	Accounts  []QuoteAccount
}

// QuoteRow là một dòng kết quả báo phí. FeeTotal chỉ có (và > 0) khi Status = ok; ErrorCode chỉ có khi error.
type QuoteRow struct {
	QuoteID, CarrierAccountID       uuid.UUID
	AccountName, CarrierKey, Status string
	Service, ServiceName, ErrorCode string
	FeeTotal                        *int64
	ExpectedDays                    *int
	ValidUntil                      *time.Time
}

// QuoteParcel là kiện dùng để báo phí và tạo vận đơn của đơn o: cân nặng weightGrams (≤ 0: DefaultWeightGramsPerItem
// của st × tổng số món, ít nhất 1 g), giá trị khai declaredValue (< 0 → 0), số món. Task tạo vận đơn dùng cùng hàm để
// InputHash của báo phí khớp.
func QuoteParcel(o order.Order, weightGrams int, declaredValue int64, st Settings) Parcel {
	items := 0
	for _, it := range o.Items {
		items += int(it.Qty)
	}
	if weightGrams <= 0 {
		weightGrams = max(st.DefaultWeightGramsPerItem*max(items, 1), 1)
	}
	return Parcel{WeightGrams: weightGrams, DeclaredValue: max(declaredValue, 0), ItemCount: items}
}

// RequestQuotes ghi một lần so phí cho đơn o (bên gọi đã đọc o theo người bán và kiểm fulfillment.quote ở kho của
// đơn), trong tx của bên gọi: mỗi tài khoản một dòng shipping_quotes pending (input_hash = InputHash của đầu vào, dịch
// vụ rỗng) và một lệnh QuoteKind (DedupeKey "<requestId>:<accountId>"). requestId sinh mới mỗi lần chạy. Lỗi: đơn đã
// có vận đơn hiện hành → ErrShipmentExists; có AccountIDs mà tài khoản không có hay của người bán khác →
// ErrAccountNotFound; tài khoản không active, nhập tay, ở kho khác kho của đơn, hãng không báo phí →
// ErrAccountUnavailable. Không AccountIDs và không tài khoản nào dùng được → Accounts rỗng, không ghi gì.
func (s *Service) RequestQuotes(ctx context.Context, tx pgx.Tx, o order.Order, in QuoteRequest, a Actor) (QuoteAccepted, error) {
	if o.CurrentShipmentID != nil {
		return QuoteAccepted{}, ErrShipmentExists
	}
	q := store.New(tx)
	rows, err := s.quoteAccounts(ctx, q, o, in.AccountIDs)
	if err != nil {
		return QuoteAccepted{}, err
	}
	st, _, err := LoadSettings(ctx, tx)
	if err != nil {
		return QuoteAccepted{}, err
	}
	reqID, err := uuid.NewV7()
	if err != nil {
		return QuoteAccepted{}, fmt.Errorf("sinh mã lần báo phí: %w", err)
	}
	out := QuoteAccepted{RequestID: reqID, Accounts: make([]QuoteAccount, 0, len(rows))}
	parcel := QuoteParcel(o, in.WeightGrams, in.DeclaredValue, st)
	for _, r := range rows {
		p := quotePayload{
			RequestID: reqID, OrderID: o.ID, AccountID: r.ID, SellerID: o.SellerID, WarehouseID: o.WarehouseID,
			WeightGrams: parcel.WeightGrams, DeclaredValue: parcel.DeclaredValue, ItemCount: parcel.ItemCount,
			Options: requestOptions(in.Options, r.DefaultOptions),
		}
		if err := s.enqueueQuote(ctx, tx, q, a.TenantID, p, r.CarrierKey, InputHash(shipTo(o), parcel, o.CODAmount, "", p.Options.options())); err != nil {
			return QuoteAccepted{}, err
		}
		out.Accounts = append(out.Accounts, QuoteAccount{ID: r.ID, Name: r.Name, CarrierKey: r.CarrierKey})
	}
	return out, nil
}

// quoteAccounts trả tài khoản sẽ báo phí cho đơn o: ids rỗng → mọi tài khoản dùng được (canQuote) của người bán của
// đơn ở kho của đơn; có ids (bỏ trùng) → đúng các tài khoản đó, lạ hay của người bán khác → ErrAccountNotFound, không
// dùng được → ErrAccountUnavailable.
func (s *Service) quoteAccounts(ctx context.Context, q *store.Queries, o order.Order, want []uuid.UUID) ([]store.QuoteAccountsRow, error) {
	ids := make([]uuid.UUID, 0, len(want))
	for _, id := range want {
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	rows, err := q.QuoteAccounts(ctx, store.QuoteAccountsParams{SellerID: o.SellerID, Ids: ids, WarehouseID: o.WarehouseID})
	if err != nil {
		return nil, fmt.Errorf("đọc tài khoản báo phí: %w", err)
	}
	if len(ids) > 0 && len(rows) != len(ids) {
		return nil, ErrAccountNotFound
	}
	out := make([]store.QuoteAccountsRow, 0, len(rows))
	for _, r := range rows {
		switch {
		case s.canQuote(r, o.WarehouseID):
			out = append(out, r)
		case len(ids) > 0:
			return nil, fmt.Errorf("tài khoản hãng %s: %w", r.ID, ErrAccountUnavailable)
		}
	}
	return out, nil
}

// requestOptions: tùy chọn của yêu cầu, không có thì tùy chọn mặc định của tài khoản (cột default_options); bỏ ghi chú.
func requestOptions(in *Options, accountDefault []byte) quoteOptions {
	if in != nil {
		return newQuoteOptions(*in)
	}
	var d optionsDoc
	if json.Unmarshal(accountDefault, &d) != nil {
		return quoteOptions{}
	}
	return newQuoteOptions(d.options())
}

// enqueueQuote chèn dòng pending (băm hash) và lệnh báo phí của payload p trong tx.
func (s *Service) enqueueQuote(ctx context.Context, tx pgx.Tx, q *store.Queries, tenant uuid.UUID, p quotePayload,
	carrierKey string, hash []byte,
) error {
	if err := q.InsertPendingQuote(ctx, store.InsertPendingQuoteParams{
		OrderID: p.OrderID, RequestID: p.RequestID, CarrierAccountID: p.AccountID, CarrierKey: carrierKey, InputHash: hash,
	}); err != nil {
		return fmt.Errorf("ghi báo phí đang chờ: %w", err)
	}
	if _, _, err := s.queue().Enqueue(ctx, tx, QuoteKind(), queue.NewJob{
		TenantID: &tenant, Payload: p, DedupeKey: p.RequestID.String() + ":" + p.AccountID.String(),
	}); err != nil {
		return fmt.Errorf("ghi lệnh báo phí: %w", err)
	}
	return nil
}

// canQuote: tài khoản active, gọi API, ở kho wh, hãng có adapter báo phí.
func (s *Service) canQuote(r store.QuoteAccountsRow, wh uuid.UUID) bool {
	if r.Status != AccountActive || r.Mode != ModeAPI || r.WarehouseID != wh {
		return false
	}
	ad, ok := s.adapter(r.CarrierKey)
	return ok && ad.Capabilities().Quote
}

// ListQuotes trả kết quả lần so phí requestID của đơn orderID (nil: lần gần nhất; đơn chưa so phí → uuid.Nil, rỗng):
// ok theo phí tăng dần, rồi pending, rồi lỗi. Bên gọi đã đọc đơn và kiểm quyền ở kho của đơn.
func (*Service) ListQuotes(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, requestID *uuid.UUID) (uuid.UUID, []QuoteRow, error) {
	q := store.New(tx)
	var req uuid.UUID
	if requestID != nil {
		req = *requestID
	} else {
		r, err := q.LatestQuoteRequest(ctx, orderID)
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, []QuoteRow{}, nil
		}
		if err != nil {
			return uuid.Nil, nil, fmt.Errorf("đọc lần báo phí gần nhất: %w", err)
		}
		req = r
	}
	rows, err := q.ListQuotes(ctx, store.ListQuotesParams{OrderID: orderID, RequestID: req})
	if err != nil {
		return uuid.Nil, nil, fmt.Errorf("đọc kết quả báo phí: %w", err)
	}
	out := make([]QuoteRow, 0, len(rows))
	for _, r := range rows {
		v := QuoteRow{
			QuoteID: r.ID, CarrierAccountID: r.CarrierAccountID, AccountName: r.AccountName, CarrierKey: r.CarrierKey,
			Status: r.Status, Service: r.Service, ServiceName: r.ServiceName, ErrorCode: r.ErrorCode.String,
		}
		if r.Status == quoteStatusOK && r.FeeTotal.Valid && r.FeeTotal.Int64 > 0 {
			fee := r.FeeTotal.Int64
			v.FeeTotal = &fee
		}
		if r.ExpectedDays.Valid {
			d := int(r.ExpectedDays.Int32)
			v.ExpectedDays = &d
		}
		if r.ValidUntil.Valid {
			t := r.ValidUntil.Time.UTC()
			v.ValidUntil = &t
		}
		out = append(out, v)
	}
	return req, out, nil
}

// cleanupQuotes xóa báo phí tạo trước now − 7 ngày (lô 1000), trừ báo phí vận đơn đã dùng.
func cleanupQuotes(ctx context.Context, tx pgx.Tx, now time.Time) (int64, error) {
	n, err := store.New(tx).DeleteOldQuotes(ctx, store.DeleteOldQuotesParams{
		Before: now.Add(-quoteRetention), MaxRows: quoteCleanupBatch,
	})
	if err != nil {
		return 0, fmt.Errorf("dọn báo phí cũ: %w", err)
	}
	return n, nil
}
