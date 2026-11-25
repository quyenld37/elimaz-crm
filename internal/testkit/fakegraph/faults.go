package fakegraph

import (
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type faultKind int

const (
	faultError faultKind = iota + 1
	faultHang
	faultDrop
)

// fault là một lỗi dùng một lần cho yêu cầu kế tiếp tới một đường.
type fault struct {
	kind faultKind
	spec GraphErrorSpec
	wait time.Duration
}

func (s *Server) addFault(path string, f fault) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path = trimSlash(path)
	s.faults[path] = append(s.faults[path], f)
}

// popFault lấy lỗi kế tiếp của path; gọi dưới s.mu.
func (s *Server) popFault(path string) (fault, bool) {
	q := s.faults[path]
	if len(q) == 0 {
		return fault{}, false
	}
	s.faults[path] = q[1:]
	return q[0], true
}

// FailNext làm yêu cầu kế tiếp tới path (không tiền tố phiên bản) nhận lỗi e, sau khi máy giả đã đọc trọn thân.
func (s *Server) FailNext(path string, e GraphErrorSpec) {
	s.addFault(path, fault{kind: faultError, spec: e})
}

// HangAfterRead làm yêu cầu kế tiếp tới path được đọc trọn thân rồi treo d (hoặc tới khi bên gọi bỏ) trước khi trả lời
// bình thường.
func (s *Server) HangAfterRead(path string, d time.Duration) {
	s.addFault(path, fault{kind: faultHang, wait: d})
}

// DropAfterRead làm yêu cầu kế tiếp tới path được đọc trọn thân (máy giả đã nhận) rồi đóng kết nối không trả lời.
func (s *Server) DropAfterRead(path string) { s.addFault(path, fault{kind: faultDrop}) }

// RefuseNextConn đóng mọi kết nối đang giữ và đóng ngay kết nối mới kế tiếp trước khi bắt tay TLS: bên gọi chưa gửi
// được gì (chỉ với New).
func (s *Server) RefuseNextConn() {
	s.refs.mu.Lock()
	s.refs.refuse++
	s.refs.mu.Unlock()
	s.ts.CloseClientConnections()
}

// SetUsagePct đặt phần trăm mức dùng trả trong X-App-Usage (call_count, total_time, total_cputime) của mọi câu trả
// lời; 0 là không gửi header.
func (s *Server) SetUsagePct(pct int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.usage = pct
}

// RevokeToken thu hồi token: mọi lời gọi sau bằng token này nhận lỗi 190.
func (s *Server) RevokeToken(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ti := s.st.tokens[token]; ti != nil {
		ti.revoked = true
	}
}

// applyFault thực hiện lỗi f; trả true khi đã trả lời (yêu cầu không đi tiếp).
func (s *Server) applyFault(w http.ResponseWriter, r *http.Request, f fault) bool {
	switch f.kind {
	case faultError:
		writeError(w, f.spec)
		return true
	case faultHang:
		select {
		case <-time.After(f.wait):
		case <-r.Context().Done():
			return true
		}
		return false
	case faultDrop:
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				_ = conn.Close()
			}
		}
		return true
	}
	return false
}

func usageJSON(pct int) string {
	return fmt.Sprintf(`{"call_count":%d,"total_time":%d,"total_cputime":%d}`, pct, pct, pct)
}

func businessUsageJSON(pct, regainMin int) string {
	return fmt.Sprintf(`{"fake-business":[{"type":"pages","call_count":%d,"total_time":%d,"total_cputime":%d,"estimated_time_to_regain_access":%d}]}`,
		pct, pct, pct, regainMin)
}

func parseForm(b []byte) (url.Values, error) {
	v, err := url.ParseQuery(string(b))
	if err != nil {
		return nil, fmt.Errorf("đọc thân form: %w", err)
	}
	return v, nil
}
