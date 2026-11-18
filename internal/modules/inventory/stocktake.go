package inventory

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/modules/inventory/internal/store"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Kiểm kho (kế hoạch M1, Task M1-35; 05 mục 2 "Kiểm kho", KH05, KH07): mở phiên theo kho (StartStocktake), ghi số
// đếm từng biến thể (RecordCount), xem trước (GetStocktake), áp dụng (Writer.ApplyStocktake) hoặc hủy
// (CancelStocktake).
//
// So với phần trên kệ = on_hand − in_transit chụp lúc ghi dòng (hàng đang giao không bị coi là thiếu); áp dụng cộng
// delta = counted − expected_on_shelf vào on_hand, không đụng held, in_transit, giữ hàng hay đơn. Đếm thiếu dưới phần
// đang giữ: vẫn ghi ("có thể bán" âm), trả Shortages và ghi lệnh cảnh báo (quyết định Q8 của kế hoạch M1).
//
// Thứ tự khóa (docs/adr/0046-thu-tu-khoa.md nhóm 11): stocktakes (dòng phiên) → stocktake_lines → stock_balances theo
// variant_id tăng dần (một kho mỗi phiên). RecordCount đọc số dư không khóa.

// Trạng thái phiên kiểm (M7 dùng đúng các tên này).
const (
	StocktakeOpen      = "open"
	StocktakeApplied   = "applied"
	StocktakeCancelled = "cancelled"
)

const (
	docStocktake      = "stocktake"
	stepApply         = "apply"
	maxCounted        = 1_000_000
	warehouseActive   = "active"
	stocktakeOpenUniq = "stocktakes_one_open"
)

// Stocktake là một phiên kiểm kho.
type Stocktake struct {
	ID, WarehouseID      uuid.UUID
	Code, Status         string
	StartedBy, AppliedBy *uuid.UUID
	AppliedAt            *time.Time
	CreatedAt            time.Time
	Version              int64
}

// StocktakeLine là một dòng đếm kèm xem trước: Current là số dư hiện tại của biến thể ở kho của phiên;
// AvailableAfter = Current.Available + Delta (có thể bán sau khi áp dụng nếu số dư không đổi tiếp).
type StocktakeLine struct {
	VariantID                       uuid.UUID
	ExpectedOnShelf, Counted, Delta int32
	CountedAt                       time.Time
	CountedBy                       *uuid.UUID
	Version                         int64
	Current                         Balance
	AvailableAfter                  int32
}

// Shortage là biến thể vừa chỉnh mà "có thể bán" âm (đếm thiếu dưới phần đang giữ).
type Shortage struct {
	VariantID uuid.UUID
	Available int32
}

// ApplyResult là kết quả áp dụng: phiên sau khi áp dụng, số dòng đã chỉnh số dư, các biến thể thiếu so với phần giữ.
type ApplyResult struct {
	Stocktake Stocktake
	Adjusted  int
	Shortages []Shortage
}

// StartStocktake mở phiên kiểm ở kho warehouseID (mã KK-<số> từ sequence). Lỗi: ErrNotFound (kho không có hoặc của
// tenant khác), ErrInvalidInput (kho đã đóng, tác nhân lạ), ErrStocktakeOpen (kho đã có phiên mở; vi phạm unique làm
// hỏng tx: bên gọi lùi transaction).
func StartStocktake(ctx context.Context, tx pgx.Tx, warehouseID uuid.UUID, a Actor) (Stocktake, error) {
	if !a.valid() {
		return Stocktake{}, fmt.Errorf("%w: tác nhân %q", ErrInvalidInput, a.Type)
	}
	q := store.New(tx)
	status, err := q.GetWarehouseStatus(ctx, warehouseID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Stocktake{}, ErrNotFound
	case err != nil:
		return Stocktake{}, fmt.Errorf("đọc kho: %w", err)
	case status != warehouseActive:
		return Stocktake{}, fmt.Errorf("%w: kho đã đóng", ErrInvalidInput)
	}
	n, err := q.NextStocktakeNumber(ctx)
	if err != nil {
		return Stocktake{}, fmt.Errorf("sinh mã phiên kiểm: %w", err)
	}
	id, err := q.InsertStocktake(ctx, store.InsertStocktakeParams{
		Code: fmt.Sprintf("KK-%06d", n), StartedBy: actorUUID(a), WarehouseID: warehouseID,
	})
	if pe, ok := errors.AsType[*pgconn.PgError](err); ok && pe.Code == "23505" && pe.ConstraintName == stocktakeOpenUniq {
		return Stocktake{}, ErrStocktakeOpen
	}
	if err != nil {
		return Stocktake{}, fmt.Errorf("ghi phiên kiểm: %w", err)
	}
	return getStocktake(ctx, q, id)
}

// RecordCount ghi số đếm counted (0…1 000 000) của biến thể variantID vào phiên mở stocktakeID, chụp
// expected_on_shelf = on_hand − in_transit hiện tại (đọc thường, không khóa); version phiên tăng 1. lineVersion nil:
// dòng mới (dòng đã có → ErrVersionConflict); khác nil: ghi lại dòng đã có đúng version (chụp lại). Lỗi: ErrNotFound,
// ErrStocktakeNotOpen, ErrVersionConflict, ErrInvalidInput (số sai, biến thể không có).
func RecordCount(ctx context.Context, tx pgx.Tx, stocktakeID, variantID uuid.UUID, counted int32, lineVersion *int64,
	a Actor,
) (StocktakeLine, error) {
	switch {
	case !a.valid():
		return StocktakeLine{}, fmt.Errorf("%w: tác nhân %q", ErrInvalidInput, a.Type)
	case counted < 0 || counted > maxCounted:
		return StocktakeLine{}, fmt.Errorf("%w: số đếm %d", ErrInvalidInput, counted)
	case variantID == uuid.Nil:
		return StocktakeLine{}, fmt.Errorf("%w: biến thể trống", ErrInvalidInput)
	}
	q := store.New(tx)
	st, err := lockOpen(ctx, q, stocktakeID, nil)
	if err != nil {
		return StocktakeLine{}, err
	}
	bs, err := Availability(ctx, tx, st.WarehouseID, []uuid.UUID{variantID})
	if err != nil {
		return StocktakeLine{}, err
	}
	expected := bs[0].OnHand - bs[0].InTransit
	cur, err := q.GetStocktakeLine(ctx, store.GetStocktakeLineParams{StocktakeID: stocktakeID, VariantID: variantID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if lineVersion != nil {
			return StocktakeLine{}, ErrVersionConflict
		}
		n, err := q.InsertStocktakeLine(ctx, store.InsertStocktakeLineParams{
			ExpectedOnShelf: expected, Counted: counted, CountedBy: actorUUID(a), VariantID: variantID,
			StocktakeID: stocktakeID,
		})
		if err != nil {
			return StocktakeLine{}, fmt.Errorf("ghi dòng đếm: %w", err)
		}
		if n == 0 {
			return StocktakeLine{}, fmt.Errorf("%w: biến thể %s không có", ErrInvalidInput, variantID)
		}
	case err != nil:
		return StocktakeLine{}, fmt.Errorf("đọc dòng đếm: %w", err)
	case lineVersion == nil || *lineVersion != cur:
		return StocktakeLine{}, ErrVersionConflict
	default:
		if _, err := q.UpdateStocktakeLine(ctx, store.UpdateStocktakeLineParams{
			ExpectedOnShelf: expected, Counted: counted, CountedBy: actorUUID(a), StocktakeID: stocktakeID,
			VariantID: variantID, Version: cur,
		}); err != nil {
			return StocktakeLine{}, fmt.Errorf("ghi lại dòng đếm: %w", err)
		}
	}
	if err := q.TouchStocktake(ctx, stocktakeID); err != nil {
		return StocktakeLine{}, fmt.Errorf("tăng version phiên kiểm: %w", err)
	}
	ls, err := listLines(ctx, q, stocktakeID, &variantID)
	if err != nil {
		return StocktakeLine{}, err
	}
	if len(ls) != 1 {
		return StocktakeLine{}, fmt.Errorf("đọc lại dòng đếm: %d dòng", len(ls))
	}
	return ls[0], nil
}

// ApplyStocktake áp dụng phiên mở id đúng version trong tx của bên gọi: đổi trạng thái (một câu UPDATE có điều kiện:
// hai yêu cầu song song → một thắng), rồi mỗi dòng delta ≠ 0 theo variant_id tăng dần cộng delta vào on_hand qua
// applyDelta (sổ doc_type stocktake, doc_id = id phiên, step apply) và phát stock.changed. Giảm chỉ khi
// on_hand − in_transit ≥ |delta| (hàng đang giao vẫn trừ được khi giao xong); không đủ → *InsufficientError (Available
// = on_hand − in_transit), bên gọi lùi transaction. Không đụng held, in_transit, giữ hàng. Biến thể vừa chỉnh có
// "có thể bán" âm vào Shortages và một lệnh ShortageAlertKind được ghi cùng tx. Lỗi khác: ErrNotFound,
// ErrStocktakeNotOpen, ErrVersionConflict, ErrInvalidInput.
func (w Writer) ApplyStocktake(ctx context.Context, tx pgx.Tx, id uuid.UUID, version int64, a Actor) (ApplyResult, error) {
	if !a.valid() {
		return ApplyResult{}, fmt.Errorf("%w: tác nhân %q", ErrInvalidInput, a.Type)
	}
	if w.Queue == nil {
		return ApplyResult{}, errNotWired
	}
	q := store.New(tx)
	row, err := q.ApplyStocktake(ctx, store.ApplyStocktakeParams{AppliedBy: actorUUID(a), ID: id, Version: version})
	if errors.Is(err, pgx.ErrNoRows) {
		_, err := lockOpen(ctx, q, id, &version)
		if err == nil {
			err = ErrVersionConflict // đã khóa mà vẫn khớp: không xảy ra; giữ để an toàn
		}
		return ApplyResult{}, err
	}
	if err != nil {
		return ApplyResult{}, fmt.Errorf("áp dụng phiên kiểm: %w", err)
	}
	lines, err := listLines(ctx, q, id, nil)
	if err != nil {
		return ApplyResult{}, err
	}
	changed, tenant, err := w.applyLines(ctx, tx, row.WarehouseID, id, lines, a)
	if err != nil {
		return ApplyResult{}, err
	}
	res := ApplyResult{Adjusted: len(changed)}
	if res.Shortages, err = shortages(ctx, tx, row.WarehouseID, changed); err != nil {
		return ApplyResult{}, err
	}
	if len(res.Shortages) > 0 {
		if _, _, err := w.Queue.Enqueue(ctx, tx, shortageAlertKind(), queue.NewJob{
			TenantID: &tenant, DedupeKey: id.String(),
			Payload: shortagePayload{StocktakeID: id, Code: row.Code, Count: len(res.Shortages)},
		}); err != nil {
			return ApplyResult{}, fmt.Errorf("ghi lệnh cảnh báo thiếu hàng: %w", err)
		}
	}
	if res.Stocktake, err = getStocktake(ctx, q, id); err != nil {
		return ApplyResult{}, err
	}
	return res, nil
}

// applyLines chèn dòng số dư còn thiếu (theo variant_id tăng dần) rồi áp các dòng delta ≠ 0; trả biến thể đã chỉnh và
// tenant của chúng.
func (w Writer) applyLines(ctx context.Context, tx pgx.Tx, warehouseID, id uuid.UUID, lines []StocktakeLine, a Actor,
) ([]uuid.UUID, uuid.UUID, error) {
	q := store.New(tx)
	var changed []uuid.UUID
	for _, l := range lines {
		if l.Delta == 0 {
			continue
		}
		if err := q.EnsureBalance(ctx, store.EnsureBalanceParams{VariantID: l.VariantID, WarehouseID: warehouseID}); err != nil {
			return nil, uuid.Nil, fmt.Errorf("chèn số dư: %w", err)
		}
		changed = append(changed, l.VariantID)
	}
	var tenant uuid.UUID
	for _, l := range lines {
		if l.Delta == 0 {
			continue
		}
		r, err := w.applyCount(ctx, tx, warehouseID, id, l, a)
		if err != nil {
			return nil, uuid.Nil, err
		}
		tenant = r.TenantID
	}
	return changed, tenant, nil
}

// shortages trả các biến thể trong changed có "có thể bán" âm.
func shortages(ctx context.Context, tx pgx.Tx, warehouseID uuid.UUID, changed []uuid.UUID) ([]Shortage, error) {
	if len(changed) == 0 {
		return nil, nil
	}
	bs, err := Availability(ctx, tx, warehouseID, changed)
	if err != nil {
		return nil, err
	}
	var out []Shortage
	for _, b := range bs {
		if b.Available < 0 {
			out = append(out, Shortage{VariantID: b.VariantID, Available: b.Available})
		}
	}
	return out, nil
}

// applyCount cộng delta của dòng l vào on_hand rồi phát stock.changed.
func (w Writer) applyCount(ctx context.Context, tx pgx.Tx, warehouseID, id uuid.UUID, l StocktakeLine, a Actor) (balanceRow, error) {
	c := condNone
	if l.Delta < 0 {
		c = condOnShelf
	}
	r, ok, err := applyDelta(ctx, tx, l.VariantID, warehouseID, bucketOnHand, l.Delta, c, doc{Type: docStocktake, ID: id},
		stepApply, a)
	if err != nil {
		return balanceRow{}, err
	}
	if !ok {
		bs, err := Availability(ctx, tx, warehouseID, []uuid.UUID{l.VariantID})
		if err != nil {
			return balanceRow{}, err
		}
		return balanceRow{}, &InsufficientError{
			VariantID: l.VariantID, WarehouseID: warehouseID, Requested: abs(l.Delta),
			Available: bs[0].OnHand - bs[0].InTransit,
		}
	}
	return r, w.changed(ctx, tx, r)
}

// CancelStocktake khóa phiên id, kiểm còn mở và đúng version, rồi hủy; số dư không đổi. Lỗi: ErrNotFound, ErrStocktakeNotOpen,
// ErrVersionConflict, ErrInvalidInput (tác nhân lạ).
func CancelStocktake(ctx context.Context, tx pgx.Tx, id uuid.UUID, version int64, a Actor) error {
	q := store.New(tx)
	if _, err := lockOpen(ctx, q, id, &version); err != nil {
		return err
	}
	if !a.valid() {
		return fmt.Errorf("%w: tác nhân %q", ErrInvalidInput, a.Type)
	}
	if _, err := q.CancelStocktake(ctx, store.CancelStocktakeParams{CancelledBy: actorUUID(a), ID: id, Version: version}); err != nil {
		return fmt.Errorf("hủy phiên kiểm: %w", err) // đã khóa và kiểm: 0 dòng không xảy ra
	}
	return nil
}

// GetStocktake trả phiên id và các dòng đếm (theo variant_id) kèm xem trước. Không có hoặc của tenant khác →
// ErrNotFound.
func GetStocktake(ctx context.Context, tx pgx.Tx, id uuid.UUID) (Stocktake, []StocktakeLine, error) {
	q := store.New(tx)
	s, err := getStocktake(ctx, q, id)
	if err != nil {
		return Stocktake{}, nil, err
	}
	ls, err := listLines(ctx, q, id, nil)
	if err != nil {
		return Stocktake{}, nil, err
	}
	return s, ls, nil
}

func getStocktake(ctx context.Context, q *store.Queries, id uuid.UUID) (Stocktake, error) {
	r, err := q.GetStocktake(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Stocktake{}, ErrNotFound
	}
	if err != nil {
		return Stocktake{}, fmt.Errorf("đọc phiên kiểm: %w", err)
	}
	s := Stocktake{
		ID: r.ID, WarehouseID: r.WarehouseID, Code: r.Code, Status: r.Status, CreatedAt: r.CreatedAt, Version: r.Version,
		StartedBy: fromPgUUID(r.StartedBy), AppliedBy: fromPgUUID(r.AppliedBy),
	}
	if r.AppliedAt.Valid {
		t := r.AppliedAt.Time.UTC()
		s.AppliedAt = &t
	}
	return s, nil
}

func listLines(ctx context.Context, q *store.Queries, id uuid.UUID, variantID *uuid.UUID) ([]StocktakeLine, error) {
	rows, err := q.ListStocktakeLines(ctx, store.ListStocktakeLinesParams{StocktakeID: id, VariantID: pgUUID(variantID)})
	if err != nil {
		return nil, fmt.Errorf("đọc dòng đếm: %w", err)
	}
	out := make([]StocktakeLine, 0, len(rows))
	for _, r := range rows {
		out = append(out, StocktakeLine{
			VariantID: r.VariantID, ExpectedOnShelf: r.ExpectedOnShelf, Counted: r.Counted, Delta: r.Delta,
			CountedAt: r.CountedAt.UTC(), CountedBy: fromPgUUID(r.CountedBy), Version: r.Version,
			Current: Balance{
				VariantID: r.VariantID, OnHand: r.OnHand, Held: r.Held, InTransit: r.InTransit, Damaged: r.Damaged,
				Available: r.Available,
			},
			AvailableAfter: r.Available + r.Delta,
		})
	}
	return out, nil
}

// lockOpen khóa dòng phiên id và kiểm còn mở (và đúng version khi version khác nil).
func lockOpen(ctx context.Context, q *store.Queries, id uuid.UUID, version *int64) (store.LockStocktakeRow, error) {
	s, err := q.LockStocktake(ctx, id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return s, ErrNotFound
	case err != nil:
		return s, fmt.Errorf("khóa phiên kiểm: %w", err)
	case s.Status != StocktakeOpen:
		return s, ErrStocktakeNotOpen
	case version != nil && s.Version != *version:
		return s, ErrVersionConflict
	}
	return s, nil
}
