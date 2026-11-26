package customer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/customer/internal/store"
)

// Nguồn của tên khách (cột customers.name_source, Task 152): tên tạm khi tạo từ kênh, tên hồ sơ trên kênh, tên nhân
// viên nhập hay sửa. Tên từ kênh không bao giờ đè tên NameSourceStaff.
const (
	NameSourcePlaceholder = "placeholder"
	NameSourceChannel     = "channel"
	NameSourceStaff       = "staff"
)

// SetDoNotContact bật hay tắt cờ "không liên hệ" (02 mục 3: chặn mọi tin tự động) trên hồ sơ đại diện của customerID,
// sau khi khóa dòng gốc (LockRoot). Bật ghi do_not_contact_at = now; tắt xóa mốc. Đã đúng trạng thái → không ghi gì.
// Ghi audit customer.do_not_contact (trước, sau chỉ là giá trị cờ; không tên, không danh tính) và báo hiệu
// customer.changed. Không có (hay của tenant khác) → ErrNotFound.
func (w Writer) SetDoNotContact(ctx context.Context, tx pgx.Tx, customerID uuid.UUID, on bool, a Actor, now time.Time) error {
	if err := w.check(); err != nil {
		return err
	}
	root, err := lockRoot(ctx, tx, customerID)
	if err != nil {
		return err
	}
	if root.doNotContact == on {
		return nil
	}
	var at pgtype.Timestamptz
	if on {
		at = pgtype.Timestamptz{Time: now, Valid: true}
	}
	if _, err := store.New(tx).SetDoNotContact(ctx, store.SetDoNotContactParams{
		Enabled: on, At: at, Now: now, ID: root.rootID,
	}); err != nil {
		return fmt.Errorf("đặt cờ không liên hệ: %w", err)
	}
	if err := w.audit(ctx, tx, root.tenantID, a, "customer.do_not_contact", root.rootID,
		map[string]any{"do_not_contact": !on}, map[string]any{"do_not_contact": on}); err != nil {
		return err
	}
	return w.signal(ctx, tx, root.tenantID, root.rootID)
}

// IsDoNotContact trả cờ "không liên hệ" của hồ sơ đại diện (gốc) của customerID: hồ sơ đã gộp theo cờ của gốc; hoàn
// tác gộp thì về cờ của chính nó. Không có (hay của tenant khác) → ErrNotFound.
func IsDoNotContact(ctx context.Context, tx pgx.Tx, customerID uuid.UUID) (bool, error) {
	on, err := store.New(tx).GetDoNotContactOfRoot(ctx, customerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, fmt.Errorf("đọc cờ không liên hệ: %w", err)
	}
	return on, nil
}

// SetNameFromChannel đặt tên lấy từ hồ sơ trên kênh cho hồ sơ customerID (CH08) khi tên hiện tại không phải của nhân
// viên (name_source placeholder hay channel) và khác tên mới: đổi full_name, name_search (NormalizeName), name_source =
// channel, tăng version (lần sửa của nhân viên cầm version cũ gặp ErrVersionConflict). Tên nhân viên nhập hay sửa
// không bao giờ bị đè (changed = false). Tên sai → ErrInvalidInput; không có (hay của tenant khác) → ErrNotFound. Ghi
// audit customer.name_from_channel (chỉ tên trường, người làm là hệ thống) và báo hiệu customer.changed. Câu đổi khóa
// dòng khách (ADR-46 nhóm 5): bên gọi gọi trước khi khóa dòng hội thoại (nhóm 6).
func (w Writer) SetNameFromChannel(ctx context.Context, tx pgx.Tx, customerID uuid.UUID, name string) (bool, error) {
	if err := w.check(); err != nil {
		return false, err
	}
	n, err := validName(name)
	if err != nil {
		return false, err
	}
	row, err := store.New(tx).SetNameFromChannel(ctx, store.SetNameFromChannelParams{
		FullName: n, NameSearch: NormalizeName(n), ID: customerID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := RootOf(ctx, tx, customerID); err != nil {
			return false, err
		}
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("đặt tên từ kênh: %w", err)
	}
	if err := w.audit(ctx, tx, row.TenantID, Actor{Type: ActorSystem}, "customer.name_from_channel", customerID, nil,
		fieldsAudit([]string{"full_name"})); err != nil {
		return false, err
	}
	if err := w.signal(ctx, tx, row.TenantID, row.RootID); err != nil {
		return false, err
	}
	return true, nil
}
