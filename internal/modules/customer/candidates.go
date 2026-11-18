package customer

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer/internal/store"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// Candidate là một nhóm khách "có thể là cùng một khách" với nhóm đang xem: gốc, mã, tên, chỉ số của gốc và các SĐT
// khớp (theo thứ tự).
type Candidate struct {
	RootID        uuid.UUID
	Code          string
	FullName      Name
	MatchedPhones []logging.Phone
	Stats         Stats
}

// MergeCandidates trả các nhóm khác (theo mã gốc) có danh tính phone trùng SĐT trong địa chỉ (chưa xóa) của nhóm chứa
// id hoặc SĐT người nhận trong 50 đơn gần nhất của nhóm (đọc ord.orders_v). Chỉ gợi ý, không bao giờ tự gộp (05 mục 1).
// Không có (hay tenant khác) → ErrNotFound.
func MergeCandidates(ctx context.Context, tx pgx.Tx, id uuid.UUID) ([]Candidate, error) {
	root, err := RootOf(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	rows, err := store.New(tx).ListMergeCandidates(ctx, root)
	if err != nil {
		return nil, fmt.Errorf("tìm khách có thể trùng: %w", err)
	}
	out := make([]Candidate, 0, len(rows))
	for _, r := range rows {
		phones := make([]logging.Phone, 0, len(r.MatchedPhones))
		for _, p := range r.MatchedPhones {
			phones = append(phones, logging.Phone(p))
		}
		out = append(out, Candidate{
			RootID: r.RootID, Code: r.Code, FullName: Name(r.FullName), MatchedPhones: phones,
			Stats: statsOf(r.OrderCount, r.DeliveredCount, r.ReturnedCount, r.TotalSpent, r.LastOrderAt),
		})
	}
	return out, nil
}
