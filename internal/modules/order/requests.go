package order

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/order/internal/store"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

// Yêu cầu về đơn hàng (kế hoạch M5, Task 195 / M5-12, ADR-28; luồng của admin chuyển sang CRM theo "Bổ sung Task
// 450"). Khách gửi từ trang đơn trên website (yêu cầu hủy, đổi, trả, câu hỏi; ảnh kèm đi qua kho tệp M1 chế độ
// private); nhân viên trả lời (hiện cho khách hay ghi chú nội bộ) và đổi trạng thái. Việc thật (hủy đơn, lên đơn đổi,
// hoàn tiền) nhân viên làm ở màn đơn; yêu cầu chỉ ghi quyết định và trao đổi.

// Loại yêu cầu.
const (
	RequestCancel   = "cancel"
	RequestExchange = "exchange"
	RequestReturn   = "return"
	RequestQuestion = "question"
)

// Trạng thái yêu cầu (sơ đồ ở requestTransitions).
const (
	RequestReceived      = "received"
	RequestInReview      = "in_review"
	RequestNeedsInfo     = "needs_info"
	RequestConfirmed     = "confirmed"
	RequestItemsReceived = "items_received"
	RequestResolved      = "resolved"
	RequestRejected      = "rejected"
	RequestCancelled     = "cancelled"
)

// Nguồn và tác giả tin.
const (
	RequestSourceWeb = "web"
	AuthorCustomer   = "customer"
	AuthorStaff      = "staff"
)

// Giới hạn của yêu cầu (khớp website hiện tại và CHECK của bảng).
const (
	MaxRequestFiles       = 5
	MaxRequestLines       = 30
	MaxRequestLineQty     = 20
	MinMessageRunes       = 5
	MaxMessageRunes       = 1000
	MaxLineNoteRunes      = 300
	MaxRejectReasonRunes  = 500
	MaxTrackingCodeRunes  = 60
	MinShipAddressRunes   = 10
	MaxShipAddressRunes   = 300
	MinPackageCodeRunes   = 3
	MaxPackageCodeRunes   = 40
	MaxRequestsPerOrder24 = 5
	MaxRequestListLimit   = 100
)

// Báo hiệu màn hình của yêu cầu (chỉ mã yêu cầu; người có order.requests.read nhận).
const (
	TopicOrderRequestSignal = "order_request.changed"
	permRequestsRead        = "order.requests.read"
	entityOrderRequest      = "order_request"
)

// requestTransitions là sơ đồ trạng thái của admin (Task 450). cancelled chỉ do khách rút khi còn received.
var requestTransitions = map[string][]string{ //nolint:gochecknoglobals // bảng hằng
	RequestReceived:      {RequestInReview, RequestNeedsInfo, RequestConfirmed, RequestResolved, RequestRejected},
	RequestInReview:      {RequestNeedsInfo, RequestConfirmed, RequestResolved, RequestRejected},
	RequestNeedsInfo:     {RequestInReview, RequestRejected},
	RequestConfirmed:     {RequestItemsReceived, RequestRejected},
	RequestItemsReceived: {RequestResolved, RequestRejected},
}

// CanTransitionRequest báo cạnh from → to có trong sơ đồ của nhân viên (không xét loại yêu cầu).
func CanTransitionRequest(from, to string) bool { return slices.Contains(requestTransitions[from], to) }

// requestClosed: trạng thái cuối.
func requestClosed(status string) bool {
	return status == RequestResolved || status == RequestRejected || status == RequestCancelled
}

// Lý do của khách: cấp yêu cầu theo loại, và cấp dòng đổi (website hiện tại, spec 22/Q06).
var (
	cancelReasons   = []string{"CHANGED_MIND", "DUPLICATE", "ADDRESS_CHANGE", "OTHER"}                                       //nolint:gochecknoglobals // hằng
	exchangeReasons = []string{"SIZE", "QUALITY", "WRONG_ITEM", "OTHER"}                                                     //nolint:gochecknoglobals // hằng
	lineReasons     = []string{"TOO_SMALL", "TOO_LARGE", "FIT", "COLOR", "NOT_AS_PICTURED", "DEFECT", "WRONG_ITEM", "OTHER"} //nolint:gochecknoglobals,lll // hằng
	photoReasons    = []string{"DEFECT", "WRONG_ITEM"}                                                                       //nolint:gochecknoglobals // hằng
)

// RequestLine là một dòng của yêu cầu đổi, trả: biến thể đã mua, số lượng, lý do của dòng, biến thể muốn đổi sang (tùy
// chọn), ghi chú của khách (≤ 300 ký tự).
type RequestLine struct {
	VariantID     uuid.UUID  `json:"variantId"`
	Qty           int        `json:"qty"`
	Reason        string     `json:"reason,omitempty"`
	WantVariantID *uuid.UUID `json:"wantVariantId,omitempty"`
	Note          string     `json:"note,omitempty"`
}

// CreateRequestInput là yêu cầu khách gửi từ website: loại, lý do (mã), lời nhắn 5–1000 ký tự, dòng (đổi, trả), ảnh
// (mã tệp mục đích order_request_photo, tối đa 5), khách đã xác nhận hàng còn tem nhãn (đổi, trả).
type CreateRequestInput struct {
	Kind, ReasonCode, Note string
	Lines                  []RequestLine
	FileIDs                []uuid.UUID
	ConditionConfirmed     bool
}

// Request là phần chung của một yêu cầu.
type Request struct {
	ID, OrderID       uuid.UUID
	Ref, Kind, Status string
	Version           int
}

// AuditFields là giá trị đưa vào audit: không lời nhắn, không nội dung tin (dữ liệu cá nhân).
func (r Request) AuditFields() map[string]any {
	return map[string]any{"ref": r.Ref, "kind": r.Kind, "status": r.Status, "version": r.Version}
}

// ShipBack là hướng dẫn gửi trả khi xác nhận đổi: địa chỉ của shop, mã gói, hạn gửi (23:59:59 giờ Việt Nam của ngày
// hạn, UTC khi lưu).
type ShipBack struct {
	Address     string    `json:"address"`
	PackageCode string    `json:"packageCode"`
	Deadline    time.Time `json:"deadline"`
}

// Outcome là kết quả khi đóng: mã vận đơn của đơn đổi (resolved, tùy chọn) hay lý do từ chối (rejected).
type Outcome struct {
	TrackingCode string `json:"trackingCode,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

// RequestLineDetail là dòng kèm mã, tên chụp lúc đặt của biến thể trong đơn.
type RequestLineDetail struct {
	RequestLine
	SKU, Name string
}

// RequestMessage là một tin của yêu cầu. AuthorID: mã nhân viên (staff) hay mã khóa dịch vụ (customer, qua website).
type RequestMessage struct {
	ID                uuid.UUID
	AuthorType        string
	AuthorID          string
	Body              string
	VisibleToCustomer bool
	CreatedAt         time.Time
}

// RequestDetail là một yêu cầu đủ trường: đơn, lời nhắn của khách, dòng, hướng dẫn gửi trả, kết quả, tin (theo thời
// gian), mã tệp ảnh (theo thứ tự gửi).
type RequestDetail struct {
	Request
	OrderCode, ExternalRef, ReasonCode, Source, CustomerNote string
	ConditionConfirmed                                       bool
	Lines                                                    []RequestLineDetail
	ShipBack                                                 *ShipBack
	Outcome                                                  *Outcome
	CreatedAt, UpdatedAt                                     time.Time
	ResolvedAt                                               *time.Time
	ResolvedBy                                               *uuid.UUID
	Messages                                                 []RequestMessage
	FileIDs                                                  []uuid.UUID
}

// RequestSummary là một dòng của danh sách cho nhân viên.
type RequestSummary struct {
	Request
	OrderCode, ReasonCode string
	CreatedAt, UpdatedAt  time.Time
}

// RequestChange là yêu cầu trước và sau một lần đổi (bên gọi ghi audit; order không nhập identity).
type RequestChange struct {
	Before, After Request
}

// refAlphabet: 32 ký tự không lẫn (bỏ 0, 1, I, O); mã YC- + 8 ký tự (40 bit ngẫu nhiên).
const refAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"

// newRequestRef sinh mã yêu cầu khách thấy trên web (ngẫu nhiên, không đoán được số yêu cầu của shop).
func newRequestRef() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b) // crypto/rand.Read không trả lỗi (Go 1.24+)
	for i := range b {
		b[i] = refAlphabet[int(b[i])%len(refAlphabet)]
	}
	return "YC-" + string(b)
}

// runesBetween: s là UTF-8 hợp lệ, số ký tự trong [lo, hi].
func runesBetween(s string, lo, hi int) bool {
	n := utf8.RuneCountInString(s)
	return utf8.ValidString(s) && n >= lo && n <= hi
}

// publishRequest báo hiệu màn hình nhân viên (order.requests.read) có yêu cầu id mới hay vừa đổi.
func (w Writer) publishRequest(ctx context.Context, tx pgx.Tx, tenant, id uuid.UUID) error {
	if w.Realtime == nil {
		return nil
	}
	if err := w.Realtime.Publish(ctx, tx, realtime.Signal{
		TenantID: tenant, Topic: TopicOrderRequestSignal, Requires: permRequestsRead,
		Entity: realtime.Entity{Type: entityOrderRequest, ID: id.String()},
	}); err != nil {
		return fmt.Errorf("báo hiệu yêu cầu về đơn: %w", err)
	}
	return nil
}

// GetRequest đọc yêu cầu id trên đơn của người bán sellerID (ADR-29; yêu cầu chỉ có trên đơn web của người bán mặc
// định) của tenant đang chạy, không khóa, kèm tin, dòng, tệp. Không có (kể cả tenant, người bán khác) →
// ErrRequestNotFound.
func GetRequest(ctx context.Context, tx pgx.Tx, sellerID, id uuid.UUID) (RequestDetail, error) {
	q := store.New(tx)
	r, err := q.GetOrderRequest(ctx, store.GetOrderRequestParams{ID: id, SellerID: sellerID})
	if errors.Is(err, pgx.ErrNoRows) {
		return RequestDetail{}, ErrRequestNotFound
	}
	if err != nil {
		return RequestDetail{}, fmt.Errorf("đọc yêu cầu về đơn: %w", err)
	}
	d := RequestDetail{
		Request: Request{
			ID: r.ID, OrderID: r.OrderID, Ref: r.Ref, Kind: r.Kind, Status: r.Status, Version: int(r.Version),
		},
		OrderCode: r.OrderCode, ExternalRef: r.OrderExternalRef.String, ReasonCode: r.ReasonCode, Source: r.Source,
		CustomerNote: r.CustomerNote.String, ConditionConfirmed: r.ConditionConfirmed, CreatedAt: r.CreatedAt.UTC(),
		UpdatedAt: r.UpdatedAt.UTC(),
	}
	if r.ResolvedAt.Valid {
		t := r.ResolvedAt.Time.UTC()
		d.ResolvedAt = &t
	}
	if r.ResolvedBy.Valid {
		u := uuid.UUID(r.ResolvedBy.Bytes)
		d.ResolvedBy = &u
	}
	if err := decodeRequestJSON(r.ShipBack, &d.ShipBack); err != nil {
		return RequestDetail{}, err
	}
	if err := decodeRequestJSON(r.Outcome, &d.Outcome); err != nil {
		return RequestDetail{}, err
	}
	if d.Lines, err = requestLineDetails(ctx, q, sellerID, r.OrderID, r.Lines); err != nil {
		return RequestDetail{}, err
	}
	if d.Messages, err = requestMessages(ctx, q, r.ID); err != nil {
		return RequestDetail{}, err
	}
	if d.FileIDs, err = q.ListOrderRequestFiles(ctx, r.ID); err != nil {
		return RequestDetail{}, fmt.Errorf("đọc ảnh của yêu cầu: %w", err)
	}
	return d, nil
}

// decodeRequestJSON đọc cột jsonb (NULL → giữ nil) vào *dst.
func decodeRequestJSON[T any](raw []byte, dst **T) error {
	if len(raw) == 0 {
		return nil
	}
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		return fmt.Errorf("đọc cột json của yêu cầu: %w", err)
	}
	*dst = &v
	return nil
}

func requestLineDetails(ctx context.Context, q *store.Queries, sellerID, orderID uuid.UUID, raw []byte,
) ([]RequestLineDetail, error) {
	var lines []RequestLine
	if err := json.Unmarshal(raw, &lines); err != nil {
		return nil, fmt.Errorf("đọc dòng của yêu cầu: %w", err)
	}
	items, err := q.OrderItemQuantities(ctx, store.OrderItemQuantitiesParams{SellerID: sellerID, OrderID: orderID})
	if err != nil {
		return nil, fmt.Errorf("đọc dòng đơn của yêu cầu: %w", err)
	}
	out := make([]RequestLineDetail, 0, len(lines))
	for _, l := range lines {
		d := RequestLineDetail{RequestLine: l}
		for _, it := range items {
			if it.VariantID == l.VariantID {
				d.SKU, d.Name = it.Sku, it.Name
			}
		}
		out = append(out, d)
	}
	return out, nil
}

func requestMessages(ctx context.Context, q *store.Queries, id uuid.UUID) ([]RequestMessage, error) {
	rows, err := q.ListOrderRequestMessages(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("đọc tin của yêu cầu: %w", err)
	}
	out := make([]RequestMessage, 0, len(rows))
	for _, m := range rows {
		out = append(out, RequestMessage{
			ID: m.ID, AuthorType: m.AuthorType, AuthorID: m.AuthorID.String, Body: m.Body,
			VisibleToCustomer: m.VisibleToCustomer, CreatedAt: m.CreatedAt.UTC(),
		})
	}
	return out, nil
}

// insertMessage chèn tin (chỉ ghi thêm) của yêu cầu id.
func insertMessage(ctx context.Context, q *store.Queries, tenant, id uuid.UUID, author, authorID, body string,
	visible bool, now time.Time,
) (RequestMessage, error) {
	r, err := q.InsertOrderRequestMessage(ctx, store.InsertOrderRequestMessageParams{
		TenantID: tenant, RequestID: id, AuthorType: author, AuthorID: pgtype.Text{String: authorID, Valid: authorID != ""},
		Body: body, VisibleToCustomer: visible, Now: now,
	})
	if err != nil {
		return RequestMessage{}, fmt.Errorf("ghi tin của yêu cầu: %w", err)
	}
	return RequestMessage{
		ID: r.ID, AuthorType: author, AuthorID: authorID, Body: body, VisibleToCustomer: visible, CreatedAt: r.CreatedAt.UTC(),
	}, nil
}

// cleanMessage cắt khoảng trắng hai đầu và kiểm 5–1000 ký tự; sai → ErrInvalidInput.
func cleanMessage(s string) (string, error) {
	s = strings.TrimSpace(s)
	if !runesBetween(s, MinMessageRunes, MaxMessageRunes) {
		return "", fmt.Errorf("%w: tin 5–1000 ký tự", ErrInvalidInput)
	}
	return s, nil
}
