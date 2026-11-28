package fulfillment

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/lists"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/shipstate"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/store"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng lists (kế hoạch M3, Task M3-20; VC16, VC18): đơn kẹt, "Cần xem", cập nhật lần cuối. Lịch fulfillment.stuck
// (3.600 s, theo tenant) đánh dấu / bỏ dấu stuck_since theo StuckHours của cài đặt, lệnh tạo đã kết thúc mà vận đơn còn
// creating, anomaly stock_short mở; từ 08:00 giờ Việt Nam ghi một lệnh cảnh báo mỗi ngày (chỉ số lượng). Danh sách và
// số đếm theo phạm vi kho của bên gọi; đóng việc Cần xem kèm cách xử lý (in_doubt_create phải qua ResolveCreate).

const (
	// StuckScheduleName là tên lịch đơn kẹt (và loại lệnh của nó).
	StuckScheduleName = "fulfillment.stuck"
	// StuckAlertKind là tên loại lệnh gửi cảnh báo đơn kẹt mỗi ngày (khóa stuck-shipments:<ngày>:<tenant>).
	StuckAlertKind = "fulfillment.stuck_alert"
	stuckEvery     = time.Hour
	stuckTimeout   = 30 * time.Second
	stuckAlertKey  = "stuck-shipments:"
	listDefault    = 50
	listMax        = 100
	resolutionMax  = 1000
	anomalyOpen    = "open"
)

// Lỗi của Cần xem.
var (
	// ErrAnomalyNotFound: không có việc Cần xem mang mã đó (hay của tenant khác, hay người gọi không thấy kho của nó).
	ErrAnomalyNotFound = errors.New("fulfillment: không có việc cần xem")
	// ErrAnomalyNotResolvable: việc đã đóng, hoặc loại không đóng ở đây (lists.Resolvable: in_doubt_create qua
	// ResolveCreate; manual_cancel, in_doubt_cancel qua SetManualStatus).
	ErrAnomalyNotResolvable = errors.New("fulfillment: việc cần xem không đóng được ở đây")
)

// listsHandlers: lệnh của lịch đơn kẹt khi có database. Lệnh cảnh báo (NewStuckAlertCommand) do cmd/crm nối cùng
// Alerter của các lệnh cảnh báo khác.
func (m *Module) listsHandlers() []commands.Handler {
	if m.d.DB == nil {
		return nil
	}
	return []commands.Handler{stuckCommand(*m)}
}

// rejected là kết quả "từ chối" (không chạy lại) với lý do reason.
func rejected(reason string) commands.Outcome {
	return commands.Outcome{Status: commands.StatusRejected, Reason: reason}
}

func (*Module) listsInbound() []webhooks.InboundHandler { return nil }
func (*Module) listsSources() []webhooks.Source         { return nil }
func (*Module) listsConsumers() []events.Consumer       { return nil }

// listsSchedules: lịch fulfillment.stuck (3.600 s, theo tenant) khi có database (cùng điều kiện với lệnh của nó).
func (m *Module) listsSchedules() []schedules.Schedule {
	if m.d.DB == nil {
		return nil
	}
	return []schedules.Schedule{{Name: StuckScheduleName, Every: stuckEvery, PerTenant: true, Kind: stuckKind()}}
}

func (*Module) listsCleanups() []schedules.CleanupFunc { return nil }

// stuckKind: việc của lịch là một lệnh (như fulfillment.poll): còn chờ quá một kỳ thì expired.
func stuckKind() queue.Kind { return commands.CommandKind(StuckScheduleName, stuckEvery, stuckTimeout) }

// stuckCommand là lệnh của lịch đơn kẹt (dựng từ Module): một transaction của chính nó (tính lại dấu từ dữ liệu nên
// chạy lại an toàn), không gọi ra ngoài.
type stuckCommand Module

func (stuckCommand) Kind() queue.Kind       { return stuckKind() }
func (stuckCommand) AutoRetryNotSent() bool { return true }

// Lookup: chỉ đổi database của CRM; chạy lại không gửi gì ra ngoài.
func (stuckCommand) Lookup(context.Context, queue.Job) (string, error) {
	return commands.StatusNotSent, nil
}

// Execute đánh dấu / bỏ dấu kẹt của tenant ở giờ của đồng hồ Service (SyncStuckShipments), và từ 08:00 giờ Việt Nam,
// khi còn vận đơn kẹt, ghi lệnh cảnh báo DedupeKey stuck-shipments:<ngày>:<tenant> (một lệnh mỗi ngày). Kết quả
// {"marked", "cleared"}. Việc không có tenant → rejected bad_payload (payload {slot} của Ticker không dùng: giờ lấy từ
// đồng hồ). Lỗi database → not_sent (kỳ còn hạn thì chạy lại).
func (c stuckCommand) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	if j.TenantID == nil {
		return rejected(reasonBadPayload), nil
	}
	now := c.d.Now().UTC()
	var marked, cleared int
	err := tenancy.WithTenant(ctx, c.d.DB, *j.TenantID, func(tx pgx.Tx) error {
		marked, cleared = 0, 0 // RetryTx có thể chạy lại
		set, _, err := LoadSettings(ctx, tx)
		if err != nil {
			return err
		}
		if marked, cleared, err = syncStuck(ctx, tx, set.StuckHours, now); err != nil {
			return err
		}
		return c.Service.enqueueStuckAlert(ctx, tx, *j.TenantID, now)
	}, db.TxWorker)
	if err != nil {
		return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonReadFailed}, nil //nolint:nilerr // chạy lại trong kỳ
	}
	b, err := json.Marshal(map[string]int{"marked": marked, "cleared": cleared})
	if err != nil {
		return commands.Outcome{}, fmt.Errorf("mã hóa kết quả đơn kẹt: %w", err)
	}
	return commands.Outcome{Status: commands.StatusDone, Result: b}, nil
}

// syncStuck tính lại dấu kẹt của mọi vận đơn của tenant tại now; trả số dòng vừa đánh dấu, vừa bỏ dấu. Lệnh tạo của
// vận đơn creating (pending/unknown theo dữ liệu của fulfillment) đọc qua commands.Status: expired hay dead (bỏ tay,
// không qua OnTerminal) → kẹt.
func syncStuck(ctx context.Context, tx pgx.Tx, hours map[string]int, now time.Time) (marked, cleared int, err error) {
	q := store.New(tx)
	creating, err := q.StuckCreatingJobs(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("đọc vận đơn đang gửi hãng: %w", err)
	}
	ended := []uuid.UUID{}
	for _, r := range creating {
		st, err := commands.Status(ctx, tx, r.CreateJobID)
		switch {
		case errors.Is(err, commands.ErrCommandNotFound):
			continue // lệnh đã bị dọn: không đủ căn cứ, để người xem qua Cần xem
		case err != nil:
			return 0, 0, err //nolint:wrapcheck // commands đã bọc
		case st == queue.StatusExpired || st == queue.StatusDead:
			ended = append(ended, r.ID)
		}
	}
	h, err := json.Marshal(hours)
	if err != nil {
		return 0, 0, fmt.Errorf("mã hóa ngưỡng kẹt: %w", err)
	}
	rows, err := q.SyncStuckShipments(ctx, store.SyncStuckShipmentsParams{Now: now, Hours: h, EndedCreating: ended})
	if err != nil {
		return 0, 0, fmt.Errorf("đánh dấu vận đơn kẹt: %w", err)
	}
	for _, stuck := range rows {
		if stuck {
			marked++
		} else {
			cleared++
		}
	}
	return marked, cleared, nil
}

type stuckAlertPayload struct {
	Day string `json:"day"`
}

func stuckAlertKind() queue.Kind { return commands.CommandKind(StuckAlertKind, 0, authAlertTimeout) }

// enqueueStuckAlert ghi lệnh cảnh báo ngày khi now từ 08:00 giờ Việt Nam và tenant còn vận đơn kẹt; một lệnh mỗi
// (tenant, ngày): lượt sau trong ngày trùng khóa, không ghi thêm.
func (s *Service) enqueueStuckAlert(ctx context.Context, tx pgx.Tx, tenant uuid.UUID, now time.Time) error {
	day, ok := lists.AlertDay(now)
	if !ok {
		return nil
	}
	counts, err := stuckCounts(ctx, tx)
	if err != nil {
		return err
	}
	if lists.AlertText(counts) == "" {
		return nil
	}
	if _, _, err := s.queue().Enqueue(ctx, tx, stuckAlertKind(), queue.NewJob{
		TenantID: &tenant, Payload: stuckAlertPayload{Day: day}, DedupeKey: stuckAlertKey + day + ":" + tenant.String(),
	}); err != nil {
		return fmt.Errorf("ghi lệnh cảnh báo đơn kẹt: %w", err)
	}
	return nil
}

func stuckCounts(ctx context.Context, tx pgx.Tx) (map[string]int64, error) {
	rows, err := store.New(tx).CountStuckByStatus(ctx)
	if err != nil {
		return nil, fmt.Errorf("đếm vận đơn kẹt: %w", err)
	}
	out := make(map[string]int64, len(rows))
	for _, r := range rows {
		out[r.Status] = r.N
	}
	return out, nil
}

// NewStuckAlertCommand trả lệnh StuckAlertKind: đếm vận đơn kẹt theo trạng thái lúc gửi (transaction ngắn trên d), rồi
// Notify(key "stuck-shipments:<ngày>:<tenant>", normal, văn bản chỉ có số lượng; không còn vận đơn kẹt → không gửi).
// n nil → rejected alerter_missing; đọc lỗi hay Notify lỗi → not_sent (Notify lỗi log một lần ở đây; logger nil: không
// log). cmd/crm đăng ký cùng các lệnh cảnh báo khác (dùng chung Alerter).
func NewStuckAlertCommand(d *db.DB, n Notifier, logger *slog.Logger) commands.Handler { //nolint:ireturn // Runner nhận Handler
	return stuckAlert{d: d, n: n, log: logger}
}

type stuckAlert struct {
	d   *db.DB
	n   Notifier
	log *slog.Logger
}

func (stuckAlert) Kind() queue.Kind       { return stuckAlertKind() }
func (stuckAlert) AutoRetryNotSent() bool { return true }

func (a stuckAlert) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	var p stuckAlertPayload
	switch {
	case json.Unmarshal(j.Payload, &p) != nil || p.Day == "" || j.TenantID == nil:
		return rejected(reasonBadPayload), nil
	case a.n == nil:
		return rejected(reasonAlerter), nil
	}
	var counts map[string]int64
	if err := tenancy.WithTenant(ctx, a.d, *j.TenantID, func(tx pgx.Tx) (err error) {
		counts, err = stuckCounts(ctx, tx)
		return err
	}, db.TxWorker); err != nil {
		return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonReadFailed}, nil //nolint:nilerr // đọc lại sau
	}
	text := lists.AlertText(counts)
	if text == "" {
		return commands.Outcome{Status: commands.StatusDone}, nil
	}
	if err := a.n.Notify(ctx, stuckAlertKey+p.Day+":"+j.TenantID.String(), severityNormal, text); err != nil {
		if a.log != nil {
			a.log.WarnContext(ctx, "gửi cảnh báo đơn kẹt lỗi", slog.String("error", queue.ErrorText(err)))
		}
		return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonAlertFailed}, nil
	}
	return commands.Outcome{Status: commands.StatusDone}, nil
}

// StuckFilter là trang đơn kẹt cần đọc. Status rỗng, CarrierAccountID nil: không lọc. Phạm vi kho: AllWarehouses, hoặc
// đúng các kho WarehouseIDs (rỗng: không kho nào). Cursor: NextCursor của trang trước ("" trang đầu). Limit ≤ 0 → 50,
// tối đa 100.
type StuckFilter struct {
	// SellerID: chỉ vận đơn của người bán này (ADR-29; bắt buộc, uuid.Nil không khớp vận đơn nào).
	SellerID         uuid.UUID
	Status           string
	CarrierAccountID *uuid.UUID
	AllWarehouses    bool
	WarehouseIDs     []uuid.UUID
	Cursor           string
	Limit            int
}

// StuckShipment là một dòng "Đơn kẹt": chỉ mã, trạng thái, mốc giờ (không địa chỉ, không tên khách).
// HoursSinceUpdate: số giờ từ sự kiện cuối của hãng (không có: mốc đổi trạng thái) tới bây giờ.
type StuckShipment struct {
	ShipmentID, OrderID                        uuid.UUID
	OrderCode, CarrierKey, AccountName, Status string
	TrackingCode, Label, LastSyncError         string
	StuckSince                                 time.Time
	LastCarrierEventAt, LastSyncedAt           *time.Time
	HoursSinceUpdate                           int32
}

// ListStuck trả một trang đơn kẹt (vận đơn mở có stuck_since), kẹt lâu nhất trước, và con trỏ trang sau ("" khi hết);
// tx của bên gọi (tenancy.WithTenant). Con trỏ hỏng → ErrInvalidPage.
func (s *Service) ListStuck(ctx context.Context, tx pgx.Tx, f StuckFilter) ([]StuckShipment, string, error) {
	after, err := parseListCursor(f.Cursor)
	if err != nil {
		return nil, "", err
	}
	limit := pageLimit(f.Limit)
	p := store.ListStuckShipmentsParams{
		SellerID: f.SellerID, AccountID: db.NullUUID(f.CarrierAccountID), AllWarehouses: f.AllWarehouses,
		WarehouseIds: f.WarehouseIDs, Lim: int32(limit + 1), //nolint:gosec // limit ≤ listMax
	}
	if f.Status != "" {
		p.Status = pgtype.Text{String: f.Status, Valid: true}
	}
	p.AfterAt, p.AfterID = cursorAfter(after)
	rows, err := store.New(tx).ListStuckShipments(ctx, p)
	if err != nil {
		return nil, "", fmt.Errorf("đọc đơn kẹt: %w", err)
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[limit-1]
		next = lists.Cursor{At: last.StuckSince.UTC(), ID: last.ID}.String()
	}
	now := s.d.Now().UTC()
	out := make([]StuckShipment, 0, len(rows))
	for _, r := range rows {
		updated := r.LastStatusAt
		if r.LastCarrierEventAt.Valid {
			updated = r.LastCarrierEventAt.Time
		}
		out = append(out, StuckShipment{
			ShipmentID: r.ID, OrderID: r.OrderID, OrderCode: r.OrderCode, CarrierKey: r.CarrierKey,
			AccountName: r.AccountName, Status: r.Status, TrackingCode: r.TrackingCode.String,
			Label:         shipstate.Display(shipstate.Status(r.Status), r.SubStatus, ""),
			LastSyncError: r.LastSyncError.String, StuckSince: r.StuckSince.UTC(),
			LastCarrierEventAt: tsPtr(r.LastCarrierEventAt), LastSyncedAt: tsPtr(r.LastSyncedAt),
			HoursSinceUpdate: lists.HoursSince(now, updated),
		})
	}
	return out, next, nil
}

// ReviewFilter là trang Cần xem cần đọc (phạm vi kho, con trỏ, cỡ trang như StuckFilter).
type ReviewFilter struct {
	// SellerID: chỉ việc của vận đơn của người bán này, cả số đếm (ADR-29; bắt buộc).
	SellerID      uuid.UUID
	AllWarehouses bool
	WarehouseIDs  []uuid.UUID
	Cursor        string
	Limit         int
}

// ReviewItem là một việc Cần xem (anomaly mở): Label là câu tiếng Việt kèm hướng xử lý; Detail chỉ mã, trạng thái, số
// tiền (giá trị không phải chuỗi giữ dạng JSON).
type ReviewItem struct {
	AnomalyID, ShipmentID, OrderID uuid.UUID
	OrderCode, Kind, Label         string
	Detail                         map[string]string
	CreatedAt                      time.Time
}

// ReviewCounts là số đếm của các danh sách vận hành trong phạm vi kho (tệp task 111, Quyết định 4).
type ReviewCounts struct {
	Review, Stuck, CallbacksOpen, CallbacksOverdue, InDoubt int
}

// ReviewPage là một trang Cần xem kèm số đếm; Next: con trỏ trang sau ("" khi hết).
type ReviewPage struct {
	Items  []ReviewItem
	Counts ReviewCounts
	Next   string
}

// ListForReview trả một trang việc Cần xem, cũ nhất trước, kèm số đếm (cùng phạm vi kho); tx của bên gọi. Con trỏ hỏng
// → ErrInvalidPage.
func (s *Service) ListForReview(ctx context.Context, tx pgx.Tx, f ReviewFilter) (ReviewPage, error) {
	after, err := parseListCursor(f.Cursor)
	if err != nil {
		return ReviewPage{}, err
	}
	limit := pageLimit(f.Limit)
	ids := f.WarehouseIDs // nil: "= ANY (NULL)" không khớp kho nào
	p := store.ListOpenAnomaliesParams{
		SellerID: f.SellerID, AllWarehouses: f.AllWarehouses, WarehouseIds: ids,
		Lim: int32(limit + 1), //nolint:gosec // limit ≤ listMax
	}
	p.AfterAt, p.AfterID = cursorAfter(after)
	q := store.New(tx)
	rows, err := q.ListOpenAnomalies(ctx, p)
	if err != nil {
		return ReviewPage{}, fmt.Errorf("đọc việc cần xem: %w", err)
	}
	c, err := q.ReviewCounts(ctx, store.ReviewCountsParams{
		SellerID: f.SellerID, Now: s.d.Now().UTC(), AllWarehouses: f.AllWarehouses, WarehouseIds: ids, InDoubtKinds: lists.InDoubtKinds(),
	})
	if err != nil {
		return ReviewPage{}, fmt.Errorf("đếm việc vận hành: %w", err)
	}
	page := ReviewPage{Counts: ReviewCounts{
		Review: int(c.Review), Stuck: int(c.Stuck), CallbacksOpen: int(c.CallbacksOpen),
		CallbacksOverdue: int(c.CallbacksOverdue), InDoubt: int(c.InDoubt),
	}}
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[limit-1]
		page.Next = lists.Cursor{At: last.CreatedAt.UTC(), ID: last.ID}.String()
	}
	page.Items = make([]ReviewItem, 0, len(rows))
	for _, r := range rows {
		page.Items = append(page.Items, ReviewItem{
			AnomalyID: r.ID, ShipmentID: r.ShipmentID, OrderID: r.OrderID, OrderCode: r.OrderCode, Kind: r.Kind,
			Label: lists.Label(r.Kind), Detail: detailStrings(r.Detail), CreatedAt: r.CreatedAt.UTC(),
		})
	}
	return page, nil
}

// AnomalyResolveInput là yêu cầu đóng một việc Cần xem: Resolution (bắt buộc, ≤ 1.000 ký tự sau khi bỏ khoảng trắng hai đầu);
// Authorize nhận kho của tài khoản hãng của vận đơn (nil: từ chối).
type AnomalyResolveInput struct {
	Resolution string
	Authorize  Authorize
}

// Anomaly là một việc Cần xem sau khi đóng.
type Anomaly struct {
	ID, ShipmentID           uuid.UUID
	Kind, Status, Resolution string
	ResolvedAt               *time.Time
	CreatedAt                time.Time
}

// ResolveAnomaly đóng việc Cần xem id kèm cách xử lý (người đóng a.UserID) trong tx của bên gọi, rồi tính lại
// needs_review của vận đơn (false khi không còn việc mở). Khóa vận đơn trước anomaly (ADR-46 nhóm 8). Resolution rỗng
// hay dài quá → ErrShipInvalid; không có (hay tenant khác) → ErrAnomalyNotFound; Authorize từ chối → lỗi của nó; đã
// đóng, hay loại có đường riêng (in_doubt_create: ResolveCreate; manual_cancel, in_doubt_cancel: SetManualStatus) →
// ErrAnomalyNotResolvable.
func (s *Service) ResolveAnomaly(ctx context.Context, tx pgx.Tx, id uuid.UUID, in AnomalyResolveInput, a Actor) (Anomaly, error) {
	res := strings.TrimSpace(in.Resolution)
	if res == "" || utf8.RuneCountInString(res) > resolutionMax {
		return Anomaly{}, fmt.Errorf("cách xử lý việc cần xem: %w", ErrShipInvalid)
	}
	q := store.New(tx)
	ref, err := q.AnomalyForResolve(ctx, id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Anomaly{}, ErrAnomalyNotFound
	case err != nil:
		return Anomaly{}, fmt.Errorf("đọc việc cần xem: %w", err)
	}
	if in.Authorize == nil {
		return Anomaly{}, ErrAnomalyNotFound
	}
	if err := in.Authorize(ref.WarehouseID); err != nil {
		return Anomaly{}, err
	}
	if err := q.LockShipmentForAnomaly(ctx, ref.ShipmentID); err != nil {
		return Anomaly{}, fmt.Errorf("khóa vận đơn của việc cần xem: %w", err)
	}
	st, err := q.AnomalyKindState(ctx, id)
	if err != nil {
		return Anomaly{}, fmt.Errorf("đọc lại việc cần xem: %w", err)
	}
	if st.Status != anomalyOpen || !lists.Resolvable(st.Kind) {
		return Anomaly{}, ErrAnomalyNotResolvable
	}
	r, err := q.ResolveAnomaly(ctx, store.ResolveAnomalyParams{Resolution: res, ResolvedBy: db.NullUUID(&a.UserID), ID: id})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Anomaly{}, ErrAnomalyNotResolvable
	case err != nil:
		return Anomaly{}, fmt.Errorf("đóng việc cần xem: %w", err)
	}
	at := r.ResolvedAt.UTC()
	return Anomaly{
		ID: r.ID, ShipmentID: r.ShipmentID, Kind: r.Kind, Status: r.Status, Resolution: r.Resolution,
		ResolvedAt: &at, CreatedAt: r.CreatedAt.UTC(),
	}, nil
}

// cursorAfter đổi con trỏ trang trước thành tham số after_at, after_id của câu danh sách (nil: trang đầu, cả hai NULL).
func cursorAfter(c *lists.Cursor) (at pgtype.Timestamptz, id pgtype.UUID) {
	if c != nil {
		at, id = pgtype.Timestamptz{Time: c.At, Valid: true}, pgtype.UUID{Bytes: c.ID, Valid: true}
	}
	return at, id
}

// parseListCursor: "" → nil; hỏng → ErrInvalidPage.
func parseListCursor(s string) (*lists.Cursor, error) {
	if s == "" {
		return nil, nil //nolint:nilnil // nil: trang đầu
	}
	c, err := lists.ParseCursor(s)
	if err != nil {
		return nil, fmt.Errorf("con trỏ danh sách vận hành: %w", ErrInvalidPage)
	}
	return &c, nil
}

// pageLimit: n ≤ 0 → listDefault, tối đa listMax.
func pageLimit(n int) int { return min(cmp.Or(max(n, 0), listDefault), listMax) }

// detailStrings đổi detail (đối tượng JSON) thành map chuỗi: chuỗi giữ nguyên, giá trị khác giữ dạng JSON.
func detailStrings(raw []byte) map[string]string {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		var s string
		if json.Unmarshal(v, &s) == nil {
			out[k] = s
		} else {
			out[k] = string(v)
		}
	}
	return out
}
