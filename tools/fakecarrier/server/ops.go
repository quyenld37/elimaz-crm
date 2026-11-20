package server

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
)

// Phí báo của hãng giả (đồng): cước gốc tới 500 g, mỗi 500 g thêm, phụ phí nhanh, cho thử hàng; COD và bảo hiểm 0,5 %.
const (
	quoteBase          = 22000
	quotePerHalfKg     = 5000
	quoteFastSurcharge = 10000
	quoteTryFee        = 2000
	halfKg             = 500
	percentDivisor     = 200 // 0,5 %
)

// location là một đơn vị địa giới tĩnh của hãng giả.
type location struct {
	Level  string `json:"level"`
	Code   string `json:"code"`
	Name   string `json:"name"`
	Parent string `json:"parent,omitempty"`
}

// routesM3 thêm các route M3: kiểm kết nối, báo phí, hủy, hành trình, nhãn PDF, sửa địa chỉ, giao lại, địa giới.
func (s *Server) routesM3(mux *http.ServeMux) {
	mux.Handle("GET /v1/me", s.auth(opHealth, s.withMode(opHealth, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})))
	mux.Handle("POST /v1/quote", s.auth(opQuote, s.withMode(opQuote, s.quote)))
	mux.Handle("POST /v1/parcels/{tracking}/cancel", s.auth(opCancel, s.withMode(opCancel, s.cancel)))
	mux.Handle("GET /v1/parcels/{tracking}/events", s.auth(opTrack, s.withMode(opTrack, s.events)))
	mux.Handle("POST /v1/labels", s.auth(opLabels, s.withMode(opLabels, s.labels)))
	mux.Handle("POST /v1/parcels/{tracking}/address", s.auth(opAddress, s.withMode(opAddress, s.updateAddress)))
	mux.Handle("POST /v1/parcels/{tracking}/redeliver", s.auth(opRedeliver, s.withMode(opRedeliver, s.redeliver)))
	mux.Handle("GET /v1/locations", s.auth(opLocations, s.withMode(opLocations, s.locations)))
}

// withMode áp chế độ lỗi của thao tác op: fail_before_send → 503 (chưa làm); reject, auth_revoked, rate_limited → 422,
// 401, 429; timeout_after_send → làm rồi giữ yêu cầu tới khi bên gọi bỏ, trả 504; ok → làm.
func (s *Server) withMode(op string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mode := s.st.opMode(op)
		switch {
		case mode == modeFailBeforeSend:
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "not_sent"})
		case failMode(w, mode):
		case mode == modeTimeoutAfterSend:
			next(discard{h: http.Header{}}, r)
			select {
			case <-r.Context().Done():
			case <-time.After(s.cfg.Hang):
			}
			writeJSON(w, http.StatusGatewayTimeout, map[string]string{"error": "timeout"})
		default:
			next(w, r)
		}
	}
}

// discard là ResponseWriter bỏ trả lời (chế độ timeout_after_send: việc đã làm nhưng bên gọi không nhận được).
type discard struct{ h http.Header }

func (d discard) Header() http.Header       { return d.h }
func (discard) Write(b []byte) (int, error) { return len(b), nil }
func (discard) WriteHeader(int)             {}

type quoteReq struct {
	FromProvince  string `json:"fromProvince"`
	ToProvince    string `json:"toProvince"`
	ToWard        string `json:"toWard"`
	WeightGrams   int    `json:"weightGrams"`
	DeclaredValue int64  `json:"declaredValue"`
	COD           int64  `json:"cod"`
	Service       string `json:"service"`
	Inspection    string `json:"inspection"`
	FeeOnRefuse   int64  `json:"feeOnRefuse"`
}

type quote struct {
	Service      string           `json:"service"`
	Name         string           `json:"name"`
	Total        int64            `json:"total"`
	Breakdown    map[string]int64 `json:"breakdown"`
	ExpectedDays int              `json:"expectedDays"`
}

// quote (POST /v1/quote): hai dịch vụ std, fast (hoặc đúng service hỏi); cân nặng ≤ 0, tiền âm, thiếu tỉnh nhận → 422.
func (s *Server) quote(w http.ResponseWriter, r *http.Request) {
	var in quoteReq
	if err := decode(r, &in); err != nil || in.WeightGrams <= 0 || in.COD < 0 || in.DeclaredValue < 0 ||
		in.FeeOnRefuse < 0 || in.ToProvince == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_quote"})
		return
	}
	main := int64(quoteBase + quotePerHalfKg*((max(in.WeightGrams, halfKg)-1)/halfKg))
	extra := map[string]int64{"cod": in.COD / percentDivisor, "insurance": in.DeclaredValue / percentDivisor}
	if in.Inspection == "try" {
		extra["inspection"] = quoteTryFee
	}
	var out []quote
	for _, svc := range []struct {
		key, name string
		add       int64
		days      int
	}{{"std", "Tiêu chuẩn", 0, 3}, {"fast", "Nhanh", quoteFastSurcharge, 1}} {
		if in.Service != "" && in.Service != svc.key {
			continue
		}
		b := map[string]int64{"main": main + svc.add}
		total := main + svc.add
		for k, v := range extra {
			b[k] = v
			total += v
		}
		out = append(out, quote{Service: svc.key, Name: svc.name, Total: total, Breakdown: b, ExpectedDays: svc.days})
	}
	writeJSON(w, http.StatusOK, map[string][]quote{"quotes": out})
}

// cancel (POST /v1/parcels/{tracking}/cancel): kiện chưa lấy → hủy, done; đã hủy → done (gọi lại an toàn); đã lấy →
// rejected_picked_up; không có kiện → 404.
func (s *Server) cancel(w http.ResponseWriter, r *http.Request) {
	now := s.now()
	s.parcelResult(w, r, func(p *parcel) (string, bool) {
		switch p.Status {
		case rawCancelled:
			return resultDone, false
		case rawCreated:
			p.setStatus("", rawCancelled, now)
			return resultDone, true
		}
		return resultRejectedPickedUp, false
	})
}

// updateAddress (POST /v1/parcels/{tracking}/address): kiện chưa giao xong, chưa hủy → done; còn lại → rejected.
func (s *Server) updateAddress(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ProvinceCode string `json:"provinceCode"`
		WardCode     string `json:"wardCode"`
		Line         string `json:"line"`
	}
	if err := decode(r, &in); err != nil || in.ProvinceCode == "" || in.WardCode == "" || strings.TrimSpace(in.Line) == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_address"})
		return
	}
	s.parcelResult(w, r, func(p *parcel) (string, bool) {
		if slices.Contains([]string{rawCreated, rawPickedUp, rawInTransit, rawDeliveryFailed}, p.Status) {
			return resultDone, false
		}
		return resultRejected, false
	})
}

// redeliver (POST /v1/parcels/{tracking}/redeliver): kiện giao thất bại → giao lại (out_for_delivery), done; còn lại →
// rejected.
func (s *Server) redeliver(w http.ResponseWriter, r *http.Request) {
	now := s.now()
	s.parcelResult(w, r, func(p *parcel) (string, bool) {
		if p.Status != rawDeliveryFailed {
			return resultRejected, false
		}
		p.setStatus("", rawOutForDelivery, now)
		return resultDone, true
	})
}

// parcelResult áp f lên kiện của đường {tracking}, trả {result}; không có kiện → 404.
func (s *Server) parcelResult(w http.ResponseWriter, r *http.Request, f func(p *parcel) (string, bool)) {
	res, ok, err := s.st.change(r.PathValue("tracking"), f)
	switch {
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
	case !ok:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
	default:
		writeJSON(w, http.StatusOK, map[string]string{"result": res})
	}
}

// events (GET /v1/parcels/{tracking}/events): hành trình; phí thực tế luôn có, tiền COD thu khi đã giao.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	p, ok := s.pathParcel(w, r)
	if !ok {
		return
	}
	out := map[string]any{"tracking": p.Tracking, "events": p.Events, "feeActual": p.Fee}
	if p.Status == rawDelivered {
		out["codCollected"] = p.CODAmount
	}
	writeJSON(w, http.StatusOK, out)
}

// labels (POST /v1/labels {trackings}): một PDF giả, mỗi kiện một trang; danh sách rỗng hay có mã lạ → 422, 404.
func (s *Server) labels(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Trackings []string `json:"trackings"`
	}
	if err := decode(r, &in); err != nil || len(in.Trackings) == 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_labels"})
		return
	}
	var b strings.Builder
	b.WriteString("%PDF-1.4\n")
	for _, t := range in.Trackings {
		if _, ok := s.st.byTracking(t); !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		fmt.Fprintf(&b, "%% nhãn kiện %s\n", t)
	}
	b.WriteString("%%EOF\n")
	writeJSON(w, http.StatusOK, map[string]any{"pdf": []byte(b.String()), "pages": len(in.Trackings)})
}

// locations (GET /v1/locations?level=&parent=): danh sách tĩnh tỉnh và phường (mã hãng giả trùng mã CRM).
func (*Server) locations(w http.ResponseWriter, r *http.Request) {
	all := []location{
		{Level: "province", Code: "01", Name: "Hà Nội"},
		{Level: "province", Code: "79", Name: "TP Hồ Chí Minh"},
		{Level: "ward", Code: "00004", Name: "Phường Ba Đình", Parent: "01"},
		{Level: "ward", Code: "00070", Name: "Phường Hoàn Kiếm", Parent: "01"},
		{Level: "ward", Code: "26734", Name: "Phường Sài Gòn", Parent: "79"},
	}
	level, parent := r.URL.Query().Get("level"), r.URL.Query().Get("parent")
	out := []location{}
	for _, l := range all {
		if l.Level == level && (parent == "" || l.Parent == parent) {
			out = append(out, l)
		}
	}
	writeJSON(w, http.StatusOK, map[string][]location{"locations": out})
}
