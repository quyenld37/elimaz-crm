// Package customer là hồ sơ khách (spec 05 mục 1). Bản M1 (kế hoạch M1, Task M1-30, lát mỏng Q-X1) chỉ có tạo khách
// mới với họ tên; M2 mở rộng bảng customer.customers bằng ALTER (mã khách, danh tính, địa chỉ, gộp trùng).
//
// Khi nào không dùng: module khác không đọc bảng customer.* (đọc view customer.customers_v); không ghi tên khách vào
// log, audit, sự kiện (dữ liệu cá nhân).
package customer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer/internal/store"
)

// ErrInvalidInput là lỗi khi họ tên rỗng, quá 120 ký tự hay có ký tự điều khiển. Lớp HTTP đổi sang mã ở internal/httpapi/errors.go.
var ErrInvalidInput = errors.New("customer: dữ liệu khách không hợp lệ")

const maxNameRunes = 120

// Lifecycle là giai đoạn của hồ sơ khách (cột customers.lifecycle, 05 mục 1): lead khi mới nhắn hay được tạo, customer
// từ đơn đầu tiên.
type Lifecycle string

// Giá trị của Lifecycle; khớp CHECK customers_lifecycle_check (migration 0201).
const (
	LifecycleLead     Lifecycle = "lead"
	LifecycleCustomer Lifecycle = "customer"
)

// IdentityKind là loại danh tính của khách (cột customer_identities.kind, 05 mục 1).
type IdentityKind string

// Giá trị của IdentityKind; khớp CHECK của customer_identities.kind (migration 0201). fb_psid có scope là mã Page.
const (
	IdentityPhone      IdentityKind = "phone"
	IdentityFBPSID     IdentityKind = "fb_psid"
	IdentityZaloUID    IdentityKind = "zalo_uid"
	IdentityEmail      IdentityKind = "email"
	IdentityWebAccount IdentityKind = "web_account"
)

// Create tạo khách mới (hồ sơ chưa gộp: root_id = id) trong tx của bên gọi (tenancy.WithTenant) và trả mã khách.
// fullName được bỏ khoảng trắng hai đầu; phải 1–120 ký tự, không ký tự điều khiển.
func Create(ctx context.Context, tx pgx.Tx, fullName string) (uuid.UUID, error) {
	name := strings.TrimSpace(fullName)
	if n := utf8.RuneCountInString(name); n == 0 || n > maxNameRunes || !utf8.ValidString(name) ||
		strings.ContainsFunc(name, isControl) {
		return uuid.Nil, ErrInvalidInput
	}
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, fmt.Errorf("sinh mã khách: %w", err)
	}
	if err := store.New(tx).InsertCustomer(ctx, store.InsertCustomerParams{ID: id, FullName: name}); err != nil {
		return uuid.Nil, fmt.Errorf("ghi khách: %w", err)
	}
	return id, nil
}

func isControl(r rune) bool { return r < 0x20 || (r >= 0x7f && r <= 0x9f) }
