// Package datadeletion là callback xóa dữ liệu của Meta (kế hoạch M4, Task 179; spec 02 mục 5; Q30): ingest nhận yêu
// cầu (Endpoint: kiểm chữ ký, ghi platform.data_deletion_requests hoặc tệp tạm, trả mã xác nhận và trang trạng thái);
// worker chạy việc platform.data_deletion (Processor) gọi các Deleter do module đăng ký ở mọi tenant rồi đánh dấu xong.
// Gói nền tảng không nhập module: cmd/crm truyền Deleter (vd channel.DeleteOAuthDataFor).
package datadeletion

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/platform/internal/store"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

const (
	// ScheduleName là lịch (không tenant) xử lý yêu cầu xóa chưa xong, mỗi Every.
	ScheduleName = "platform.data_deletion"
	// Every là chu kỳ của lịch ScheduleName.
	Every = 60 * time.Second
	// batchSize: số yêu cầu tối đa mỗi lượt (phần còn lại để kỳ sau).
	batchSize = 100
)

// UserRefHash là băm lưu thay mã người dùng app id: hex SHA-256 (64 ký tự). Một chỗ cho cả đường nhận yêu cầu và
// đường ghi phiên đăng nhập (channel), để hai bên so được với nhau.
func UserRefHash(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}

// Deleter xóa dữ liệu của người dùng app có băm userRefHash trong tenant đang đặt của tx (RLS); trả số dòng đã xóa.
// Chạy lại được (tx có thể lùi rồi chạy lại); không gọi mạng.
type Deleter func(ctx context.Context, tx pgx.Tx, userRefHash string) (int64, error)

// Processor là handler (transactional, không tenant) của lịch ScheduleName.
type Processor struct {
	deleters []Deleter
}

// NewProcessor trả Processor gọi lần lượt deleters.
func NewProcessor(deleters ...Deleter) *Processor { return &Processor{deleters: deleters} }

// Schedule là lịch của Processor (cmd/crm định nghĩa ở worker).
func (p *Processor) Schedule() schedules.Schedule {
	return schedules.Schedule{Name: ScheduleName, Every: Every, Kind: p.Kind()}
}

// Kind là loại việc: transactional, hạn sống một kỳ (kỳ sau lấy lại yêu cầu chưa xong).
func (*Processor) Kind() queue.Kind {
	return queue.Kind{Name: "task." + ScheduleName, Policy: queue.Policy{MaxAttempts: 3, ExpiresAfter: Every}}
}

// Handle xử lý tới batchSize yêu cầu chưa xong (cũ trước): trước hết, với mỗi yêu cầu và mỗi tenant, đặt app.tenant_id
// rồi gọi mọi Deleter (ADR-46 nhóm 3), bỏ đặt tenant; sau cùng mới đánh dấu completed_at có điều kiện cho cả lô (nhóm
// 13 luôn cuối transaction; hai lượt chồng nhau chỉ một lượt đánh dấu). Danh sách yêu cầu đọc không khóa để giữ đúng
// thứ tự khóa. Lỗi của Deleter → cả tx lùi, kỳ sau làm lại. Kết quả {"completed": số yêu cầu}.
func (p *Processor) Handle(ctx context.Context, _ queue.Job, tx pgx.Tx) (json.RawMessage, error) {
	q := store.New(tx)
	reqs, err := q.ListPendingDataDeletions(ctx, batchSize)
	if err != nil {
		return nil, fmt.Errorf("đọc yêu cầu xóa chưa xong: %w", err)
	}
	if len(reqs) == 0 {
		return json.RawMessage(`{"completed":0}`), nil
	}
	tenants, err := q.ListTenantIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách tenant: %w", err)
	}
	for _, r := range reqs {
		if err := p.deleteEverywhere(ctx, tx, tenants, r.UserRefHash); err != nil {
			return nil, fmt.Errorf("xóa dữ liệu của yêu cầu %s: %w", r.Code, err)
		}
	}
	now := pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	completed := 0
	for _, r := range reqs {
		n, err := q.CompleteDataDeletion(ctx, store.CompleteDataDeletionParams{Now: now, Code: r.Code})
		if err != nil {
			return nil, fmt.Errorf("đánh dấu yêu cầu %s đã xong: %w", r.Code, err)
		}
		completed += int(n)
	}
	return json.RawMessage(fmt.Sprintf(`{"completed":%d}`, completed)), nil
}

// deleteEverywhere gọi mọi Deleter trong từng tenant rồi bỏ đặt tenant (kể cả khi lỗi, tx sẽ lùi).
func (p *Processor) deleteEverywhere(ctx context.Context, tx pgx.Tx, tenants []uuid.UUID, hash string) error {
	for _, t := range tenants {
		if err := tenancy.SetTenant(ctx, tx, t); err != nil {
			return err //nolint:wrapcheck // tenancy đã bọc
		}
		for _, d := range p.deleters {
			if _, err := d(ctx, tx, hash); err != nil {
				return fmt.Errorf("tenant %s: %w", t, err)
			}
		}
	}
	return tenancy.SetTenant(ctx, tx, uuid.Nil) //nolint:wrapcheck // tenancy đã bọc
}
