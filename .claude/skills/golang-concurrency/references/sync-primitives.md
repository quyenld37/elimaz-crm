# Sync Primitives Deep Dive

> Dự án ELIMAZ CRM: mọi trạng thái dùng chung (counter, cache, pool, `sync.Once`) là trường của struct do `main` dựng và truyền vào, không phải biến toàn cục của gói; không `init()`. Các ví dụ dưới đây đã đổi theo luật đó (xem `../SOURCE.md`).

## Table of Contents

- [sync.Mutex](#syncmutex)
  - [Embedding Convention](#embedding-convention)
- [sync.RWMutex](#syncrwmutex)
- [sync/atomic](#syncatomic)
- [sync.Map](#syncmap)
- [sync.Pool](#syncpool)
- [sync.Once](#synconce)
- [sync.WaitGroup](#syncwaitgroup)
  - [Go 1.25+: `wg.Go`](#go-125-wggo)
  - [Go <1.25 fallback](#go-125-fallback)
- [golang.org/x/sync/singleflight (cần duyệt)](#golangorgxsyncsingleflight-cần-duyệt)
- [golang.org/x/sync/errgroup (cần duyệt)](#golangorgxsyncerrgroup-cần-duyệt)
  - [Bounded Concurrency with SetLimit](#bounded-concurrency-with-setlimit)

## sync.Mutex

Protects shared state with exclusive access. MUST hold the lock for the shortest time possible — NEVER hold a mutex across I/O, network calls, or channel operations.

```go
type SafeCache struct {
    mu    sync.Mutex
    items map[string]string
}

func (c *SafeCache) Get(key string) (string, bool) {
    c.mu.Lock()
    defer c.mu.Unlock()
    v, ok := c.items[key]
    return v, ok
}

func (c *SafeCache) Set(key, value string) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.items[key] = value
}
```

### Embedding Convention

Embed the mutex as an unexported field, placed directly above the fields it protects:

```go
type Registry struct {
    mu      sync.Mutex // protects entries
    entries map[string]Entry
}
```

## sync.RWMutex

SHOULD be used when reads greatly outnumber writes. Multiple goroutines can hold `RLock` simultaneously; `Lock` is exclusive.

```go
type Config struct {
    mu     sync.RWMutex
    values map[string]string
}

func (c *Config) Get(key string) string {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return c.values[key]
}

func (c *Config) Set(key, value string) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.values[key] = value
}
```

**Pitfall**: Do not upgrade RLock to Lock — this deadlocks. Release RLock first, then acquire Lock.

## sync/atomic

Lock-free operations for simple values. SHOULD be preferred over Mutex for simple counter operations. Faster than mutex for low-contention counters and flags.

```go
// ✓ Good — atomic for a simple counter, owned by a struct
type Stats struct {
    requests atomic.Int64
}

func (s *Stats) HandleRequest() {
    s.requests.Add(1)
}

func (s *Stats) Count() int64 {
    return s.requests.Load()
}
```

```go
// ✓ Good — atomic.Bool for a shutdown flag
type Server struct {
    shuttingDown atomic.Bool
}

func (s *Server) Shutdown() {
    s.shuttingDown.Store(true)
}

func (s *Server) IsRunning() bool {
    return !s.shuttingDown.Load()
}
```

Go 1.19+ provides typed atomics (`atomic.Int64`, `atomic.Bool`, `atomic.Pointer[T]`) — prefer these over raw `atomic.AddInt64`/`atomic.LoadInt64`.

## sync.Map

SHOULD only be used for write-once/read-many patterns. Optimized for two common patterns: (1) keys are written once and read many times, (2) multiple goroutines read/write disjoint key sets. For other patterns, a plain `map` + `sync.RWMutex` is faster.

```go
type Cache struct {
    m sync.Map
}

func (c *Cache) Get(key string) (any, bool) {
    return c.m.Load(key)
}

func (c *Cache) Set(key string, value any) {
    c.m.Store(key, value)
}

func (c *Cache) GetOrSet(key string, compute func() any) any {
    if v, ok := c.m.Load(key); ok {
        return v
    }
    v, _ := c.m.LoadOrStore(key, compute())
    return v
}
```

**When NOT to use `sync.Map`**: when you need to iterate, get the length, or when writes are frequent and keys overlap heavily. Use `sync.RWMutex` + `map` instead.

## sync.Pool

Reuse temporary objects to reduce GC pressure. MUST NOT store pointers to stack-allocated objects. Objects in the pool may be reclaimed at any GC cycle — do not store persistent state.

```go
type Encoder struct {
    bufPool sync.Pool // zero value is usable; New set in the constructor
}

func NewEncoder() *Encoder {
    e := &Encoder{}
    e.bufPool.New = func() any { return new(bytes.Buffer) }
    return e
}

func (e *Encoder) Process(data []byte) string {
    buf := e.bufPool.Get().(*bytes.Buffer)
    defer func() {
        buf.Reset()
        e.bufPool.Put(buf)
    }()

    buf.Write(data)
    // ... transform ...
    return buf.String()
}
```

**Rules**:

- Always `Reset()` before `Put()` — returning dirty objects causes bugs
- Do not assume an object from `Get()` is zeroed — the `New` func only runs if the pool is empty
- Best for short-lived, frequently allocated objects (buffers, encoders, temporary structs)

## sync.Once

MUST be used for one-time initialization. Execute exactly once, regardless of how many goroutines call it concurrently. Thread-safe by design.

Dự án: kết nối database và cấu hình được dựng một lần, tường minh, trong `main` (lỗi thì trả về và thoát), không khởi tạo lười bằng `sync.Once` + `panic`. `sync.Once` hợp với việc chỉ được làm một lần trong đời một đối tượng, như đóng:

```go
type Client struct {
    closeOnce sync.Once
    closeErr  error
    conn      *pgx.Conn
}

// Close is safe to call more than once; every caller sees the first result.
func (c *Client) Close(ctx context.Context) error {
    c.closeOnce.Do(func() {
        c.closeErr = c.conn.Close(ctx)
    })
    return c.closeErr
}
```

Go 1.21+ also provides `sync.OnceFunc`, `sync.OnceValue`, and `sync.OnceValues` for simpler use cases — keep the returned function in a struct field, not a package variable:

```go
type Catalog struct {
    index func() (map[string]int, error)
}

func NewCatalog(load func() (map[string]int, error)) *Catalog {
    return &Catalog{index: sync.OnceValues(load)} // computed on first use, errors returned, no panic
}
```

## sync.WaitGroup

Use `sync.WaitGroup` when you only need to wait for a set of goroutines to finish.

### Go 1.25+: `wg.Go`

`WaitGroup.Go` starts a goroutine, adds it to the group, and removes it from the group when the function returns.

```go
func processAll(items []Item) {
    var wg sync.WaitGroup

    for _, item := range items {
        // Go 1.22+ loop variables are per-iteration when the module has `go 1.22+`.
        // Do not add `item := item` solely for closure capture in modern modules.
        wg.Go(func() {
            process(item)
        })
    }

    wg.Wait()
}
```

Rules:

- `WaitGroup.Go` is Go 1.25+, not Go 1.24.
- The function passed to `wg.Go` must not panic.
- `WaitGroup` does not propagate errors and does not cancel siblings.
- For first-error-wins, cancellation, concurrency limits, or returned values, combine `WaitGroup` with `context.WithCancelCause` and per-goroutine result slots (or `golang.org/x/sync/errgroup` once approved — ngoài ADR-44).

**Benefits of `wg.Go()`**:

- No manual `Add`/`Done` bookkeeping
- Lower risk of `Add`/`Wait` ordering bugs
- Cleaner API for simple fire-and-wait work

**When to use**: Go 1.25+ projects for simple goroutines that must all finish, do not return errors, do not need cancellation, and must not panic. When work returns errors or needs cancellation/limits, add context and result slots as above.

### Go <1.25 fallback

```go
func processAll(ctx context.Context, items []Item) {
    var wg sync.WaitGroup
    for _, item := range items {
        wg.Add(1) // Add BEFORE go
        go func(item Item) {
            defer wg.Done()
            process(ctx, item)
        }(item)
    }
    wg.Wait() // blocks until all goroutines finish
}
```

```go
// ✗ Bad — Add inside the goroutine (race: Wait may return before Add runs)
go func() {
    wg.Add(1)
    defer wg.Done()
    process(item)
}()
```

## golang.org/x/sync/singleflight (cần duyệt)

> Ngoài ADR-44: chỉ dùng khi đã được duyệt. Nếu dùng, `singleflight.Group` là trường của struct sở hữu, không phải biến gói.

Deduplicates concurrent calls for the same key. When multiple goroutines request the same resource simultaneously, only one executes; the rest wait and share the result.

```go
type UserLoader struct {
    group singleflight.Group
    store userStore
}

func (l *UserLoader) GetUser(ctx context.Context, id string) (*User, error) {
    v, err, _ := l.group.Do(id, func() (any, error) {
        // Only one goroutine executes this for a given id
        return l.store.QueryUser(ctx, id)
    })
    if err != nil {
        return nil, err
    }
    return v.(*User), nil
}
```

**Use cases**: cache stampede prevention, deduplicating expensive lookups (DB, API), rate-limited external service calls.

## golang.org/x/sync/errgroup (cần duyệt)

> Ngoài ADR-44: chỉ dùng khi đã được duyệt; mặc định xem bảng "WaitGroup vs errgroup" trong SKILL.md.

Goroutine group with error propagation. Returns the first error from any goroutine. With `WithContext`, cancels remaining goroutines on first error.

```go
func fetchAll(ctx context.Context, urls []string) ([]Response, error) {
    g, ctx := errgroup.WithContext(ctx) // cancel siblings on first error
    results := make([]Response, len(urls))

    for i, url := range urls {
        g.Go(func() error {
            resp, err := fetch(ctx, url)
            if err != nil {
                return fmt.Errorf("fetching %s: %w", url, err)
            }
            results[i] = resp // safe: each goroutine writes to its own index
            return nil
        })
    }

    if err := g.Wait(); err != nil {
        return nil, err
    }
    return results, nil
}
```

### Bounded Concurrency with SetLimit

SHOULD use `SetLimit` to bound concurrency and avoid unbounded goroutine spawning.

```go
g, ctx := errgroup.WithContext(ctx)
g.SetLimit(10) // at most 10 goroutines run concurrently

for _, task := range tasks {
    g.Go(func() error {
        return process(ctx, task)
    })
}
return g.Wait()
```

This replaces hand-rolled worker pools for most use cases.
