// Package lists là hai danh sách đọc của đối soát COD (kế hoạch M3, Task M3-32): "Lệch đối soát" (TI05, TI06) và "Quá
// hạn chưa có tiền". Chỉ đọc, trong tx của tenant của bên gọi.
package lists

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/cash/internal/store"
	"elimaz.com.vn/crm/internal/platform/db"
)

// Loại mục của "Lệch đối soát".
const (
	// KindStatementLine: dòng duplicate, external, hay amount_mismatch (vận đơn còn lệch) của bảng kê đã chốt.
	KindStatementLine = "statement_line"
	// KindOpenRemainder: vận đơn đã có tiền về mà carrier_holding còn khác 0, không do dòng lệch nào ở trên.
	KindOpenRemainder = "open_remainder"
	// KindRemittedThenReturned: phát hiện tiền đã về rồi hãng sửa thành hoàn.
	KindRemittedThenReturned = "remitted_then_returned"
)

// maxLimit là số mục lớn nhất một trang.
const maxLimit = 200

// Discrepancy là một mục "Lệch đối soát". ID là mã mục (dòng bảng kê, vận đơn hay phát hiện) dùng làm con trỏ trang;
// trường không có ở loại mục là giá trị rỗng (nil, 0, "").
type Discrepancy struct {
	Kind, OrderCode, TrackingCode, MatchStatus string
	ID                                         uuid.UUID
	StatementID, ShipmentID                    *uuid.UUID
	LineNo                                     int32
	COD                                        int64
	Expected, Diff                             *int64
}

// Discrepancies đọc tối đa limit mục mới nhất trước, sau mã mục before (nil: trang đầu).
func Discrepancies(ctx context.Context, tx pgx.Tx, before *uuid.UUID, limit int) ([]Discrepancy, error) {
	rows, err := store.New(tx).ListDiscrepancies(ctx, store.ListDiscrepanciesParams{
		BeforeID: db.NullUUID(before), Lim: int32(min(max(limit, 1), maxLimit)),
	})
	if err != nil {
		return nil, fmt.Errorf("đọc lệch đối soát: %w", err)
	}
	out := make([]Discrepancy, 0, len(rows))
	for _, r := range rows {
		d := Discrepancy{
			Kind: r.Kind, ID: r.ItemID, OrderCode: r.OrderCode, TrackingCode: r.TrackingCode, MatchStatus: r.MatchStatus,
			LineNo: r.LineNo, COD: r.CodAmount, ShipmentID: uuidPtr(r.ShipmentID), Expected: int8Ptr(r.ExpectedAmount),
			Diff: int8Ptr(r.DiffAmount),
		}
		if r.StatementID != uuid.Nil {
			d.StatementID = new(r.StatementID)
		}
		out = append(out, d)
	}
	return out, nil
}

func uuidPtr(v pgtype.UUID) *uuid.UUID {
	if !v.Valid {
		return nil
	}
	return new(uuid.UUID(v.Bytes))
}

func int8Ptr(v pgtype.Int8) *int64 {
	if !v.Valid {
		return nil
	}
	return new(v.Int64)
}

// day là một ngày (đếm số ngày quá hạn).
const day = 24 * time.Hour
