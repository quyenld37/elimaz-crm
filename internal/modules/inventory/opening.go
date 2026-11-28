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
)

// Kiểm kho mở sổ (kế hoạch M7, Task 262 = M7-17; MO02): một phiên kiểm của M1 loại opening trên kho trống. Mở
// (CreateOpening), đếm bằng RecordCount của M1, đăng (Writer.PostOpening: phải đếm đủ mọi biến thể phải đếm, kho vẫn
// trống; áp dụng bằng đúng đường áp dụng của kiểm kho M1 nên tồn mở sổ = số đếm, sổ doc_type stocktake, doc_id = id
// phiên), kiểm lại (CheckOpening, mốc A của đêm mở). Sau mở sổ, sai số sửa bằng phiếu adjust có lý do của M1.
//
// Biến thể phải đếm: active, của mẫu draft hoặc active chưa xóa, mọi người bán của tenant (view opening_status_v).
// Thứ tự khóa (ADR-46 nhóm 11): warehouses (chỉ lúc mở) → stocktakes → stocktake_lines → stock_balances theo
// variant_id tăng dần.

const openingOneUniq = "stocktakes_one_opening"

// OpeningStatus là một dòng của inventory.opening_status_v.
type OpeningStatus struct {
	StocktakeID, WarehouseID    uuid.UUID
	Status                      string
	LinesExpected, LinesCounted int32
	UnitsCounted                int64
	AppliedAt                   *time.Time
}

// PostResult là kết quả đăng phiên mở sổ: phiên sau khi đăng, số biến thể phải đếm, số dòng đã đếm, tổng số đếm.
type PostResult struct {
	Stocktake                   Stocktake
	LinesExpected, CountedLines int
	Units                       int64
}

// OpeningMismatch là một biến thể lệch khi kiểm lại: số đếm, tổng sổ on_hand của phiên, on_hand hiện tại.
type OpeningMismatch struct {
	VariantID                      uuid.UUID
	SKU                            string
	Counted, LedgerOpening, OnHand int32
}

// CheckResult là kết quả kiểm lại phiên mở sổ đã đăng. ChangedSince: kho đã có chứng từ khác (sau giờ mở bán) nên chỉ
// so số đếm với sổ, không so on_hand.
type CheckResult struct {
	OK, ChangedSince bool
	Mismatches       []OpeningMismatch
}

// CreateOpening mở phiên mở sổ ở kho warehouseID trong tx của bên gọi; trả phiên và số biến thể phải đếm. Khóa dòng kho
// (hai lần mở cùng kho chạy lần lượt), kiểm chưa có phiên mở sổ chưa hủy, kho trống, rồi StartStocktake của M1 và đặt
// kind = opening. Lỗi: ErrNotFound (kho không có, của tenant khác), ErrInvalidInput (kho đóng, tác nhân lạ),
// ErrOpeningExists, ErrWarehouseNotEmpty, ErrStocktakeOpen (kho đang có phiên kiểm thường mở). Vi phạm unique làm hỏng
// tx: bên gọi lùi transaction.
func CreateOpening(ctx context.Context, tx pgx.Tx, warehouseID uuid.UUID, a Actor) (Stocktake, int, error) {
	if !a.valid() {
		return Stocktake{}, 0, fmt.Errorf("%w: tác nhân %q", ErrInvalidInput, a.Type)
	}
	q := store.New(tx)
	if _, err := q.LockWarehouseForOpening(ctx, warehouseID); errors.Is(err, pgx.ErrNoRows) {
		return Stocktake{}, 0, ErrNotFound
	} else if err != nil {
		return Stocktake{}, 0, fmt.Errorf("khóa kho: %w", err)
	}
	exists, err := q.OpeningExists(ctx, warehouseID)
	if err != nil {
		return Stocktake{}, 0, fmt.Errorf("kiểm phiên mở sổ đã có: %w", err)
	}
	if exists {
		return Stocktake{}, 0, ErrOpeningExists
	}
	if err := checkEmpty(ctx, q, warehouseID, nil); err != nil {
		return Stocktake{}, 0, err
	}
	s, err := StartStocktake(ctx, tx, warehouseID, a)
	if err != nil {
		return Stocktake{}, 0, err
	}
	_, err = q.SetStocktakeOpening(ctx, s.ID)
	if pe, ok := errors.AsType[*pgconn.PgError](err); ok && pe.Code == "23505" && pe.ConstraintName == openingOneUniq {
		return Stocktake{}, 0, ErrOpeningExists
	}
	if err != nil {
		return Stocktake{}, 0, fmt.Errorf("đặt phiên mở sổ: %w", err)
	}
	s.Kind = StocktakeKindOpening
	o, err := GetOpening(ctx, tx, s.ID)
	if err != nil {
		return Stocktake{}, 0, err
	}
	return s, int(o.LinesExpected), nil
}

// PostOpening đăng phiên mở sổ id trong tx của bên gọi: khóa phiên (còn mở), đòi đếm đủ mọi biến thể phải đếm và kho
// vẫn trống, chèn dòng số dư 0 cho mọi dòng đếm (theo variant_id tăng dần), áp dụng bằng đường của ApplyStocktake
// (delta = số đếm, sổ doc_type stocktake, stock.changed), rồi kiểm lại kho trống bỏ qua sổ của phiên (phiếu ghi sổ
// chen giữa → lùi). Lỗi: ErrNotFound (không có, không phải phiên mở sổ), ErrStocktakeNotOpen (đã đăng, đã hủy),
// *UncountedLinesError, ErrWarehouseNotEmpty, ErrInvalidInput (tác nhân lạ). Lỗi nào bên gọi cũng lùi transaction.
func (w Writer) PostOpening(ctx context.Context, tx pgx.Tx, id uuid.UUID, a Actor) (PostResult, error) {
	q := store.New(tx)
	st, err := q.LockStocktake(ctx, id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return PostResult{}, ErrNotFound
	case err != nil:
		return PostResult{}, fmt.Errorf("khóa phiên mở sổ: %w", err)
	case st.Kind != StocktakeKindOpening:
		return PostResult{}, ErrNotFound
	case st.Status != StocktakeOpen:
		return PostResult{}, ErrStocktakeNotOpen
	}
	n, err := q.CountOpeningUncounted(ctx, id)
	if err != nil {
		return PostResult{}, fmt.Errorf("đếm dòng chưa đếm: %w", err)
	}
	if n > 0 {
		return PostResult{}, &UncountedLinesError{N: int(n)}
	}
	stocked, err := q.OpeningHasStockedLine(ctx, id)
	if err != nil {
		return PostResult{}, fmt.Errorf("kiểm dòng đếm trên kho có hàng: %w", err)
	}
	if stocked {
		return PostResult{}, ErrWarehouseNotEmpty
	}
	if err := checkEmpty(ctx, q, st.WarehouseID, &id); err != nil {
		return PostResult{}, err
	}
	lines, err := listLines(ctx, q, id, nil)
	if err != nil {
		return PostResult{}, err
	}
	res := PostResult{CountedLines: len(lines)}
	for _, l := range lines {
		if err := q.EnsureBalance(ctx, store.EnsureBalanceParams{VariantID: l.VariantID, WarehouseID: st.WarehouseID}); err != nil {
			return PostResult{}, fmt.Errorf("chèn số dư: %w", err)
		}
		res.Units += int64(l.Counted)
	}
	r, err := w.apply(ctx, tx, id, st.Version, StocktakeKindOpening, a)
	if err != nil {
		return PostResult{}, err
	}
	if err := checkEmpty(ctx, q, st.WarehouseID, &id); err != nil {
		return PostResult{}, err
	}
	o, err := GetOpening(ctx, tx, id)
	if err != nil {
		return PostResult{}, err
	}
	res.Stocktake, res.LinesExpected = r.Stocktake, int(o.LinesExpected)
	return res, nil
}

// CheckOpening kiểm lại phiên mở sổ đã đăng id: với mỗi biến thể có dòng đếm hay có sổ của phiên, số đếm phải bằng tổng
// sổ on_hand của phiên; khi kho chưa có chứng từ nào khác, on_hand hiện tại cũng phải bằng số đếm. Lỗi: ErrNotFound
// (không có, không phải phiên mở sổ), ErrOpeningNotPosted (chưa đăng).
func CheckOpening(ctx context.Context, tx pgx.Tx, id uuid.UUID) (CheckResult, error) {
	q := store.New(tx)
	s, err := getStocktake(ctx, q, id)
	switch {
	case err != nil:
		return CheckResult{}, err
	case s.Kind != StocktakeKindOpening:
		return CheckResult{}, ErrNotFound
	case s.Status != StocktakeApplied:
		return CheckResult{}, ErrOpeningNotPosted
	}
	changed, err := q.OpeningLedgerOtherDocs(ctx, store.OpeningLedgerOtherDocsParams{WarehouseID: s.WarehouseID, StocktakeID: id})
	if err != nil {
		return CheckResult{}, fmt.Errorf("kiểm chứng từ sau mở sổ: %w", err)
	}
	rows, err := q.CompareOpening(ctx, store.CompareOpeningParams{WarehouseID: s.WarehouseID, StocktakeID: id})
	if err != nil {
		return CheckResult{}, fmt.Errorf("so phiên mở sổ với sổ: %w", err)
	}
	res := CheckResult{ChangedSince: changed, Mismatches: []OpeningMismatch{}}
	for _, r := range rows {
		if r.Counted != r.LedgerOpening || (!changed && r.OnHand != r.Counted) {
			res.Mismatches = append(res.Mismatches, OpeningMismatch{
				VariantID: r.VariantID, SKU: r.Sku, Counted: r.Counted, LedgerOpening: r.LedgerOpening, OnHand: r.OnHand,
			})
		}
	}
	res.OK = len(res.Mismatches) == 0
	return res, nil
}

// GetOpening đọc phiên mở sổ id từ opening_status_v. Không có, của tenant khác hay không phải phiên mở sổ → ErrNotFound.
func GetOpening(ctx context.Context, tx pgx.Tx, id uuid.UUID) (OpeningStatus, error) {
	r, err := store.New(tx).GetOpeningStatus(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return OpeningStatus{}, ErrNotFound
	}
	if err != nil {
		return OpeningStatus{}, fmt.Errorf("đọc phiên mở sổ: %w", err)
	}
	return openingOut(r), nil
}

// ListOpenings trả các phiên mở sổ (tối đa 200, mới nhất trước) của kho warehouseID, hoặc mọi kho khi nil.
func ListOpenings(ctx context.Context, tx pgx.Tx, warehouseID *uuid.UUID) ([]OpeningStatus, error) {
	rows, err := store.New(tx).ListOpeningStatus(ctx, pgUUID(warehouseID))
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách phiên mở sổ: %w", err)
	}
	out := make([]OpeningStatus, 0, len(rows))
	for _, r := range rows {
		out = append(out, openingOut(r))
	}
	return out, nil
}

func openingOut(r store.InventoryOpeningStatusV) OpeningStatus {
	o := OpeningStatus{
		StocktakeID: r.StocktakeID, WarehouseID: r.WarehouseID, Status: r.Status, LinesExpected: r.LinesExpected,
		LinesCounted: r.LinesCounted, UnitsCounted: r.UnitsCounted,
	}
	if r.AppliedAt.Valid {
		t := r.AppliedAt.Time.UTC()
		o.AppliedAt = &t
	}
	return o
}

// checkEmpty trả ErrWarehouseNotEmpty khi kho có dòng sổ, giữ hàng chưa xong hay số dư khác 0; stocktakeID khác nil:
// bỏ qua sổ của phiên đó (kiểm lại khi đăng).
func checkEmpty(ctx context.Context, q *store.Queries, warehouseID uuid.UUID, stocktakeID *uuid.UUID) error {
	notEmpty, err := q.WarehouseNotEmpty(ctx, store.WarehouseNotEmptyParams{WarehouseID: warehouseID, StocktakeID: pgUUID(stocktakeID)})
	if err != nil {
		return fmt.Errorf("kiểm kho trống: %w", err)
	}
	if notEmpty {
		return ErrWarehouseNotEmpty
	}
	return nil
}
