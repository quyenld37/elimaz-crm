package order

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/order/internal/store"
)

// Khóa cố định của nguồn đơn hệ thống (B12): mốc sau tìm nguồn theo khóa (SourceByKey), không theo tên — nhân viên đổi
// tên nguồn được mà không làm mất khóa.
const (
	SourceKeyManual      = "manual"
	SourceKeyWeb         = "web"
	SourceKeyMessenger   = "messenger"
	SourceKeyComment     = "comment"
	SourceKeyLiveNoon    = "live_noon"
	SourceKeyLiveEvening = "live_evening"
	SourceKeyZalo        = "zalo"
	SourceKeyPhone       = "phone"
	SourceKeyExchange    = "exchange"
	SourceKeyReorder     = "reorder"
)

// ErrTrafficSourceNotFound là lỗi khi không có nguồn đơn mang khóa này trong tenant (chưa chạy EnsureDefaults).
var ErrTrafficSourceNotFound = errors.New("order: không có nguồn đơn")

// defaultSources: nguồn đơn mặc định theo thứ tự hiện (position 1…).
func defaultSources() [10][2]string {
	return [10][2]string{
		{SourceKeyManual, "Nhập tay"},
		{SourceKeyWeb, "Website"},
		{SourceKeyMessenger, "Messenger"},
		{SourceKeyComment, "Comment"},
		{SourceKeyLiveNoon, "Live trưa"},
		{SourceKeyLiveEvening, "Live tối"},
		{SourceKeyZalo, "Zalo"},
		{SourceKeyPhone, "Điện thoại"},
		{SourceKeyExchange, "Đổi trả"},
		{SourceKeyReorder, "Lên lại đơn"},
	}
}

// EnsureDefaults tạo (nếu chưa có) dòng cài đặt đơn của tenant (mặc định của bảng: hạn đơn nháp 1440 phút, cửa sổ nghi
// trùng 1440 phút, ngưỡng duyệt giảm 10%, cảnh báo hoàn 7 ngày) và 10 nguồn đơn hệ thống có khóa cố định. Chạy lại
// được: nguồn đã có khóa (kể cả đã đổi tên), hay trùng tên một nguồn nhân viên tạo, thì bỏ qua. Chạy trong
// tenancy.WithTenant của tenantID (seed-dev).
func EnsureDefaults(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID) error {
	q := store.New(tx)
	if err := q.EnsureOrderSettings(ctx, tenantID); err != nil {
		return fmt.Errorf("tạo cài đặt đơn: %w", err)
	}
	for i, s := range defaultSources() {
		if err := q.InsertTrafficSourceIfMissing(ctx, store.InsertTrafficSourceIfMissingParams{
			TenantID: tenantID, Key: s[0], Name: s[1], Position: int32(i + 1),
		}); err != nil {
			return fmt.Errorf("tạo nguồn đơn %s: %w", s[0], err)
		}
	}
	return nil
}

// SourceByKey trả mã nguồn đơn có khóa key (SourceKey…) của tenant đang chạy; không có → ErrTrafficSourceNotFound.
// Cho M4 (nguồn theo Page), M5 (đơn web).
func SourceByKey(ctx context.Context, tx pgx.Tx, key string) (uuid.UUID, error) {
	id, err := store.New(tx).TrafficSourceByKey(ctx, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrTrafficSourceNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("đọc nguồn đơn: %w", err)
	}
	return id, nil
}
