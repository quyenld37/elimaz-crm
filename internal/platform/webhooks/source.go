// Package webhooks là đường vào webhook ở vai trò ingest (spec 04 mục 5.2, 11): kiểm chữ ký trên thân gốc, lưu nguyên
// văn vào platform.webhook_deliveries cùng một việc tách gói trong một transaction rồi trả 200; database không ghi
// được thì ghi tệp tạm (Spool) và nạp lại sau (Drainer); chặn địa chỉ gửi sai chữ ký dồn dập (Lockout).
//
// Nguồn (kênh, hãng) hiện thực Source; ingest không cần database để kiểm chữ ký hay trả bước GET xác minh. Tách gói
// và định tuyến tenant chạy ở worker (Task 15), không trong request.
package webhooks

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"elimaz.com.vn/crm/internal/platform/queue"
)

// InboundEvent là một sự kiện đã chuẩn hóa tách ra từ một gói webhook (phần "Vào" của MessagingAdapter, 04 mục 4).
type InboundEvent struct {
	Type string
	// DedupeKey chống trùng theo từng sự kiện (tầng 1, jobs.dedupe_key).
	DedupeKey string
	// Route tìm tenant qua platform.tenant_routes.
	Route Route
	// OrderKey: các sự kiện cùng khóa xử lý lần lượt theo OccurredAt.
	OrderKey   string
	OccurredAt time.Time
	Payload    json.RawMessage
}

// Route là khóa định tuyến của một sự kiện: (Kind, ExternalKey) của platform.tenant_routes.
type Route struct {
	Kind, ExternalKey string
}

// SplitResult là kết quả tách một gói: các sự kiện biết xử lý và số sự kiện bị bỏ qua theo loại (loại lạ không phải
// lỗi, chỉ đếm).
type SplitResult struct {
	Events  []InboundEvent
	Skipped map[string]int
}

// Source là một nguồn webhook (/hooks/<Key>).
type Source interface {
	// Key là tên nguồn trong đường dẫn /hooks/<Key> và cột source.
	Key() string
	// Challenge trả lời bước GET xác minh; ok = false khi mã xác minh sai hoặc nguồn không có bước này.
	Challenge(q url.Values) (answer string, ok bool)
	// VerifySignature kiểm chữ ký trên thân gốc raw (so thời gian hằng).
	VerifySignature(raw []byte, h http.Header) bool
	// Split tách một gói thành các sự kiện; lỗi nghĩa là thân không đọc được.
	Split(raw []byte) (SplitResult, error)
}

// RoutedSource là nguồn nhận ở đường /hooks/<Key>/<routeKey> (M3 Q-8: mỗi tài khoản hãng một khóa tuyến ngẫu nhiên
// 32 ký tự): ingest lưu routeKey cùng thân gốc, worker tách bằng SplitRouted với đúng khóa đó (thường đặt
// Route{Kind, ExternalKey: routeKey} cho mọi sự kiện). Bản nhận không có routeKey (đường /hooks/<Key>) vẫn tách bằng
// Split. Nguồn không hiện thực RoutedSource gọi đường có routeKey → 404.
type RoutedSource interface {
	Source
	// SplitRouted tách một gói nhận ở đường có routeKey; lỗi nghĩa là thân không đọc được.
	SplitRouted(raw []byte, routeKey string) (SplitResult, error)
}

// routeKeyPattern là mẫu của routeKey trên đường dẫn và cột route_key (migration 0291).
var routeKeyPattern = regexp.MustCompile(`^[A-Za-z0-9]{32}$`)

// Đường sự kiện vào tới (Inbound.Via): webhook qua ingest, hoặc do worker đọc chủ động (M4) rồi ghi bằng EnqueueInbound.
const (
	ViaWebhook     = "webhook"
	ViaLiveStream  = "live_stream"
	ViaLivePoll    = "live_poll"
	ViaCommentPoll = "comment_poll"
	ViaBackfill    = "backfill"
	ViaHistory     = "history"
)

func validVia(v string) bool {
	switch v {
	case ViaWebhook, ViaLiveStream, ViaLivePoll, ViaCommentPoll, ViaBackfill, ViaHistory:
		return true
	}
	return false
}

// SplitKind là loại việc tách gói (việc chưa định tuyến, TenantID nil; payload {"deliveryId"}).
//
//nolint:gochecknoglobals // Interfaces của kế hoạch M0 Task 14 khai là var; chỉ đọc, không mã nào gán lại.
var SplitKind = queue.Kind{Name: "webhook.split", Policy: queue.DefaultPolicy}
