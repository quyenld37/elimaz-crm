package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/platform/internal/store"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// ErrCommandNotResolvable báo lệnh không ở in_doubt hay dead (đang chờ, đang chạy, đã xong, đã bỏ), hoặc loại lệnh không có
// trong danh sách handler của bên gọi.
var ErrCommandNotResolvable = errors.New("commands: lệnh không ở trạng thái người quyết được")

// ResolveManually đặt lệnh jobID thành done với kết quả result do người quyết (nhân viên tra bên ngoài thấy đã thực
// hiện: vận đơn đã có ở hãng, Task M3-12), trong tx của bên gọi (ngữ cảnh tenant, tenancy.WithTenant hay WriteTx). Chỉ
// cho lệnh in_doubt hay dead; gọi DoneHook.OnDone của handler cùng loại trong hs (trước câu đổi platform.jobs, ADR-46
// nhóm 13: jobs sửa cuối) với đúng result. Không gửi gì ra ngoài. Lỗi: không có lệnh (hay của tenant khác) →
// ErrCommandNotFound; trạng thái khác, đã đổi trong lúc xử lý, hay không có handler của loại → ErrCommandNotResolvable;
// lỗi của OnDone trả nguyên (bên gọi lùi transaction).
func ResolveManually(ctx context.Context, tx pgx.Tx, jobID uuid.UUID, result json.RawMessage, hs []Handler) error {
	q := store.New(tx)
	r, err := q.CommandForResolve(ctx, jobID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("xử lý tay lệnh %s: %w", jobID, ErrCommandNotFound)
	case err != nil:
		return fmt.Errorf("đọc lệnh %s: %w", jobID, err)
	case r.Status != queue.StatusInDoubt && r.Status != queue.StatusDead:
		return fmt.Errorf("xử lý tay lệnh %s (%s): %w", jobID, r.Status, ErrCommandNotResolvable)
	}
	var h Handler
	for _, c := range hs {
		if c.Kind().Name == r.Kind {
			h = c
		}
	}
	if h == nil {
		return fmt.Errorf("xử lý tay lệnh %s: không có handler %s: %w", jobID, r.Kind, ErrCommandNotResolvable)
	}
	j := queue.Job{
		ID: r.ID, Kind: r.Kind, Payload: r.Payload, Attempts: int(r.Attempts), CreatedAt: r.CreatedAt,
		TenantID: tenantOf(r.TenantID),
	}
	if dh, ok := h.(DoneHook); ok {
		if err := dh.OnDone(ctx, j, result, tx); err != nil {
			return fmt.Errorf("hook done của lệnh %s: %w", jobID, err)
		}
	}
	n, err := q.ResolveCommandManually(ctx, store.ResolveCommandManuallyParams{ID: jobID, Result: result})
	switch {
	case err != nil:
		return fmt.Errorf("ghi done cho lệnh %s: %w", jobID, err)
	case n == 0:
		return fmt.Errorf("xử lý tay lệnh %s: %w", jobID, ErrCommandNotResolvable)
	}
	return nil
}

// tenantOf: tenant của lệnh (nil với lệnh cấp hệ thống).
func tenantOf(v pgtype.UUID) *uuid.UUID {
	if v.Valid {
		t := uuid.UUID(v.Bytes)
		return &t
	}
	return nil
}
