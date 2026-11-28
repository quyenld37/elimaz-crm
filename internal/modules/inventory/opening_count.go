package inventory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory/internal/store"
)

// Đếm cộng dồn nhiều người cùng lúc (kế hoạch M7, Task 263 = M7-18): mỗi lần quét cộng (hoặc trừ, sửa quét thừa) vào
// dòng đếm của biến thể bằng một câu có điều kiện, không đọc số ra Go rồi ghi lại, nên nhiều người quét song song không
// mất lần nào. Chỉ cho phiên mở sổ: cộng dồn giữ khóa chia sẻ phiên và không tăng version phiên, nên với phiên kiểm thường
// của M1 sẽ phá "áp dụng đúng bản đã xem trước" (ApplyStocktake theo version); phiên thường đếm bằng RecordCount.
//
// Thứ tự khóa (ADR-46 nhóm 11): stocktakes (FOR SHARE: PostOpening khóa FOR UPDATE nên chờ mọi lần cộng đang chạy, và
// lần cộng sau khi đăng thấy phiên applied) → stocktake_lines.

// Giới hạn một lần cộng.
const maxIncrement = 1000

// IncrementLine cộng by (−1000…1000, khác 0) vào số đếm của biến thể có mã quét code (SKU không phân biệt hoa thường,
// hoặc barcode đúng nguyên chuỗi; bỏ khoảng trắng đầu cuối) trong phiên mở sổ stocktakeID, chụp lại expected_on_shelf =
// on_hand − in_transit như RecordCount; trả biến thể và số đếm sau khi cộng. Lỗi: ErrNotFound (phiên không có, của
// tenant khác), ErrInvalidInput (by ngoài khoảng, mã trống, tác nhân lạ, phiên kiểm thường, số đếm vượt 1 000 000),
// ErrStocktakeNotOpen, ErrUnknownCode, *AmbiguousCodeError, ErrNegativeCount (trừ quá số đã đếm hay dòng chưa có).
func IncrementLine(ctx context.Context, tx pgx.Tx, stocktakeID uuid.UUID, code string, by int32, a Actor, now time.Time,
) (uuid.UUID, int32, error) {
	code = strings.TrimSpace(code)
	switch {
	case !a.valid():
		return uuid.Nil, 0, fmt.Errorf("%w: tác nhân %q", ErrInvalidInput, a.Type)
	case by == 0 || by < -maxIncrement || by > maxIncrement:
		return uuid.Nil, 0, fmt.Errorf("%w: số cộng %d", ErrInvalidInput, by)
	case code == "":
		return uuid.Nil, 0, fmt.Errorf("%w: mã quét trống", ErrInvalidInput)
	}
	q := store.New(tx)
	st, err := shareOpening(ctx, q, stocktakeID)
	if err != nil {
		return uuid.Nil, 0, err
	}
	found, err := resolveCodes(ctx, q, []string{code})
	if err != nil {
		return uuid.Nil, 0, err
	}
	cands := found[code]
	switch {
	case len(cands) == 0:
		return uuid.Nil, 0, ErrUnknownCode
	case len(cands) > 1:
		return uuid.Nil, 0, &AmbiguousCodeError{Candidates: cands}
	}
	v := cands[0]
	bs, err := Availability(ctx, tx, st.WarehouseID, []uuid.UUID{v.VariantID})
	if err != nil {
		return uuid.Nil, 0, err
	}
	expected := bs[0].OnHand - bs[0].InTransit
	var counted int32
	if by > 0 {
		counted, err = q.AddToStocktakeLine(ctx, store.AddToStocktakeLineParams{
			VariantID: v.VariantID, SellerID: v.SellerID, ExpectedOnShelf: expected, By: by, Now: now.UTC(),
			CountedBy: actorUUID(a), StocktakeID: stocktakeID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, 0, fmt.Errorf("%w: số đếm vượt %d", ErrInvalidInput, maxCounted)
		}
	} else {
		counted, err = q.SubtractFromStocktakeLine(ctx, store.SubtractFromStocktakeLineParams{
			By: by, ExpectedOnShelf: expected, Now: now.UTC(), CountedBy: actorUUID(a),
			StocktakeID: stocktakeID, VariantID: v.VariantID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, 0, ErrNegativeCount
		}
	}
	if err != nil {
		return uuid.Nil, 0, fmt.Errorf("cộng số đếm: %w", err)
	}
	return v.VariantID, counted, nil
}

// shareOpening khóa chia sẻ phiên id và đòi phiên mở sổ còn mở.
func shareOpening(ctx context.Context, q *store.Queries, id uuid.UUID) (store.ShareStocktakeRow, error) {
	st, err := q.ShareStocktake(ctx, id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return st, ErrNotFound
	case err != nil:
		return st, fmt.Errorf("khóa phiên kiểm: %w", err)
	case st.Kind != StocktakeKindOpening:
		return st, fmt.Errorf("%w: đếm cộng dồn chỉ cho phiên mở sổ", ErrInvalidInput)
	case st.Status != StocktakeOpen:
		return st, ErrStocktakeNotOpen
	}
	return st, nil
}

// resolveCodes tra các mã quét codes (đã bỏ khoảng trắng) và trả mã → mọi biến thể khớp (theo mã biến thể tăng dần).
func resolveCodes(ctx context.Context, q *store.Queries, codes []string) (map[string][]CodeCandidate, error) {
	rows, err := q.VariantsByCodes(ctx, codes)
	if err != nil {
		return nil, fmt.Errorf("tra mã quét: %w", err)
	}
	out := make(map[string][]CodeCandidate, len(codes))
	for _, r := range rows {
		out[r.Code] = append(out[r.Code], CodeCandidate{VariantID: r.VariantID, SellerID: r.SellerID, SKU: r.Sku})
	}
	return out, nil
}
