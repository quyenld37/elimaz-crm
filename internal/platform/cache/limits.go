package cache

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrBadLimits báo RATE_LIMITS_JSON sai (JSON hỏng, tên hay trường lạ, số ngoài khoảng); tiến trình không lên.
var ErrBadLimits = errors.New("cache: RATE_LIMITS_JSON không hợp lệ")

// Limits là các giới hạn tần suất của website (QĐ23 kế hoạch M5; đoán mã giảm giá Task 482). Lấy bằng DefaultLimits
// hoặc ParseLimits.
type Limits struct {
	// WebKey: mỗi khóa dịch vụ (chủ thể "svc:<mã>").
	WebKey Limit
	// FlashCustomer: lượt bấm mua flash sale của mỗi khách (SubjectKey "phone").
	FlashCustomer Limit
	// FlashIP: lượt bấm mua flash sale của mỗi IP (SubjectKey "ip").
	FlashIP Limit
	// WebOrderCustomer: đơn web của mỗi khách (SubjectKey "phone").
	WebOrderCustomer Limit
	// CouponGuessCustomer: lần nhập mã giảm giá sai của mỗi khách (SubjectKey "phone"; Task 482).
	CouponGuessCustomer Limit
	// CouponGuessIP: lần nhập mã giảm giá sai của mỗi IP khách (SubjectKey "ip"; IPv6 theo /64), hoặc của mỗi khóa dịch
	// vụ khi website không chuyển IP (Task 482).
	CouponGuessIP Limit
}

// DefaultLimits trả số của QĐ23: khóa dịch vụ 1.200/phút, khách 10 lượt bấm mua/10 s, IP 30/10 s, khách 20 đơn
// web/phút; đoán mã (Task 482): khách 10 lần sai/15 phút, IP 30 lần sai/15 phút; FallbackMax (Valkey ngừng) là một nửa.
func DefaultLimits() Limits {
	return Limits{
		WebKey:           Limit{Name: "web_key", Max: 1200, Window: time.Minute, FallbackMax: 600},         //nolint:mnd // QĐ23
		FlashCustomer:    Limit{Name: "flash_customer", Max: 10, Window: 10 * time.Second, FallbackMax: 5}, //nolint:mnd // QĐ23
		FlashIP:          Limit{Name: "flash_ip", Max: 30, Window: 10 * time.Second, FallbackMax: 15},      //nolint:mnd // QĐ23
		WebOrderCustomer: Limit{Name: "web_order_customer", Max: 20, Window: time.Minute, FallbackMax: 10}, //nolint:mnd // QĐ23
		CouponGuessCustomer: Limit{
			Name: "coupon_guess_customer", Max: 10, Window: 15 * time.Minute, FallbackMax: 5, //nolint:mnd // Task 482
		},
		CouponGuessIP: Limit{Name: "coupon_guess_ip", Max: 30, Window: 15 * time.Minute, FallbackMax: 15}, //nolint:mnd // Task 482
	}
}

// limitOverride là một mục của RATE_LIMITS_JSON; trường vắng giữ số mặc định.
type limitOverride struct {
	Max         *int `json:"max"`
	WindowS     *int `json:"window_s"`
	FallbackMax *int `json:"fallback_max"`
}

// ParseLimits đọc RATE_LIMITS_JSON: {"web_key": {"max": 1500, "window_s": 60, "fallback_max": 700}, …}, chỉ ghi tên
// cần đè (web_key, flash_customer, flash_ip, web_order_customer, coupon_guess_customer, coupon_guess_ip), trường vắng
// giữ mặc định. Rỗng → DefaultLimits.
// Tên lạ, trường lạ, số không nguyên, giới hạn sau khi đè không hợp lệ (xem Limit) → ErrBadLimits.
func ParseLimits(raw string) (Limits, error) {
	ls := DefaultLimits()
	if strings.TrimSpace(raw) == "" {
		return ls, nil
	}
	var in map[string]*limitOverride
	dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return Limits{}, fmt.Errorf("%w: %w", ErrBadLimits, err)
	}
	if dec.More() {
		return Limits{}, fmt.Errorf("%w: thừa dữ liệu sau JSON", ErrBadLimits)
	}
	byName := map[string]*Limit{
		ls.WebKey.Name: &ls.WebKey, ls.FlashCustomer.Name: &ls.FlashCustomer,
		ls.FlashIP.Name: &ls.FlashIP, ls.WebOrderCustomer.Name: &ls.WebOrderCustomer,
		ls.CouponGuessCustomer.Name: &ls.CouponGuessCustomer, ls.CouponGuessIP.Name: &ls.CouponGuessIP,
	}
	for name, o := range in {
		l, ok := byName[name]
		if !ok {
			return Limits{}, fmt.Errorf("%w: tên %q lạ", ErrBadLimits, name)
		}
		if o == nil {
			return Limits{}, fmt.Errorf("%w: %s là null", ErrBadLimits, name)
		}
		if o.Max != nil {
			l.Max = *o.Max
		}
		if o.FallbackMax != nil {
			l.FallbackMax = *o.FallbackMax
		}
		if o.WindowS != nil {
			if *o.WindowS <= 0 || time.Duration(*o.WindowS) > maxWindow/time.Second {
				return Limits{}, fmt.Errorf("%w: %s.window_s ngoài [1, 3600]", ErrBadLimits, name)
			}
			l.Window = time.Duration(*o.WindowS) * time.Second
		}
		if !l.valid() {
			return Limits{}, fmt.Errorf("%w: %s cần 1 ≤ fallback_max ≤ max, window_s trong [1, 3600]", ErrBadLimits, name)
		}
	}
	return ls, nil
}
