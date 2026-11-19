package customer

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// Lỗi nghiệp vụ của customer khai ở M2 (kế hoạch M2, Task M2-02 khai một lần; task sau chỉ dùng). ErrInvalidInput của
// M1 nằm ở customer.go. Lớp HTTP đổi sang mã ở internal/httpapi/errors.go. Không lỗi nào mang SĐT, tên, địa chỉ.
var (
	// ErrNotFound: không có khách (hay danh tính, địa chỉ, nhãn, lần gộp, lần nhập), kể cả của tenant khác.
	ErrNotFound = errors.New("customer: không có")
	// ErrVersionConflict: version gửi lên khác version hiện tại.
	ErrVersionConflict = errors.New("customer: đã đổi phiên bản")
	// ErrInvalidPhone: SĐT không chuẩn hóa được về dạng +84….
	ErrInvalidPhone = errors.New("customer: SĐT không hợp lệ")
	// ErrIdentityTaken: danh tính (SĐT, …) đã thuộc hồ sơ khác; lỗi cụ thể là *IdentityTakenError.
	ErrIdentityTaken = errors.New("customer: danh tính đã thuộc khách khác")
	// ErrWardNotInProvince: xã/phường không thuộc tỉnh đã chọn.
	ErrWardNotInProvince = errors.New("customer: xã không thuộc tỉnh")
	// ErrUnknownAdminUnit: mã tỉnh hay xã không có trong danh mục.
	ErrUnknownAdminUnit = errors.New("customer: không có đơn vị hành chính")
	// ErrQueryTooShort: chuỗi tìm quá ngắn.
	ErrQueryTooShort = errors.New("customer: chuỗi tìm quá ngắn")
	// ErrMergeSameCustomer: hai hồ sơ đã chung một gốc.
	ErrMergeSameCustomer = errors.New("customer: hai hồ sơ đã là một khách")
	// ErrMergeNotLatest: còn lần gộp sau chạm cùng hồ sơ phải hoàn tác trước (Q14); lỗi cụ thể là *MergeNotLatestError.
	ErrMergeNotLatest = errors.New("customer: lần gộp không còn là lần gần nhất")
	// ErrMergeWindowExpired: quá 30 ngày kể từ lần gộp.
	ErrMergeWindowExpired = errors.New("customer: quá hạn hoàn tác gộp")
	// ErrImportTooLarge: tệp nhập vượt dung lượng hay số dòng cho phép.
	ErrImportTooLarge = errors.New("customer: tệp nhập quá lớn")
	// ErrImportFileInvalid: tệp không ở trạng thái đã tải, sai mục đích hoặc không đọc được.
	ErrImportFileInvalid = errors.New("customer: tệp nhập không dùng được")
	// ErrInvalidTagRule: quy tắc nhãn có trường hay phép so sánh không hỗ trợ.
	ErrInvalidTagRule = errors.New("customer: quy tắc nhãn không hợp lệ")
	// ErrTagNameTaken: đã có nhãn khách cùng tên trong shop (Task M2-12).
	ErrTagNameTaken = errors.New("customer: đã có nhãn cùng tên")
	// ErrTagRuleManaged: nhãn trên khách do quy tắc gắn, không gỡ tay được (Task M2-12; quy tắc: Task M2-25).
	ErrTagRuleManaged = errors.New("customer: nhãn do quy tắc quản")
)

// IdentityTakenError là chi tiết của ErrIdentityTaken: gốc của hồ sơ đang giữ danh tính (chỉ mã, không giá trị).
type IdentityTakenError struct {
	OwnerRootID uuid.UUID
}

func (e *IdentityTakenError) Error() string {
	return fmt.Sprintf("customer: danh tính đã thuộc khách %s", e.OwnerRootID)
}

// Is cho errors.Is(err, ErrIdentityTaken).
func (e *IdentityTakenError) Is(target error) bool { return target == ErrIdentityTaken }

// MergeNotLatestError là chi tiết của ErrMergeNotLatest: lần gộp phải hoàn tác trước.
type MergeNotLatestError struct {
	BlockingMergeID uuid.UUID
}

func (e *MergeNotLatestError) Error() string {
	return fmt.Sprintf("customer: phải hoàn tác lần gộp %s trước", e.BlockingMergeID)
}

// Is cho errors.Is(err, ErrMergeNotLatest).
func (e *MergeNotLatestError) Is(target error) bool { return target == ErrMergeNotLatest }
