package customer

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer/internal/store"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// HasPhone báo SĐT p (dạng nhập bất kỳ NormalizePhone nhận) có phải danh tính phone của một hồ sơ trong nhóm gộp của
// khách customerID không (kế hoạch M5, Task 202, A15: website hoàn tất đơn nháp flash sale bằng SĐT). SĐT sai dạng,
// không ai giữ, hay khách không có → false, nil. Chỉ đọc, không khóa.
func HasPhone(ctx context.Context, tx pgx.Tx, customerID uuid.UUID, p logging.Phone) (bool, error) {
	phone, err := NormalizePhone(string(p))
	if err != nil {
		return false, nil //nolint:nilerr // SĐT sai dạng không khớp khách nào
	}
	o, found, err := identityOwner(ctx, tx, IdentityPhone, "", string(phone))
	if err != nil || !found {
		return false, err
	}
	root, err := store.New(tx).GetRootOf(ctx, customerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("đọc gốc của khách: %w", err)
	}
	return root == o.rootID, nil
}
