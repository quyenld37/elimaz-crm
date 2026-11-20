package fulfillment_test

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// Tenant chưa có dòng cài đặt: LoadSettings trả đúng mặc định của kế hoạch (Task M3-04), version 0.
func TestLoadSettings_Defaults(t *testing.T) {
	s := testkit.OpenStaff(t)
	a := s.Tenant(t, "A")
	var got fulfillment.Settings
	var version int64
	if err := tenancy.WithTenant(t.Context(), s.App, a, func(tx pgx.Tx) (err error) {
		got, version, err = fulfillment.LoadSettings(t.Context(), tx)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if version != 0 {
		t.Errorf("chưa có dòng: version phải 0, có %d", version)
	}
	want := map[string][2]int{
		"stuck awaiting_pickup": {got.StuckHours["awaiting_pickup"], 48}, "stuck shipping": {got.StuckHours["shipping"], 72},
		"stuck delivery_failed": {got.StuckHours["delivery_failed"], 24}, "stuck returning": {got.StuckHours["returning"], 168},
		"stuck cancel_requested": {got.StuckHours["cancel_requested"], 24},
		"poll awaiting_pickup":   {got.PollMinutes["awaiting_pickup"], 120}, "poll shipping": {got.PollMinutes["shipping"], 60},
		"poll delivery_failed": {got.PollMinutes["delivery_failed"], 30}, "poll returning": {got.PollMinutes["returning"], 360},
		"poll cancel_requested": {got.PollMinutes["cancel_requested"], 15},
		"callback":              {got.CallbackDeadlineHours, 24}, "weight": {got.DefaultWeightGramsPerItem, 300},
		"quote": {got.QuoteValidMinutes, 30}, "watch": {got.ReturnRisk.WatchMinReturned, 1},
		"high": {got.ReturnRisk.HighMinReturned, 2}, "rate": {got.ReturnRisk.HighRateBP, 3000},
		"cod overdue": {got.CODOverdueDays, 14}, "aged held": {got.AgedDays.Held, 20},
		"aged transit": {got.AgedDays.InTransit, 180},
	}
	for name, v := range want {
		if v[0] != v[1] {
			t.Errorf("%s: mong %d, có %d", name, v[1], v[0])
		}
	}
	if len(got.StuckHours) != 5 || len(got.PollMinutes) != 5 {
		t.Errorf("mong 5 trạng thái mỗi bảng, có %v %v", got.StuckHours, got.PollMinutes)
	}
	if err := got.Validate(); err != nil {
		t.Errorf("mặc định phải hợp lệ: %v", err)
	}
}

// Validate: mọi số > 0, HighRateBP 1–10.000, đủ và chỉ đúng 5 trạng thái ở hai bảng; lỗi nêu tên trường.
func TestSettings_Validate(t *testing.T) {
	cases := []struct {
		name, field string
		edit        func(*fulfillment.Settings)
	}{
		{"rate 0", "returnRisk.highRateBP", func(s *fulfillment.Settings) { s.ReturnRisk.HighRateBP = 0 }},
		{"rate 10001", "returnRisk.highRateBP", func(s *fulfillment.Settings) { s.ReturnRisk.HighRateBP = 10001 }},
		{"watch 0", "returnRisk.watchMinReturned", func(s *fulfillment.Settings) { s.ReturnRisk.WatchMinReturned = 0 }},
		{"high 0", "returnRisk.highMinReturned", func(s *fulfillment.Settings) { s.ReturnRisk.HighMinReturned = 0 }},
		{"callback 0", "callbackDeadlineHours", func(s *fulfillment.Settings) { s.CallbackDeadlineHours = 0 }},
		{"weight âm", "defaultWeightGramsPerItem", func(s *fulfillment.Settings) { s.DefaultWeightGramsPerItem = -1 }},
		{"quote 0", "quoteValidMinutes", func(s *fulfillment.Settings) { s.QuoteValidMinutes = 0 }},
		{"cod 0", "codOverdueDays", func(s *fulfillment.Settings) { s.CODOverdueDays = 0 }},
		{"held 0", "agedDays.held", func(s *fulfillment.Settings) { s.AgedDays.Held = 0 }},
		{"transit 0", "agedDays.inTransit", func(s *fulfillment.Settings) { s.AgedDays.InTransit = 0 }},
		{"stuck 0", "stuckHours.shipping", func(s *fulfillment.Settings) { s.StuckHours["shipping"] = 0 }},
		{"stuck thiếu", "stuckHours.returning", func(s *fulfillment.Settings) { delete(s.StuckHours, "returning") }},
		{"stuck lạ", "stuckHours.lost", func(s *fulfillment.Settings) { s.StuckHours["lost"] = 5 }},
		{"poll 0", "pollMinutes.cancel_requested", func(s *fulfillment.Settings) { s.PollMinutes["cancel_requested"] = 0 }},
		{"poll lạ", "pollMinutes.x", func(s *fulfillment.Settings) { s.PollMinutes["x"] = 1 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := fulfillment.DefaultSettings()
			c.edit(&s)
			err := s.Validate()
			var se *fulfillment.SettingOutOfRangeError
			if !errors.Is(err, fulfillment.ErrSettingOutOfRange) || !errors.As(err, &se) || se.Field != c.field {
				t.Fatalf("mong ErrSettingOutOfRange trường %s, có %v", c.field, err)
			}
		})
	}
	ok := fulfillment.DefaultSettings()
	ok.ReturnRisk.HighRateBP = 10000
	if err := ok.Validate(); err != nil {
		t.Errorf("HighRateBP 10000 hợp lệ: %v", err)
	}
	ok.ReturnRisk.HighRateBP = 1
	if err := ok.Validate(); err != nil {
		t.Errorf("HighRateBP 1 hợp lệ: %v", err)
	}
	a, b := fulfillment.DefaultSettings(), fulfillment.DefaultSettings()
	a.StuckHours["shipping"] = 1
	if b.StuckHours["shipping"] != 72 {
		t.Error("DefaultSettings phải trả map mới mỗi lần")
	}
}

// SaveSettings: ngoài khoảng → không ghi; version 0 chèn; version cũ → ErrVersionConflict; đọc lại đúng giá trị mới.
func TestSaveSettings(t *testing.T) {
	s := testkit.OpenStaff(t)
	a := s.Tenant(t, "A")
	actor := fulfillment.Actor{TenantID: a, UserID: s.User(t, a, "ql@elimaz.test", "active")}
	run := func(fn func(tx pgx.Tx) error) error { return tenancy.WithTenant(t.Context(), s.App, a, fn) }
	save := func(st fulfillment.Settings, version int64) (int64, error) {
		var v int64
		err := run(func(tx pgx.Tx) (err error) {
			v, err = fulfillment.SaveSettings(t.Context(), tx, st, version, actor)
			return err //nolint:wrapcheck // test
		})
		return v, err
	}
	bad := fulfillment.DefaultSettings()
	bad.ReturnRisk.HighRateBP = 0
	if _, err := save(bad, 0); !errors.Is(err, fulfillment.ErrSettingOutOfRange) {
		t.Fatalf("mong ErrSettingOutOfRange, có %v", err)
	}
	next := fulfillment.DefaultSettings()
	next.StuckHours["shipping"] = 96
	if v, err := save(next, 0); err != nil || v != 1 {
		t.Fatalf("chèn: mong version 1, có %d %v", v, err)
	}
	if _, err := save(next, 0); !errors.Is(err, fulfillment.ErrVersionConflict) {
		t.Fatalf("version cũ: mong ErrVersionConflict, có %v", err)
	}
	if err := run(func(tx pgx.Tx) error {
		got, v, err := fulfillment.LoadSettings(t.Context(), tx)
		if err == nil && (v != 1 || got.StuckHours["shipping"] != 96 || got.StuckHours["returning"] != 168) {
			t.Errorf("đọc lại: %+v version %d", got, v)
		}
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
}
