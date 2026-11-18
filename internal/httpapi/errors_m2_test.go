package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/testkit"
)

// TestDomainErrors_M2: mọi sentinel M2 (kế hoạch M2, Task M2-02) đổi sang đúng mã HTTP và mã lỗi, kể cả khi đã bọc
// một lần bằng %w; mã của M1 giữ nguyên.
func TestDomainErrors_M2(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		// M1, giữ nguyên.
		{order.ErrNotForSale, http.StatusUnprocessableEntity, "variant_not_for_sale"},
		{order.ErrMixedSellers, http.StatusUnprocessableEntity, "mixed_sellers"},
		{order.ErrTotalOverflow, http.StatusUnprocessableEntity, "total_overflow"},
		{order.ErrDuplicateOrder, http.StatusUnprocessableEntity, "idempotency_key_reused"},
		{order.ErrInvalidInput, http.StatusBadRequest, "invalid_input"},
		{order.ErrNotFound, http.StatusNotFound, "not_found"},
		{customer.ErrInvalidInput, http.StatusBadRequest, "invalid_input"},
		// order (M1 chưa ánh xạ, M2 thêm).
		{order.ErrShipmentAlreadyAttached, http.StatusConflict, "shipment_already_attached"},
		{order.ErrVersionConflict, http.StatusConflict, "version_conflict"},
		{order.ErrLockedByShipment, http.StatusConflict, "order_locked_by_shipment"},
		{order.ErrInvalidTransition, http.StatusUnprocessableEntity, "invalid_transition"},
		{order.ErrAddressIncomplete, http.StatusUnprocessableEntity, "address_incomplete"},
		{order.ErrNeedsApproval, http.StatusUnprocessableEntity, "needs_approval"},
		{order.ErrDiscountNeedsApproval, http.StatusForbidden, "discount_needs_approval"},
		{order.ErrVariantNotFound, http.StatusUnprocessableEntity, "variant_not_found"},
		{order.ErrLineDiscountExceeds, http.StatusUnprocessableEntity, "line_discount_exceeds"},
		{order.ErrOrderDiscountExceeds, http.StatusUnprocessableEntity, "order_discount_exceeds"},
		{order.ErrTrafficSourceInactive, http.StatusUnprocessableEntity, "traffic_source_inactive"},
		{order.ErrReturnQtyExceeds, http.StatusUnprocessableEntity, "return_qty_exceeds"},
		{order.ErrReturnNotAllowed, http.StatusUnprocessableEntity, "return_not_allowed"},
		{order.ErrSettingOutOfRange, http.StatusUnprocessableEntity, "setting_out_of_range"},
		{order.ErrTrafficSourceNameTaken, http.StatusConflict, "already_exists"}, // Task M2-15
		// customer.
		{customer.ErrNotFound, http.StatusNotFound, "not_found"},
		{customer.ErrVersionConflict, http.StatusConflict, "version_conflict"},
		{customer.ErrInvalidPhone, http.StatusUnprocessableEntity, "invalid_phone"},
		{customer.ErrIdentityTaken, http.StatusConflict, "identity_taken"},
		{customer.ErrWardNotInProvince, http.StatusUnprocessableEntity, "ward_not_in_province"},
		{customer.ErrUnknownAdminUnit, http.StatusUnprocessableEntity, "unknown_admin_unit"},
		{customer.ErrQueryTooShort, http.StatusUnprocessableEntity, "query_too_short"},
		{customer.ErrMergeSameCustomer, http.StatusUnprocessableEntity, "merge_same_customer"},
		{customer.ErrMergeNotLatest, http.StatusConflict, "merge_not_latest"},
		{customer.ErrMergeWindowExpired, http.StatusUnprocessableEntity, "merge_window_expired"},
		{customer.ErrImportTooLarge, http.StatusUnprocessableEntity, "import_too_large"},
		{customer.ErrImportFileInvalid, http.StatusUnprocessableEntity, "import_file_invalid"},
		{customer.ErrInvalidTagRule, http.StatusUnprocessableEntity, "invalid_tag_rule"},
		// promotion.
		{promotion.ErrCouponRejected, http.StatusUnprocessableEntity, "coupon_rejected"},
		{promotion.ErrCouponLockedAfterUse, http.StatusConflict, "coupon_locked_after_use"},
		{promotion.ErrMaxUsesBelowUsed, http.StatusUnprocessableEntity, "max_uses_below_used"},
		{promotion.ErrCodeTaken, http.StatusConflict, "code_taken"},
	}
	for _, c := range cases {
		rec := domainErrorRecorder(t, fmt.Errorf("việc thử: %w", c.err))
		if rec.Code != c.status || !strings.Contains(rec.Body.String(), `"error":"`+c.code+`"`) {
			t.Errorf("%v: mong %d %s, có %d %s", c.err, c.status, c.code, rec.Code, rec.Body.String())
		}
	}
	// Lỗi nội bộ (dữ liệu đơn lệch trạng thái): không phải lỗi nghiệp vụ, để writeHandlerError ghi log và trả 500.
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil)
	if writeDomainError(httptest.NewRecorder(), req, fmt.Errorf("việc thử: %w", order.ErrStatusConflict)) {
		t.Error("order.ErrStatusConflict không được ánh xạ thành lỗi nghiệp vụ")
	}
}

// TestDomainErrors_M2Details: bốn lỗi có trường chi tiết (chỉ mã, không dữ liệu cá nhân).
func TestDomainErrors_M2Details(t *testing.T) {
	owner, merge := uuid.New(), uuid.New()
	for _, c := range []struct {
		err          error
		status       int
		field, value string
	}{
		{&customer.IdentityTakenError{OwnerRootID: owner}, http.StatusConflict, "ownerId", owner.String()},
		{&customer.MergeNotLatestError{BlockingMergeID: merge}, http.StatusConflict, "blockingMergeId", merge.String()},
		{&promotion.RejectError{Reason: promotion.ReasonExhausted}, http.StatusUnprocessableEntity, "reason", "exhausted"},
		{&order.SettingOutOfRangeError{Field: order.FieldReturnWarningDays}, http.StatusUnprocessableEntity, "field", "returnWarningDays"},
	} {
		rec := domainErrorRecorder(t, fmt.Errorf("việc thử: %w", c.err))
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if rec.Code != c.status || body[c.field] != c.value || body["request_id"] == nil {
			t.Errorf("%T: mong %d %s=%s, có %d %s", c.err, c.status, c.field, c.value, rec.Code, rec.Body.String())
		}
	}
	if !errors.Is(&customer.IdentityTakenError{}, customer.ErrIdentityTaken) ||
		!errors.Is(&customer.MergeNotLatestError{}, customer.ErrMergeNotLatest) ||
		!errors.Is(&promotion.RejectError{}, promotion.ErrCouponRejected) ||
		!errors.Is(&order.SettingOutOfRangeError{}, order.ErrSettingOutOfRange) {
		t.Error("kiểu lỗi chi tiết phải Is sentinel của nó")
	}
}

func domainErrorRecorder(t *testing.T, err error) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil)
	if !writeDomainError(rec, req, err) {
		t.Fatalf("%v không được nhận là lỗi nghiệp vụ", err)
	}
	return rec
}

// TestM2Routes_Unimplemented: operation M2 chưa làm trả 501 not_implemented sau khi qua xác thực và quyền; không thẻ
// 401, không grant 403; route ghi thiếu Idempotency-Key 400.
func TestM2Routes_Unimplemented(t *testing.T) {
	staff, keys := testkit.OpenStaff(t), testkit.AccessKeyServer(t)
	tenant := staff.Tenant(t, "A")
	owner := staff.User(t, tenant, "chu@elimaz.test", "active")
	staff.Grant(t, tenant, owner, []string{"customer.read", "order.create"}, "all", "")
	staff.User(t, tenant, "khong-quyen@elimaz.test", "active")
	srv := testServer(t, staff, keys, 0)

	rec := serveAs(srv, keys, http.MethodGet, "/api/v1/customers", "chu@elimaz.test", "", "")
	expect(t, "chủ, chưa làm", rec, http.StatusNotImplemented, "not_implemented")
	if !strings.Contains(rec.Body.String(), `"request_id":"`) {
		t.Errorf("thân 501 thiếu request_id: %s", rec.Body.String())
	}
	expect(t, "không grant", serveAs(srv, keys, http.MethodGet, "/api/v1/customers", "khong-quyen@elimaz.test", "", ""),
		http.StatusForbidden, "forbidden")
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/customers", nil)
	noToken := httptest.NewRecorder()
	srv.ServeHTTP(noToken, req)
	expect(t, "không thẻ", noToken, http.StatusUnauthorized, "unauthenticated")
	expect(t, "tạo đơn thiếu Idempotency-Key", serveAs(srv, keys, http.MethodPost, "/api/v1/orders", "chu@elimaz.test", "",
		orderBody(0, line(uuid.New(), 1))), http.StatusBadRequest, "idempotency_key_required")
}

// TestOrderAPI_M2FieldsPending: thân kiểu M1 vẫn 201 (TestOrderAPI_CreateGet); couponCode chưa có nghiệp vụ (Task
// M2-17) thì 501, không bị bỏ qua lặng lẽ (trường M2 khác có từ Task M2-16, test ở handlers_order_m2_test.go); thiếu
// cả customer lẫn customerId, hay thiếu địa chỉ khi không gửi intent (luật M1) → 400 invalid_input. Không đơn nào được
// tạo.
func TestOrderAPI_M2FieldsPending(t *testing.T) {
	e := newOrderAPIEnv(t)
	v := e.variant(e.shop.Seller, 1000, 5)
	base := orderBody(0, line(v, 1))
	for i, c := range []struct {
		name, body string
		status     int
		code       string
	}{
		{"couponCode", strings.Replace(base, `{"customer"`, `{"couponCode":"SALE10","customer"`, 1), http.StatusNotImplemented, "not_implemented"},
		{"không khách", strings.Replace(base, `"customer":{"fullName":"Chị Lan"},`, ``, 1), http.StatusBadRequest, "invalid_input"},
		{
			"không địa chỉ", strings.Replace(base, `,"shipAddress":{"name":"Chị Lan","phone":"+84901234567","line":"12 Lê Lợi, Q1"}`, ``, 1),
			http.StatusBadRequest, "invalid_input",
		},
	} {
		if c.body == base {
			t.Fatalf("%s: thân thử không đổi", c.name)
		}
		expect(t, c.name, e.post(fmt.Sprintf("m2-%d", i), c.body), c.status, c.code)
	}
	if n := e.count(`SELECT count(*) FROM ord.orders`); n != 0 {
		t.Errorf("%d đơn, mong 0", n)
	}
}
