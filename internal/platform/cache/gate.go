package cache

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/valkey-io/valkey-go"

	"elimaz.com.vn/crm/internal/platform/metrics"
)

// ErrBadState báo SetState nhận remaining âm (lỗi lập trình của bên gọi).
var ErrBadState = errors.New("cache: trạng thái món không hợp lệ")

// Lý do từ chối của Admission.Reason.
const (
	// ReasonBusy: vượt số vé mỗi giây của món hay của cả shop.
	ReasonBusy = "busy"
	// ReasonInflight: tiến trình đang xử lý đủ MaxInflight lượt mua.
	ReasonInflight = "inflight"
)

// Mặc định của cổng (QĐ23): 100 vé/giây mỗi món, 300 vé/giây cả shop, 64 lượt đang xử lý mỗi tiến trình api; khi
// Valkey ngừng dùng 50% các số mỗi giây.
const (
	defaultGatePerSecond       = 100
	defaultGateGlobalPerSecond = 300
	defaultGateMaxInflight     = 64
	// gateCounterTTL: hạn sống của khóa bộ đếm một giây (đồng hồ các tiến trình api lệch nhau vẫn đếm cùng khóa).
	gateCounterTTL = 3 * time.Second
	// gateStateTTL: hạn sống của trạng thái món; vòng dựng lại (M5-21) ghi lại thường xuyên hơn, ngừng thì tự hết.
	gateStateTTL = 10 * time.Second
	// inflightRetry: RetryAfter khi đầy chỗ đang xử lý.
	inflightRetry = time.Second
	// busyJitter: phần ngẫu nhiên tối đa cộng vào RetryAfter của "busy" để khách không cùng quay lại một lúc.
	busyJitter = 500 * time.Millisecond
	// gateMs: một giây tính bằng ms (cửa sổ của bộ đếm).
	gateMs = int64(time.Second / time.Millisecond)
)

// Kết quả của gateScript.
const (
	gateOK         = 0
	gateItemBusy   = 1
	gateGlobalBusy = 2
)

// gateScript đếm một vé trong giây hiện tại: tăng bộ đếm món (KEYS[1]); quá ARGV[1] → 1 (không chạm bộ đếm shop);
// tăng bộ đếm shop (KEYS[2]); quá ARGV[2] → trả lại lượt của món, 2; được → 0. Khóa mới (hay mất hạn) đặt hạn ARGV[3] ms.
const gateScript = `local n = redis.call('INCR', KEYS[1])
if redis.call('PTTL', KEYS[1]) < 0 then redis.call('PEXPIRE', KEYS[1], ARGV[3]) end
if n > tonumber(ARGV[1]) then return 1 end
local g = redis.call('INCR', KEYS[2])
if redis.call('PTTL', KEYS[2]) < 0 then redis.call('PEXPIRE', KEYS[2], ARGV[3]) end
if g > tonumber(ARGV[2]) then redis.call('DECR', KEYS[1]) return 2 end
return 0`

// stateScript ghi trạng thái món vào hash KEYS[1] (remaining, soldout, until = mốc hết hạn ms theo đồng hồ của cổng)
// và đặt hạn ARGV[4] ms.
const stateScript = `redis.call('HSET', KEYS[1], 'remaining', ARGV[1], 'soldout', ARGV[2], 'until', ARGV[3])
redis.call('PEXPIRE', KEYS[1], ARGV[4])
return 1`

// GateOptions là tham số của NewAdmissionGate. Số ≤ 0 dùng mặc định QĐ23: PerSecond 100, GlobalPerSecond 300,
// MaxInflight 64, FallbackPerSecond và FallbackGlobalPerSecond là 50% số tương ứng (tối thiểu 1); số dự phòng lớn hơn
// số chính thì hạ bằng số chính. Now nil dùng time.Now.
type GateOptions struct {
	PerSecond, GlobalPerSecond, FallbackPerSecond, FallbackGlobalPerSecond, MaxInflight int
	Now                                                                                 func() time.Time
}

// Admission là vé của một lượt mua. Admitted: được đi tiếp; bên gọi phải gọi Release khi xử lý xong (defer ngay sau
// Admit). Không được: Reason ("busy" | "inflight") và RetryAfter (> 0). Degraded: Valkey không dùng được, quyết định
// lấy từ bộ đếm trong tiến trình.
type Admission struct {
	Admitted   bool
	RetryAfter time.Duration
	Reason     string
	Degraded   bool

	g        *gate
	released atomic.Bool
}

// Release trả chỗ đang xử lý của vé. Gọi nhiều lần (kể cả song song), trên vé không được nhận hay vé nil đều an toàn:
// chỗ chỉ trả một lần.
func (a *Admission) Release() {
	if a == nil || a.g == nil || !a.released.CompareAndSwap(false, true) {
		return
	}
	a.g.setInflight(a.g.inflight.Add(-1))
}

// AdmissionGate điều tiết lượt mua flash sale (kế hoạch M5 Task 187) trên Valkey gate: số vé mỗi giây theo món và
// theo shop, số lượt đang xử lý mỗi tiến trình, và cờ hết suất, số còn của món (do vòng dựng lại M5-21 ghi).
//
// Khi nào không dùng: không phải chốt nghiệp vụ (không bán vượt chốt trong PostgreSQL); không gọi trong hàm của
// RetryTx (gọi mạng); SoldOut known = false nghĩa là "không biết", bên gọi phải xuống database, không bao giờ coi là
// hết; mỗi tiến trình api đếm dự phòng riêng (ngưỡng thật khi dự phòng là ngưỡng dự phòng × số tiến trình).
type AdmissionGate interface {
	// Admit xin một vé cho món item của tenant. Không trả lỗi: Valkey lỗi thì đếm trong tiến trình theo giây với hai
	// ngưỡng dự phòng và đặt Degraded. Vé được nhận phải Release.
	Admit(ctx context.Context, tenant, item uuid.UUID) *Admission
	// SoldOut đọc cờ hết suất; known = false khi chưa có trạng thái, trạng thái quá 10 s hay Valkey lỗi.
	SoldOut(ctx context.Context, tenant, item uuid.UUID) (soldOut, known bool)
	// SetState ghi số còn và cờ hết suất của món, sống 10 s. remaining âm → ErrBadState; Valkey lỗi → lỗi bọc
	// ErrUnavailable (bên gọi bỏ qua, lượt sau ghi lại).
	SetState(ctx context.Context, tenant, item uuid.UUID, remaining int, soldOut bool) error
	// ClearState xóa ngay trạng thái của món (Task 520: món rời đợt đang hoạt động — đóng, hủy đợt): sau đó SoldOut
	// known = false, Remaining không có món. Món chưa có trạng thái không lỗi; Valkey lỗi → lỗi bọc ErrUnavailable
	// (trạng thái vẫn tự hết sau 10 s).
	ClearState(ctx context.Context, tenant, item uuid.UUID) error
	// Remaining trả số còn của các món có trạng thái còn hạn; món chưa có trạng thái không có trong map; Valkey lỗi →
	// map rỗng. Luôn khác nil.
	Remaining(ctx context.Context, tenant uuid.UUID, items []uuid.UUID) map[uuid.UUID]int
}

type gate struct {
	c        *Conn
	o        GateOptions
	m        *metrics.Registry
	inflight atomic.Int64

	fbMu sync.Mutex // giữ qua cả hai bộ đếm dự phòng (món, shop) để kiểm và tính là một bước
	fb   *fallbackCounter
}

// NewAdmissionGate dựng AdmissionGate trên c (kết nối gate); c nil hợp lệ (luôn dự phòng, SoldOut luôn không biết).
// m nil thì không ghi gate_admitted, gate_busy (nhãn lý do), gate_degraded, gauge gate_inflight.
func NewAdmissionGate(c *Conn, o GateOptions, m *metrics.Registry) AdmissionGate { //nolint:ireturn // Interfaces của kế hoạch M5 Task 187: bên dùng (httpapi) cắm bản giả
	return newGate(c, o, m)
}

func newGate(c *Conn, o GateOptions, m *metrics.Registry) *gate {
	if o.PerSecond <= 0 {
		o.PerSecond = defaultGatePerSecond
	}
	if o.GlobalPerSecond <= 0 {
		o.GlobalPerSecond = defaultGateGlobalPerSecond
	}
	if o.MaxInflight <= 0 {
		o.MaxInflight = defaultGateMaxInflight
	}
	o.FallbackPerSecond = fallbackOf(o.FallbackPerSecond, o.PerSecond)
	o.FallbackGlobalPerSecond = fallbackOf(o.FallbackGlobalPerSecond, o.GlobalPerSecond)
	if o.Now == nil {
		o.Now = time.Now
	}
	return &gate{c: c, o: o, m: m, fb: newFallbackCounter(fallbackMaxSubjects)}
}

// fallbackOf: ngưỡng dự phòng v, ≤ 0 thì 50% của main (tối thiểu 1), không quá main.
func fallbackOf(v, main int) int {
	if v <= 0 {
		v = max(main/2, 1) //nolint:mnd // 50% (QĐ23)
	}
	return min(v, main)
}

func (g *gate) Admit(ctx context.Context, tenant, item uuid.UUID) *Admission {
	if !g.acquire() {
		g.inc("gate_busy", ReasonInflight)
		return &Admission{Reason: ReasonInflight, RetryAfter: inflightRetry}
	}
	now := g.o.Now()
	nowMs := now.UnixMilli()
	startMs := floorDiv(nowMs, gateMs) * gateMs
	a := &Admission{}
	res, err := g.count(ctx, tenant, item, startMs/gateMs)
	if err != nil {
		g.inc("gate_degraded", "")
		a.Degraded = true
		res = g.countFallback(tenant, item, startMs)
	}
	if res != gateOK {
		g.setInflight(g.inflight.Add(-1))
		g.inc("gate_busy", ReasonBusy)
		a.Reason = ReasonBusy
		a.RetryAfter = time.Duration(startMs+gateMs-nowMs)*time.Millisecond + rand.N(busyJitter+time.Millisecond) //nolint:gosec // độ lệch thời gian chờ, không phải bí mật
		return a
	}
	g.inc("gate_admitted", "")
	a.Admitted, a.g = true, g
	return a
}

// acquire giữ một chỗ đang xử lý nếu còn (không chờ).
func (g *gate) acquire() bool {
	for {
		n := g.inflight.Load()
		if n >= int64(g.o.MaxInflight) {
			return false
		}
		if g.inflight.CompareAndSwap(n, n+1) {
			g.setInflight(n + 1)
			return true
		}
	}
}

// count chạy gateScript cho giây sec; lỗi (ngừng hay lỗi lệnh) → bên gọi đi đường dự phòng.
func (g *gate) count(ctx context.Context, tenant, item uuid.UUID, sec int64) (int64, error) {
	s := strconv.FormatInt(sec, 10)
	itemKey, err := EntityKey("gt", tenant, item.String(), s)
	if err != nil {
		return 0, err
	}
	allKey, err := EntityKey("gt", tenant, "all", s)
	if err != nil {
		return 0, err
	}
	var res int64
	err = g.c.Do(ctx, "gate.admit", func(ctx context.Context, cl valkey.Client) error {
		var e error
		res, e = cl.Do(ctx, cl.B().Eval().Script(gateScript).Numkeys(2).Key(itemKey, allKey).Arg(
			strconv.Itoa(g.o.PerSecond), strconv.Itoa(g.o.GlobalPerSecond),
			strconv.FormatInt(gateCounterTTL.Milliseconds(), 10),
		).Build()).AsInt64()
		return e //nolint:wrapcheck // Do phân loại lỗi thật của valkey-go
	})
	return res, err
}

// countFallback đếm một vé trong cửa sổ giây [startMs, startMs+1000) bằng bộ đếm trong tiến trình: kiểm cả món và
// shop rồi mới tính cả hai (lượt bị từ chối không tiêu lượt của bộ đếm nào).
func (g *gate) countFallback(tenant, item uuid.UUID, startMs int64) int64 {
	itemKey := "gt\x00" + tenant.String() + "\x00" + item.String()
	allKey := "gt\x00" + tenant.String() + "\x00all"
	g.fbMu.Lock()
	defer g.fbMu.Unlock()
	if ok, _ := g.fb.allow(itemKey, startMs, gateMs, g.o.FallbackPerSecond, false); !ok {
		return gateItemBusy
	}
	if ok, _ := g.fb.allow(allKey, startMs, gateMs, g.o.FallbackGlobalPerSecond, false); !ok {
		return gateGlobalBusy
	}
	g.fb.allow(itemKey, startMs, gateMs, g.o.FallbackPerSecond, true)
	g.fb.allow(allKey, startMs, gateMs, g.o.FallbackGlobalPerSecond, true)
	return gateOK
}

func (g *gate) SoldOut(ctx context.Context, tenant, item uuid.UUID) (soldOut, known bool) {
	vals, err := g.readStates(ctx, tenant, []uuid.UUID{item}, "soldout")
	if err != nil || vals[0] == nil {
		return false, false
	}
	return *vals[0] == "1", true
}

func (g *gate) SetState(ctx context.Context, tenant, item uuid.UUID, remaining int, soldOut bool) error {
	if remaining < 0 {
		return fmt.Errorf("%w: remaining %d", ErrBadState, remaining)
	}
	key, err := EntityKey("fs", tenant, item.String())
	if err != nil {
		return err
	}
	flag := "0"
	if soldOut {
		flag = "1"
	}
	until := g.o.Now().Add(gateStateTTL).UnixMilli()
	err = g.c.Do(ctx, "gate.setstate", func(ctx context.Context, cl valkey.Client) error {
		return eval(ctx, cl, stateScript, key, strconv.Itoa(remaining), flag, strconv.FormatInt(until, 10),
			strconv.FormatInt(gateStateTTL.Milliseconds(), 10)).Error()
	})
	if err != nil {
		return fmt.Errorf("cache: ghi trạng thái món: %w", err)
	}
	return nil
}

func (g *gate) ClearState(ctx context.Context, tenant, item uuid.UUID) error {
	key, err := EntityKey("fs", tenant, item.String())
	if err != nil {
		return err
	}
	err = g.c.Do(ctx, "gate.clearstate", func(ctx context.Context, cl valkey.Client) error {
		return cl.Do(ctx, cl.B().Del().Key(key).Build()).Error()
	})
	if err != nil {
		return fmt.Errorf("cache: xóa trạng thái món: %w", err)
	}
	return nil
}

func (g *gate) Remaining(ctx context.Context, tenant uuid.UUID, items []uuid.UUID) map[uuid.UUID]int {
	out := map[uuid.UUID]int{}
	if len(items) == 0 {
		return out
	}
	vals, err := g.readStates(ctx, tenant, items, "remaining")
	if err != nil {
		return out
	}
	for i, v := range vals {
		if v == nil {
			continue
		}
		if n, e := strconv.Atoi(*v); e == nil && n >= 0 {
			out[items[i]] = n
		}
	}
	return out
}

// readStates đọc trường field của hash trạng thái từng món (một lượt gửi); phần tử nil khi món chưa có trạng thái,
// thiếu trường hay trạng thái đã quá hạn theo đồng hồ của cổng (until ≤ bây giờ).
func (g *gate) readStates(ctx context.Context, tenant uuid.UUID, items []uuid.UUID, field string) ([]*string, error) {
	keys := make([]string, len(items))
	for i, it := range items {
		key, err := EntityKey("fs", tenant, it.String())
		if err != nil {
			return nil, err
		}
		keys[i] = key
	}
	raw := make([][2]*string, len(items))
	err := g.c.Do(ctx, "gate.state", func(ctx context.Context, cl valkey.Client) error {
		cmds := make(valkey.Commands, len(keys))
		for i, k := range keys {
			cmds[i] = cl.B().Hmget().Key(k).Field(field, "until").Build()
		}
		for i, r := range cl.DoMulti(ctx, cmds...) {
			pair, e := hmgetPair(r)
			if e != nil {
				return e
			}
			raw[i] = pair
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	nowMs := g.o.Now().UnixMilli()
	out := make([]*string, len(items))
	for i, r := range raw {
		if r[0] == nil || r[1] == nil {
			continue
		}
		if until, e := strconv.ParseInt(*r[1], 10, 64); e == nil && until > nowMs {
			out[i] = r[0]
		}
	}
	return out, nil
}

// hmgetPair đọc kết quả HMGET hai trường; trường không có là nil.
func hmgetPair(r valkey.ValkeyResult) ([2]*string, error) {
	var out [2]*string
	arr, err := r.ToArray()
	if err != nil {
		return out, err //nolint:wrapcheck // Do phân loại lỗi thật của valkey-go
	}
	for j := 0; j < len(out) && j < len(arr); j++ {
		if arr[j].IsNil() {
			continue
		}
		s, err := arr[j].ToString()
		if err != nil {
			return out, err //nolint:wrapcheck // Do phân loại lỗi thật của valkey-go
		}
		out[j] = &s
	}
	return out, nil
}

func (g *gate) setInflight(n int64) {
	if g.m != nil {
		g.m.Gauge("gate_inflight", float64(n), "")
	}
}

func (g *gate) inc(name, label string) {
	if g.m != nil {
		g.m.Inc(name, label, 1)
	}
}
