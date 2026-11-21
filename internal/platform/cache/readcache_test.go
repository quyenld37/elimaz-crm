package cache_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/valkey-io/valkey-go"

	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/testkit"
)

// loader đếm số lần load được gọi và trả val.
type loader struct {
	calls atomic.Int64
	val   []byte
}

func (l *loader) load(context.Context) ([]byte, error) {
	l.calls.Add(1)
	return l.val, nil
}

func get185(t *testing.T, rc cache.ReadCache, tenant uuid.UUID, space cache.Space, id string, ttl time.Duration,
	l *loader,
) cache.Source {
	t.Helper()
	v, src, err := rc.GetOrLoad(t.Context(), tenant, space, id, ttl, l.load)
	if err != nil {
		t.Fatalf("GetOrLoad %s/%s: %v", space, id, err)
	}
	if !bytes.Equal(v, l.val) {
		t.Fatalf("GetOrLoad %s/%s = %q, muốn %q", space, id, v, l.val)
	}
	return src
}

// counts gom Count của các dòng chỉ số theo khóa "tên/nhãn".
func counts(rows []metrics.Row) map[string]int64 {
	m := map[string]int64{}
	for _, r := range rows {
		m[r.Name+"/"+r.Label] += r.Count
	}
	return m
}

// rcFixture là bộ đệm đọc trên một Valkey thật dùng chung cho các ca của TestReadCache_HitMissInvalidateBump.
type rcFixture struct {
	rc            cache.ReadCache
	conn          *cache.Conn
	tenant, other uuid.UUID
	ttl           time.Duration
}

func TestReadCache_HitMissInvalidateBump(t *testing.T) {
	vk := testkit.StartValkey(t)
	reg := metrics.NewRegistry()
	conn := dial(t, vk.URL("crm_cache"), nil)
	f := rcFixture{
		rc: cache.NewReadCache(conn, cache.ReadCacheOptions{Metrics: reg}), conn: conn,
		tenant: uuid.New(), other: uuid.New(), ttl: time.Minute,
	}
	t.Run("lần hai đọc từ bộ đệm", func(t *testing.T) { rcFromCache(t, f) })
	t.Run("Invalidate một mã", func(t *testing.T) { rcInvalidateOne(t, f) })
	t.Run("BumpSpace làm mới cả space, space khác giữ", func(t *testing.T) { rcBumpSpace(t, f) })
	t.Run("TTL hết thì tải lại", func(t *testing.T) { rcTTL(t, f) })
	t.Run("giá trị quá MaxValueBytes không ghi", func(t *testing.T) { rcTooLarge(t, f) })
	t.Run("mã sai không gọi load", func(t *testing.T) { rcBadID(t, f) })
	t.Run("lỗi của load trả nguyên, không ghi", func(t *testing.T) { rcLoadError(t, f) })
	c := counts(reg.Drain())
	if c["cache_hit/product"] == 0 || c["cache_miss/product"] == 0 {
		t.Fatalf("thiếu chỉ số cache_hit/cache_miss nhãn product: %v", c)
	}
}

// rcFromCache: lần hai đọc từ bộ đệm.
func rcFromCache(t *testing.T, f rcFixture) {
	t.Helper()
	rc, tenant, other, ttl := f.rc, f.tenant, f.other, f.ttl
	l := &loader{val: []byte(`{"p":1}`)}
	if src := get185(t, rc, tenant, cache.SpaceProduct, "p1", ttl, l); src != cache.SourceDB {
		t.Fatalf("lần 1 Source = %q, muốn db", src)
	}
	if src := get185(t, rc, tenant, cache.SpaceProduct, "p1", ttl, l); src != cache.SourceCache {
		t.Fatalf("lần 2 Source = %q, muốn cache", src)
	}
	if n := l.calls.Load(); n != 1 {
		t.Fatalf("load gọi %d lần, muốn 1", n)
	}
	// Tenant khác cùng mã: khóa khác.
	l2 := &loader{val: []byte(`{"p":2}`)}
	if src := get185(t, rc, other, cache.SpaceProduct, "p1", ttl, l2); src != cache.SourceDB {
		t.Fatalf("tenant khác Source = %q, muốn db", src)
	}
}

// rcInvalidateOne: Invalidate một mã.
func rcInvalidateOne(t *testing.T, f rcFixture) {
	t.Helper()
	rc, tenant, ttl := f.rc, f.tenant, f.ttl
	l1, l2 := &loader{val: []byte("a")}, &loader{val: []byte("b")}
	get185(t, rc, tenant, cache.SpaceAvailability, "v1", ttl, l1)
	get185(t, rc, tenant, cache.SpaceAvailability, "v2", ttl, l2)
	if err := rc.Invalidate(t.Context(), tenant, cache.SpaceAvailability, "v1"); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}
	if src := get185(t, rc, tenant, cache.SpaceAvailability, "v1", ttl, l1); src != cache.SourceDB {
		t.Fatalf("sau Invalidate v1 Source = %q, muốn db", src)
	}
	if src := get185(t, rc, tenant, cache.SpaceAvailability, "v2", ttl, l2); src != cache.SourceCache {
		t.Fatalf("v2 không bị xóa mà Source = %q", src)
	}
	if l1.calls.Load() != 2 || l2.calls.Load() != 1 {
		t.Fatalf("load v1 %d lần (muốn 2), v2 %d lần (muốn 1)", l1.calls.Load(), l2.calls.Load())
	}
	// Không mã nào: không lỗi, không làm gì.
	if err := rc.Invalidate(t.Context(), tenant, cache.SpaceAvailability); err != nil {
		t.Fatalf("Invalidate không mã: %v", err)
	}
}

// rcBumpSpace: BumpSpace làm mới cả space, space khác giữ.
func rcBumpSpace(t *testing.T, f rcFixture) {
	t.Helper()
	rc, tenant, other, ttl := f.rc, f.tenant, f.other, f.ttl
	pages := []*loader{{val: []byte("1")}, {val: []byte("2")}, {val: []byte("3")}}
	tax := &loader{val: []byte("t")}
	for i, l := range pages {
		get185(t, rc, tenant, cache.SpaceProductsPage, fmt.Sprintf("page-%d", i), ttl, l)
	}
	get185(t, rc, tenant, cache.SpaceTaxonomy, "all", ttl, tax)
	otherPage := &loader{val: []byte("o")}
	get185(t, rc, other, cache.SpaceProductsPage, "page-0", ttl, otherPage)
	if err := rc.BumpSpace(t.Context(), tenant, cache.SpaceProductsPage); err != nil {
		t.Fatalf("BumpSpace: %v", err)
	}
	for i, l := range pages {
		if src := get185(t, rc, tenant, cache.SpaceProductsPage, fmt.Sprintf("page-%d", i), ttl, l); src != cache.SourceDB {
			t.Fatalf("page-%d sau BumpSpace Source = %q, muốn db", i, src)
		}
		if n := l.calls.Load(); n != 2 {
			t.Fatalf("page-%d load %d lần, muốn 2", i, n)
		}
	}
	if src := get185(t, rc, tenant, cache.SpaceTaxonomy, "all", ttl, tax); src != cache.SourceCache {
		t.Fatalf("space khác bị làm mới: Source = %q", src)
	}
	if src := get185(t, rc, other, cache.SpaceProductsPage, "page-0", ttl, otherPage); src != cache.SourceCache {
		t.Fatalf("tenant khác bị làm mới: Source = %q", src)
	}
}

// rcTTL: TTL hết thì tải lại.
func rcTTL(t *testing.T, f rcFixture) {
	t.Helper()
	rc, tenant := f.rc, f.tenant
	l := &loader{val: []byte("x")}
	get185(t, rc, tenant, cache.SpaceFlash, "cur", 100*time.Millisecond, l)
	time.Sleep(300 * time.Millisecond) // Valkey tự hết hạn theo đồng hồ của nó; ngủ dài hơn TTL chỉ để chắc
	if src := get185(t, rc, tenant, cache.SpaceFlash, "cur", 100*time.Millisecond, l); src != cache.SourceDB {
		t.Fatalf("sau TTL Source = %q, muốn db", src)
	}
}

// rcTooLarge: giá trị quá MaxValueBytes không ghi.
func rcTooLarge(t *testing.T, f rcFixture) {
	t.Helper()
	rc, conn, tenant, ttl := f.rc, f.conn, f.tenant, f.ttl
	l := &loader{val: bytes.Repeat([]byte("z"), 2<<20)}
	if src := get185(t, rc, tenant, cache.SpaceProduct, "big", ttl, l); src != cache.SourceDB {
		t.Fatalf("Source = %q", src)
	}
	if src := get185(t, rc, tenant, cache.SpaceProduct, "big", ttl, l); src != cache.SourceDB {
		t.Fatalf("lần hai Source = %q, muốn db (không được ghi)", src)
	}
	key := "c:" + tenant.String() + ":product:0:big"
	var n int64
	if err := conn.Do(t.Context(), "exists", func(ctx context.Context, cl valkey.Client) error {
		var err error
		n, err = cl.Do(ctx, cl.B().Exists().Key(key).Build()).AsInt64()
		return err //nolint:wrapcheck // Do phân loại lỗi thật của valkey-go
	}); err != nil || n != 0 {
		t.Fatalf("EXISTS %s = %d, %v; muốn 0", key, n, err)
	}
}

// rcBadID: mã sai không gọi load.
func rcBadID(t *testing.T, f rcFixture) {
	t.Helper()
	rc, tenant, ttl := f.rc, f.tenant, f.ttl
	l := &loader{val: []byte("x")}
	for _, id := range []string{"", "a:b", "có dấu"} {
		if _, _, err := rc.GetOrLoad(t.Context(), tenant, cache.SpaceProduct, id, ttl, l.load); !errors.Is(err, cache.ErrBadKey) {
			t.Fatalf("id %q: lỗi %v, muốn ErrBadKey", id, err)
		}
	}
	if err := rc.Invalidate(t.Context(), tenant, cache.SpaceProduct, "a:b"); !errors.Is(err, cache.ErrBadKey) {
		t.Fatalf("Invalidate id sai: %v", err)
	}
	if l.calls.Load() != 0 {
		t.Fatalf("load bị gọi với mã sai")
	}
}

// rcLoadError: lỗi của load trả nguyên, không ghi.
func rcLoadError(t *testing.T, f rcFixture) {
	t.Helper()
	rc, tenant, ttl := f.rc, f.tenant, f.ttl
	errLoad := errors.New("db hỏng")
	fail := func(context.Context) ([]byte, error) { return nil, errLoad }
	if _, _, err := rc.GetOrLoad(t.Context(), tenant, cache.SpaceTaxonomy, "err", ttl, fail); !errors.Is(err, errLoad) {
		t.Fatalf("lỗi %v, muốn errLoad", err)
	}
	l := &loader{val: []byte("ok")}
	if src := get185(t, rc, tenant, cache.SpaceTaxonomy, "err", ttl, l); src != cache.SourceDB {
		t.Fatalf("sau lỗi Source = %q, muốn db", src)
	}
}

// Bản tải bắt đầu trước Invalidate mà xong sau đó không được ghi lại bản cũ (QĐ-185-3); bản tải trước BumpSpace cũng vậy.
func TestReadCache_StaleLoadNotWrittenBack(t *testing.T) {
	vk := testkit.StartValkey(t)
	rc := cache.NewReadCache(dial(t, vk.URL("crm_cache"), nil), cache.ReadCacheOptions{})
	tenant := uuid.New()
	for _, c := range []struct {
		name  string
		space cache.Space
		clear func() error
	}{
		{"Invalidate", cache.SpaceProduct, func() error { return rc.Invalidate(t.Context(), tenant, cache.SpaceProduct, "p") }},
		{"BumpSpace", cache.SpaceProductsPage, func() error { return rc.BumpSpace(t.Context(), tenant, cache.SpaceProductsPage) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			slow := func(context.Context) ([]byte, error) {
				close(started)
				<-release
				return []byte("cũ"), nil
			}
			done := make(chan error, 1)
			go func() {
				_, _, err := rc.GetOrLoad(t.Context(), tenant, c.space, "p", time.Minute, slow)
				done <- err
			}()
			<-started
			if err := c.clear(); err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			close(release)
			if err := <-done; err != nil {
				t.Fatalf("GetOrLoad: %v", err)
			}
			fresh := &loader{val: []byte("mới")}
			if src := get185(t, rc, tenant, c.space, "p", time.Minute, fresh); src != cache.SourceDB {
				t.Fatalf("bản cũ đã được ghi lại: Source = %q", src)
			}
		})
	}
}

func TestReadCache_SingleFlight(t *testing.T) {
	vk := testkit.StartValkey(t)
	rc := cache.NewReadCache(dial(t, vk.URL("crm_cache"), nil), cache.ReadCacheOptions{})
	tenant := uuid.New()
	release := make(chan struct{})
	var calls atomic.Int64
	slow := func(context.Context) ([]byte, error) {
		calls.Add(1)
		<-release
		return []byte("v"), nil
	}
	const n = 100
	var wg sync.WaitGroup
	errs := make(chan error, n)
	var ready sync.WaitGroup
	ready.Add(n)
	for range n {
		wg.Go(func() {
			ready.Done()
			v, _, err := rc.GetOrLoad(t.Context(), tenant, cache.SpaceProduct, "hot", time.Minute, slow)
			if err == nil && string(v) != "v" {
				err = fmt.Errorf("giá trị %q", v)
			}
			errs <- err
		})
	}
	ready.Wait()
	// Lời gọi đầu đã vào load thì thả. Goroutine chưa kịp nhập lượt gộp thì đọc bộ đệm (ghi trước khi lượt kết thúc),
	// hoặc kiểm lại trong lượt gộp mới: số load vẫn là 1 dù goroutine tới lúc nào.
	for calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if c := calls.Load(); c != 1 {
		t.Fatalf("load gọi %d lần, muốn 1", c)
	}
}

func TestReadCache_ValkeyDownFallsBack(t *testing.T) {
	vk := testkit.StartValkey(t)
	reg := metrics.NewRegistry()
	rc := cache.NewReadCache(dial(t, vk.URL("crm_cache"), nil), cache.ReadCacheOptions{Metrics: reg})
	tenant := uuid.New()
	l := &loader{val: []byte("db")}
	get185(t, rc, tenant, cache.SpaceProduct, "p", time.Minute, l)
	vk.Stop(t)
	for i := range 5 {
		if src := get185(t, rc, tenant, cache.SpaceProduct, "p", time.Minute, l); src != cache.SourceFallback {
			t.Fatalf("lần %d Source = %q, muốn db_fallback", i, src)
		}
	}
	if n := l.calls.Load(); n != 6 {
		t.Fatalf("load gọi %d lần, muốn 1 + 5", n)
	}
	if err := rc.BumpSpace(t.Context(), tenant, cache.SpaceProduct); !errors.Is(err, cache.ErrUnavailable) {
		t.Fatalf("BumpSpace khi Valkey ngừng: %v, muốn ErrUnavailable", err)
	}
	if err := rc.Invalidate(t.Context(), tenant, cache.SpaceProduct, "p"); !errors.Is(err, cache.ErrUnavailable) {
		t.Fatalf("Invalidate khi Valkey ngừng: %v, muốn ErrUnavailable", err)
	}
	if got := counts(reg.Drain())["cache_fallback/product"]; got != 5 {
		t.Fatalf("cache_fallback = %d, muốn 5", got)
	}
}

// Conn nil (không cấu hình Valkey) đi đường dự phòng; FallbackConcurrency 2 cho đúng 2 lần tải đồng thời mỗi space,
// lần thứ 3 trở đi nhận ErrBusy ngay; space khác có semaphore riêng.
func TestReadCache_FallbackBusy(t *testing.T) {
	reg := metrics.NewRegistry()
	rc := cache.NewReadCache(nil, cache.ReadCacheOptions{FallbackConcurrency: 2, Metrics: reg})
	tenant := uuid.New()
	release := make(chan struct{})
	var inLoad sync.WaitGroup
	inLoad.Add(2)
	slow := func(context.Context) ([]byte, error) {
		inLoad.Done()
		<-release
		return []byte("v"), nil
	}
	type res struct {
		src cache.Source
		err error
	}
	out := make(chan res, 5)
	var wg sync.WaitGroup
	for i := range 2 {
		wg.Go(func() {
			_, src, err := rc.GetOrLoad(t.Context(), tenant, cache.SpaceProduct, fmt.Sprintf("p%d", i), time.Minute, slow)
			out <- res{src, err}
		})
	}
	inLoad.Wait() // hai chỗ của semaphore đã bị giữ
	for i := range 3 {
		_, _, err := rc.GetOrLoad(t.Context(), tenant, cache.SpaceProduct, fmt.Sprintf("q%d", i), time.Minute, slow)
		if !errors.Is(err, cache.ErrBusy) {
			t.Fatalf("lời gọi thứ %d khi đầy: %v, muốn ErrBusy", i+3, err)
		}
	}
	l := &loader{val: []byte("t")}
	if src := get185(t, rc, tenant, cache.SpaceTaxonomy, "all", time.Minute, l); src != cache.SourceFallback {
		t.Fatalf("space khác Source = %q, muốn db_fallback", src)
	}
	close(release)
	wg.Wait()
	close(out)
	for r := range out {
		if r.err != nil || r.src != cache.SourceFallback {
			t.Fatalf("lời gọi giữ chỗ: %q, %v", r.src, r.err)
		}
	}
	// Đã trả chỗ: gọi tiếp được.
	if src := get185(t, rc, tenant, cache.SpaceProduct, "p9", time.Minute, &loader{val: []byte("v")}); src != cache.SourceFallback {
		t.Fatalf("sau khi trả chỗ Source = %q", src)
	}
	if b := counts(reg.Drain())["cache_busy/product"]; b != 3 {
		t.Fatalf("cache_busy = %d, muốn 3", b)
	}
	if err := rc.BumpSpace(t.Context(), tenant, cache.SpaceProduct); !errors.Is(err, cache.ErrUnavailable) {
		t.Fatalf("BumpSpace với Conn nil: %v", err)
	}
}

// Năm lời gọi đồng thời, load chậm 200 ms, FallbackConcurrency 2: đúng 2 thành công, 3 ErrBusy (ca của kế hoạch).
func TestReadCache_FallbackBusyConcurrent(t *testing.T) {
	rc := cache.NewReadCache(nil, cache.ReadCacheOptions{FallbackConcurrency: 2})
	tenant := uuid.New()
	slow := func(ctx context.Context) ([]byte, error) {
		select {
		case <-time.After(200 * time.Millisecond):
		case <-ctx.Done():
		}
		return []byte("v"), nil
	}
	var ok, busy atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range 5 {
		wg.Go(func() {
			<-start
			_, _, err := rc.GetOrLoad(t.Context(), tenant, cache.SpaceProduct, fmt.Sprintf("p%d", i), time.Minute, slow)
			switch {
			case err == nil:
				ok.Add(1)
			case errors.Is(err, cache.ErrBusy):
				busy.Add(1)
			default:
				t.Errorf("lỗi lạ: %v", err)
			}
		})
	}
	close(start)
	wg.Wait()
	// Goroutine tới khi hai lần tải đầu đã xong (máy rất bận) có thể giành chỗ trống; trần 200 ms chỉ để thường thì
	// cả năm cùng chạy. Quy luật luôn đúng: không bao giờ quá 2 lần tải cùng lúc, tổng là 5.
	if ok.Load()+busy.Load() != 5 || ok.Load() < 2 {
		t.Fatalf("thành công %d, bận %d", ok.Load(), busy.Load())
	}
	if ok.Load() == 2 && busy.Load() != 3 {
		t.Fatalf("thành công 2 mà bận %d", busy.Load())
	}
}
