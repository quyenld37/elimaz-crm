// Package vtptest là máy chủ giả của Viettel Post cho test (httptest): nói đúng định dạng dây của tài liệu
// partner2.viettelpost.vn (đăng nhập, danh mục V3, getPriceAll, getPrice, createOrder; phong bì status/error/message/
// data; mã lỗi trong thân) và cài carriertest.Harness (chế độ lỗi theo thao tác, đếm lời gọi, hỏi kiện theo mã đơn
// của đối tác). Trả lời thành công là mẫu chép từ tài liệu (vtp/testdata, bên gọi nạp vào Options).
//
// Khi nào không dùng: không chạy ngoài test; không làm mẫu cho hãng khác (mỗi hãng một máy chủ giả <hãng>test).
package vtptest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"elimaz.com.vn/crm/internal/adapters/carrier/carriertest"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

// Thao tác riêng của VTP (ngoài carriertest.OpCreate, carriertest.OpQuote): đăng nhập, danh mục địa giới.
const (
	OpLogin     = "login"
	OpLocations = "locations"
)

// ModeTokenExpired là chế độ lỗi riêng: trả mã 202 "Token invalid or expired" đúng một lần rồi về ok.
const ModeTokenExpired = "token_expired"

// Options là tài khoản hợp lệ và các trả lời mẫu (JSON nguyên văn của tài liệu: phong bì, hoặc mảng trần của
// getPriceAll). Hang là thời gian treo tối đa của chế độ timeout_after_create (0 → 5 s).
type Options struct {
	Username, Password                                    string
	Login, Provinces, Wards, PriceAll, Price, CreateOrder json.RawMessage
	Hang                                                  time.Duration
}

// Server là máy chủ giả; dùng qua httptest.NewServer(s).
type Server struct {
	opts Options

	mu       sync.Mutex
	modes    map[string]string
	calls    map[string]int
	tokens   map[string]bool
	orders   map[string]string // ORDER_NUMBER của đối tác → mã vận đơn VTP
	bodies   map[string][]json.RawMessage
	tokenSeq int
	seq      int
}

// New dựng máy chủ giả với tài khoản và mẫu trả lời o.
func New(o Options) *Server {
	if o.Hang <= 0 {
		o.Hang = 5 * time.Second
	}
	s := &Server{opts: o}
	s.Reset()
	return s
}

// Reset xóa kiện, mã truy cập, chế độ, bộ đếm.
func (s *Server) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.modes = map[string]string{}
	s.calls = map[string]int{}
	s.tokens = map[string]bool{}
	s.orders = map[string]string{}
	s.bodies = map[string][]json.RawMessage{}
}

// SetMode đặt chế độ lỗi của thao tác op.
func (s *Server) SetMode(op, mode string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.modes[op] = mode
}

// Push không làm gì: VTP không có API hành trình (Track false); webhook ở Task 126.
func (*Server) Push(string, fulfillment.CarrierEvent) {}

// Has cho biết đã có đơn với mã của đối tác code (ORDER_NUMBER gửi lên).
func (s *Server) Has(code string) (ok bool) {
	s.locked(func() { _, ok = s.orders[code] })
	return ok
}

// locked chạy f khi giữ khóa của máy chủ.
func (s *Server) locked(f func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f()
}

// Calls đếm số yêu cầu tới thao tác op (kể cả yêu cầu bị lỗi theo chế độ).
func (s *Server) Calls(op string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[op]
}

// Bodies trả thân JSON các yêu cầu đã nhận của op, theo thứ tự.
func (s *Server) Bodies(op string) []json.RawMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]json.RawMessage(nil), s.bodies[op]...)
}

// ExpireTokens làm mọi mã truy cập đã cấp hết hạn (lần gọi sau với mã cũ nhận mã 202).
func (s *Server) ExpireTokens() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens = map[string]bool{}
}

// ServeHTTP định tuyến theo đường dẫn của tài liệu.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/v2/user/Login":
		s.login(w, body)
	case r.Method == http.MethodGet && r.URL.Path == "/v3/categories/listProvinceNew":
		s.guarded(w, r, OpLocations, body, func() { raw(w, s.opts.Provinces) })
	case r.Method == http.MethodGet && r.URL.Path == "/v3/categories/listWardsNew":
		s.guarded(w, r, OpLocations, body, func() {
			if _, err := strconv.ParseInt(r.URL.Query().Get("provinceId"), 10, 64); err != nil {
				envelope(w, 203, "provinceId")
				return
			}
			raw(w, s.opts.Wards)
		})
	case r.Method == http.MethodPost && r.URL.Path == "/v2/order/getPriceAll":
		s.guarded(w, r, carriertest.OpQuote, body, func() { raw(w, s.opts.PriceAll) })
	case r.Method == http.MethodPost && r.URL.Path == "/v2/order/getPrice":
		s.guarded(w, r, carriertest.OpQuote, body, func() { raw(w, s.opts.Price) })
	case r.Method == http.MethodPost && r.URL.Path == "/v2/order/createOrder":
		s.guarded(w, r, carriertest.OpCreate, body, func() { s.create(w, r, body) })
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) login(w http.ResponseWriter, body []byte) {
	var in struct {
		Username string `json:"USERNAME"`
		Password string `json:"PASSWORD"`
	}
	s.mu.Lock()
	s.calls[OpLogin]++
	mode := s.modes[OpLogin]
	s.mu.Unlock()
	if mode == carriertest.ModeRateLimited {
		tooMany(w)
		return
	}
	if json.Unmarshal(body, &in) != nil || in.Username != s.opts.Username || in.Password != s.opts.Password ||
		mode == carriertest.ModeAuthRevoked {
		envelope(w, 204, "Invalid owner account or password!")
		return
	}
	s.mu.Lock()
	s.tokenSeq++
	tok := fmt.Sprintf("vtptest-token-%d", s.tokenSeq)
	s.tokens[tok] = true
	s.mu.Unlock()
	var rep map[string]any
	if err := json.Unmarshal(s.opts.Login, &rep); err != nil {
		http.Error(w, "mẫu đăng nhập hỏng", http.StatusInternalServerError)
		return
	}
	if d, ok := rep["data"].(map[string]any); ok {
		d["token"] = tok
	}
	writeJSON(w, rep)
}

// guarded đếm lời gọi, áp chế độ lỗi chung và kiểm header Token trước khi chạy ok.
func (s *Server) guarded(w http.ResponseWriter, r *http.Request, op string, body []byte, ok func()) {
	s.mu.Lock()
	s.calls[op]++
	if len(body) > 0 {
		s.bodies[op] = append(s.bodies[op], json.RawMessage(bytes.Clone(body)))
	}
	mode := s.modes[op]
	if mode == ModeTokenExpired {
		s.modes[op] = carriertest.ModeOK
	}
	valid := s.tokens[r.Header.Get("Token")]
	s.mu.Unlock()
	switch {
	case !valid:
		envelope(w, 202, "Token invalid or expired")
	case mode == ModeTokenExpired, mode == carriertest.ModeAuthRevoked:
		envelope(w, 202, "Token invalid or expired")
	case mode == carriertest.ModeRateLimited:
		tooMany(w)
	case mode == carriertest.ModeFailBeforeCreate, mode == carriertest.ModeFailBeforeSend:
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
	case mode == carriertest.ModeReject:
		envelope(w, 204, "Lỗi dữ liệu không hợp lệ")
	default:
		ok()
	}
}

type createIn struct {
	OrderNumber      string `json:"ORDER_NUMBER"`
	CheckUnique      bool   `json:"CHECK_UNIQUE"`
	OrderService     string `json:"ORDER_SERVICE"`
	SenderProvince   *int64 `json:"SENDER_PROVINCE"`
	SenderWard       *int64 `json:"SENDER_WARD"`
	ReceiverProvince *int64 `json:"RECEIVER_PROVINCE"`
	ReceiverWard     *int64 `json:"RECEIVER_WARD"`
	ReceiverPhone    string `json:"RECEIVER_PHONE"`
}

func (s *Server) create(w http.ResponseWriter, r *http.Request, body []byte) {
	var in createIn
	if err := json.Unmarshal(body, &in); err != nil || in.OrderNumber == "" || in.OrderService == "" ||
		in.SenderProvince == nil || in.SenderWard == nil || in.ReceiverProvince == nil || in.ReceiverWard == nil ||
		in.ReceiverPhone == "" {
		envelope(w, 203, "Lỗi trường không được bỏ trống")
		return
	}
	s.mu.Lock()
	if _, dup := s.orders[in.OrderNumber]; dup && in.CheckUnique {
		s.mu.Unlock()
		envelope(w, 206, "Mã đơn hàng đã tồn tại trên hệ thống")
		return
	}
	s.seq++
	tracking := fmt.Sprintf("1587818%04d", s.seq)
	s.orders[in.OrderNumber] = tracking
	mode := s.modes[carriertest.OpCreate]
	s.mu.Unlock()
	if mode == carriertest.ModeTimeoutAfterCreate {
		select {
		case <-r.Context().Done():
		case <-time.After(s.opts.Hang):
		}
		return
	}
	var rep map[string]any
	if err := json.Unmarshal(s.opts.CreateOrder, &rep); err != nil {
		http.Error(w, "mẫu tạo đơn hỏng", http.StatusInternalServerError)
		return
	}
	if d, ok := rep["data"].(map[string]any); ok {
		d["ORDER_NUMBER"] = tracking
	}
	writeJSON(w, rep)
}

func tooMany(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "7")
	http.Error(w, "too many requests", http.StatusTooManyRequests)
}

// envelope trả lỗi theo phong bì của VTP (HTTP 200, mã trong thân).
func envelope(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, map[string]any{"status": status, "error": true, "message": msg, "data": nil})
}

func raw(w http.ResponseWriter, b json.RawMessage) {
	w.Header().Set("Content-Type", "application/json;charset=UTF-8")
	_, _ = w.Write(bytes.TrimSpace(b))
}

func writeJSON(w http.ResponseWriter, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		http.Error(w, strings.TrimSpace(err.Error()), http.StatusInternalServerError)
		return
	}
	raw(w, b)
}
