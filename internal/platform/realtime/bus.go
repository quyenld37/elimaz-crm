// Package realtime phát báo hiệu "đối tượng X vừa đổi" tới màn hình nhân viên qua SSE (spec 04 mục 6, ADR-05,
// ADR-17). Báo hiệu không mang dữ liệu: màn hình nhận rồi tải lại bằng API thường, nên mất báo hiệu không mất dữ liệu.
//
// Phần công khai: Signal (báo hiệu; System: báo hiệu cho tiến trình, không tới SSE), Bus (phát, đăng ký nhận của nhân
// viên và của hệ thống, trạng thái), PGHub (bản hiện thực bằng LISTEN/NOTIFY của PostgreSQL, tự nối lại) và ServeSSE
// (luồng text/event-stream của GET /api/v1/stream, gộp theo lô, nhịp tim).
//
// Khi nào không dùng: không gửi dữ liệu nghiệp vụ hay dữ liệu cá nhân qua báo hiệu (chỉ mã đối tượng); không dùng
// làm hàng đợi hay sự kiện nội bộ (báo hiệu được phép mất: dùng events.Emit, commands); không phát ngoài transaction
// nghiệp vụ (Publish nhận tx để báo hiệu chỉ đi khi commit).
package realtime

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
)

// Tên sự kiện SSE.
const (
	// EventResync: màn hình tải lại mọi danh sách đang mở (khung đầu của mỗi lần nối, và sau khi hub nối lại).
	EventResync = "resync"
	// EventChanged: một đối tượng vừa đổi; data {"topic","entity","scope"}.
	EventChanged = "changed"
)

// maxPayload: JSON của một báo hiệu tối đa 7.000 byte (NOTIFY của PostgreSQL giới hạn 8.000 byte).
const maxPayload = 7000

var (
	// ErrInvalidSignal là lỗi khi báo hiệu thiếu tenant, thiếu chủ đề, Requires không phải quyền đã biết, hoặc Requires
	// là quyền theo đích mà thiếu Scope; hoặc báo hiệu hệ thống có Requires hay Scope.
	ErrInvalidSignal = errors.New("realtime: báo hiệu không hợp lệ")
	// ErrSignalTooLarge là lỗi khi JSON của báo hiệu vượt 7.000 byte.
	ErrSignalTooLarge = errors.New("realtime: báo hiệu quá 7.000 byte")
)

// Entity là đối tượng vừa đổi: loại và mã (ví dụ {"conversation", "<uuid>"}). Không đặt nội dung vào ID.
type Entity struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// Signal là một báo hiệu: chỉ nhân viên cùng TenantID và có quyền Requires trên Scope (identity.Can) nhận được. Scope
// nil chỉ được với quyền không theo đích (identity.ScopeNone). Không mang dữ liệu cá nhân hay nội dung (chỉ chủ đề và mã).
//
// System: báo hiệu cho tiến trình (ví dụ xóa khóa bộ đệm, kế hoạch M5 Task 185), không bao giờ tới kết nối SSE của
// nhân viên; chỉ người đăng ký SubscribeSystem có tiền tố khớp Topic nhận. Báo hiệu hệ thống không có Requires, Scope.
type Signal struct {
	TenantID uuid.UUID
	Topic    string
	Requires identity.Permission
	Scope    *identity.Scope
	Entity   Entity
	System   bool
}

// TopicSystemResync là chủ đề của báo hiệu hệ thống (TenantID rỗng) gửi cho mọi người đăng ký hệ thống mỗi lần kết
// nối LISTEN nghe được (lần đầu và sau khi nối lại): báo hiệu trong lúc rớt có thể đã mất, bên nhận làm mới tất cả.
const TopicSystemResync = "system.resync"

// WarehouseScope là Scope của báo hiệu đòi quyền theo kho warehouseID (identity.ScopeWarehouse), cho module không
// được nhập identity theo cột "Được gọi" của 04 mục 3 (inventory).
func WarehouseScope(warehouseID string) *identity.Scope {
	return &identity.Scope{Type: identity.ScopeWarehouse, ID: warehouseID}
}

// Frame là một khung SSE gửi cho một kết nối: Event là EventResync hoặc EventChanged, Data là JSON.
type Frame struct {
	Event string
	Data  json.RawMessage
}

// Bus là chỗ phát và nhận báo hiệu, để đổi tầng dưới (hôm nay LISTEN/NOTIFY, sau này kênh phát của Valkey, spec 04
// mục 6) mà không sửa nghiệp vụ hay màn hình.
type Bus interface {
	// Publish phát s trong tx: chỉ tới client khi tx commit; tx lùi thì không ai nhận.
	Publish(ctx context.Context, tx pgx.Tx, s Signal) error
	// Subscribe nhận khung cho nhân viên u (đã lọc tenant và quyền) qua send; send không được chặn lâu. cancel hủy
	// đăng ký, gọi nhiều lần được.
	Subscribe(u identity.AuthUser, send func(Frame)) (cancel func())
	// SubscribeSystem nhận báo hiệu hệ thống có Topic bắt đầu bằng topicPrefix (mọi tenant), và TopicSystemResync,
	// qua fn; fn được gọi từ goroutine của tầng dưới nên không được chặn. cancel hủy đăng ký, gọi nhiều lần được.
	SubscribeSystem(topicPrefix string, fn func(Signal)) (cancel func())
	// Status: tầng dưới đang nghe được không, và số client đang đăng ký (kết nối SSE, không tính người đăng ký hệ thống).
	Status() (listening bool, clients int)
}

// wireScope là phạm vi trong JSON của báo hiệu và của khung changed.
type wireScope struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
}

// wireSignal là payload của NOTIFY.
type wireSignal struct {
	TenantID uuid.UUID  `json:"t"`
	Topic    string     `json:"topic"`
	Requires string     `json:"req"`
	Scope    *wireScope `json:"scope,omitempty"`
	Entity   Entity     `json:"entity"`
	System   bool       `json:"sys,omitempty"`
}

// changedData là data của khung changed (không có tenant, quyền).
type changedData struct {
	Topic  string     `json:"topic"`
	Entity Entity     `json:"entity"`
	Scope  *wireScope `json:"scope,omitempty"`
}

// encodeSignal kiểm s rồi trả JSON của NOTIFY.
func encodeSignal(s Signal) (string, error) {
	if s.System {
		if s.TenantID == uuid.Nil || s.Topic == "" || s.Requires != "" || s.Scope != nil {
			return "", ErrInvalidSignal
		}
		return marshalSignal(wireSignal{TenantID: s.TenantID, Topic: s.Topic, Entity: s.Entity, System: true})
	}
	if s.TenantID == uuid.Nil || s.Topic == "" || !identity.IsKnown(s.Requires) {
		return "", ErrInvalidSignal
	}
	// Quyền theo đích mà không có đích: identity.Can cho mọi ai có quyền đó trên bất kỳ đích nào → từ chối.
	if identity.KindOf(s.Requires) == identity.ScopeByTarget && s.Scope == nil {
		return "", ErrInvalidSignal
	}
	w := wireSignal{TenantID: s.TenantID, Topic: s.Topic, Requires: string(s.Requires), Entity: s.Entity}
	if s.Scope != nil {
		w.Scope = &wireScope{Type: s.Scope.Type, ID: s.Scope.ID}
	}
	return marshalSignal(w)
}

// marshalSignal trả JSON của w, tối đa maxPayload byte.
func marshalSignal(w wireSignal) (string, error) {
	b, err := json.Marshal(w)
	if err != nil {
		return "", errors.Join(ErrInvalidSignal, err)
	}
	if len(b) > maxPayload {
		return "", ErrSignalTooLarge
	}
	return string(b), nil
}

// resyncFrame là khung resync (data {}).
func resyncFrame() Frame { return Frame{Event: EventResync, Data: json.RawMessage("{}")} }
