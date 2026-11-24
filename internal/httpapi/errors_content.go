package httpapi

// Bảng đổi lỗi của vùng content (banner trang chủ, Task 453) sang mã HTTP (luật Task 441: thêm lỗi cho vùng chỉ sửa
// tệp này, x-error-codes của openapi.yaml và errors_snapshot_test.go).

import (
	"net/http"

	"elimaz.com.vn/crm/internal/modules/content"
)

// Mã lỗi chỉ vùng content trả.
const (
	codeTooManyBanners         = "too_many_banners"
	codeInvalidBanner          = "invalid_banner"
	codeBannerCampaignRequired = "banner_campaign_required"
	codeBannerCategoryRequired = "banner_category_required"
	codeBannerImageRequired    = "banner_image_required"
	codeBannerWindowInvalid    = "banner_window_invalid"
	codeTooManyPins            = "too_many_pins"
	codePinNotInSource         = "pin_not_in_source"
	codeBannerImageInvalid     = "banner_image_invalid"
)

// contentError đổi lỗi nghiệp vụ của banner trang chủ sang mã HTTP: revision cũ → 409 version_conflict; luật của
// danh sách → 422 với mã riêng (ảnh bị từ chối dùng file_rejected chung với ảnh yêu cầu về đơn).
func contentError(err error) (status int, code string, ok bool) {
	return matchError(err, []errorMapping{
		{content.ErrRevisionConflict, http.StatusConflict, codeVersionConflict},
		{content.ErrTooManyBanners, http.StatusUnprocessableEntity, codeTooManyBanners},
		{content.ErrInvalidBanner, http.StatusUnprocessableEntity, codeInvalidBanner},
		{content.ErrCampaignRequired, http.StatusUnprocessableEntity, codeBannerCampaignRequired},
		{content.ErrCategoryRequired, http.StatusUnprocessableEntity, codeBannerCategoryRequired},
		{content.ErrImageRequired, http.StatusUnprocessableEntity, codeBannerImageRequired},
		{content.ErrWindowInvalid, http.StatusUnprocessableEntity, codeBannerWindowInvalid},
		{content.ErrTooManyPins, http.StatusUnprocessableEntity, codeTooManyPins},
		{content.ErrPinNotInSource, http.StatusUnprocessableEntity, codePinNotInSource},
		{content.ErrFileRejected, http.StatusUnprocessableEntity, codeFileRejected},
		{content.ErrImageInvalid, http.StatusUnprocessableEntity, codeBannerImageInvalid},
	})
}
