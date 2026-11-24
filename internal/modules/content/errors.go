package content

import "errors"

// Lỗi nghiệp vụ của banner trang chủ (Task 453). Lỗi kiểm một banner bọc kèm số thứ tự "banner <n>: …".
var (
	// ErrRevisionConflict: revision gửi lên khác revision hiện tại của danh sách (người khác vừa lưu).
	ErrRevisionConflict = errors.New("content: danh sách banner đã đổi, tải lại")
	// ErrTooManyBanners: quá MaxBanners banner.
	ErrTooManyBanners = errors.New("content: quá số banner tối đa")
	// ErrInvalidBanner: banner sai dạng (mã rỗng hay trùng, loại, màu, đích lạ, chữ quá dài, ghim trùng).
	ErrInvalidBanner = errors.New("content: banner không hợp lệ")
	// ErrCampaignRequired: banner chiến dịch chưa chọn chiến dịch.
	ErrCampaignRequired = errors.New("content: banner chiến dịch chưa chọn chiến dịch")
	// ErrCategoryRequired: banner danh mục chưa chọn danh mục.
	ErrCategoryRequired = errors.New("content: banner danh mục chưa chọn danh mục")
	// ErrImageRequired: banner ảnh thiếu ảnh máy tính, mô tả ảnh hay nơi đến khi bấm.
	ErrImageRequired = errors.New("content: banner ảnh thiếu ảnh, mô tả hay nơi đến")
	// ErrWindowInvalid: giờ kết thúc không sau giờ bắt đầu.
	ErrWindowInvalid = errors.New("content: giờ kết thúc phải sau giờ bắt đầu")
	// ErrTooManyPins: ghim quá MaxPins sản phẩm.
	ErrTooManyPins = errors.New("content: ghim quá số sản phẩm tối đa")
	// ErrPinNotInSource: sản phẩm ghim không đang bán hay không thuộc nguồn của banner (chiến dịch, danh mục).
	ErrPinNotInSource = errors.New("content: sản phẩm ghim không thuộc nguồn của banner")
	// ErrFileRejected: ảnh banner bị kho tệp từ chối (không phải ảnh, quá nhỏ, quá lớn…).
	ErrFileRejected = errors.New("content: ảnh banner bị từ chối")
	// ErrImageInvalid: ảnh banner không có, đã xóa hay không phải ảnh tải lên cho banner.
	ErrImageInvalid = errors.New("content: ảnh banner không dùng được")
)
