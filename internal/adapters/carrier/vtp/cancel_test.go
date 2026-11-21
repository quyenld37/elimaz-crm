package vtp_test

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"elimaz.com.vn/crm/internal/adapters/carrier/carriertest"
	"elimaz.com.vn/crm/internal/adapters/carrier/vtp/vtptest"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

// mustCreate tạo một vận đơn trên máy chủ giả, trả mã vận đơn VTP.
func (e *env) mustCreate(t *testing.T, code string) string {
	t.Helper()
	res, err := e.ad.CreateShipment(t.Context(), e.acc, createReq(e.acc, code, "VHT"))
	if err != nil || res.TrackingCode == "" {
		t.Fatalf("CreateShipment(%s) = %+v %v", code, res, err)
	}
	return res.TrackingCode
}

// VC05: hủy vận đơn chưa lấy → UpdateOrder TYPE 4 theo mã vận đơn VTP → CancelDone; máy chủ ghi 107.
func TestCancelNotPickedUp(t *testing.T) {
	e := newEnv(t)
	tracking := e.mustCreate(t, "E261121CAN001")
	res, err := e.ad.Cancel(t.Context(), e.acc, tracking)
	if err != nil || res != fulfillment.CancelDone {
		t.Fatalf("Cancel = %q %v, muốn done", res, err)
	}
	b := bodyField(t, e.srv, carriertest.OpCancel, 0)
	if b["TYPE"] != float64(4) || b["ORDER_NUMBER"] != tracking || b["NOTE"] == "" {
		t.Errorf("thân UpdateOrder = %v, muốn TYPE 4, ORDER_NUMBER %s, có NOTE", b, tracking)
	}
	if st := e.srv.Status(tracking); st != "107" {
		t.Errorf("trạng thái ở máy chủ giả = %q, muốn 107", st)
	}
}

// Hủy vận đơn đã lấy (ORDER_STATUS ≥ 200): tài liệu chỉ nói TYPE 4 "chỉ áp dụng với đơn chưa nhận thành công", không
// có mã lỗi riêng cho "đã lấy" → lỗi từ chối (người xem), không đoán CancelRejectedPickedUp (Quyết định Q2).
func TestCancelPickedUpIsRejected(t *testing.T) {
	e := newEnv(t)
	tracking := e.mustCreate(t, "E261121CAN002")
	e.srv.Push(tracking, fulfillment.CarrierEvent{Key: "k", RawStatus: "200"})
	res, err := e.ad.Cancel(t.Context(), e.acc, tracking)
	ce := carrierErr(t, "hủy đơn đã lấy", err, fulfillment.ClassRejected)
	if res != "" || ce.Code != "vtp_204" || !errors.Is(err, fulfillment.ErrRejected) {
		t.Errorf("hủy đơn đã lấy = %q, mã %q, %v", res, ce.Code, err)
	}
	if strings.Contains(err.Error(), "đã lấy hàng") {
		t.Error("lỗi chép thông báo của hãng")
	}
	if st := e.srv.Status(tracking); st != "200" {
		t.Errorf("trạng thái ở máy chủ giả = %q, muốn giữ 200", st)
	}
}

// Lớp lỗi của hủy: sau khi gửi mà không rõ (quá giờ, 205, 5xx) → không rõ (in_doubt, không tự gửi lại); 429 → chờ;
// 503 → chưa gửi; mã truy cập hết hạn → đăng nhập lại một lần rồi hủy được; mã vận đơn rỗng → từ chối, không gọi.
func TestCancelErrorClasses(t *testing.T) {
	e := newEnv(t)
	for _, c := range []struct {
		mode string
		want fulfillment.ErrorClass
	}{
		{carriertest.ModeTimeoutAfterSend, fulfillment.ClassUnknownOutcome},
		{vtptest.ModeSystemError, fulfillment.ClassUnknownOutcome},
		{carriertest.ModeRateLimited, fulfillment.ClassRateLimited},
		{carriertest.ModeFailBeforeSend, fulfillment.ClassNotSentRetry},
		{carriertest.ModeAuthRevoked, fulfillment.ClassAuth},
	} {
		t.Run(c.mode, func(t *testing.T) {
			tracking := e.mustCreate(t, "E261121CE"+c.mode)
			e.srv.SetMode(carriertest.OpCancel, c.mode)
			defer e.srv.SetMode(carriertest.OpCancel, carriertest.ModeOK)
			ctx, cancel := contextTimeout(t, 500*time.Millisecond)
			defer cancel()
			res, err := e.ad.Cancel(ctx, e.acc, tracking)
			wantClass(t, c.mode, err, c.want)
			if res != "" {
				t.Errorf("%s: kết quả = %q, muốn rỗng", c.mode, res)
			}
			if c.want == fulfillment.ClassUnknownOutcome && (errors.Is(err, fulfillment.ErrNotSent) || errors.Is(err, fulfillment.ErrRejected)) {
				t.Errorf("%s: lỗi không rõ mà bọc %v", c.mode, err)
			}
		})
	}
	t.Run("token hết hạn", func(t *testing.T) {
		tracking := e.mustCreate(t, "E261121CETOK")
		logins := e.srv.Calls(vtptest.OpLogin)
		e.srv.SetMode(carriertest.OpCancel, vtptest.ModeTokenExpired)
		res, err := e.ad.Cancel(t.Context(), e.acc, tracking)
		if err != nil || res != fulfillment.CancelDone || e.srv.Calls(vtptest.OpLogin) != logins+1 {
			t.Errorf("hủy sau token hết hạn = %q %v, đăng nhập thêm %d", res, err, e.srv.Calls(vtptest.OpLogin)-logins)
		}
	})
	t.Run("mã rỗng", func(t *testing.T) {
		n := e.srv.Calls(carriertest.OpCancel)
		_, err := e.ad.Cancel(t.Context(), e.acc, "")
		wantClass(t, "mã rỗng", err, fulfillment.ClassRejected)
		if e.srv.Calls(carriertest.OpCancel) != n {
			t.Error("mã rỗng vẫn gọi hãng")
		}
	})
}

// Hủy lại đơn đã hủy: tài liệu không nói hãng trả gì (máy chủ giả trả 204) → CancelIdempotent khai false (Q-24).
func TestCancelTwiceNotIdempotent(t *testing.T) {
	e := newEnv(t)
	tracking := e.mustCreate(t, "E261121CAN003")
	if _, err := e.ad.Cancel(t.Context(), e.acc, tracking); err != nil {
		t.Fatal(err)
	}
	_, err := e.ad.Cancel(t.Context(), e.acc, tracking)
	wantClass(t, "hủy lần hai", err, fulfillment.ClassRejected)
	if e.ad.Capabilities().CancelIdempotent {
		t.Error("CancelIdempotent phải false khi chưa kiểm chứng ở môi trường thử của hãng")
	}
}

// Phát tiếp (UpdateOrder TYPE 3) chỉ khi đơn ở 505 (phát thất bại, chờ shop duyệt): được → done; đơn ở trạng thái
// khác → hãng từ chối → UpdateRejected (không lỗi); ghi chú cắt còn 150 ký tự; quá giờ → không rõ.
func TestRedeliver(t *testing.T) {
	e := newEnv(t)
	tracking := e.mustCreate(t, "E261121RED001")
	res, err := e.ad.Redeliver(t.Context(), e.acc, tracking, "giao lại")
	if err != nil || res != fulfillment.UpdateRejected {
		t.Errorf("phát tiếp đơn chưa ở 505 = %q %v, muốn rejected", res, err)
	}
	e.srv.Push(tracking, fulfillment.CarrierEvent{Key: "k", RawStatus: "505"})
	note := strings.Repeat("Giao giờ hành chính. ", 20)
	res, err = e.ad.Redeliver(t.Context(), e.acc, tracking, note)
	if err != nil || res != fulfillment.UpdateDone {
		t.Fatalf("phát tiếp đơn 505 = %q %v, muốn done", res, err)
	}
	b := bodyField(t, e.srv, vtptest.OpUpdate, 1)
	n, _ := b["NOTE"].(string)
	if b["TYPE"] != float64(3) || b["ORDER_NUMBER"] != tracking || utf8.RuneCountInString(n) != 150 || !strings.HasPrefix(note, n) {
		t.Errorf("thân phát tiếp = %v (NOTE %d ký tự)", b, utf8.RuneCountInString(n))
	}
	e.srv.SetMode(vtptest.OpUpdate, carriertest.ModeTimeoutAfterSend)
	ctx, cancel := contextTimeout(t, 300*time.Millisecond)
	defer cancel()
	_, err = e.ad.Redeliver(ctx, e.acc, tracking, "")
	wantClass(t, "phát tiếp quá giờ", err, fulfillment.ClassUnknownOutcome)
}

// Sửa địa chỉ: API edit của VTP đòi gửi lại toàn bộ đơn (người gửi, dịch vụ, cách trả cước…), interface chỉ có địa chỉ
// mới → không làm (UpdateAddress false), trả ErrNotSupported.
func TestUpdateAddressNotSupported(t *testing.T) {
	e := newEnv(t)
	_, err := e.ad.UpdateAddress(t.Context(), e.acc, "1587818", fulfillment.Address{})
	if !errors.Is(err, fulfillment.ErrNotSupported) {
		t.Errorf("UpdateAddress = %v, muốn ErrNotSupported", err)
	}
}
