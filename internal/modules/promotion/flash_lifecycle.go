package promotion

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/promotion/internal/store"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/schedules"
)

// Vòng đời đợt flash sale (kế hoạch M5, Task 200 / M5-17; ADR-40, QĐ31): lên lịch giữ suất bằng chứng từ giữ cho đợt
// của inventory (held + suất, trải đơn vị), thêm suất, đóng đợt trả đơn vị còn trống về tồn, nhịp mở và đóng theo giờ.
// Mọi hàm chạy trong tx tenant của bên gọi. Thứ tự khóa (ADR-46): flash_sales (đợt) → flash_sale_items (món, chỉ thêm
// suất) → [nhóm 11] stock_balances của các món theo (variant_id, warehouse_id) tăng dần → flash_sale_units.

// Sự kiện của đợt (aggregate flash_sale, mã đợt, Seq = version của đợt sau khi đổi). Consumer webcache đổi thành báo
// hiệu bộ đệm website và báo hiệu màn quản lý đợt.
const (
	TopicFlashSaleOpened  = "flash_sale.opened"
	TopicFlashSaleClosed  = "flash_sale.closed"
	TopicFlashSaleChanged = "flash_sale.changed"

	aggregateFlashSale = "flash_sale"
)

// Lý do đóng đợt (cột closed_reason).
const (
	FlashClosedManual = "manual"
	FlashClosedEnded  = "ended"
)

// FlashTickSchedule là lịch mở/đóng đợt theo giờ: mỗi 15 s, theo tenant.
const (
	FlashTickSchedule = "promotion.flash_tick"
	flashTickEvery    = 15 * time.Second

	stepFlashSchedule = "schedule"
)

var (
	// ErrFlashOverlap báo: một biến thể của đợt đã ở đợt khác đã lên lịch hay đang mở có thời gian chồng.
	ErrFlashOverlap = errors.New("promotion: biến thể đã ở đợt flash sale khác chồng giờ")
	// ErrFlashEmpty báo: đợt chưa có món nên không lên lịch được.
	ErrFlashEmpty = errors.New("promotion: đợt flash sale chưa có món")
	// ErrFlashNotActive báo: đợt không ở trạng thái đã lên lịch hay đang mở (thêm suất, đóng đợt).
	ErrFlashNotActive = errors.New("promotion: đợt flash sale không ở trạng thái đã lên lịch hay đang mở")
	// ErrFlashEnded báo: giờ kết thúc của đợt đã qua lúc lên lịch.
	ErrFlashEnded = errors.New("promotion: đợt flash sale đã quá giờ kết thúc")
	// ErrFlashInsufficient báo: không đủ hàng có thể bán cho suất của một hay nhiều món; lỗi cụ thể là
	// *FlashShortageError.
	ErrFlashInsufficient = errors.New("promotion: không đủ hàng cho suất flash sale")
)

// FlashShortItem là một món thiếu hàng: cần Need suất, có thể bán Sellable.
type FlashShortItem struct {
	VariantID      uuid.UUID
	SKU            string
	Need, Sellable int
}

// FlashShortageError liệt kê mọi món thiếu (theo thứ tự món của đợt); không ghi gì.
type FlashShortageError struct{ Items []FlashShortItem }

func (e *FlashShortageError) Error() string {
	return fmt.Sprintf("promotion: không đủ hàng cho %d món flash sale", len(e.Items))
}

// Is cho errors.Is(err, ErrFlashInsufficient).
func (e *FlashShortageError) Is(target error) bool { return target == ErrFlashInsufficient }

// FlashSaleEvent là payload của sự kiện đợt (chỉ mã, A16).
type FlashSaleEvent struct {
	FlashSaleID uuid.UUID `json:"flashSaleId"`
	Status      string    `json:"status"`
	Version     int       `json:"version"`
}

// FlashLive là số liệu đang chạy của một đợt, theo thứ tự món.
type FlashLive struct{ Items []FlashLiveItem }

// FlashLiveItem là số liệu của một món: số suất, đơn vị theo trạng thái, số khách đã lấy.
type FlashLiveItem struct {
	ItemID                                            uuid.UUID
	Quantity, Available, Held, Sold, Released, Buyers int
}

// ScheduleFlashSale lên lịch đợt nháp saleID khi version còn khớp: phải có món, giờ kết thúc sau now, không biến thể
// nào ở đợt khác đã lên lịch hay đang mở chồng giờ, đủ hàng cho mọi món; rồi giữ suất mọi món (held +, đơn vị trống),
// đợt → scheduled, phát flash_sale.changed. Lỗi: ErrNotFound, ErrNotEditable (không nháp), ErrVersionConflict,
// ErrFlashEnded, ErrFlashEmpty, ErrFlashOverlap, *FlashShortageError; mọi lỗi không ghi gì (bên gọi lùi tx).
func (w Writer) ScheduleFlashSale(ctx context.Context, tx pgx.Tx, saleID uuid.UUID, version int, a Actor,
	now time.Time,
) (FlashSaleChange, error) {
	q := store.New(tx)
	before, err := lockDraftSale(ctx, q, saleID)
	if err != nil {
		return FlashSaleChange{}, err
	}
	if before.Version != version {
		return FlashSaleChange{}, ErrVersionConflict
	}
	if !now.Before(before.EndsAt) {
		return FlashSaleChange{}, ErrFlashEnded
	}
	items, err := q.FlashItemsBySale(ctx, saleID)
	if err != nil {
		return FlashSaleChange{}, fmt.Errorf("đọc món flash sale: %w", err)
	}
	if len(items) == 0 {
		return FlashSaleChange{}, ErrFlashEmpty
	}
	refs := flashRefs(items, stepFlashSchedule)
	// Khóa số dư trước rồi mới kiểm chồng giờ: đợt khác cùng biến thể đang lên lịch thì chờ khóa ở đây, câu kiểm sau
	// thấy đợt đó đã commit (READ COMMITTED).
	short, err := inventory.LockFlashStock(ctx, tx, refs)
	if err != nil {
		return FlashSaleChange{}, fmt.Errorf("kiểm tồn cho đợt flash sale: %w", err)
	}
	overlaps, err := q.FlashOverlaps(ctx, store.FlashOverlapsParams{
		SaleID: saleID, StartsAt: before.StartsAt, EndsAt: before.EndsAt,
	})
	if err != nil {
		return FlashSaleChange{}, fmt.Errorf("kiểm đợt chồng giờ: %w", err)
	}
	if len(overlaps) > 0 {
		return FlashSaleChange{}, fmt.Errorf("%d biến thể: %w", len(overlaps), ErrFlashOverlap)
	}
	if len(short) > 0 {
		return FlashSaleChange{}, shortageError(items, short)
	}
	iw, ia := w.inventory(), inventoryActor(a)
	for _, r := range refs {
		if err := iw.ReserveForFlash(ctx, tx, r, ia); err != nil {
			return FlashSaleChange{}, fmt.Errorf("giữ suất món %s: %w", r.ItemID, err)
		}
	}
	after, err := w.setFlashStatus(ctx, tx, q, before, FlashScheduled, "", TopicFlashSaleChanged)
	if err != nil {
		return FlashSaleChange{}, err
	}
	return FlashSaleChange{Before: before, After: after}, nil
}

// AddSlots thêm add (1–20000) suất cho món itemID của đợt saleID đã lên lịch hay đang mở: quantity + add (tổng ≤
// 20000), giữ thêm hàng (held +, đơn vị trống), version của món và của đợt + 1, phát flash_sale.changed. Lỗi:
// ErrInvalidInput, ErrNotFound, ErrFlashNotActive, *FlashShortageError.
func (w Writer) AddSlots(ctx context.Context, tx pgx.Tx, saleID, itemID uuid.UUID, add int, a Actor) (FlashItemChange, error) {
	if add < 1 || add > flashMaxQuantity {
		return FlashItemChange{}, fmt.Errorf("số suất thêm: %w", ErrInvalidInput)
	}
	q := store.New(tx)
	sale, err := lockActiveSale(ctx, q, saleID)
	if err != nil {
		return FlashItemChange{}, err
	}
	row, err := q.FlashItemForUpdate(ctx, store.FlashItemForUpdateParams{ID: itemID, FlashSaleID: saleID})
	if errors.Is(err, pgx.ErrNoRows) {
		return FlashItemChange{}, ErrNotFound
	}
	if err != nil {
		return FlashItemChange{}, fmt.Errorf("khóa món flash sale: %w", err)
	}
	before := flashItemFrom(row)
	if before.Quantity+add > flashMaxQuantity {
		return FlashItemChange{}, fmt.Errorf("tổng số suất: %w", ErrInvalidInput)
	}
	ref := inventory.FlashItemRef{
		ItemID: itemID, VariantID: before.VariantID, WarehouseID: before.WarehouseID, Quantity: add,
		Step: fmt.Sprintf("add_%d", before.Version+1),
	}
	short, err := inventory.LockFlashStock(ctx, tx, []inventory.FlashItemRef{ref})
	if err != nil {
		return FlashItemChange{}, fmt.Errorf("kiểm tồn cho suất thêm: %w", err)
	}
	if len(short) > 0 {
		items, err := q.FlashItemsBySale(ctx, saleID)
		if err != nil {
			return FlashItemChange{}, fmt.Errorf("đọc món flash sale: %w", err)
		}
		return FlashItemChange{}, shortageError(items, short)
	}
	if err := w.inventory().AddFlashUnits(ctx, tx, ref, add, inventoryActor(a)); err != nil {
		return FlashItemChange{}, fmt.Errorf("giữ suất thêm: %w", err)
	}
	upd, err := q.AddFlashItemQuantity(ctx, store.AddFlashItemQuantityParams{
		Add: int32(add), ID: itemID,
	})
	if err != nil {
		return FlashItemChange{}, fmt.Errorf("tăng số suất: %w", err)
	}
	bumped, err := q.BumpFlashSaleVersion(ctx, saleID)
	if err != nil {
		return FlashItemChange{}, fmt.Errorf("tăng version đợt: %w", err)
	}
	if err := w.emitFlash(ctx, tx, bumped.TenantID, saleID, sale.Status, int(bumped.Version), TopicFlashSaleChanged); err != nil {
		return FlashItemChange{}, err
	}
	return FlashItemChange{Before: before, After: flashItemFrom(store.FlashItemForUpdateRow(upd))}, nil
}

// CloseFlashSale đóng đợt saleID đã lên lịch hay đang mở với lý do reason (FlashClosedManual | FlashClosedEnded): mọi
// đơn vị còn trống của từng món trả về tồn (held −), đợt → closed, phát flash_sale.closed. Đơn vị đang giữ cho đơn giữ
// nguyên. Lỗi: ErrInvalidInput (lý do lạ), ErrNotFound, ErrFlashNotActive.
func (w Writer) CloseFlashSale(ctx context.Context, tx pgx.Tx, saleID uuid.UUID, reason string, a Actor) (
	FlashSaleChange, error,
) {
	if reason != FlashClosedManual && reason != FlashClosedEnded {
		return FlashSaleChange{}, fmt.Errorf("lý do đóng đợt: %w", ErrInvalidInput)
	}
	q := store.New(tx)
	before, err := lockActiveSale(ctx, q, saleID)
	if err != nil {
		return FlashSaleChange{}, err
	}
	after, err := w.closeLocked(ctx, tx, q, before, reason, inventoryActor(a))
	if err != nil {
		return FlashSaleChange{}, err
	}
	return FlashSaleChange{Before: before, After: after}, nil
}

// Tick là nhịp của lịch FlashTickSchedule cho tenant của tx: khóa các đợt đến hạn theo id tăng dần; đợt đã lên lịch
// hay đang mở có ends_at ≤ now → đóng với lý do ended; đợt đã lên lịch có starts_at ≤ now → open, phát
// flash_sale.opened một lần. Chạy lại không làm gì thêm (đợt đã đổi không còn đến hạn). Nhiều đợt cần đóng: khóa số
// dư của mọi món của mọi đợt đó trong một câu theo (variant_id, warehouse_id) tăng dần trước khi đóng đợt nào (ADR-46
// nhóm 11 trên cả transaction; khóa theo từng đợt có thể deadlock với một đơn nhiều dòng).
func (w Writer) Tick(ctx context.Context, tx pgx.Tx, now time.Time) (opened, closed int, err error) {
	q := store.New(tx)
	due, err := q.DueFlashSales(ctx, now)
	if err != nil {
		return 0, 0, fmt.Errorf("khóa đợt flash sale đến hạn: %w", err)
	}
	var toOpen, toClose []FlashSale
	var refs []inventory.FlashItemRef
	for _, r := range due {
		s := flashSaleFrom(store.FlashSaleByIDRow(r))
		if now.Before(s.EndsAt) {
			toOpen = append(toOpen, s)
			continue
		}
		items, err := q.FlashItemsBySale(ctx, s.ID)
		if err != nil {
			return 0, 0, fmt.Errorf("đọc món flash sale: %w", err)
		}
		toClose, refs = append(toClose, s), append(refs, flashRefs(items, "")...)
	}
	if _, err := inventory.LockFlashStock(ctx, tx, refs); err != nil {
		return 0, 0, fmt.Errorf("khóa số dư của các đợt flash sale hết giờ: %w", err)
	}
	sys := inventory.Actor{Type: inventory.ActorSystem}
	for _, s := range toClose {
		if _, err := w.closeLocked(ctx, tx, q, s, FlashClosedEnded, sys); err != nil {
			return 0, 0, err
		}
	}
	for _, s := range toOpen {
		if _, err := w.setFlashStatus(ctx, tx, q, s, FlashOpen, "", TopicFlashSaleOpened); err != nil {
			return 0, 0, err
		}
	}
	return len(toOpen), len(toClose), nil
}

// Live trả số liệu đang chạy của đợt saleID; không có → ErrNotFound.
func Live(ctx context.Context, tx pgx.Tx, saleID uuid.UUID) (FlashLive, error) {
	q := store.New(tx)
	if _, err := q.FlashSaleByID(ctx, saleID); errors.Is(err, pgx.ErrNoRows) {
		return FlashLive{}, ErrNotFound
	} else if err != nil {
		return FlashLive{}, fmt.Errorf("đọc đợt flash sale: %w", err)
	}
	rows, err := q.FlashLiveItems(ctx, saleID)
	if err != nil {
		return FlashLive{}, fmt.Errorf("đọc số liệu đợt flash sale: %w", err)
	}
	out := FlashLive{Items: make([]FlashLiveItem, 0, len(rows))}
	for _, r := range rows {
		out.Items = append(out.Items, FlashLiveItem{
			ItemID: r.ID, Quantity: int(r.Quantity), Available: int(r.Available), Held: int(r.Held), Sold: int(r.Sold),
			Released: int(r.Released), Buyers: int(r.Buyers),
		})
	}
	return out, nil
}

// closeLocked đóng đợt s (đã khóa, scheduled hay open): khóa số dư mọi món theo (variant, warehouse) tăng dần, trả đơn
// vị trống của từng món theo cùng thứ tự, đợt → closed, phát flash_sale.closed.
func (w Writer) closeLocked(ctx context.Context, tx pgx.Tx, q *store.Queries, s FlashSale, reason string,
	a inventory.Actor,
) (FlashSale, error) {
	items, err := q.FlashItemsBySale(ctx, s.ID)
	if err != nil {
		return FlashSale{}, fmt.Errorf("đọc món flash sale: %w", err)
	}
	refs := flashRefs(items, "")
	if _, err := inventory.LockFlashStock(ctx, tx, refs); err != nil {
		return FlashSale{}, fmt.Errorf("khóa số dư của đợt flash sale: %w", err)
	}
	iw := w.inventory()
	for _, r := range refs {
		if _, err := iw.CloseFlashItem(ctx, tx, r, a); err != nil {
			return FlashSale{}, fmt.Errorf("trả suất món %s: %w", r.ItemID, err)
		}
	}
	return w.setFlashStatus(ctx, tx, q, s, FlashClosed, reason, TopicFlashSaleClosed)
}

// setFlashStatus đổi trạng thái đợt s (đã khóa) sang to (version + 1) và phát topic.
func (w Writer) setFlashStatus(ctx context.Context, tx pgx.Tx, q *store.Queries, s FlashSale, to, reason, topic string) (
	FlashSale, error,
) {
	row, err := q.SetFlashSaleStatus(ctx, store.SetFlashSaleStatusParams{
		Status: to, ClosedReason: pgtype.Text{String: reason, Valid: reason != ""}, ID: s.ID, FromStatus: s.Status,
	})
	if err != nil {
		return FlashSale{}, fmt.Errorf("đổi trạng thái đợt flash sale: %w", err)
	}
	after := FlashSale{
		ID: row.ID, Name: row.Name, StartsAt: row.StartsAt, EndsAt: row.EndsAt, Status: row.Status,
		ClosedReason: row.ClosedReason.String, CreatedBy: row.CreatedBy, CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt, Version: int(row.Version),
	}
	if err := w.emitFlash(ctx, tx, row.TenantID, s.ID, to, after.Version, topic); err != nil {
		return FlashSale{}, err
	}
	return after, nil
}

// emitFlash phát sự kiện đợt (Seq = version sau khi đổi, tăng dưới khóa dòng đợt).
func (w Writer) emitFlash(ctx context.Context, tx pgx.Tx, tenant, saleID uuid.UUID, status string, version int,
	topic string,
) error {
	if w.Events == nil || w.Queue == nil {
		return fmt.Errorf("phát %s: Writer thiếu Events, Queue: %w", topic, events.ErrInvalidEvent)
	}
	if _, _, err := w.Events.Emit(ctx, tx, w.Queue, events.Event{
		TenantID: tenant, AggregateType: aggregateFlashSale, AggregateID: saleID.String(), Seq: int64(version),
		Topic: topic, Payload: FlashSaleEvent{FlashSaleID: saleID, Status: status, Version: version},
	}); err != nil {
		return fmt.Errorf("phát %s: %w", topic, err)
	}
	return nil
}

func (w Writer) inventory() inventory.Writer {
	return inventory.Writer{Events: w.Events, Queue: w.Queue, Realtime: w.Realtime}
}

// inventoryActor: nhân viên (UserID) hay hệ thống (không UserID) cho sổ tồn.
func inventoryActor(a Actor) inventory.Actor {
	if a.UserID == uuid.Nil {
		return inventory.Actor{Type: inventory.ActorSystem}
	}
	return inventory.Actor{Type: inventory.ActorStaff, ID: a.UserID}
}

// flashRefs là tham chiếu món cho inventory, theo (variant_id, warehouse_id) tăng dần (thứ tự khóa số dư, ADR-46
// nhóm 11; một đợt mỗi biến thể một món nên thứ tự là duy nhất).
func flashRefs(items []store.FlashItemsBySaleRow, step string) []inventory.FlashItemRef {
	refs := make([]inventory.FlashItemRef, 0, len(items))
	for _, it := range items {
		refs = append(refs, inventory.FlashItemRef{
			ItemID: it.ID, VariantID: it.VariantID, WarehouseID: it.WarehouseID, Quantity: int(it.Quantity), Step: step,
		})
	}
	slices.SortFunc(refs, func(x, y inventory.FlashItemRef) int {
		return cmp.Or(compareUUID(x.VariantID, y.VariantID), compareUUID(x.WarehouseID, y.WarehouseID))
	})
	return refs
}

// shortageError dựng *FlashShortageError theo thứ tự món của đợt (items), kèm SKU.
func shortageError(items []store.FlashItemsBySaleRow, short []inventory.FlashShortage) error {
	e := &FlashShortageError{}
	for _, it := range items {
		if i := slices.IndexFunc(short, func(s inventory.FlashShortage) bool { return s.ItemID == it.ID }); i >= 0 {
			e.Items = append(e.Items, FlashShortItem{
				VariantID: it.VariantID, SKU: it.Sku, Need: short[i].Need, Sellable: short[i].Sellable,
			})
		}
	}
	return e
}

func compareUUID(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) }

// lockActiveSale khóa dòng đợt id; không có → ErrNotFound, không scheduled/open → ErrFlashNotActive.
func lockActiveSale(ctx context.Context, q *store.Queries, id uuid.UUID) (FlashSale, error) {
	return lockSale(ctx, q, id, ErrFlashNotActive, FlashScheduled, FlashOpen)
}

// flashTickKind: việc của lịch nhịp; quá một kỳ chưa chạy thì hết hạn (kỳ sau chạy lại).
func flashTickKind() queue.Kind {
	return queue.Kind{
		Name: "task." + FlashTickSchedule, Policy: queue.Policy{MaxAttempts: 3, ExpiresAfter: flashTickEvery},
	}
}

func flashTickSchedule() schedules.Schedule {
	return schedules.Schedule{Name: FlashTickSchedule, Every: flashTickEvery, PerTenant: true, Kind: flashTickKind()}
}

// flashTickHandler chạy Tick trong tx việc của tenant (hàng đợi đặt ngữ cảnh tenant); kết quả chỉ có số.
type flashTickHandler struct {
	w   Writer
	now func() time.Time
}

func (flashTickHandler) Kind() queue.Kind { return flashTickKind() }

// Handle chạy một nhịp; kết quả {"opened","closed"}.
func (h flashTickHandler) Handle(ctx context.Context, j queue.Job, tx pgx.Tx) (json.RawMessage, error) {
	if j.TenantID == nil {
		return nil, fmt.Errorf("việc %s: thiếu tenant: %w", j.ID, queue.ErrPermanent)
	}
	opened, closed, err := h.w.Tick(ctx, tx, h.now())
	if err != nil {
		return nil, err
	}
	out, err := json.Marshal(map[string]int{"opened": opened, "closed": closed})
	if err != nil {
		return nil, fmt.Errorf("mã hóa kết quả nhịp flash sale: %w", err)
	}
	return out, nil
}
