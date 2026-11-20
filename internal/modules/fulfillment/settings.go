package fulfillment

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/settings"
)

// SettingsKey là khóa cài đặt vận chuyển trong platform.settings (kế hoạch M3, Task M3-04).
const SettingsKey = "fulfillment"

// maxRateBP: tỉ lệ tính bằng phần vạn (basis point), 10.000 = 100%.
const maxRateBP = 10000

var (
	// ErrSettingOutOfRange báo cài đặt vận chuyển sai luật; chi tiết (tên trường) là *SettingOutOfRangeError.
	ErrSettingOutOfRange = errors.New("fulfillment: cài đặt ngoài khoảng cho phép")
	// ErrVersionConflict báo version gửi lên không khớp bản đang lưu.
	ErrVersionConflict = errors.New("fulfillment: version không khớp")
)

// SettingOutOfRangeError là chi tiết của ErrSettingOutOfRange: trường đầu tiên sai (tên JSON, vd. "returnRisk.highRateBP",
// "stuckHours.shipping").
type SettingOutOfRangeError struct{ Field string }

func (e *SettingOutOfRangeError) Error() string {
	return "fulfillment: cài đặt " + e.Field + " ngoài khoảng cho phép"
}

// Is cho errors.Is(err, ErrSettingOutOfRange).
func (e *SettingOutOfRangeError) Is(target error) bool { return target == ErrSettingOutOfRange }

// ReturnRisk là ngưỡng mức "khách hay hoàn" (Task M3-21): theo dõi từ WatchMinReturned đơn hoàn; cao từ
// HighMinReturned đơn hoàn và tỉ lệ hoàn ≥ HighRateBP phần vạn.
type ReturnRisk struct {
	WatchMinReturned int `json:"watchMinReturned"`
	HighMinReturned  int `json:"highMinReturned"`
	HighRateBP       int `json:"highRateBP"`
}

// AgedDays là ngưỡng "Giữ hàng quá hạn" (Task M3-22): hàng giữ cho đơn chưa gửi quá Held ngày, hàng đang đi quá
// InTransit ngày.
type AgedDays struct {
	Held      int `json:"held"`
	InTransit int `json:"inTransit"`
}

// Settings là cài đặt vận chuyển của shop (kế hoạch M3, Task M3-04). Hai bảng theo trạng thái vận đơn có đúng năm khóa
// của StuckStatuses.
type Settings struct {
	// StuckHours: vận đơn ở trạng thái quá số giờ này thì vào "Đơn kẹt" (Task M3-20).
	StuckHours map[string]int `json:"stuckHours"`
	// CallbackDeadlineHours: hạn gọi lại khách sau khi giao thất bại (Task M3-19).
	CallbackDeadlineHours int `json:"callbackDeadlineHours"`
	// PollMinutes: chu kỳ tra bù trạng thái theo lịch (Task M3-11).
	PollMinutes map[string]int `json:"pollMinutes"`
	// DefaultWeightGramsPerItem: cân nặng mỗi món khi đơn chưa nhập cân (Task M3-12, M3-13).
	DefaultWeightGramsPerItem int `json:"defaultWeightGramsPerItem"`
	// QuoteValidMinutes: báo phí còn dùng được trong bao lâu (Task M3-13).
	QuoteValidMinutes int        `json:"quoteValidMinutes"`
	ReturnRisk        ReturnRisk `json:"returnRisk"`
	// CODOverdueDays: vận đơn giao xong quá số ngày này chưa có tiền về thì vào "Quá hạn chưa có tiền" (Task M3-32).
	CODOverdueDays int      `json:"codOverdueDays"`
	AgedDays       AgedDays `json:"agedDays"`
}

// StuckStatuses là năm trạng thái vận đơn có ngưỡng kẹt và chu kỳ tra bù, theo thứ tự hiện.
func StuckStatuses() []string {
	return []string{"awaiting_pickup", "shipping", "delivery_failed", "returning", "cancel_requested"}
}

// DefaultSettings trả cài đặt mặc định (map mới mỗi lần gọi).
func DefaultSettings() Settings {
	return Settings{
		StuckHours: map[string]int{
			"awaiting_pickup": 48, "shipping": 72, "delivery_failed": 24, "returning": 168, "cancel_requested": 24,
		},
		CallbackDeadlineHours: 24,
		PollMinutes: map[string]int{
			"awaiting_pickup": 120, "shipping": 60, "delivery_failed": 30, "returning": 360, "cancel_requested": 15,
		},
		DefaultWeightGramsPerItem: 300,
		QuoteValidMinutes:         30,
		ReturnRisk:                ReturnRisk{WatchMinReturned: 1, HighMinReturned: 2, HighRateBP: 3000},
		CODOverdueDays:            14,
		AgedDays:                  AgedDays{Held: 20, InTransit: 180},
	}
}

// Validate kiểm luật: mọi số > 0; HighRateBP 1–10.000; StuckHours, PollMinutes có đúng năm khóa của StuckStatuses.
// Sai → *SettingOutOfRangeError của trường đầu tiên.
func (s Settings) Validate() error {
	for _, f := range []struct {
		name string
		v    int
	}{
		{"callbackDeadlineHours", s.CallbackDeadlineHours},
		{"defaultWeightGramsPerItem", s.DefaultWeightGramsPerItem},
		{"quoteValidMinutes", s.QuoteValidMinutes},
		{"returnRisk.watchMinReturned", s.ReturnRisk.WatchMinReturned},
		{"returnRisk.highMinReturned", s.ReturnRisk.HighMinReturned},
		{"returnRisk.highRateBP", s.ReturnRisk.HighRateBP},
		{"codOverdueDays", s.CODOverdueDays},
		{"agedDays.held", s.AgedDays.Held},
		{"agedDays.inTransit", s.AgedDays.InTransit},
	} {
		if f.v <= 0 {
			return &SettingOutOfRangeError{Field: f.name}
		}
	}
	if s.ReturnRisk.HighRateBP > maxRateBP {
		return &SettingOutOfRangeError{Field: "returnRisk.highRateBP"}
	}
	if err := validateByStatus("stuckHours", s.StuckHours); err != nil {
		return err
	}
	return validateByStatus("pollMinutes", s.PollMinutes)
}

func validateByStatus(name string, m map[string]int) error {
	statuses := StuckStatuses()
	for _, k := range slices.Sorted(maps.Keys(m)) {
		if !slices.Contains(statuses, k) {
			return &SettingOutOfRangeError{Field: name + "." + k}
		}
	}
	for _, k := range statuses {
		if v, ok := m[k]; !ok || v <= 0 {
			return &SettingOutOfRangeError{Field: name + "." + k}
		}
	}
	return nil
}

// AuditFields là phần trước/sau của dòng audit khi sửa cài đặt (chỉ số, không dữ liệu cá nhân).
func (s Settings) AuditFields() map[string]any {
	return map[string]any{
		"stuckHours": maps.Clone(s.StuckHours), "callbackDeadlineHours": s.CallbackDeadlineHours,
		"pollMinutes": maps.Clone(s.PollMinutes), "defaultWeightGramsPerItem": s.DefaultWeightGramsPerItem,
		"quoteValidMinutes": s.QuoteValidMinutes, "returnRisk": s.ReturnRisk, "codOverdueDays": s.CODOverdueDays,
		"agedDays": s.AgedDays,
	}
}

// LoadSettings đọc cài đặt vận chuyển của tenant trong tx: khóa thiếu lấy mặc định; version 0 khi chưa lưu lần nào.
func LoadSettings(ctx context.Context, tx pgx.Tx) (Settings, int64, error) {
	s, v, err := settings.Get(ctx, tx, SettingsKey, DefaultSettings())
	if err != nil {
		return Settings{}, 0, fmt.Errorf("đọc cài đặt vận chuyển: %w", err)
	}
	return s, v, nil
}

// SaveSettings kiểm s rồi ghi khi version còn khớp (0: chưa lưu lần nào) trong tx của bên gọi (một mình, ADR-46 nhóm
// 12c); trả version mới. Sai luật → ErrSettingOutOfRange; version lệch → ErrVersionConflict. Audit do bên gọi ghi.
func SaveSettings(ctx context.Context, tx pgx.Tx, s Settings, version int64, a Actor) (int64, error) {
	if err := s.Validate(); err != nil {
		return 0, err
	}
	actor := ""
	if a.UserID != uuid.Nil {
		actor = a.UserID.String()
	}
	err := settings.Put(ctx, tx, SettingsKey, version, s, actor)
	if errors.Is(err, settings.ErrVersionConflict) {
		return 0, fmt.Errorf("ghi cài đặt vận chuyển: %w", ErrVersionConflict)
	}
	if err != nil {
		return 0, fmt.Errorf("ghi cài đặt vận chuyển: %w", err)
	}
	return version + 1, nil
}
