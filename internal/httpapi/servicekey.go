package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Mặc định của bộ kiểm khóa dịch vụ (kế hoạch M5 Task 189).
const (
	defaultKeyCacheTTL  = 5 * time.Second
	defaultKeyTouchGap  = time.Minute
	keyFlushTimeout     = 5 * time.Second
	keyTouchKickBufSize = 1
)

// ServiceKeyOptions là tham số của NewServiceKeyVerifier. CacheTTL 0 → 5 s (hạn của bản đệm khi báo hiệu thu hồi bị
// mất); TouchEvery 0 → 1 phút (last_used_at ghi tối đa một lần mỗi khoảng này mỗi khóa); Now nil → time.Now; Logger nil
// → slog.Default.
type ServiceKeyOptions struct {
	CacheTTL   time.Duration
	TouchEvery time.Duration
	Now        func() time.Time
	Logger     *slog.Logger
}

// KeyVerifier là bộ kiểm khóa dịch vụ thật (Deps.ServiceKeys): tách tiền tố, tìm tenant (tenancy.Resolve
// service_key), đọc dòng khóa trong WithTenant, đệm trong tiến trình theo tiền tố tối đa CacheTTL, xóa ngay khi nhận
// báo hiệu hệ thống identity.TopicServiceKeyRevoked hay realtime.TopicSystemResync; so băm và CIDR ở
// identity.CheckServiceKey. Ghi last_used_at bằng goroutine của Run (chủ là cmd/crm), không chặn yêu cầu. Không bao giờ
// đệm lỗi hay "không có", không log khóa. An toàn khi gọi từ nhiều goroutine.
type KeyVerifier struct {
	d      *db.DB
	o      ServiceKeyOptions
	cancel func() // hủy đăng ký báo hiệu (Run gọi khi dừng)

	mu      sync.Mutex
	gen     uint64 // tăng mỗi lần xóa đệm: bản đọc bắt đầu trước lần xóa không được lưu
	entries map[string]keyEntry
	lastUse map[uuid.UUID]time.Time    // lần ghi gần nhất (theo Now) của mỗi khóa
	pending map[uuid.UUID]pendingTouch // chờ Run ghi

	kick    chan struct{}
	touches atomic.Int64 // số khóa đã ghi last_used_at (test)
}

type keyEntry struct {
	rec     identity.ServiceKeyRecord
	expires time.Time
}

type pendingTouch struct {
	tenant uuid.UUID
	at     time.Time
}

// NewServiceKeyVerifier dựng bộ kiểm và đăng ký nhận báo hiệu thu hồi qua bus (nil: chỉ dựa vào CacheTTL). Gọi Run
// (một goroutine có chủ) để ghi last_used_at và hủy đăng ký khi dừng.
func NewServiceKeyVerifier(d *db.DB, bus realtime.Bus, o ServiceKeyOptions) *KeyVerifier {
	if o.CacheTTL <= 0 {
		o.CacheTTL = defaultKeyCacheTTL
	}
	if o.TouchEvery <= 0 {
		o.TouchEvery = defaultKeyTouchGap
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	v := &KeyVerifier{
		d: d, o: o, cancel: func() {},
		entries: map[string]keyEntry{}, lastUse: map[uuid.UUID]time.Time{}, pending: map[uuid.UUID]pendingTouch{},
		kick: make(chan struct{}, keyTouchKickBufSize),
	}
	if bus != nil {
		v.cancel = bus.SubscribeSystem(identity.TopicServiceKeyRevoked, v.onSignal)
	}
	return v
}

// onSignal xóa bản đệm của tiền tố bị thu hồi; resync (báo hiệu có thể đã mất) xóa hết. Gọi từ goroutine của bus,
// không chặn.
func (v *KeyVerifier) onSignal(s realtime.Signal) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.gen++
	switch s.Topic {
	case realtime.TopicSystemResync:
		clear(v.entries)
	case identity.TopicServiceKeyRevoked:
		delete(v.entries, s.Entity.ID)
	}
}

// Verify kiểm khóa đầy đủ bearer và IP của máy gọi (địa chỉ rỗng khi không có). Lỗi: identity.ErrInvalidKey,
// ErrRevoked, ErrIPNotAllowed, hoặc lỗi database (không mang khóa).
func (v *KeyVerifier) Verify(ctx context.Context, bearer string, clientIP netip.Addr) (identity.AuthUser, error) {
	prefix, ok := identity.SplitServiceKey(bearer)
	if !ok {
		return identity.AuthUser{}, identity.ErrInvalidKey
	}
	rec, err := v.record(ctx, prefix)
	if err != nil {
		return identity.AuthUser{}, err
	}
	u, err := identity.CheckServiceKey(rec, bearer, clientIP)
	if err != nil {
		return identity.AuthUser{}, err //nolint:wrapcheck // sentinel của identity, lớp HTTP đổi mã
	}
	v.noteUse(rec)
	return u, nil
}

// record trả dòng khóa của tiền tố: bản đệm còn hạn, không thì đọc database và đệm (trừ khi có lần xóa đệm trong lúc đọc).
func (v *KeyVerifier) record(ctx context.Context, prefix string) (identity.ServiceKeyRecord, error) {
	now := v.o.Now()
	v.mu.Lock()
	e, hit := v.entries[prefix]
	gen := v.gen
	v.mu.Unlock()
	if hit && now.Before(e.expires) {
		return e.rec, nil
	}
	tenant, ok, err := tenancy.Resolve(ctx, v.d, identity.RouteKindServiceKey, prefix)
	if err != nil {
		return identity.ServiceKeyRecord{}, fmt.Errorf("kiểm khóa dịch vụ: %w", err)
	}
	if !ok {
		return identity.ServiceKeyRecord{}, identity.ErrInvalidKey
	}
	var rec identity.ServiceKeyRecord
	err = tenancy.WithTenant(ctx, v.d, tenant, func(tx pgx.Tx) error {
		var err error
		rec, err = identity.LoadServiceKey(ctx, tx, prefix)
		return err //nolint:wrapcheck // sentinel hoặc lỗi đã bọc của identity
	}, db.TxAPI)
	if err != nil {
		return identity.ServiceKeyRecord{}, err //nolint:wrapcheck // như trên
	}
	v.mu.Lock()
	if v.gen == gen {
		v.entries[prefix] = keyEntry{rec: rec, expires: now.Add(v.o.CacheTTL)}
	}
	v.mu.Unlock()
	return rec, nil
}

// noteUse xếp một lần ghi last_used_at khi lần ghi trước của khóa đã cách ít nhất TouchEvery; báo Run không chặn.
func (v *KeyVerifier) noteUse(rec identity.ServiceKeyRecord) {
	now := v.o.Now()
	v.mu.Lock()
	last, seen := v.lastUse[rec.ID]
	due := !seen || now.Sub(last) >= v.o.TouchEvery
	if due {
		v.lastUse[rec.ID] = now
		v.pending[rec.ID] = pendingTouch{tenant: rec.TenantID, at: now}
	}
	v.mu.Unlock()
	if due {
		select {
		case v.kick <- struct{}{}:
		default: // Run đã có tín hiệu chờ: lần ghi sau gom cả khóa này
		}
	}
}

// Run ghi last_used_at mỗi khi có lần dùng mới tới hạn, tới khi ctx bị hủy; lúc dừng ghi nốt phần còn chờ (tối đa 5 s)
// rồi hủy đăng ký báo hiệu. cmd/crm chạy một goroutine và chờ nó kết thúc.
func (v *KeyVerifier) Run(ctx context.Context) {
	defer v.cancel()
	for {
		select {
		case <-ctx.Done():
			fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), keyFlushTimeout)
			v.flush(fctx)
			cancel()
			return
		case <-v.kick:
			v.flush(ctx)
		}
	}
}

// flush ghi mọi lần dùng đang chờ, mỗi tenant một transaction. Lỗi chỉ ghi cảnh báo (mốc tham khảo, không phải nghiệp
// vụ): lần đó bị bỏ, lần dùng sau phút kế tiếp ghi lại.
func (v *KeyVerifier) flush(ctx context.Context) {
	v.mu.Lock()
	batch := maps.Clone(v.pending)
	clear(v.pending)
	v.mu.Unlock()
	byTenant := map[uuid.UUID]map[time.Time][]uuid.UUID{}
	for id, p := range batch {
		if byTenant[p.tenant] == nil {
			byTenant[p.tenant] = map[time.Time][]uuid.UUID{}
		}
		byTenant[p.tenant][p.at] = append(byTenant[p.tenant][p.at], id)
	}
	for tenant, byAt := range byTenant {
		err := tenancy.WithTenant(ctx, v.d, tenant, func(tx pgx.Tx) error {
			for at, ids := range byAt {
				if err := identity.TouchServiceKeys(ctx, tx, ids, at); err != nil {
					return err //nolint:wrapcheck // đã bọc trong identity
				}
			}
			return nil
		}, db.TxAPI)
		if err != nil {
			v.o.Logger.LogAttrs(ctx, slog.LevelWarn, "không ghi được lần dùng khóa dịch vụ", logging.Err(err))
			continue
		}
		for _, ids := range byAt {
			v.touches.Add(int64(len(ids)))
		}
	}
}
