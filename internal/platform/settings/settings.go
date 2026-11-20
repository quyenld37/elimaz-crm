// Package settings giữ cài đặt theo tenant dạng khóa → object JSON, có version (bảng platform.settings, kế hoạch M3
// Task M3-04; Q-35, B17; spec 05 mục 5). Module chủ khóa (vd. fulfillment, khóa "fulfillment") định nghĩa kiểu Go,
// mặc định và luật kiểm của cài đặt mình; gói này chỉ đọc, ghi.
//
// Get trả mặc định khi chưa có dòng (version 0); khóa thiếu trong JSON đã lưu giữ giá trị mặc định (map gộp theo
// khóa). Put chèn (version 0) hoặc sửa khi version còn khớp, lệch → ErrVersionConflict. Mọi lời gọi chạy trong
// transaction có ngữ cảnh tenant (tenancy.WithTenant / WriteTx): RLS chọn dòng của tenant. Thứ tự khóa: ADR-46 nhóm
// 12c — chỉ sửa một mình trong transaction của lệnh PUT cài đặt.
//
// Khi nào không dùng: cài đặt có cột riêng, có CHECK hay khóa ngoại cần database kiểm (dùng bảng riêng của module,
// như ord.order_settings); dữ liệu nghiệp vụ, danh sách lớn, bí mật (khóa API mã hóa bằng fieldcrypt ở bảng của module).
package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/platform/internal/store"
)

var (
	// ErrVersionConflict báo version gửi lên không khớp dòng hiện có (hay chèn mới khi đã có dòng).
	ErrVersionConflict = errors.New("settings: version không khớp")
	// ErrInvalid báo khóa sai dạng, version âm, giá trị không phải object JSON, người ghi không phải uuid.
	ErrInvalid = errors.New("settings: đầu vào sai")
)

// keyPattern khớp CHECK của platform.settings.key.
var keyPattern = regexp.MustCompile(`^[a-z][a-z0-9_.]{0,63}$`)

// Get đọc cài đặt key của tenant trong tx: chưa có dòng → bản sao của def, version 0; có dòng → def phủ bởi JSON đã
// lưu (trường, khóa map thiếu giữ mặc định), version của dòng. def không bị sửa.
func Get[T any](ctx context.Context, tx pgx.Tx, key string, def T) (T, int64, error) { //nolint:ireturn // trả đúng kiểu cài đặt của bên gọi (tham số kiểu), không phải interface
	var out T
	if !keyPattern.MatchString(key) {
		return out, 0, fmt.Errorf("đọc cài đặt %q: %w", key, ErrInvalid)
	}
	base, err := json.Marshal(def)
	if err != nil {
		return out, 0, fmt.Errorf("chép mặc định của cài đặt %s: %w", key, err)
	}
	if err := json.Unmarshal(base, &out); err != nil {
		return out, 0, fmt.Errorf("chép mặc định của cài đặt %s: %w", key, err)
	}
	row, err := store.New(tx).GetSetting(ctx, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, 0, nil
	}
	if err != nil {
		return out, 0, fmt.Errorf("đọc cài đặt %s: %w", key, err)
	}
	if err := json.Unmarshal(row.Value, &out); err != nil {
		return out, 0, fmt.Errorf("giải cài đặt %s: %w", key, err)
	}
	return out, row.Version, nil
}

// Put ghi v (mã hóa JSON, phải là object) vào khóa key của tenant trong tx: version 0 chèn mới, version > 0 sửa dòng
// có đúng version đó (version tăng 1). Đã có dòng khi chèn, hoặc version lệch → ErrVersionConflict. actor là mã nhân
// viên ghi (rỗng: hệ thống).
func Put(ctx context.Context, tx pgx.Tx, key string, version int64, v any, actor string) error {
	if !keyPattern.MatchString(key) || version < 0 {
		return fmt.Errorf("ghi cài đặt %q: %w", key, ErrInvalid)
	}
	value, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("mã hóa cài đặt %s: %w", key, err)
	}
	if len(value) == 0 || value[0] != '{' {
		return fmt.Errorf("ghi cài đặt %s: giá trị không phải object: %w", key, ErrInvalid)
	}
	var by pgtype.UUID
	if actor != "" {
		id, err := uuid.Parse(actor)
		if err != nil {
			return fmt.Errorf("ghi cài đặt %s: người ghi: %w", key, ErrInvalid)
		}
		by = pgtype.UUID{Bytes: id, Valid: true}
	}
	q := store.New(tx)
	if version == 0 {
		_, err = q.InsertSetting(ctx, store.InsertSettingParams{Key: key, Value: value, UpdatedBy: by})
	} else {
		_, err = q.UpdateSetting(ctx, store.UpdateSettingParams{Key: key, Value: value, UpdatedBy: by, Version: version})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("ghi cài đặt %s: %w", key, ErrVersionConflict)
	}
	if err != nil {
		return fmt.Errorf("ghi cài đặt %s: %w", key, err)
	}
	return nil
}
