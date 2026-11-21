// Package vtptest là máy chủ giả của Viettel Post cho test (httptest): nói đúng định dạng dây của tài liệu
// partner2.viettelpost.vn (đăng nhập, danh mục V3, getPriceAll, getPrice, createOrder, UpdateOrder, printing-code, máy
// in nhãn DigitalizePrint; phong bì status/error/message/data; mã lỗi trong thân) và cài carriertest.Harness (chế độ
// lỗi theo thao tác, đếm lời gọi, hỏi kiện theo mã đơn của đối tác, đẩy trạng thái). Trả lời thành công là mẫu chép từ
// tài liệu (vtp/testdata, bên gọi nạp vào Options).
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

// Thao tác riêng của VTP (ngoài carriertest.OpCreate, OpQuote, OpCancel — UpdateOrder TYPE 4, OpLabels —
// printing-code): đăng nhập, danh mục địa giới, UpdateOrder loại khác 4 (phát tiếp, duyệt hoàn).
const (
	OpLogin     = "login"
	OpLocations = "locations"
	OpUpdate    = "update"
)

// Chế độ lỗi riêng: ModeTokenExpired trả mã 202 "Token invalid or expired" đúng một lần rồi về ok; ModeSystemError
// trả mã 205 "Lỗi hệ thống" (hãng không nói đã xử lý chưa).
const (
	ModeTokenExpired = "token_expired"
	ModeSystemError  = "system_error"
)

// statusNew là trạng thái máy chủ giả gán cho đơn mới tạo (tài liệu không có mã cho đơn chưa lấy; mọi mã < 200).
const statusNew = "-100"

// Print là một yêu cầu tải nhãn máy in nhận: header Token (phải rỗng: mã truy cập không gửi sang máy in), tham số
// type, bill (mã in), showPostage.
type Print struct {
	Token, Type, Bill, ShowPostage string
}

// Options là tài khoản hợp lệ và các trả lời mẫu (JSON nguyên văn của tài liệu: phong bì, hoặc mảng trần của
// getPriceAll). Hang là thời gian treo tối đa của chế độ timeout_after_create, timeout_after_send (0 → 5 s).
type Options struct {
	Username, Password                                    string
	Login, Provinces, Wards, PriceAll, Price, CreateOrder json.RawMessage
	UpdateOrder, PrintingCode                             json.RawMessage
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
	parcels  map[string]string // mã vận đơn VTP → mã trạng thái hiện tại
	prints   map[string]int    // mã in → số vận đơn
	printLog []Print
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
	s.parcels = map[string]string{}
	s.prints = map[string]int{}
	s.printLog = nil
	s.bodies = map[string][]json.RawMessage{}
}

// SetMode đặt chế độ lỗi của thao tác op.
func (s *Server) SetMode(op, mode string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.modes[op] = mode
}

// Push đặt trạng thái hiện tại của vận đơn tracking thành ev.RawStatus (UpdateOrder kiểm theo trạng thái này); vận đơn
// lạ bị bỏ qua. VTP không có API hành trình nên sự kiện không đọc lại được.
func (s *Server) Push(tracking string, ev fulfillment.CarrierEvent) {
	s.locked(func() {
		if _, ok := s.parcels[tracking]; ok {
			s.parcels[tracking] = ev.RawStatus
		}
	})
}

// Status trả trạng thái hiện tại của vận đơn tracking ("" khi không có).
func (s *Server) Status(tracking string) (st string) {
	s.locked(func() { st = s.parcels[tracking] })
	return st
}

// Prints trả các yêu cầu tải nhãn máy in đã nhận, theo thứ tự.
func (s *Server) Prints() (ps []Print) {
	s.locked(func() { ps = append([]Print(nil), s.printLog...) })
	return ps
}

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
	case r.Method == http.MethodPost && r.URL.Path == "/v2/order/UpdateOrder":
		s.updateOrder(w, r, body)
	case r.Method == http.MethodPost && r.URL.Path == "/v2/order/printing-code":
		s.guarded(w, r, carriertest.OpLabels, body, func() { s.printingCode(w, body) })
	case r.Method == http.MethodGet && r.URL.Path == "/DigitalizePrint/report.do":
		s.report(w, r)
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
	case mode == ModeSystemError:
		envelope(w, 205, "Lỗi hệ thống")
	case mode == carriertest.ModeTimeoutAfterSend:
		s.hang(r)
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
	s.parcels[tracking] = statusNew
	mode := s.modes[carriertest.OpCreate]
	s.mu.Unlock()
	if mode == carriertest.ModeTimeoutAfterCreate {
		s.hang(r)
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

// hang treo tới khi bên gọi bỏ yêu cầu hay hết Options.Hang, không trả lời.
func (s *Server) hang(r *http.Request) {
	select {
	case <-r.Context().Done():
	case <-time.After(s.opts.Hang):
	}
}

// Loại cập nhật của UpdateOrder (trang "Cập nhật trạng thái vận đơn"): 3 phát tiếp (đơn ở 505), 4 hủy (đơn < 200).
const (
	typeRedeliver = 3
	typeCancel    = 4
	statusReturn  = "505"
)

type updateIn struct {
	Type        int    `json:"TYPE"`
	OrderNumber string `json:"ORDER_NUMBER"`
	Note        string `json:"NOTE"`
}

// updateOrder: TYPE 4 là thao tác cancel (chế độ lỗi của OpCancel), loại khác là OpUpdate. Hủy đơn chưa lấy → 107;
// đơn đã lấy hay đã hủy → 204 (tài liệu không có mẫu lỗi, máy chủ giả chọn 204). Phát tiếp chỉ khi đơn ở 505 → 508.
func (s *Server) updateOrder(w http.ResponseWriter, r *http.Request, body []byte) {
	var in updateIn
	_ = json.Unmarshal(body, &in) // thân hỏng: kiểm trong ok
	op := OpUpdate
	if in.Type == typeCancel {
		op = carriertest.OpCancel
	}
	s.guarded(w, r, op, body, func() {
		if in.OrderNumber == "" || (in.Type != typeCancel && in.Type != typeRedeliver) {
			envelope(w, 203, "Lỗi trường không được bỏ trống")
			return
		}
		s.mu.Lock()
		st, known := s.parcels[in.OrderNumber]
		n, _ := strconv.Atoi(st)
		ok := false
		switch {
		case !known:
		case in.Type == typeCancel && n < 200 && n != 101 && n != 107:
			s.parcels[in.OrderNumber], ok = "107", true
		case in.Type == typeRedeliver && st == statusReturn:
			s.parcels[in.OrderNumber], ok = "508", true
		}
		s.mu.Unlock()
		switch {
		case !ok:
			envelope(w, 204, "Đơn hàng đã lấy hàng hoặc không ở trạng thái cho phép")
		case in.Type == typeCancel:
			raw(w, s.opts.UpdateOrder)
		default:
			writeJSON(w, map[string]any{"status": 200, "error": false, "message": "OK", "data": nil})
		}
	})
}

// maxPrintOrders là số vận đơn tối đa một mã in (tài liệu: "tối đa 100 vận đơn").
const maxPrintOrders = 100

type printIn struct {
	ExpiryTime int64    `json:"EXPIRY_TIME"`
	OrderArray []string `json:"ORDER_ARRAY"`
}

// printingCode cấp mã in cho ORDER_ARRAY (1–100 mã đã tạo, hạn trong tương lai); mã có ký tự +, /, = như mẫu.
func (s *Server) printingCode(w http.ResponseWriter, body []byte) {
	var in printIn
	if json.Unmarshal(body, &in) != nil || len(in.OrderArray) == 0 || len(in.OrderArray) > maxPrintOrders ||
		in.ExpiryTime <= time.Now().UnixMilli() {
		envelope(w, 203, "Lỗi trường không được bỏ trống")
		return
	}
	s.mu.Lock()
	for _, c := range in.OrderArray {
		if _, ok := s.parcels[c]; !ok {
			s.mu.Unlock()
			envelope(w, 204, "Mã vận đơn không tồn tại")
			return
		}
	}
	s.seq++
	code := fmt.Sprintf("vtptest+in/%d=", s.seq)
	s.prints[code] = len(in.OrderArray)
	s.mu.Unlock()
	var rep map[string]any
	if err := json.Unmarshal(s.opts.PrintingCode, &rep); err != nil {
		http.Error(w, "mẫu mã in hỏng", http.StatusInternalServerError)
		return
	}
	rep["message"] = code
	writeJSON(w, rep)
}

// report là máy in nhãn: mã in đã cấp → PDF một trang mỗi vận đơn; mã lạ → trang HTML (như một trang báo lỗi).
func (s *Server) report(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	s.mu.Lock()
	s.printLog = append(s.printLog, Print{
		Token: r.Header.Get("Token"), Type: q.Get("type"), Bill: q.Get("bill"), ShowPostage: q.Get("showPostage"),
	})
	n := s.prints[q.Get("bill")]
	s.mu.Unlock()
	if n == 0 {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html>Không tìm thấy mã in</html>")
		return
	}
	var b strings.Builder
	b.WriteString("%PDF-1.4\n")
	for i := range n {
		fmt.Fprintf(&b, "%d 0 obj << /Type /Page >> endobj\n", i+1)
	}
	b.WriteString("%%EOF\n")
	w.Header().Set("Content-Type", "application/pdf")
	_, _ = io.WriteString(w, b.String())
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
