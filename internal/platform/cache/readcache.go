package cache

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/valkey-io/valkey-go"

	"elimaz.com.vn/crm/internal/platform/metrics"
)

// Space là một vùng của bộ đệm đọc (kế hoạch M5 Task 185): mỗi vùng có số thế hệ riêng theo tenant, BumpSpace làm mới
// cả vùng một lần.
type Space string

// Các vùng của bộ đệm đọc website (QĐ14 của kế hoạch M5).
const (
	SpaceProduct      Space = "product"
	SpaceProductsPage Space = "products_page"
	SpaceTaxonomy     Space = "taxonomy"
	SpaceAvailability Space = "availability"
	SpaceFlash        Space = "flash"
	SpaceLocations    Space = "locations"
	SpaceSold30       Space = "sold30"
	SpaceCarriers     Space = "carriers"
	SpaceShipQuote    Space = "shipquote"
	SpaceEstimates    Space = "estimates"
	// SpaceCampaigns: chiến dịch giảm giá web (Task 452), làm mới theo promotion.web_campaign.changed.
	SpaceCampaigns Space = "campaigns"
	// SpaceWebIntake: shop có đang tạm dừng nhận đơn web (Task 456), làm mới theo order.web_intake.changed.
	SpaceWebIntake Space = "web_intake"
	// SpaceBanners: banner trang chủ đang hiện (Task 453), làm mới theo content.banners.changed và
	// promotion.web_campaign.changed.
	SpaceBanners Space = "banners"
	// SpaceLooks: bộ phối cho website (Task 454; trang chủ và mọi bộ mở bằng link), làm mới theo content.looks.changed.
	SpaceLooks Space = "looks"
)

// Spaces trả mọi vùng của bộ đệm đọc (resync làm mới tất cả).
func Spaces() []Space {
	all := [...]Space{
		SpaceProduct, SpaceProductsPage, SpaceTaxonomy, SpaceAvailability, SpaceFlash, SpaceLocations,
		SpaceSold30, SpaceCarriers, SpaceShipQuote, SpaceEstimates, SpaceCampaigns, SpaceWebIntake,
		SpaceBanners, SpaceLooks,
	}
	return all[:]
}

// Source là nơi GetOrLoad lấy giá trị: bộ đệm, PostgreSQL qua load (thiếu trong bộ đệm), hay PostgreSQL vì Valkey
// không dùng được (đường dự phòng có giới hạn).
type Source string

// Ba nguồn của GetOrLoad.
const (
	SourceCache    Source = "cache"
	SourceDB       Source = "db"
	SourceFallback Source = "db_fallback"
)

// ErrBusy báo đường dự phòng (Valkey không dùng được) của một vùng đã đủ FallbackConcurrency lần tải đồng thời; lớp
// HTTP đổi thành 503 retry_later.
var ErrBusy = errors.New("cache: fallback busy")

// errLoadPanicked là kết quả của lượt gộp khi load của người gọi đầu panic (người chờ cùng khóa không treo).
var errLoadPanicked = errors.New("cache: load panic")

// Mặc định của ReadCache.
const (
	defaultFallbackConcurrency = 8
	defaultMaxValueBytes       = 1 << 20
	// tombstoneTTL: dấu "đã xóa" của Invalidate sống chừng này, chặn một lần tải bắt đầu trước lần xóa ghi lại bản cũ
	// (QĐ-185-3).
	tombstoneTTL = 5 * time.Second
	// Byte đầu của giá trị trong Valkey: 'v' + dữ liệu, hoặc dấu đã xóa.
	valueMark     = 'v'
	tombstoneMark = "t"
)

// ReadCache là bộ đệm đọc trên Valkey cho các API đọc của website (kế hoạch M5 Task 185). Valkey chỉ làm nhanh: mọi
// lỗi Valkey đều đọc thẳng load (PostgreSQL), có giới hạn số lần tải đồng thời theo vùng.
//
// Khi nào không dùng: dữ liệu quyết định nghiệp vụ (giá khi tạo đơn, tồn khi giữ hàng) luôn đọc PostgreSQL trong
// transaction; không gọi trong transaction (ADR-46); id không mang dữ liệu cá nhân (dùng SubjectKey ở nơi khác).
type ReadCache interface {
	// GetOrLoad trả giá trị của (tenant, space, id): có trong bộ đệm thì SourceCache; thiếu thì gọi load một lần cho
	// mọi lời gọi đồng thời cùng khóa trong tiến trình, ghi lại với hạn ttl (giá trị quá MaxValueBytes thì không ghi),
	// SourceDB; Valkey không dùng được thì gọi load dưới semaphore của space, SourceFallback, đầy thì ErrBusy. id phải
	// khớp ^[A-Za-z0-9_-]{1,100}$ (ErrBadKey, load không được gọi). Lỗi của load trả lên (bọc), không ghi gì.
	GetOrLoad(ctx context.Context, tenant uuid.UUID, space Space, id string, ttl time.Duration,
		load func(context.Context) ([]byte, error)) ([]byte, Source, error)
	// Invalidate xóa các id của space (thế hệ hiện tại). Valkey không dùng được: lỗi bọc ErrUnavailable.
	Invalidate(ctx context.Context, tenant uuid.UUID, space Space, ids ...string) error
	// BumpSpace tăng thế hệ của space: mọi id của space đó thành thiếu. Valkey không dùng được: lỗi bọc ErrUnavailable.
	BumpSpace(ctx context.Context, tenant uuid.UUID, space Space) error
}

// ReadCacheOptions cấu hình NewReadCache; số 0 lấy mặc định (FallbackConcurrency 8 mỗi space, MaxValueBytes 1 MiB).
// Metrics nil thì không ghi chỉ số cache_hit, cache_miss, cache_fallback, cache_busy (nhãn space).
type ReadCacheOptions struct {
	FallbackConcurrency int
	MaxValueBytes       int
	Metrics             *metrics.Registry
}

// Script đọc thế hệ và giá trị trong một lượt đi về. KEYS[1] = g:{tenant}:{space}; ARGV[1] = c:{tenant}:{space}:,
// ARGV[2] = id. Trả {thế hệ, giá trị hoặc nil}.
const getScript = `local gen = redis.call('GET', KEYS[1]) or '0'
return {gen, redis.call('GET', ARGV[1] .. gen .. ':' .. ARGV[2])}`

// Script ghi sau khi tải: chỉ khi thế hệ chưa đổi và khóa chưa có (kể cả dấu đã xóa). KEYS[1] = khóa thế hệ;
// ARGV = khóa giá trị, thế hệ lúc đọc, giá trị, hạn ms.
const setScript = `if (redis.call('GET', KEYS[1]) or '0') ~= ARGV[2] then return 0 end
if redis.call('SET', ARGV[1], ARGV[3], 'PX', ARGV[4], 'NX') then return 1 end
return 0`

// Script xóa: ghi dấu đã xóa cho từng id ở thế hệ hiện tại. KEYS[1] = khóa thế hệ; ARGV = tiền tố, hạn ms của dấu,
// dấu, rồi các id.
const invalidateScript = `local gen = redis.call('GET', KEYS[1]) or '0'
for i = 4, #ARGV do redis.call('SET', ARGV[1] .. gen .. ':' .. ARGV[i], ARGV[3], 'PX', ARGV[2]) end
return #ARGV - 3`

type readCache struct {
	c *Conn
	o ReadCacheOptions

	flights flightGroup
	semMu   sync.Mutex
	sems    map[Space]chan struct{}
}

// NewReadCache dựng ReadCache trên c; c nil hợp lệ (không có Valkey: mọi lần đọc đi đường dự phòng).
func NewReadCache(c *Conn, o ReadCacheOptions) ReadCache { //nolint:ireturn // Interfaces của kế hoạch M5 Task 185: bên dùng (httpapi, webcache) cắm bản giả
	if o.FallbackConcurrency <= 0 {
		o.FallbackConcurrency = defaultFallbackConcurrency
	}
	if o.MaxValueBytes <= 0 {
		o.MaxValueBytes = defaultMaxValueBytes
	}
	return &readCache{
		c: c, o: o,
		flights: flightGroup{m: map[string]*flight{}},
		sems:    map[Space]chan struct{}{},
	}
}

// keys trả khóa thế hệ g:{tenant}:{space} và tiền tố c:{tenant}:{space}: sau khi kiểm space.
func keys(tenant uuid.UUID, space Space) (genKey, prefix string, err error) {
	if !keyPart.MatchString(string(space)) {
		return "", "", fmt.Errorf("%w: space", ErrBadKey)
	}
	base := tenant.String() + ":" + string(space)
	return "g:" + base, "c:" + base + ":", nil
}

func checkID(id string) error {
	if !keyPart.MatchString(id) {
		return fmt.Errorf("%w: id", ErrBadKey)
	}
	return nil
}

func (r *readCache) GetOrLoad(ctx context.Context, tenant uuid.UUID, space Space, id string, ttl time.Duration,
	load func(context.Context) ([]byte, error),
) ([]byte, Source, error) {
	genKey, prefix, err := keys(tenant, space)
	if err != nil {
		return nil, "", err
	}
	if err := checkID(id); err != nil {
		return nil, "", err
	}
	gen, val, hit, err := r.read(ctx, genKey, prefix, id)
	if err != nil {
		return r.fallback(ctx, space, load)
	}
	if hit {
		r.inc("cache_hit", space)
		return val, SourceCache, nil
	}
	r.inc("cache_miss", space)
	res := r.flights.do(ctx, prefix+gen+":"+id, func() flightResult {
		// Kiểm lại trong lượt gộp: lời gọi đọc thiếu ngay trước khi lượt trước ghi xong không tải lần nữa.
		gen2, val2, hit2, err2 := r.read(ctx, genKey, prefix, id)
		if err2 == nil && hit2 {
			return flightResult{val: val2, src: SourceCache}
		}
		// Một khách bỏ đi không làm hỏng các khách đang chờ cùng khóa: load chạy với ctx không hủy theo người gọi đầu.
		v, lerr := load(context.WithoutCancel(ctx))
		if lerr != nil {
			return flightResult{err: fmt.Errorf("readcache: tải %s: %w", space, lerr)}
		}
		if err2 == nil && len(v) <= r.o.MaxValueBytes && ttl > 0 {
			r.write(ctx, genKey, prefix+gen2+":"+id, gen2, v, ttl)
		}
		return flightResult{val: v, src: SourceDB}
	})
	return res.val, res.src, res.err
}

// read chạy getScript: thế hệ hiện tại, giá trị và có trúng không (dấu đã xóa là thiếu).
func (r *readCache) read(ctx context.Context, genKey, prefix, id string) (gen string, val []byte, hit bool, err error) {
	var arr []valkey.ValkeyMessage
	err = r.c.Do(ctx, "readcache.get", func(ctx context.Context, cl valkey.Client) error {
		var e error
		arr, e = eval(ctx, cl, getScript, genKey, prefix, id).ToArray()
		return e //nolint:wrapcheck // Do phân loại lỗi thật của valkey-go
	})
	if err != nil {
		return "", nil, false, fmt.Errorf("readcache: đọc: %w", err)
	}
	if len(arr) != 2 { //nolint:mnd // {thế hệ, giá trị} của getScript
		return "", nil, false, fmt.Errorf("readcache: kết quả script có %d phần", len(arr))
	}
	gen, err = arr[0].ToString()
	if err != nil {
		return "", nil, false, fmt.Errorf("readcache: thế hệ: %w", err)
	}
	if _, perr := strconv.ParseInt(gen, 10, 64); perr != nil {
		return "", nil, false, fmt.Errorf("readcache: thế hệ không phải số: %w", perr)
	}
	s, err := arr[1].ToString()
	switch {
	case valkey.IsValkeyNil(err):
		return gen, nil, false, nil
	case err != nil:
		return "", nil, false, fmt.Errorf("readcache: giá trị: %w", err)
	case len(s) == 0 || s[0] != valueMark:
		return gen, nil, false, nil
	}
	return gen, []byte(s[1:]), true, nil
}

// write ghi v sau khi tải; lỗi bỏ qua (giá trị vẫn trả cho người gọi, lần sau tải lại).
func (r *readCache) write(ctx context.Context, genKey, key, gen string, v []byte, ttl time.Duration) {
	ms := max(ttl.Milliseconds(), 1)
	_ = r.c.Do(ctx, "readcache.set", func(ctx context.Context, cl valkey.Client) error {
		return eval(ctx, cl, setScript, genKey, key, gen, string(valueMark)+string(v), strconv.FormatInt(ms, 10)).Error()
	})
}

// fallback gọi load khi Valkey không dùng được, giữ một chỗ của semaphore space; hết chỗ thì ErrBusy ngay.
func (r *readCache) fallback(ctx context.Context, space Space, load func(context.Context) ([]byte, error)) ([]byte, Source, error) {
	sem := r.sem(space)
	select {
	case sem <- struct{}{}:
	default:
		r.inc("cache_busy", space)
		return nil, "", ErrBusy
	}
	defer func() { <-sem }()
	r.inc("cache_fallback", space)
	v, err := load(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("readcache: tải %s (dự phòng): %w", space, err)
	}
	return v, SourceFallback, nil
}

func (r *readCache) sem(space Space) chan struct{} {
	r.semMu.Lock()
	defer r.semMu.Unlock()
	s, ok := r.sems[space]
	if !ok {
		s = make(chan struct{}, r.o.FallbackConcurrency)
		r.sems[space] = s
	}
	return s
}

func (r *readCache) Invalidate(ctx context.Context, tenant uuid.UUID, space Space, ids ...string) error {
	genKey, prefix, err := keys(tenant, space)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	for _, id := range ids {
		if err := checkID(id); err != nil {
			return err
		}
	}
	args := append([]string{prefix, strconv.FormatInt(tombstoneTTL.Milliseconds(), 10), tombstoneMark}, ids...)
	if err := r.c.Do(ctx, "readcache.invalidate", func(ctx context.Context, cl valkey.Client) error {
		return eval(ctx, cl, invalidateScript, genKey, args...).Error()
	}); err != nil {
		return fmt.Errorf("readcache: xóa %s: %w", space, err)
	}
	return nil
}

func (r *readCache) BumpSpace(ctx context.Context, tenant uuid.UUID, space Space) error {
	genKey, _, err := keys(tenant, space)
	if err != nil {
		return err
	}
	if err := r.c.Do(ctx, "readcache.bump", func(ctx context.Context, cl valkey.Client) error {
		return cl.Do(ctx, cl.B().Incr().Key(genKey).Build()).Error()
	}); err != nil {
		return fmt.Errorf("readcache: tăng thế hệ %s: %w", space, err)
	}
	return nil
}

// eval chạy script Lua với một khóa (khóa thế hệ) và các tham số. Dùng EVAL (Valkey giữ bản đã dịch theo mã băm), mỗi
// script vài trăm byte.
func eval(ctx context.Context, cl valkey.Client, script, key string, args ...string) valkey.ValkeyResult {
	return cl.Do(ctx, cl.B().Eval().Script(script).Numkeys(1).Key(key).Arg(args...).Build())
}

func (r *readCache) inc(name string, space Space) {
	if r.o.Metrics != nil {
		r.o.Metrics.Inc(name, string(space), 1)
	}
}

// flightGroup gộp các lời gọi đồng thời cùng khóa thành một (người gọi đầu chạy fn, người tới sau chờ kết quả hoặc
// tự bỏ khi ctx của họ hủy). Viết riêng thay cho golang.org/x/sync/singleflight (ngoài ADR-44).
type flightGroup struct {
	mu sync.Mutex
	m  map[string]*flight
}

type flight struct {
	done chan struct{}
	res  flightResult
}

type flightResult struct {
	val []byte
	src Source
	err error
}

func (g *flightGroup) do(ctx context.Context, key string, fn func() flightResult) flightResult {
	g.mu.Lock()
	if f, ok := g.m[key]; ok {
		g.mu.Unlock()
		select {
		case <-f.done:
			return f.res
		case <-ctx.Done():
			return flightResult{err: fmt.Errorf("readcache: chờ lượt tải: %w", ctx.Err())}
		}
	}
	f := &flight{done: make(chan struct{}), res: flightResult{err: errLoadPanicked}}
	g.m[key] = f
	g.mu.Unlock()
	defer func() {
		g.mu.Lock()
		delete(g.m, key)
		g.mu.Unlock()
		close(f.done)
	}()
	f.res = fn()
	return f.res
}
