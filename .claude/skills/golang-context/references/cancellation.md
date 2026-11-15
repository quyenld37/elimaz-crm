# Cancellation, Timeouts & Deadlines

## Table of Contents

- [Cancellation](#cancellation)
  - [Why `defer cancel()` matters](#why-defer-cancel-matters)
- [Timeouts and Deadlines](#timeouts-and-deadlines)
  - [`context.WithTimeout` — relative duration](#contextwithtimeout--relative-duration)
  - [`context.WithDeadline` — absolute point in time](#contextwithdeadline--absolute-point-in-time)
  - [Nested timeouts take the shorter deadline](#nested-timeouts-take-the-shorter-deadline)
- [Listening for Cancellation](#listening-for-cancellation)
  - [The `select` pattern](#the-select-pattern)
  - [Checking cancellation in loops](#checking-cancellation-in-loops)
- [`context.AfterFunc` (Go 1.21+)](#contextafterfunc-go-121)
- [`context.WithoutCancel` (Go 1.21+)](#contextwithoutcancel-go-121)

## Cancellation

`context.WithCancel` returns a derived context and a `cancel` function. When `cancel()` is called, the context's `Done()` channel is closed, signaling all listeners to stop.

```go
func processItems(ctx context.Context, items []Item) error {
    ctx, cancel := context.WithCancel(ctx)
    defer cancel() // always defer cancel to free resources

    errs := make([]error, len(items))
    var wg sync.WaitGroup
    for i, item := range items {
        wg.Go(func() {
            if err := processOne(ctx, item); err != nil {
                errs[i] = err
                cancel() // cancel remaining goroutines on first error
            }
        })
    }
    wg.Wait() // dự án: chờ mọi goroutine thoát trước khi trả về, không bỏ goroutine chạy tiếp

    if err := errors.Join(errs...); err != nil {
        return fmt.Errorf("processing items: %w", err)
    }
    return nil
}
```

### Why `defer cancel()` matters

Every `WithCancel`, `WithTimeout`, and `WithDeadline` creates cancellation state; timeout/deadline contexts also use timer resources. `cancel()` MUST be called on all control-flow paths unless the function explicitly returns or transfers ownership of both the context and cancel function. In ordinary scoped work, defer cancel immediately.

```go
// ✗ Bad — cancel is never called, resources leak
func fetch(ctx context.Context) error {
    ctx, _ = context.WithTimeout(ctx, 5*time.Second)
    return doWork(ctx)
}

// ✓ Good — scoped work, defer cancel immediately
func fetch(ctx context.Context) error {
    ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
    defer cancel()
    return doWork(ctx)
}
```

## Timeouts and Deadlines

### `context.WithTimeout` — relative duration

```go
func (s *UserService) GetUser(ctx context.Context, id string) (*User, error) {
    ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
    defer cancel()

    return s.repo.FindByID(ctx, id)
}
```

### `context.WithDeadline` — absolute point in time

```go
func (s *BatchService) ProcessBatch(ctx context.Context, batch Batch) error {
    // The batch must complete by its SLA deadline
    ctx, cancel := context.WithDeadline(ctx, batch.SLADeadline)
    defer cancel()

    for _, item := range batch.Items {
        if err := s.process(ctx, item); err != nil {
            return fmt.Errorf("processing batch item %s: %w", item.ID, err)
        }
    }
    return nil
}
```

### Nested timeouts take the shorter deadline

If a parent context has a 5s timeout and you create a child with 10s, the child still expires at 5s. The shorter deadline always wins.

```go
// Parent has 2s timeout — child's 10s is effectively ignored
parentCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
defer cancel()

childCtx, childCancel := context.WithTimeout(parentCtx, 10*time.Second)
defer childCancel()
// childCtx expires after 2s, not 10s
```

## Listening for Cancellation

### The `select` pattern

Use `ctx.Done()` in a `select` statement to react to cancellation alongside other work:

```go
func poll(ctx context.Context, interval time.Duration) error {
    ticker := time.NewTicker(interval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return ctx.Err() // context.Canceled or context.DeadlineExceeded
        case <-ticker.C:
            if err := doWork(ctx); err != nil {
                return fmt.Errorf("polling: %w", err)
            }
        }
    }
}
```

### Checking cancellation in loops

For CPU-bound work, periodically check `ctx.Err()`:

```go
func processLargeDataset(ctx context.Context, items []Item) error {
    for i, item := range items {
        if ctx.Err() != nil {
            return fmt.Errorf("processing interrupted after %d/%d items: %w", i, len(items), ctx.Err())
        }
        process(item)
    }
    return nil
}
```

## `context.AfterFunc` (Go 1.21+)

Registers a callback that runs in its own goroutine when the context is cancelled. Useful for cleanup without blocking the main flow.

```go
func watchResource(ctx context.Context, res *Resource) {
    stop := context.AfterFunc(ctx, func() {
        // Runs in a new goroutine when ctx is cancelled
        res.Release()
    })

    // If you no longer need the callback, cancel it:
    // stop() returns true if the callback was successfully cancelled
    _ = stop
}
```

## `context.WithoutCancel` (Go 1.21+)

Creates a child context that is not cancelled when the parent is. Use this for work that must finish even if the request is cancelled — like writing an audit record or enqueuing follow-up tasks.

Dự án: không `go f()` trần trong handler (goroutine không có chủ, bị cắt ngang khi deploy). Việc nền đi vào bảng `jobs` trong cùng transaction, hoặc giao cho một worker có chủ (có `Shutdown` chờ việc đang chạy). `WithoutCancel` dùng cho đoạn ngắn phải chạy xong, kèm deadline riêng:

```go
func (h *Handler) CreateOrder(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()

    order, err := h.orderService.Create(ctx, req) // order + audit row + follow-up job in one transaction
    if err != nil {
        // handle error
        return
    }

    // Short cleanup that must complete even if the client disconnects.
    // WithoutCancel preserves context values (trace span) but detaches cancellation; add its own deadline.
    cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
    defer cancel()
    if err := h.notifier.Enqueue(cleanupCtx, order.ID); err != nil { // owned worker, not a bare goroutine
        // handle error
        return
    }

    w.WriteHeader(http.StatusCreated)
}
```

Without `WithoutCancel`, you'd have to choose between `ctx` (which gets cancelled when the client goes away) and `context.Background()` (which loses the trace span and other values). `WithoutCancel` gives you the best of both: values are preserved, but cancellation is detached.
