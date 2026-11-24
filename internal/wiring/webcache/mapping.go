// Package webcache giữ bộ đệm đọc của website (cache.ReadCache) khớp với PostgreSQL (kế hoạch M5 Task 185, QĐ14):
// Consumer (chạy ở worker) đổi sự kiện của module thành báo hiệu hệ thống "cache.invalidate" trong transaction của
// sự kiện; Invalidator (chạy ở api) nhận báo hiệu sau khi commit và xóa khóa Valkey, ngoài mọi transaction (ADR-46).
//
// Khi nào không dùng: không gọi Valkey từ Consumer hay từ transaction nghiệp vụ; không dùng báo hiệu này để báo màn
// hình nhân viên (báo hiệu hệ thống không bao giờ tới SSE). Gói chỉ được cmd/ nhập (luật kiến trúc (m)).
package webcache

import "elimaz.com.vn/crm/internal/platform/cache"

// TopicInvalidate là chủ đề của báo hiệu hệ thống mà Consumer phát và Invalidator nghe.
const TopicInvalidate = "cache.invalidate"

// Loại đối tượng trong Entity.Type của báo hiệu TopicInvalidate.
const (
	EntityProduct         = "product"
	EntityVariant         = "variant"
	EntityFlashSale       = "flash_sale"
	EntityFlashItem       = "flash_item"
	EntityCarrierSettings = "carrier_settings"
	EntityWebCampaign     = "web_campaign"
	EntityWebIntake       = "web_intake"
)

// action là một việc trên bộ đệm: xóa đúng mã của đối tượng trong space (byID), hoặc làm mới cả space.
type action struct {
	space cache.Space
	byID  bool
}

// actionsFor trả các việc trên bộ đệm cho một loại đối tượng; loại chưa biết thì nil.
func actionsFor(entityType string) []action {
	switch entityType {
	case EntityProduct:
		return []action{{cache.SpaceProduct, true}, {cache.SpaceProductsPage, false}}
	case EntityVariant:
		return []action{{cache.SpaceAvailability, true}}
	case EntityFlashSale, EntityFlashItem:
		return []action{{cache.SpaceFlash, false}}
	case EntityWebCampaign:
		return []action{{cache.SpaceCampaigns, false}}
	case EntityWebIntake:
		return []action{{cache.SpaceWebIntake, false}}
	case EntityCarrierSettings:
		return []action{{cache.SpaceCarriers, false}, {cache.SpaceShipQuote, false}}
	}
	return nil
}
