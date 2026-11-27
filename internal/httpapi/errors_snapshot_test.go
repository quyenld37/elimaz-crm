package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"elimaz.com.vn/crm/internal/httpapi/web"
	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/content"
	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/queries"
	"elimaz.com.vn/crm/internal/queries/webcatalog"
	"elimaz.com.vn/crm/internal/queries/weborders"
)

// Task 441: bảng ánh xạ lỗi tách theo vùng (errors_<vùng>.go). Hai bảng dưới đây chụp hành vi của errors.go TRƯỚC khi
// tách (sinh từ tệp cũ, đã chạy qua trên mã cũ); sau khi tách phải ra y hệt. Thêm lỗi mới thì thêm dòng vào đây.

// TestDomainErrorSnapshot: mọi sentinel đã ánh xạ ra đúng status và mã lỗi qua writeDomainError, cả khi để trần và khi
// đã bọc một lần bằng %w. Sentinel có mặt ở nhiều bảng (errForbiddenTarget, ErrStockShort cùng giá trị) ghi kết quả
// của bảng được kiểm trước — tức là cũng chụp thứ tự kiểm giữa các bảng.
func TestDomainErrorSnapshot(t *testing.T) {
	t.Parallel()
	cases := domainErrorSnapshot()
	if len(cases) < 150 {
		t.Fatalf("bảng chụp chỉ có %d dòng; đã mất dòng?", len(cases))
	}
	for _, c := range cases {
		for _, err := range []error{c.err, fmt.Errorf("việc thử: %w", c.err)} {
			checkDomainError(t, c.name, err, c.status, c.code)
		}
	}
}

// TestDomainErrorSnapshot_TableMembership: mỗi sentinel vẫn nằm trong đúng hàm ánh xạ của vùng cũ (handler dùng thẳng
// catalogError, customerError, … để quyết định lỗi nào là kết quả nghiệp vụ được lưu khi chống trùng), và hàm đó trả
// đúng dòng của chính nó.
func TestDomainErrorSnapshot_TableMembership(t *testing.T) {
	t.Parallel()
	cases := domainErrorMembership()
	for _, c := range cases {
		if _, _, ok := c.mapper(fmt.Errorf("việc thử: %w", c.err)); !ok {
			t.Errorf("%s không còn nhận %v", c.name, c.err)
		}
	}
	// Lỗi lạ không thuộc vùng nào.
	if _, _, ok := cashError(errors.New("lạ")); ok {
		t.Error("cashError nhận lỗi lạ")
	}
	if _, _, ok := orderError(errors.New("lạ")); ok {
		t.Error("orderError nhận lỗi lạ")
	}
}

// TestDomainErrorSnapshot_Order: lỗi khớp hai bảng (bọc cả hai sentinel) lấy kết quả của bảng đứng trước theo thứ tự cũ
// identity → catalog → files → order → customer → promotion → inventory → queries → fulfillment → cash; trong bảng
// cash, ErrPreviewStale đứng trước ErrAdjustmentBlocked, ErrAdjustTargetNotFound (Task M3-33).
func TestDomainErrorSnapshot_Order(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		first  error // lỗi của bảng kiểm trước: kết quả phải theo lỗi này
		second error
		status int
		code   string
	}{
		{"identity trước catalog", identity.ErrEmailTaken, catalog.ErrSKUTaken, http.StatusConflict, "email_taken"},
		{"catalog trước files", catalog.ErrSKUTaken, files.ErrTooLarge, http.StatusConflict, "sku_taken"},
		{"files trước order", files.ErrTooLarge, order.ErrMixedSellers, http.StatusUnprocessableEntity, "file_too_large"},
		{"order trước customer", order.ErrMixedSellers, customer.ErrInvalidPhone, http.StatusUnprocessableEntity, "mixed_sellers"},
		{"customer trước promotion", customer.ErrInvalidPhone, promotion.ErrCouponLockedAfterUse, http.StatusUnprocessableEntity, "invalid_phone"},
		{"promotion trước inventory", promotion.ErrCouponLockedAfterUse, inventory.ErrStocktakeOpen, http.StatusConflict, "coupon_locked_after_use"},
		{"inventory trước queries", inventory.ErrStocktakeOpen, queries.ErrInvalidInput, http.StatusConflict, "stocktake_open"},
		{"queries trước fulfillment", queries.ErrInvalidInput, fulfillment.ErrQuoteStale, http.StatusBadRequest, "invalid_input"},
		{"fulfillment trước cash", fulfillment.ErrQuoteStale, cash.ErrStatementChanged, http.StatusConflict, "quote_stale"},
		{"cash trước payments", cash.ErrStatementChanged, cash.ErrCODLockedByShipment, http.StatusConflict, "statement_changed"},
		{"preview_stale trước adjustment_blocked", cash.ErrPreviewStale, cash.ErrAdjustmentBlocked, http.StatusConflict, "preview_stale"},
		{"preview_stale trước adjust_target_not_found", cash.ErrPreviewStale, cash.ErrAdjustTargetNotFound, http.StatusConflict, "preview_stale"},
	}
	for _, c := range cases {
		for _, err := range []error{
			fmt.Errorf("%w: %w", c.first, c.second), fmt.Errorf("%w: %w", c.second, c.first),
		} {
			checkDomainError(t, c.name, err, c.status, c.code)
		}
	}
}

// checkDomainError: writeDomainError đổi err sang đúng status và mã lỗi code.
func checkDomainError(t *testing.T, name string, err error, status int, code string) {
	t.Helper()
	rec := domainErrorRecorder(t, err)
	if rec.Code != status || !strings.Contains(rec.Body.String(), `"error":"`+code+`"`) {
		t.Errorf("%s: mong %d %s, có %d %s", name, status, code, rec.Code, rec.Body.String())
	}
}

// snapshotCase: một sentinel và kết quả của nó trước khi tách.
type snapshotCase struct {
	name   string
	err    error
	status int
	code   string
}

// domainErrorSnapshot: bảng chụp từ errors.go trước Task 441 (sinh tự động từ tệp cũ, mỗi sentinel một dòng, theo thứ tự
// bảng cũ; sentinel có mặt hai lần chỉ ghi lần đầu).
//
//nolint:funlen // bảng dữ liệu, một dòng mỗi sentinel; tách nhỏ chỉ làm khó đối chiếu với bảng cũ
func domainErrorSnapshot() []snapshotCase {
	return []snapshotCase{
		{"identity.ErrSelfModify", identity.ErrSelfModify, http.StatusForbidden, "cannot_modify_self"},
		{"identity.ErrGrantExceedsActor", identity.ErrGrantExceedsActor, http.StatusForbidden, "grant_exceeds_actor"},
		{"identity.ErrLocked", identity.ErrLocked, http.StatusForbidden, "forbidden"},
		{"identity.ErrLastOwner", identity.ErrLastOwner, http.StatusConflict, "last_owner"},
		{"identity.ErrEmailTaken", identity.ErrEmailTaken, http.StatusConflict, "email_taken"},
		{"identity.ErrVersionConflict", identity.ErrVersionConflict, http.StatusConflict, "version_conflict"},
		{"identity.ErrStaffNotFound", identity.ErrStaffNotFound, http.StatusNotFound, "not_found"},
		{"identity.ErrInvalidInput", identity.ErrInvalidInput, http.StatusBadRequest, "invalid_input"},
		{"identity.ErrOwnerRoleLocked", identity.ErrOwnerRoleLocked, http.StatusConflict, "owner_role_locked"},
		{"identity.ErrRoleInUse", identity.ErrRoleInUse, http.StatusConflict, "role_in_use"},
		{"identity.ErrRoleArchived", identity.ErrRoleArchived, http.StatusConflict, "role_archived"},
		{"identity.ErrRoleNameTaken", identity.ErrRoleNameTaken, http.StatusConflict, "role_name_taken"},
		{"identity.ErrUnknownPermission", identity.ErrUnknownPermission, http.StatusUnprocessableEntity, "unknown_permission"},
		{"identity.ErrRoleNotFound", identity.ErrRoleNotFound, http.StatusNotFound, "not_found"},
		{"identity.ErrInvalidKey", identity.ErrInvalidKey, http.StatusUnauthorized, "unauthenticated"},
		{"identity.ErrRevoked", identity.ErrRevoked, http.StatusUnauthorized, "unauthenticated"},
		{"identity.ErrIPNotAllowed", identity.ErrIPNotAllowed, http.StatusForbidden, "ip_not_allowed"},
		{"identity.ErrPermissionNotForKey", identity.ErrPermissionNotForKey, http.StatusUnprocessableEntity, "permission_not_allowed_for_key"},
		{"identity.ErrServiceKeyNotFound", identity.ErrServiceKeyNotFound, http.StatusNotFound, "not_found"},
		{"catalog.ErrNotFound", catalog.ErrNotFound, http.StatusNotFound, "not_found"},
		{"catalog.ErrSKUTaken", catalog.ErrSKUTaken, http.StatusConflict, "sku_taken"},
		{"catalog.ErrBarcodeTaken", catalog.ErrBarcodeTaken, http.StatusConflict, "barcode_taken"},
		{"catalog.ErrDuplicateOption", catalog.ErrDuplicateOption, http.StatusConflict, "duplicate_option"},
		{"catalog.ErrCodeTaken", catalog.ErrCodeTaken, http.StatusConflict, "code_taken"},
		{"catalog.ErrVersionConflict", catalog.ErrVersionConflict, http.StatusConflict, "version_conflict"},
		{"catalog.ErrNotForSale", catalog.ErrNotForSale, http.StatusConflict, "not_for_sale"},
		{"catalog.ErrInvalidPrice", catalog.ErrInvalidPrice, http.StatusUnprocessableEntity, "invalid_price"},
		{"catalog.ErrInvalidWeight", catalog.ErrInvalidWeight, http.StatusUnprocessableEntity, "invalid_input"},
		{"catalog.ErrVariantOrderMismatch", catalog.ErrVariantOrderMismatch, http.StatusUnprocessableEntity, "invalid_input"},
		{"web.ErrBadQuery", web.ErrBadQuery, http.StatusBadRequest, "bad_request"},
		// Đơn web (Task 194): kiểu lỗi chi tiết của order.CreateFromWeb và giới hạn đơn theo SĐT.
		{"order.QuoteChangedError", &order.QuoteChangedError{}, http.StatusUnprocessableEntity, "quote_changed"},
		{"order.ItemUnavailableError", &order.ItemUnavailableError{}, http.StatusUnprocessableEntity, "item_unavailable"},
		{"order.CouponInvalidError", &order.CouponInvalidError{Reason: "exhausted"}, http.StatusUnprocessableEntity, "coupon_invalid"},
		{"tooFastError", &tooFastError{}, http.StatusTooManyRequests, "too_fast"},
		{"tooFastError coupon", &tooFastError{code: codeCouponAttemptsExceeded}, http.StatusTooManyRequests, "coupon_attempts_exceeded"},
		// Tạm dừng nhận đơn web (Task 456).
		{"order.ErrWebIntakePaused", order.ErrWebIntakePaused, http.StatusServiceUnavailable, "web_orders_paused"},
		// Hãng/dịch vụ khách chọn trên website (Task 490).
		{"order.ErrShipCarrierUnavailable", order.ErrShipCarrierUnavailable, http.StatusUnprocessableEntity, "shipping_carrier_unavailable"},
		{"order.ErrShipFeeMismatch", order.ErrShipFeeMismatch, http.StatusUnprocessableEntity, "shipping_fee_mismatch"},
		{"order.ErrShipUnverified", order.ErrShipUnverified, http.StatusServiceUnavailable, "retry_later"},
		{"order.ErrReasonRequired", order.ErrReasonRequired, http.StatusUnprocessableEntity, "invalid_input"},
		// Yêu cầu về đơn hàng (Task 195).
		{"order.ErrWindowClosed", order.ErrWindowClosed, http.StatusUnprocessableEntity, "window_closed"},
		{"order.ErrQtyExceeded", order.ErrQtyExceeded, http.StatusUnprocessableEntity, "qty_exceeded"},
		{"order.ErrPhotoRequired", order.ErrPhotoRequired, http.StatusUnprocessableEntity, "photo_required"},
		{"order.ErrTooManyFiles", order.ErrTooManyFiles, http.StatusUnprocessableEntity, "too_many_files"},
		{"order.ErrDuplicateOpen", order.ErrDuplicateOpen, http.StatusUnprocessableEntity, "duplicate_open"},
		{"order.ErrFileRejected", order.ErrFileRejected, http.StatusUnprocessableEntity, "file_rejected"},
		{"order.ErrFileNotFound", order.ErrFileNotFound, http.StatusUnprocessableEntity, "file_not_found"},
		{"order.ErrRequestNotAllowed", order.ErrRequestNotAllowed, http.StatusUnprocessableEntity, "request_not_allowed"},
		{"order.ErrRequestNotFound", order.ErrRequestNotFound, http.StatusNotFound, "not_found"},
		{"order.ErrRequestLimit", order.ErrRequestLimit, http.StatusTooManyRequests, "too_fast"},
		{"order.ErrCannotCancel", order.ErrCannotCancel, http.StatusUnprocessableEntity, "cannot_cancel"},
		// Banner trang chủ (Task 453).
		{"content.ErrRevisionConflict", content.ErrRevisionConflict, http.StatusConflict, "version_conflict"},
		{"content.ErrTooManyBanners", content.ErrTooManyBanners, http.StatusUnprocessableEntity, "too_many_banners"},
		{"content.ErrInvalidBanner", content.ErrInvalidBanner, http.StatusUnprocessableEntity, "invalid_banner"},
		{"content.ErrCampaignRequired", content.ErrCampaignRequired, http.StatusUnprocessableEntity, "banner_campaign_required"},
		{"content.ErrCategoryRequired", content.ErrCategoryRequired, http.StatusUnprocessableEntity, "banner_category_required"},
		{"content.ErrImageRequired", content.ErrImageRequired, http.StatusUnprocessableEntity, "banner_image_required"},
		{"content.ErrWindowInvalid", content.ErrWindowInvalid, http.StatusUnprocessableEntity, "banner_window_invalid"},
		{"content.ErrTooManyPins", content.ErrTooManyPins, http.StatusUnprocessableEntity, "too_many_pins"},
		{"content.ErrPinNotInSource", content.ErrPinNotInSource, http.StatusUnprocessableEntity, "pin_not_in_source"},
		{"content.ErrFileRejected", content.ErrFileRejected, http.StatusUnprocessableEntity, "file_rejected"},
		{"content.ErrImageInvalid", content.ErrImageInvalid, http.StatusUnprocessableEntity, "banner_image_invalid"},
		// Bộ phối (Task 454).
		{"content.ErrTooManyLooks", content.ErrTooManyLooks, http.StatusUnprocessableEntity, "too_many_looks"},
		{"content.ErrTooManyHomeLooks", content.ErrTooManyHomeLooks, http.StatusUnprocessableEntity, "too_many_home_looks"},
		{"content.ErrInvalidLook", content.ErrInvalidLook, http.StatusUnprocessableEntity, "invalid_look"},
		{"content.ErrLookItems", content.ErrLookItems, http.StatusUnprocessableEntity, "look_items_invalid"},
		{"content.ErrLookDot", content.ErrLookDot, http.StatusUnprocessableEntity, "look_dot_invalid"},
		{"content.ErrLookPhoto", content.ErrLookPhoto, http.StatusUnprocessableEntity, "look_photo_invalid"},
		{"content.ErrLookWindow", content.ErrLookWindow, http.StatusUnprocessableEntity, "look_window_invalid"},
		{"content.ErrLookNotFound", content.ErrLookNotFound, http.StatusNotFound, "not_found"},
		// Tài khoản kênh (M4, Task 145).
		{"channel.ErrAccountNotFound", channel.ErrAccountNotFound, http.StatusNotFound, "not_found"},
		{"channel.ErrVersionConflict", channel.ErrVersionConflict, http.StatusConflict, "version_conflict"},
		{"channel.ErrInvalidSendMode", channel.ErrInvalidSendMode, http.StatusBadRequest, "invalid_input"},
		{"channel.ErrOAuthStateInvalid", channel.ErrOAuthStateInvalid, http.StatusBadRequest, "oauth_state_invalid"},
		{"channel.ErrPageNotAccessible", channel.ErrPageNotAccessible, http.StatusUnprocessableEntity, "page_not_accessible"},
		{"channel.ErrPageConnectedElsewhere", channel.ErrPageConnectedElsewhere, http.StatusConflict, "page_connected_elsewhere"},
		{"channel.ErrConnectUnavailable", channel.ErrConnectUnavailable, http.StatusServiceUnavailable, "retry_later"},
		{"channel.ErrNoConnector", channel.ErrNoConnector, http.StatusServiceUnavailable, "retry_later"},
		{"channel.ErrNoKeyring", channel.ErrNoKeyring, http.StatusServiceUnavailable, "retry_later"},
		{"errConnectSource", errConnectSource, http.StatusUnprocessableEntity, "invalid_input"},
		{"conversation.ErrNotFound", conversation.ErrNotFound, http.StatusNotFound, "not_found"},
		{"conversation.ErrReminderInvalid", conversation.ErrReminderInvalid, http.StatusBadRequest, "invalid_input"},
		{"conversation.ErrCallInvalid", conversation.ErrCallInvalid, http.StatusBadRequest, "invalid_input"},
		{"conversation.ErrLiveVideoInvalid", conversation.ErrLiveVideoInvalid, http.StatusBadRequest, "invalid_input"},
		{"conversation.ErrLiveStatusInvalid", conversation.ErrLiveStatusInvalid, http.StatusBadRequest, "invalid_input"},
		{"conversation.ErrReminderClosed", conversation.ErrReminderClosed, http.StatusConflict, "version_conflict"},
		{"conversation.ErrReminderAssignForbidden", conversation.ErrReminderAssignForbidden, http.StatusForbidden, "forbidden"},
		{"conversation.ErrAssigneeNotAllowed", conversation.ErrAssigneeNotAllowed, http.StatusUnprocessableEntity, "assignee_not_allowed"},
		// Người phụ trách, nhãn, cài đặt Page (M4, Task 160).
		{"conversation.ErrTagNotFound", conversation.ErrTagNotFound, http.StatusNotFound, "not_found"},
		{"conversation.ErrVersionConflict", conversation.ErrVersionConflict, http.StatusConflict, "version_conflict"},
		{"conversation.ErrAlreadyAssigned", conversation.ErrAlreadyAssigned, http.StatusConflict, "already_assigned"},
		{"conversation.ErrTagNameTaken", conversation.ErrTagNameTaken, http.StatusConflict, "already_exists"},
		{"conversation.ErrInvalidInput", conversation.ErrInvalidInput, http.StatusUnprocessableEntity, "invalid_input"},
		// Ghi chú, mẫu trả lời (M4, Task 161).
		{"conversation.ErrNoteInvalid", conversation.ErrNoteInvalid, http.StatusBadRequest, "invalid_input"},
		{"conversation.ErrTemplateForbidden", conversation.ErrTemplateForbidden, http.StatusForbidden, "forbidden"},
		{"conversation.ErrShortcutTaken", conversation.ErrShortcutTaken, http.StatusConflict, "already_exists"},
		{"conversation.ErrTemplateVersionConflict", conversation.ErrTemplateVersionConflict, http.StatusConflict, "version_conflict"},
		{"conversation.ErrTemplateInvalid", conversation.ErrTemplateInvalid, http.StatusUnprocessableEntity, "invalid_input"},
		{"conversation.ErrUnknownVariable", conversation.ErrUnknownVariable, http.StatusUnprocessableEntity, "invalid_input"},
		{"conversation.ErrOrderNotOfCustomer", conversation.ErrOrderNotOfCustomer, http.StatusUnprocessableEntity, "invalid_input"},
		// Gửi tin (M4, Task 156).
		{"conversation.ErrSendInvalid", conversation.ErrSendInvalid, http.StatusBadRequest, "invalid_input"},
		{"conversation.ErrChannelReadOnly", conversation.ErrChannelReadOnly, http.StatusConflict, "channel_read_only"},
		{"conversation.ErrAttachmentNotReady", conversation.ErrAttachmentNotReady, http.StatusUnprocessableEntity, "attachment_not_ready"},
		{"conversation.ErrAttachmentRejected", conversation.ErrAttachmentRejected, http.StatusUnprocessableEntity, "attachment_rejected"},
		{"conversation.ErrInvalidStatus", conversation.ErrInvalidStatus, http.StatusUnprocessableEntity, "invalid_input"},
		{"conversation.ErrMessageNotRecoverable", conversation.ErrMessageNotRecoverable, http.StatusConflict, "version_conflict"},
		{"conversation.ErrPrivateReplyUsed", conversation.ErrPrivateReplyUsed, http.StatusConflict, "private_reply_used"},
		{"conversation.ErrNotCustomerComment", conversation.ErrNotCustomerComment, http.StatusUnprocessableEntity, "invalid_input"},
		// Chốt comment live (Task 172).
		{"conversation.ErrClaimInvalid", conversation.ErrClaimInvalid, http.StatusBadRequest, "invalid_input"},
		{"conversation.ErrCursorInvalid", conversation.ErrCursorInvalid, http.StatusBadRequest, "invalid_input"},
		{"conversation.ErrCommentClaimed", conversation.ErrCommentClaimed, http.StatusConflict, "comment_claimed"},
		{"conversation.ErrOpenDraftExists", conversation.ErrOpenDraftExists, http.StatusConflict, "open_draft_exists"},
		{"conversation.ErrSellerMismatch", conversation.ErrSellerMismatch, http.StatusUnprocessableEntity, "seller_mismatch"},
		{"conversation.ErrDraftNotOpen", conversation.ErrDraftNotOpen, http.StatusConflict, "version_conflict"},
		{"conversation.ErrDraftMismatch", conversation.ErrDraftMismatch, http.StatusUnprocessableEntity, "invalid_input"},
		{"conversation.ErrOrderCustomerMismatch", conversation.ErrOrderCustomerMismatch, http.StatusUnprocessableEntity, "conversation_customer_mismatch"},
		{"conversation.CommentClaimedError", &conversation.CommentClaimedError{ByName: "An"}, http.StatusConflict, "comment_claimed"},
		{"conversation.OpenDraftExistsError", &conversation.OpenDraftExistsError{}, http.StatusConflict, "open_draft_exists"},
		{"weborders.ErrBadLimit", weborders.ErrBadLimit, http.StatusBadRequest, "bad_request"},
		{"weborders.ErrNotFound", weborders.ErrNotFound, http.StatusNotFound, "not_found"},
		{"webcatalog.ErrBadLimit", webcatalog.ErrBadLimit, http.StatusBadRequest, "bad_request"},
		{"catalog.ErrInvalidArticle", catalog.ErrInvalidArticle, http.StatusUnprocessableEntity, "invalid_article"},
		{"catalog.ErrCategoryCycle", catalog.ErrCategoryCycle, http.StatusUnprocessableEntity, "category_cycle"},
		{"catalog.ErrCategoryTooDeep", catalog.ErrCategoryTooDeep, http.StatusUnprocessableEntity, "category_too_deep"},
		{"catalog.ErrCategoryInUse", catalog.ErrCategoryInUse, http.StatusConflict, "category_in_use"},
		{"catalog.ErrAlreadyExists", catalog.ErrAlreadyExists, http.StatusConflict, "already_exists"},
		{"catalog.ErrFileNotReady", catalog.ErrFileNotReady, http.StatusConflict, "file_not_ready"},
		{"catalog.ErrImportNotReady", catalog.ErrImportNotReady, http.StatusConflict, "import_not_ready"},
		{"catalog.ErrRestoreExpired", catalog.ErrRestoreExpired, http.StatusGone, "restore_expired"},
		{"catalog.ErrInvalidInput", catalog.ErrInvalidInput, http.StatusBadRequest, "invalid_input"},
		{"files.ErrTooLarge", files.ErrTooLarge, http.StatusUnprocessableEntity, "file_too_large"},
		{"files.ErrTypeNotAllowed", files.ErrTypeNotAllowed, http.StatusUnprocessableEntity, "file_type_not_allowed"},
		{"files.ErrUnknownPurpose", files.ErrUnknownPurpose, http.StatusUnprocessableEntity, "file_purpose_not_allowed"},
		{"files.ErrUploadNotAllowed", files.ErrUploadNotAllowed, http.StatusUnprocessableEntity, "file_purpose_not_allowed"},
		{"files.ErrInvalidFile", files.ErrInvalidFile, http.StatusBadRequest, "invalid_input"},
		{"files.ErrUploadMissing", files.ErrUploadMissing, http.StatusConflict, "upload_missing"},
		{"files.ErrNotFound", files.ErrNotFound, http.StatusNotFound, "not_found"},
		{"files.ErrUnknownPreset", files.ErrUnknownPreset, http.StatusUnprocessableEntity, "unknown_preset"},
		{"errForbiddenTarget", errForbiddenTarget, http.StatusForbidden, "forbidden"},
		{"inventory.ErrInsufficient", inventory.ErrInsufficient, http.StatusConflict, "insufficient_stock"},
		{"inventory.ErrStockShort", inventory.ErrStockShort, http.StatusConflict, "stock_short"},
		{"inventory.ErrNotFound", inventory.ErrNotFound, http.StatusNotFound, "not_found"},
		{"inventory.ErrVersionConflict", inventory.ErrVersionConflict, http.StatusConflict, "version_conflict"},
		{"inventory.ErrDocumentNotDraft", inventory.ErrDocumentNotDraft, http.StatusConflict, "document_not_draft"},
		{"inventory.ErrStocktakeOpen", inventory.ErrStocktakeOpen, http.StatusConflict, "stocktake_open"},
		{"inventory.ErrStocktakeNotOpen", inventory.ErrStocktakeNotOpen, http.StatusConflict, "stocktake_not_open"},
		{"inventory.ErrSupplierNameTaken", inventory.ErrSupplierNameTaken, http.StatusConflict, "supplier_name_taken"},
		{"inventory.ErrSupplierArchived", inventory.ErrSupplierArchived, http.StatusUnprocessableEntity, "supplier_archived"},
		{"inventory.ErrSupplierNotForKind", inventory.ErrSupplierNotForKind, http.StatusUnprocessableEntity, "supplier_not_for_kind"},
		{"inventory.ErrInvalidInput", inventory.ErrInvalidInput, http.StatusBadRequest, "invalid_input"},
		{"order.ErrOutOfStock", order.ErrOutOfStock, http.StatusConflict, "out_of_stock"},
		{"order.ErrNotForSale", order.ErrNotForSale, http.StatusUnprocessableEntity, "variant_not_for_sale"},
		{"order.ErrMixedSellers", order.ErrMixedSellers, http.StatusUnprocessableEntity, "mixed_sellers"},
		{"order.ErrTotalOverflow", order.ErrTotalOverflow, http.StatusUnprocessableEntity, "total_overflow"},
		{"order.ErrDuplicateOrder", order.ErrDuplicateOrder, http.StatusUnprocessableEntity, "idempotency_key_reused"},
		{"order.ErrInvalidInput", order.ErrInvalidInput, http.StatusBadRequest, "invalid_input"},
		{"order.ErrStockShort", order.ErrStockShort, http.StatusConflict, "stock_short"},
		{"order.ErrNotFound", order.ErrNotFound, http.StatusNotFound, "not_found"},
		{"customer.ErrInvalidInput", customer.ErrInvalidInput, http.StatusBadRequest, "invalid_input"},
		{"order.ErrShipmentAlreadyAttached", order.ErrShipmentAlreadyAttached, http.StatusConflict, "shipment_already_attached"},
		{"order.ErrVersionConflict", order.ErrVersionConflict, http.StatusConflict, "version_conflict"},
		{"order.ErrLockedByShipment", order.ErrLockedByShipment, http.StatusConflict, "order_locked_by_shipment"},
		{"order.ErrInvalidTransition", order.ErrInvalidTransition, http.StatusUnprocessableEntity, "invalid_transition"},
		{"order.ErrAddressIncomplete", order.ErrAddressIncomplete, http.StatusUnprocessableEntity, "address_incomplete"},
		{"order.ErrNeedsApproval", order.ErrNeedsApproval, http.StatusUnprocessableEntity, "needs_approval"},
		{"order.ErrDiscountNeedsApproval", order.ErrDiscountNeedsApproval, http.StatusForbidden, "discount_needs_approval"},
		{"order.ErrVariantNotFound", order.ErrVariantNotFound, http.StatusUnprocessableEntity, "variant_not_found"},
		{"order.ErrLineDiscountExceeds", order.ErrLineDiscountExceeds, http.StatusUnprocessableEntity, "line_discount_exceeds"},
		{"order.ErrOrderDiscountExceeds", order.ErrOrderDiscountExceeds, http.StatusUnprocessableEntity, "order_discount_exceeds"},
		{"order.ErrTrafficSourceInactive", order.ErrTrafficSourceInactive, http.StatusUnprocessableEntity, "traffic_source_inactive"},
		{"order.ErrReturnQtyExceeds", order.ErrReturnQtyExceeds, http.StatusUnprocessableEntity, "return_qty_exceeds"},
		{"order.ErrQtyMismatch", order.ErrQtyMismatch, http.StatusUnprocessableEntity, "qty_mismatch"},
		{"order.ErrReturnNotAllowed", order.ErrReturnNotAllowed, http.StatusUnprocessableEntity, "return_not_allowed"},
		{"order.ErrSettingOutOfRange", order.ErrSettingOutOfRange, http.StatusUnprocessableEntity, "setting_out_of_range"},
		{"order.ErrTrafficSourceNameTaken", order.ErrTrafficSourceNameTaken, http.StatusConflict, "already_exists"},
		{"fulfillment.ErrShipmentExists", fulfillment.ErrShipmentExists, http.StatusConflict, "shipment_exists"},
		{"fulfillment.ErrCarrierNotOnWeb", fulfillment.ErrCarrierNotOnWeb, http.StatusUnprocessableEntity, "carrier_not_on_web"},
		{"fulfillment.ErrLastWebCarrier", fulfillment.ErrLastWebCarrier, http.StatusUnprocessableEntity, "last_web_carrier"},
		{"fulfillment.ErrNoQuote", fulfillment.ErrNoQuote, http.StatusServiceUnavailable, "retry_later"},
		{"fulfillment.ErrOrderNotShippable", fulfillment.ErrOrderNotShippable, http.StatusConflict, "order_not_shippable"},
		{"fulfillment.ErrOrderNotFound", fulfillment.ErrOrderNotFound, http.StatusNotFound, "not_found"},
		{"fulfillment.ErrAccountNotFound", fulfillment.ErrAccountNotFound, http.StatusNotFound, "not_found"},
		{"fulfillment.ErrNotFound", fulfillment.ErrNotFound, http.StatusNotFound, "not_found"},
		{"fulfillment.ErrSettingOutOfRange", fulfillment.ErrSettingOutOfRange, http.StatusUnprocessableEntity, "setting_out_of_range"},
		{"fulfillment.ErrVersionConflict", fulfillment.ErrVersionConflict, http.StatusConflict, "version_conflict"},
		{"fulfillment.ErrCarrierNotSupported", fulfillment.ErrCarrierNotSupported, http.StatusUnprocessableEntity, "capability_not_supported"},
		{"fulfillment.ErrAccountUnavailable", fulfillment.ErrAccountUnavailable, http.StatusConflict, "carrier_account_unavailable"},
		{"fulfillment.ErrAccountTransition", fulfillment.ErrAccountTransition, http.StatusConflict, "invalid_transition"},
		{"fulfillment.ErrAccountTaken", fulfillment.ErrAccountTaken, http.StatusConflict, "already_exists"},
		{"fulfillment.ErrAccountInvalid", fulfillment.ErrAccountInvalid, http.StatusBadRequest, "invalid_input"},
		{"fulfillment.ErrAccountTargetNotFound", fulfillment.ErrAccountTargetNotFound, http.StatusNotFound, "not_found"},
		{"fulfillment.ErrInvalidPage", fulfillment.ErrInvalidPage, http.StatusBadRequest, "invalid_input"},
		{"fulfillment.ErrInvalidTracking", fulfillment.ErrInvalidTracking, http.StatusBadRequest, "invalid_input"},
		{"fulfillment.ErrStockShort", fulfillment.ErrStockShort, http.StatusConflict, "stock_short"},
		{"fulfillment.ErrQuoteStale", fulfillment.ErrQuoteStale, http.StatusConflict, "quote_stale"},
		{"fulfillment.ErrAddressUnmapped", fulfillment.ErrAddressUnmapped, http.StatusUnprocessableEntity, "address_unmapped"},
		{"fulfillment.ErrCreateInProgress", fulfillment.ErrCreateInProgress, http.StatusConflict, "create_in_progress"},
		{"fulfillment.ErrShipInvalid", fulfillment.ErrShipInvalid, http.StatusBadRequest, "invalid_input"},
		{"fulfillment.ErrTrackingCodeConflict", fulfillment.ErrTrackingCodeConflict, http.StatusConflict, "tracking_code_conflict"},
		{"fulfillment.ErrShipmentNotCancellable", fulfillment.ErrShipmentNotCancellable, http.StatusConflict, "shipment_not_cancellable"},
		{"fulfillment.ErrPartialNotAllowed", fulfillment.ErrPartialNotAllowed, http.StatusConflict, "invalid_transition"},
		{"fulfillment.ErrPartialQty", fulfillment.ErrPartialQty, http.StatusUnprocessableEntity, "qty_mismatch"},
		{"fulfillment.ErrPartialCOD", fulfillment.ErrPartialCOD, http.StatusUnprocessableEntity, "payment_exceeds_total"},
		{"fulfillment.ErrManualReasonRequired", fulfillment.ErrManualReasonRequired, http.StatusUnprocessableEntity, "invalid_input"},
		{"fulfillment.ErrManualNotAllowed", fulfillment.ErrManualNotAllowed, http.StatusConflict, "invalid_transition"},
		{"fulfillment.ErrLabelBatchInvalid", fulfillment.ErrLabelBatchInvalid, http.StatusBadRequest, "invalid_input"},
		{"fulfillment.ErrAddressChangeOpen", fulfillment.ErrAddressChangeOpen, http.StatusConflict, "already_exists"},
		{"fulfillment.ErrAddressChangeNotAllowed", fulfillment.ErrAddressChangeNotAllowed, http.StatusConflict, "invalid_transition"},
		{"fulfillment.ErrAnomalyNotFound", fulfillment.ErrAnomalyNotFound, http.StatusNotFound, "not_found"},
		{"fulfillment.ErrAnomalyNotResolvable", fulfillment.ErrAnomalyNotResolvable, http.StatusConflict, "invalid_transition"},
		{"fulfillment.ErrCallbackNotFound", fulfillment.ErrCallbackNotFound, http.StatusNotFound, "not_found"},
		{"fulfillment.ErrCallbackClosed", fulfillment.ErrCallbackClosed, http.StatusConflict, "invalid_transition"},
		{"fulfillment.ErrCallbackAssignee", fulfillment.ErrCallbackAssignee, http.StatusUnprocessableEntity, "invalid_input"},
		{"cash.ErrCarrierAccountNotFound", cash.ErrCarrierAccountNotFound, http.StatusNotFound, "not_found"},
		{"cash.ErrStatementNotFound", cash.ErrStatementNotFound, http.StatusNotFound, "not_found"},
		{"cash.ErrStatementChanged", cash.ErrStatementChanged, http.StatusConflict, "statement_changed"},
		{"cash.ErrShipmentNotFound", cash.ErrShipmentNotFound, http.StatusNotFound, "not_found"},
		{"cash.ErrUnknownStatementFormat", cash.ErrUnknownStatementFormat, http.StatusUnprocessableEntity, "invalid_input"},
		{"cash.ErrStatementFileNotReady", cash.ErrStatementFileNotReady, http.StatusConflict, "file_not_ready"},
		{"cash.ErrInvalidInput", cash.ErrInvalidInput, http.StatusBadRequest, "invalid_input"},
		{"cash.ErrPreviewStale", cash.ErrPreviewStale, http.StatusConflict, "preview_stale"},
		{"cash.ErrAdjustReasonRequired", cash.ErrAdjustReasonRequired, http.StatusUnprocessableEntity, "invalid_input"},
		{"cash.ErrAdjustmentInvalid", cash.ErrAdjustmentInvalid, http.StatusUnprocessableEntity, "invalid_input"},
		{"cash.ErrAdjustTargetNotFound", cash.ErrAdjustTargetNotFound, http.StatusNotFound, "not_found"},
		{"cash.ErrAdjustmentBlocked", cash.ErrAdjustmentBlocked, http.StatusConflict, "invalid_transition"},
		{"cash.ErrAlreadyReversed", cash.ErrAlreadyReversed, http.StatusConflict, "invalid_transition"},
		{"cash.ErrReconcileRunNotFound", cash.ErrReconcileRunNotFound, http.StatusNotFound, "not_found"},
		{"customer.ErrNotFound", customer.ErrNotFound, http.StatusNotFound, "not_found"},
		{"customer.ErrVersionConflict", customer.ErrVersionConflict, http.StatusConflict, "version_conflict"},
		{"customer.ErrInvalidPhone", customer.ErrInvalidPhone, http.StatusUnprocessableEntity, "invalid_phone"},
		{"customer.ErrIdentityTaken", customer.ErrIdentityTaken, http.StatusConflict, "identity_taken"},
		{"customer.ErrWardNotInProvince", customer.ErrWardNotInProvince, http.StatusUnprocessableEntity, "ward_not_in_province"},
		{"customer.ErrUnknownAdminUnit", customer.ErrUnknownAdminUnit, http.StatusUnprocessableEntity, "unknown_admin_unit"},
		{"customer.ErrQueryTooShort", customer.ErrQueryTooShort, http.StatusUnprocessableEntity, "query_too_short"},
		{"customer.ErrMergeSameCustomer", customer.ErrMergeSameCustomer, http.StatusUnprocessableEntity, "merge_same_customer"},
		{"customer.ErrMergeNotLatest", customer.ErrMergeNotLatest, http.StatusConflict, "merge_not_latest"},
		{"customer.ErrMergeWindowExpired", customer.ErrMergeWindowExpired, http.StatusUnprocessableEntity, "merge_window_expired"},
		{"customer.ErrImportTooLarge", customer.ErrImportTooLarge, http.StatusUnprocessableEntity, "import_too_large"},
		{"customer.ErrImportFileInvalid", customer.ErrImportFileInvalid, http.StatusUnprocessableEntity, "import_file_invalid"},
		{"customer.ErrInvalidTagRule", customer.ErrInvalidTagRule, http.StatusUnprocessableEntity, "invalid_tag_rule"},
		{"customer.ErrTagNameTaken", customer.ErrTagNameTaken, http.StatusConflict, "already_exists"},
		{"customer.ErrTagRuleManaged", customer.ErrTagRuleManaged, http.StatusConflict, "version_conflict"},
		{"promotion.ErrCouponRejected", promotion.ErrCouponRejected, http.StatusUnprocessableEntity, "coupon_rejected"},
		{"promotion.ErrCouponLockedAfterUse", promotion.ErrCouponLockedAfterUse, http.StatusConflict, "coupon_locked_after_use"},
		{"promotion.ErrMaxUsesBelowUsed", promotion.ErrMaxUsesBelowUsed, http.StatusUnprocessableEntity, "max_uses_below_used"},
		{"promotion.ErrCodeTaken", promotion.ErrCodeTaken, http.StatusConflict, "code_taken"},
		{"promotion.ErrNotFound", promotion.ErrNotFound, http.StatusNotFound, "not_found"},
		{"promotion.ErrVersionConflict", promotion.ErrVersionConflict, http.StatusConflict, "version_conflict"},
		{"promotion.ErrPreviewRequired", promotion.ErrPreviewRequired, http.StatusUnprocessableEntity, "preview_required"},
		{"promotion.ErrPercentTooHigh", promotion.ErrPercentTooHigh, http.StatusUnprocessableEntity, "percent_too_high"},
		{"promotion.ErrCampaignRunning", promotion.ErrCampaignRunning, http.StatusConflict, "campaign_running"},
		{"promotion.ErrBatchConflict", promotion.ErrBatchConflict, http.StatusUnprocessableEntity, "batch_conflict"},
		{"promotion.ErrRunInProgress", promotion.ErrRunInProgress, http.StatusConflict, "another_run_in_progress"},
		{"promotion.ErrInvalidGiftSettings", promotion.ErrInvalidGiftSettings, http.StatusUnprocessableEntity, "invalid_gift_settings"},
		{"promotion.ErrInvalidInput", promotion.ErrInvalidInput, http.StatusBadRequest, "invalid_input"},
		{"cash.ErrOrderNotFound", cash.ErrOrderNotFound, http.StatusNotFound, "not_found"},
		{"cash.ErrObligationNotFound", cash.ErrObligationNotFound, http.StatusNotFound, "not_found"},
		{"cash.ErrCODLockedByShipment", cash.ErrCODLockedByShipment, http.StatusConflict, "cod_locked_by_shipment"},
		{"cash.ErrPaymentNotAllowed", cash.ErrPaymentNotAllowed, http.StatusConflict, "invalid_transition"},
		{"cash.ErrObligationClosed", cash.ErrObligationClosed, http.StatusConflict, "invalid_transition"},
		{"cash.ErrPaymentExceedsTotal", cash.ErrPaymentExceedsTotal, http.StatusConflict, "payment_exceeds_total"},
		{"cash.ErrRefundExceedsRemaining", cash.ErrRefundExceedsRemaining, http.StatusConflict, "refund_exceeds_remaining"},
		{"cash.ErrReasonRequired", cash.ErrReasonRequired, http.StatusUnprocessableEntity, "invalid_input"},
		{"queries.ErrInvalidInput", queries.ErrInvalidInput, http.StatusBadRequest, "invalid_input"},
	}
}

// membershipCase: một sentinel và hàm ánh xạ của vùng chứa nó trước khi tách.
type membershipCase struct {
	name   string
	mapper func(error) (int, string, bool)
	err    error
}

// domainErrorMembership: sentinel của từng hàm ánh xạ trước Task 441 (sinh tự động từ tệp cũ, theo thứ tự bảng cũ).
//
//nolint:funlen // bảng dữ liệu, một dòng mỗi sentinel; tách nhỏ chỉ làm khó đối chiếu với bảng cũ
func domainErrorMembership() []membershipCase {
	return []membershipCase{
		{"channelError", channelError, channel.ErrAccountNotFound},
		{"channelError", channelError, channel.ErrVersionConflict},
		{"channelError", channelError, channel.ErrInvalidSendMode},
		{"channelError", channelError, channel.ErrOAuthStateInvalid},
		{"channelError", channelError, channel.ErrPageNotAccessible},
		{"channelError", channelError, channel.ErrPageConnectedElsewhere},
		{"channelError", channelError, channel.ErrConnectUnavailable},
		{"channelError", channelError, channel.ErrNoConnector},
		{"channelError", channelError, channel.ErrNoKeyring},
		{"channelError", channelError, errConnectSource},
		{"identityError", identityError, identity.ErrSelfModify},
		{"identityError", identityError, identity.ErrGrantExceedsActor},
		{"identityError", identityError, identity.ErrLocked},
		{"identityError", identityError, identity.ErrLastOwner},
		{"identityError", identityError, identity.ErrEmailTaken},
		{"identityError", identityError, identity.ErrVersionConflict},
		{"identityError", identityError, identity.ErrStaffNotFound},
		{"identityError", identityError, identity.ErrInvalidInput},
		{"identityError", identityError, identity.ErrOwnerRoleLocked},
		{"identityError", identityError, identity.ErrRoleInUse},
		{"identityError", identityError, identity.ErrRoleArchived},
		{"identityError", identityError, identity.ErrRoleNameTaken},
		{"identityError", identityError, identity.ErrUnknownPermission},
		{"identityError", identityError, identity.ErrRoleNotFound},
		{"catalogError", catalogError, catalog.ErrNotFound},
		{"catalogError", catalogError, catalog.ErrSKUTaken},
		{"catalogError", catalogError, catalog.ErrBarcodeTaken},
		{"catalogError", catalogError, catalog.ErrDuplicateOption},
		{"catalogError", catalogError, catalog.ErrCodeTaken},
		{"catalogError", catalogError, catalog.ErrVersionConflict},
		{"catalogError", catalogError, catalog.ErrNotForSale},
		{"catalogError", catalogError, catalog.ErrInvalidPrice},
		{"catalogError", catalogError, catalog.ErrInvalidArticle},
		{"catalogError", catalogError, catalog.ErrCategoryCycle},
		{"catalogError", catalogError, catalog.ErrCategoryTooDeep},
		{"catalogError", catalogError, catalog.ErrCategoryInUse},
		{"catalogError", catalogError, catalog.ErrAlreadyExists},
		{"catalogError", catalogError, catalog.ErrFileNotReady},
		{"catalogError", catalogError, catalog.ErrImportNotReady},
		{"catalogError", catalogError, catalog.ErrRestoreExpired},
		{"catalogError", catalogError, catalog.ErrInvalidInput},
		{"filesError", filesError, files.ErrTooLarge},
		{"filesError", filesError, files.ErrTypeNotAllowed},
		{"filesError", filesError, files.ErrUnknownPurpose},
		{"filesError", filesError, files.ErrUploadNotAllowed},
		{"filesError", filesError, files.ErrInvalidFile},
		{"filesError", filesError, files.ErrUploadMissing},
		{"filesError", filesError, files.ErrNotFound},
		{"filesError", filesError, files.ErrUnknownPreset},
		{"inventoryError", inventoryError, errForbiddenTarget},
		{"inventoryError", inventoryError, inventory.ErrInsufficient},
		{"inventoryError", inventoryError, inventory.ErrStockShort},
		{"inventoryError", inventoryError, inventory.ErrNotFound},
		{"inventoryError", inventoryError, inventory.ErrVersionConflict},
		{"inventoryError", inventoryError, inventory.ErrDocumentNotDraft},
		{"inventoryError", inventoryError, inventory.ErrStocktakeOpen},
		{"inventoryError", inventoryError, inventory.ErrStocktakeNotOpen},
		{"inventoryError", inventoryError, inventory.ErrSupplierNameTaken},
		{"inventoryError", inventoryError, inventory.ErrSupplierArchived},
		{"inventoryError", inventoryError, inventory.ErrSupplierNotForKind},
		{"inventoryError", inventoryError, inventory.ErrInvalidInput},
		{"orderError", orderError, order.ErrOutOfStock},
		{"orderError", orderError, order.ErrNotForSale},
		{"orderError", orderError, order.ErrMixedSellers},
		{"orderError", orderError, order.ErrTotalOverflow},
		{"orderError", orderError, order.ErrDuplicateOrder},
		{"orderError", orderError, order.ErrInvalidInput},
		{"orderError", orderError, order.ErrStockShort},
		{"orderError", orderError, order.ErrNotFound},
		{"orderError", orderError, customer.ErrInvalidInput},
		{"orderError", orderError, order.ErrShipmentAlreadyAttached},
		{"orderError", orderError, order.ErrVersionConflict},
		{"orderError", orderError, order.ErrLockedByShipment},
		{"orderError", orderError, order.ErrInvalidTransition},
		{"orderError", orderError, order.ErrAddressIncomplete},
		{"orderError", orderError, order.ErrNeedsApproval},
		{"orderError", orderError, order.ErrDiscountNeedsApproval},
		{"orderError", orderError, order.ErrVariantNotFound},
		{"orderError", orderError, order.ErrLineDiscountExceeds},
		{"orderError", orderError, order.ErrOrderDiscountExceeds},
		{"orderError", orderError, order.ErrTrafficSourceInactive},
		{"orderError", orderError, order.ErrReturnQtyExceeds},
		{"orderError", orderError, order.ErrQtyMismatch},
		{"orderError", orderError, order.ErrReturnNotAllowed},
		{"orderError", orderError, order.ErrSettingOutOfRange},
		{"orderError", orderError, order.ErrTrafficSourceNameTaken},
		{"fulfillmentError", fulfillmentError, errForbiddenTarget},
		{"fulfillmentError", fulfillmentError, fulfillment.ErrShipmentExists},
		{"fulfillmentError", fulfillmentError, fulfillment.ErrOrderNotShippable},
		{"fulfillmentError", fulfillmentError, fulfillment.ErrOrderNotFound},
		{"fulfillmentError", fulfillmentError, fulfillment.ErrAccountNotFound},
		{"fulfillmentError", fulfillmentError, fulfillment.ErrNotFound},
		{"fulfillmentError", fulfillmentError, fulfillment.ErrSettingOutOfRange},
		{"fulfillmentError", fulfillmentError, fulfillment.ErrVersionConflict},
		{"fulfillmentError", fulfillmentError, fulfillment.ErrCarrierNotSupported},
		{"fulfillmentError", fulfillmentError, fulfillment.ErrAccountUnavailable},
		{"fulfillmentError", fulfillmentError, fulfillment.ErrAccountTransition},
		{"fulfillmentError", fulfillmentError, fulfillment.ErrAccountTaken},
		{"fulfillmentError", fulfillmentError, fulfillment.ErrAccountInvalid},
		{"fulfillmentError", fulfillmentError, fulfillment.ErrAccountTargetNotFound},
		{"fulfillmentError", fulfillmentError, fulfillment.ErrInvalidPage},
		{"fulfillmentError", fulfillmentError, fulfillment.ErrInvalidTracking},
		{"fulfillmentError", fulfillmentError, fulfillment.ErrStockShort},
		{"fulfillmentError", fulfillmentError, fulfillment.ErrQuoteStale},
		{"fulfillmentError", fulfillmentError, fulfillment.ErrAddressUnmapped},
		{"fulfillmentError", fulfillmentError, fulfillment.ErrCreateInProgress},
		{"fulfillmentError", fulfillmentError, fulfillment.ErrShipInvalid},
		{"fulfillmentError", fulfillmentError, fulfillment.ErrTrackingCodeConflict},
		{"fulfillmentError", fulfillmentError, fulfillment.ErrShipmentNotCancellable},
		{"fulfillmentError", fulfillmentError, fulfillment.ErrPartialNotAllowed},
		{"fulfillmentError", fulfillmentError, fulfillment.ErrPartialQty},
		{"fulfillmentError", fulfillmentError, fulfillment.ErrPartialCOD},
		{"fulfillmentError", fulfillmentError, fulfillment.ErrManualReasonRequired},
		{"fulfillmentError", fulfillmentError, fulfillment.ErrManualNotAllowed},
		{"fulfillmentError", fulfillmentError, fulfillment.ErrLabelBatchInvalid},
		{"fulfillmentError", fulfillmentError, fulfillment.ErrAddressChangeOpen},
		{"fulfillmentError", fulfillmentError, fulfillment.ErrAddressChangeNotAllowed},
		{"fulfillmentError", fulfillmentError, fulfillment.ErrAnomalyNotFound},
		{"fulfillmentError", fulfillmentError, fulfillment.ErrAnomalyNotResolvable},
		{"fulfillmentError", fulfillmentError, fulfillment.ErrCallbackNotFound},
		{"fulfillmentError", fulfillmentError, fulfillment.ErrCallbackClosed},
		{"fulfillmentError", fulfillmentError, fulfillment.ErrCallbackAssignee},
		{"conversationError", conversationError, conversation.ErrNotFound},
		{"conversationError", conversationError, conversation.ErrReminderClosed},
		{"conversationError", conversationError, conversation.ErrCallInvalid},
		{"conversationError", conversationError, conversation.ErrTagNotFound},
		{"conversationError", conversationError, conversation.ErrVersionConflict},
		{"conversationError", conversationError, conversation.ErrAlreadyAssigned},
		{"conversationError", conversationError, conversation.ErrTagNameTaken},
		{"conversationError", conversationError, conversation.ErrInvalidInput},
		{"conversationError", conversationError, conversation.ErrLiveVideoInvalid},
		{"conversationError", conversationError, conversation.ErrClaimInvalid},
		{"conversationError", conversationError, conversation.ErrCommentClaimed},
		{"conversationError", conversationError, conversation.ErrOpenDraftExists},
		{"conversationError", conversationError, conversation.ErrSellerMismatch},
		{"conversationError", conversationError, conversation.ErrDraftNotOpen},
		{"conversationError", conversationError, conversation.ErrDraftMismatch},
		{"conversationError", conversationError, conversation.ErrOrderCustomerMismatch},
		{"cashError", cashError, cash.ErrCarrierAccountNotFound},
		{"cashError", cashError, cash.ErrStatementNotFound},
		{"cashError", cashError, cash.ErrStatementChanged},
		{"cashError", cashError, cash.ErrShipmentNotFound},
		{"cashError", cashError, cash.ErrUnknownStatementFormat},
		{"cashError", cashError, cash.ErrStatementFileNotReady},
		{"cashError", cashError, cash.ErrInvalidInput},
		{"cashError", cashError, cash.ErrPreviewStale},
		{"cashError", cashError, cash.ErrAdjustReasonRequired},
		{"cashError", cashError, cash.ErrAdjustmentInvalid},
		{"cashError", cashError, cash.ErrAdjustTargetNotFound},
		{"cashError", cashError, cash.ErrAdjustmentBlocked},
		{"cashError", cashError, cash.ErrAlreadyReversed},
		{"cashError", cashError, cash.ErrReconcileRunNotFound},
		{"paymentsError", paymentsError, cash.ErrOrderNotFound},
		{"paymentsError", paymentsError, cash.ErrObligationNotFound},
		{"paymentsError", paymentsError, cash.ErrCODLockedByShipment},
		{"paymentsError", paymentsError, cash.ErrPaymentNotAllowed},
		{"paymentsError", paymentsError, cash.ErrObligationClosed},
		{"paymentsError", paymentsError, cash.ErrPaymentExceedsTotal},
		{"paymentsError", paymentsError, cash.ErrRefundExceedsRemaining},
		{"paymentsError", paymentsError, cash.ErrReasonRequired},
		{"customerError", customerError, customer.ErrNotFound},
		{"customerError", customerError, customer.ErrVersionConflict},
		{"customerError", customerError, customer.ErrInvalidPhone},
		{"customerError", customerError, customer.ErrIdentityTaken},
		{"customerError", customerError, customer.ErrWardNotInProvince},
		{"customerError", customerError, customer.ErrUnknownAdminUnit},
		{"customerError", customerError, customer.ErrQueryTooShort},
		{"customerError", customerError, customer.ErrMergeSameCustomer},
		{"customerError", customerError, customer.ErrMergeNotLatest},
		{"customerError", customerError, customer.ErrMergeWindowExpired},
		{"customerError", customerError, customer.ErrImportTooLarge},
		{"customerError", customerError, customer.ErrImportFileInvalid},
		{"customerError", customerError, customer.ErrInvalidTagRule},
		{"customerError", customerError, customer.ErrTagNameTaken},
		{"customerError", customerError, customer.ErrTagRuleManaged},
		{"promotionError", promotionError, promotion.ErrCouponRejected},
		{"promotionError", promotionError, promotion.ErrCouponLockedAfterUse},
		{"promotionError", promotionError, promotion.ErrMaxUsesBelowUsed},
		{"promotionError", promotionError, promotion.ErrCodeTaken},
		{"promotionError", promotionError, promotion.ErrNotFound},
		{"promotionError", promotionError, promotion.ErrVersionConflict},
		{"promotionError", promotionError, promotion.ErrInvalidInput},
		{"queriesError", queriesError, queries.ErrInvalidInput},
	}
}
