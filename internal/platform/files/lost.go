package files

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/internal/store"
)

// LostFile là một tệp có trong CRM mà kho tệp không còn (status lost, Task M1-46, TP12).
type LostFile struct {
	ID      uuid.UUID
	Purpose string
	LostAt  time.Time
}

// LostCursor là vị trí sau dòng cuối của một trang tệp mất (thứ tự lost_at giảm, rồi id giảm).
type LostCursor struct {
	At time.Time
	ID uuid.UUID
}

// ListLost trả tối đa limit tệp lost, chưa xóa, của tenant trong tx (tenancy.WithTenant), mất gần nhất trước; after
// khác nil: chỉ các tệp đứng sau vị trí đó.
func ListLost(ctx context.Context, tx pgx.Tx, after *LostCursor, limit int) ([]LostFile, error) {
	params := store.ListLostFilesPageParams{Lim: int32(min(max(limit, 0), 1000))}
	if after != nil {
		params.HasCursor, params.CursorAt, params.CursorID = true, after.At.UTC(), after.ID
	}
	rows, err := store.New(tx).ListLostFilesPage(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("liệt kê tệp mất: %w", err)
	}
	out := make([]LostFile, len(rows))
	for i, r := range rows {
		out[i] = LostFile{ID: r.ID, Purpose: r.Purpose, LostAt: r.LostAt.Time.UTC()}
	}
	return out, nil
}
