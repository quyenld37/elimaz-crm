package order

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/order/internal/store"
)

// Settings là cài đặt đơn của shop (một dòng mỗi tenant, kế hoạch M2 Task M2-15). Khoảng hợp lệ trùng CHECK của bảng
// ord.order_settings (migration 0204): hạn đơn nháp 10–10080 phút, cửa sổ nghi trùng 0–10080 phút, ngưỡng duyệt giảm
// giá 0–10000 điểm cơ bản (1000 = 10% tạm tính), cảnh báo hoàn 0–365 ngày.
type Settings struct {
	DraftTTL, DuplicateWindow   time.Duration
	DiscountApprovalThresholdBP int
	ReturnWarningDays           int
	Version                     int64
}

// SettingsPatch là phần sửa cài đặt đơn: trường nil giữ nguyên. Thời gian tính bằng phút như hợp đồng API.
type SettingsPatch struct {
	DraftTTLMinutes, DuplicateWindowMinutes, DiscountApprovalThresholdBP, ReturnWarningDays *int
}

// SettingsChange là cài đặt trước và sau một lần sửa (bên gọi ghi audit; order không nhập identity).
type SettingsChange struct {
	Before, After Settings
}

// SettingField là tên ổn định của một cài đặt, trùng tên trường của hợp đồng API (màn hình báo lỗi theo tên này).
type SettingField string

// Tên các cài đặt đơn.
const (
	FieldDraftTTLMinutes             SettingField = "draftTtlMinutes"
	FieldDuplicateWindowMinutes      SettingField = "duplicateWindowMinutes"
	FieldDiscountApprovalThresholdBP SettingField = "discountApprovalThresholdBp"
	FieldReturnWarningDays           SettingField = "returnWarningDays"
)

// SettingOutOfRangeError là chi tiết của ErrSettingOutOfRange: trường đầu tiên (theo thứ tự hằng Field…) ngoài khoảng.
type SettingOutOfRangeError struct {
	Field    SettingField
	Min, Max int
}

func (e *SettingOutOfRangeError) Error() string {
	return fmt.Sprintf("order: cài đặt %s ngoài khoảng %d–%d", e.Field, e.Min, e.Max)
}

// Is cho errors.Is(err, ErrSettingOutOfRange).
func (e *SettingOutOfRangeError) Is(target error) bool { return target == ErrSettingOutOfRange }

// AuditFields là giá trị đưa vào audit (không có dữ liệu cá nhân), khóa theo tên trường của hợp đồng.
func (s Settings) AuditFields() map[string]any {
	return map[string]any{
		string(FieldDraftTTLMinutes):             int(s.DraftTTL / time.Minute),
		string(FieldDuplicateWindowMinutes):      int(s.DuplicateWindow / time.Minute),
		string(FieldDiscountApprovalThresholdBP): s.DiscountApprovalThresholdBP,
		string(FieldReturnWarningDays):           s.ReturnWarningDays,
		"version":                                s.Version,
	}
}

// LoadSettings đọc cài đặt đơn của tenant đang chạy (tx trong tenancy.WithTenant). Tenant chưa có dòng (chưa chạy
// EnsureDefaults) là dữ liệu lệch: trả lỗi bọc pgx.ErrNoRows.
func LoadSettings(ctx context.Context, tx pgx.Tx) (Settings, error) {
	r, err := store.New(tx).GetOrderSettings(ctx)
	if err != nil {
		return Settings{}, fmt.Errorf("đọc cài đặt đơn: %w", err)
	}
	return settingsOf(store.LockOrderSettingsRow(r)), nil
}

// UpdateSettings sửa cài đặt đơn khi version còn khớp: khóa dòng (FOR UPDATE), so version (khác → ErrVersionConflict),
// áp p, kiểm khoảng (ngoài khoảng → *SettingOutOfRangeError, errors.Is ErrSettingOutOfRange), ghi version + 1 và người
// sửa. Trả giá trị trước và sau để bên gọi ghi audit trong cùng tx. Chạy lại được trong db.RetryTx.
func (Writer) UpdateSettings(ctx context.Context, tx pgx.Tx, version int64, p SettingsPatch, a Actor) (SettingsChange, error) {
	q := store.New(tx)
	cur, err := q.LockOrderSettings(ctx)
	if err != nil {
		return SettingsChange{}, fmt.Errorf("khóa cài đặt đơn: %w", err)
	}
	if cur.Version != version {
		return SettingsChange{}, ErrVersionConflict
	}
	next := store.UpdateOrderSettingsParams{
		DraftTtlMinutes: cur.DraftTtlMinutes, DuplicateWindowMinutes: cur.DuplicateWindowMinutes,
		DiscountApprovalThresholdBp: cur.DiscountApprovalThresholdBp, ReturnWarningDays: cur.ReturnWarningDays,
		UpdatedBy: a.UserID, Version: version,
	}
	for _, f := range []struct {
		field    SettingField
		v        *int
		min, max int
		dst      *int32
	}{
		{FieldDraftTTLMinutes, p.DraftTTLMinutes, 10, 10080, &next.DraftTtlMinutes},
		{FieldDuplicateWindowMinutes, p.DuplicateWindowMinutes, 0, 10080, &next.DuplicateWindowMinutes},
		{FieldDiscountApprovalThresholdBP, p.DiscountApprovalThresholdBP, 0, 10000, &next.DiscountApprovalThresholdBp},
		{FieldReturnWarningDays, p.ReturnWarningDays, 0, 365, &next.ReturnWarningDays},
	} {
		if f.v == nil {
			continue
		}
		if *f.v < f.min || *f.v > f.max {
			return SettingsChange{}, &SettingOutOfRangeError{Field: f.field, Min: f.min, Max: f.max}
		}
		*f.dst = int32(*f.v) //nolint:gosec // đã kiểm trong khoảng 0–10080
	}
	r, err := q.UpdateOrderSettings(ctx, next)
	if errors.Is(err, pgx.ErrNoRows) {
		return SettingsChange{}, ErrVersionConflict // không xảy ra khi đã giữ khóa dòng; giữ để an toàn
	}
	if err != nil {
		return SettingsChange{}, fmt.Errorf("sửa cài đặt đơn: %w", err)
	}
	return SettingsChange{Before: settingsOf(cur), After: settingsOf(store.LockOrderSettingsRow(r))}, nil
}

func settingsOf(r store.LockOrderSettingsRow) Settings {
	return Settings{
		DraftTTL:                    time.Duration(r.DraftTtlMinutes) * time.Minute,
		DuplicateWindow:             time.Duration(r.DuplicateWindowMinutes) * time.Minute,
		DiscountApprovalThresholdBP: int(r.DiscountApprovalThresholdBp),
		ReturnWarningDays:           int(r.ReturnWarningDays),
		Version:                     r.Version,
	}
}
