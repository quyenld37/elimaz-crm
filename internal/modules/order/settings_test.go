package order_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/order"
)

// withDefaults: môi trường order có cài đặt và nguồn mặc định (order.EnsureDefaults).
func withDefaults(t *testing.T) *orderEnv {
	t.Helper()
	e := newOrderEnv(t)
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error { return order.EnsureDefaults(context.Background(), tx, e.tenant) })
	return e
}

func (e *orderEnv) settings() order.Settings {
	e.t.Helper()
	var s order.Settings
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) (err error) {
		s, err = order.LoadSettings(context.Background(), tx)
		return err //nolint:wrapcheck // test
	})
	return s
}

func (e *orderEnv) updateSettings(version int64, p order.SettingsPatch) (order.SettingsChange, error) {
	e.t.Helper()
	var ch order.SettingsChange
	err := e.staff.TryExec(context.Background(), e.tenant, func(tx pgx.Tx) (err error) {
		ch, err = order.Writer{}.UpdateSettings(context.Background(), tx, version, p, e.actor)
		return err //nolint:wrapcheck // test
	})
	return ch, err //nolint:wrapcheck // test
}

func TestSettings_Defaults(t *testing.T) {
	t.Parallel()
	e := withDefaults(t)
	s := e.settings()
	if s.DraftTTL != 1440*time.Minute || s.DuplicateWindow != 1440*time.Minute || s.DiscountApprovalThresholdBP != 1000 ||
		s.ReturnWarningDays != 7 || s.Version != 1 {
		t.Fatalf("mặc định: %+v", s)
	}
}

func TestSettings_UpdateAndVersion(t *testing.T) {
	t.Parallel()
	e := withDefaults(t)
	ch, err := e.updateSettings(1, order.SettingsPatch{DiscountApprovalThresholdBP: new(2000)})
	if err != nil {
		t.Fatal(err)
	}
	if ch.Before.DiscountApprovalThresholdBP != 1000 || ch.After.DiscountApprovalThresholdBP != 2000 ||
		ch.After.Version != 2 || ch.After.DraftTTL != 1440*time.Minute {
		t.Fatalf("đổi ngưỡng: %+v", ch)
	}
	if b, a := ch.Before.AuditFields(), ch.After.AuditFields(); b["discountApprovalThresholdBp"] != 1000 ||
		a["discountApprovalThresholdBp"] != 2000 {
		t.Errorf("AuditFields: %v → %v", b, a)
	}
	if got := e.settings(); got.DiscountApprovalThresholdBP != 2000 || got.Version != 2 {
		t.Errorf("đọc lại: %+v", got)
	}
	if _, err := e.updateSettings(1, order.SettingsPatch{ReturnWarningDays: new(10)}); !errors.Is(err, order.ErrVersionConflict) {
		t.Errorf("version cũ: %v", err)
	}
	if got := e.settings(); got.ReturnWarningDays != 7 || got.Version != 2 {
		t.Errorf("version cũ không được đổi gì: %+v", got)
	}
	ch, err = e.updateSettings(2, order.SettingsPatch{
		DraftTTLMinutes: new(10), DuplicateWindowMinutes: new(0), ReturnWarningDays: new(365),
	})
	if err != nil || ch.After.DraftTTL != 10*time.Minute || ch.After.DuplicateWindow != 0 || ch.After.ReturnWarningDays != 365 ||
		ch.After.DiscountApprovalThresholdBP != 2000 || ch.After.Version != 3 {
		t.Fatalf("đúng biên: %+v %v", ch, err)
	}
}

func TestSettings_OutOfRange(t *testing.T) {
	t.Parallel()
	e := withDefaults(t)
	for _, c := range []struct {
		name  string
		p     order.SettingsPatch
		field order.SettingField
	}{
		{"hạn nháp dưới 10", order.SettingsPatch{DraftTTLMinutes: new(5)}, order.FieldDraftTTLMinutes},
		{"hạn nháp trên 10080", order.SettingsPatch{DraftTTLMinutes: new(10081)}, order.FieldDraftTTLMinutes},
		{"cửa sổ trùng âm", order.SettingsPatch{DuplicateWindowMinutes: new(-1)}, order.FieldDuplicateWindowMinutes},
		{"cửa sổ trùng trên 10080", order.SettingsPatch{DuplicateWindowMinutes: new(10081)}, order.FieldDuplicateWindowMinutes},
		{"ngưỡng âm", order.SettingsPatch{DiscountApprovalThresholdBP: new(-1)}, order.FieldDiscountApprovalThresholdBP},
		{"ngưỡng trên 10000", order.SettingsPatch{DiscountApprovalThresholdBP: new(10001)}, order.FieldDiscountApprovalThresholdBP},
		{"cảnh báo hoàn âm", order.SettingsPatch{ReturnWarningDays: new(-1)}, order.FieldReturnWarningDays},
		{"cảnh báo hoàn trên 365", order.SettingsPatch{ReturnWarningDays: new(366)}, order.FieldReturnWarningDays},
		{"trường hợp lệ kèm trường sai", order.SettingsPatch{ReturnWarningDays: new(3), DraftTTLMinutes: new(9)}, order.FieldDraftTTLMinutes},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := e.updateSettings(1, c.p)
			oe, ok := errors.AsType[*order.SettingOutOfRangeError](err)
			if !errors.Is(err, order.ErrSettingOutOfRange) || !ok || oe.Field != c.field {
				t.Fatalf("muốn ErrSettingOutOfRange trường %s, có %v", c.field, err)
			}
		})
	}
	if got := e.settings(); got.Version != 1 || got.DraftTTL != 1440*time.Minute {
		t.Errorf("ngoài khoảng không được đổi gì: %+v", got)
	}
}
