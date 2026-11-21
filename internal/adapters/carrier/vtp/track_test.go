package vtp_test

import (
	"errors"
	"testing"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

// VC11: tài liệu VTP không có API đọc trạng thái hay hành trình (chỉ webhook) → Track, GetStatus trả ErrNotSupported,
// không gọi hãng; Capabilities.Track false (lệnh tra của lõi không tin webhook mà tra lại: vận đơn VTP chờ người).
func TestTrackNotSupported(t *testing.T) {
	e := newEnv(t)
	tracking := e.mustCreate(t, "E261121TRK001")
	calls := e.srv.Calls("track")
	if _, err := e.ad.Track(t.Context(), e.acc, tracking, "E261121TRK001"); !errors.Is(err, fulfillment.ErrNotSupported) {
		t.Errorf("Track = %v, muốn ErrNotSupported", err)
	}
	if _, err := e.ad.GetStatus(t.Context(), e.acc, tracking); !errors.Is(err, fulfillment.ErrNotSupported) {
		t.Errorf("GetStatus = %v, muốn ErrNotSupported", err)
	}
	if e.ad.Capabilities().Track || e.ad.Capabilities().LookupByRef || calls != e.srv.Calls("track") {
		t.Error("Track/LookupByRef phải false và không gọi hãng")
	}
}
