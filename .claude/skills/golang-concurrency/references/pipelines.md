# Pipelines and Worker Pools

## Table of Contents

- [Pipeline Pattern](#pipeline-pattern)
- [Fan-Out / Fan-In](#fan-out--fan-in)
- [Worker Pool with errgroup (cần duyệt)](#worker-pool-with-errgroup-cần-duyệt)
- [Bounded Concurrency with Semaphore](#bounded-concurrency-with-semaphore)
- [Pipeline Alternatives](#pipeline-alternatives)
  - [Go 1.23+ Iterators (range-over-func)](#go-123-iterators-range-over-func)
- [Goroutine Leak Detection](#goroutine-leak-detection)
- [Common Pipeline Mistakes](#common-pipeline-mistakes)

## Pipeline Pattern

A pipeline is a series of stages connected by channels, where each stage is a goroutine (or group of goroutines) that:

1. Receives values from an upstream channel
2. Processes each value
3. Sends results to a downstream channel

```go
// Stage 1: Generate integers
func generate(ctx context.Context, nums ...int) <-chan int {
    out := make(chan int)
    go func() {
        defer close(out)
        for _, n := range nums {
            select {
            case out <- n:
            case <-ctx.Done():
                return
            }
        }
    }()
    return out
}

// Stage 2: Square each integer
func square(ctx context.Context, in <-chan int) <-chan int {
    out := make(chan int)
    go func() {
        defer close(out)
        for n := range in {
            select {
            case out <- n * n:
            case <-ctx.Done():
                return
            }
        }
    }()
    return out
}

// Usage
func main() {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    ch := generate(ctx, 2, 3, 4)
    results := square(ctx, ch)

    for v := range results {
        fmt.Println(v) // 4, 9, 16
    }
}
```

**Key rules for pipelines**:

- Pipeline stages MUST accept and respect context cancellation — every stage must select on `ctx.Done()` to avoid goroutine leaks on early cancellation
- The producer (first stage) closes its output channel; each subsequent stage closes its own output
- NEVER create unbounded goroutines in pipeline stages
- Use unbuffered channels unless you have measured throughput needs

## Fan-Out / Fan-In

**Fan-out**: multiple goroutines read from the same channel to parallelize CPU-bound work. **Fan-in**: multiple channels are merged into a single output channel.

```go
// Fan-out: N workers reading from the same input channel
func fanOut(ctx context.Context, in <-chan Task, workers int) <-chan Result {
    out := make(chan Result)
    var wg sync.WaitGroup

    for i := 0; i < workers; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for {
                select {
                case task, ok := <-in:
                    if !ok {
                        return
                    }
                    select {
                    case out <- process(ctx, task):
                    case <-ctx.Done():
                        return
                    }
                case <-ctx.Done():
                    return
                }
            }
        }()
    }

    go func() {
        wg.Wait()
        close(out)
    }()
    return out
}
```

```go
// Fan-in: merge multiple channels into one
func fanIn(ctx context.Context, channels ...<-chan Result) <-chan Result {
    out := make(chan Result)
    var wg sync.WaitGroup

    for _, ch := range channels {
        wg.Add(1)
        go func(c <-chan Result) {
            defer wg.Done()
            for v := range c {
                select {
                case out <- v:
                case <-ctx.Done():
                    return
                }
            }
        }(ch)
    }

    go func() {
        wg.Wait()
        close(out)
    }()
    return out
}
```

## Worker Pool with errgroup (cần duyệt)

> Dự án: `golang.org/x/sync/errgroup` không có trong ADR-44. Mặc định dùng worker cố định (Fan-Out ở trên) hoặc semaphore bằng channel (dưới đây); chỉ dùng errgroup khi đã được duyệt và ghi `docs/phu-thuoc.md`.

Where approved, `errgroup.SetLimit` gives bounded concurrency with error propagation:

```go
func processAll(ctx context.Context, tasks []Task) error {
    g, ctx := errgroup.WithContext(ctx)
    g.SetLimit(10) // max 10 concurrent workers

    for _, task := range tasks {
        g.Go(func() error {
            return process(ctx, task)
        })
    }
    return g.Wait()
}
```

Use a hand-rolled worker pool only when you need:

- Per-worker state (connections, buffers)
- Custom backpressure or priority scheduling
- Graceful draining with in-flight task completion

## Bounded Concurrency with Semaphore

Standard-library bounded concurrency with error collection (dự án: cách mặc định):

```go
func processAll(ctx context.Context, items []Item) error {
    sem := make(chan struct{}, 10) // semaphore of 10
    errs := make([]error, len(items))
    var wg sync.WaitGroup

    for i, item := range items {
        select {
        case sem <- struct{}{}: // acquire
        case <-ctx.Done():
            wg.Wait()
            return errors.Join(append(errs, ctx.Err())...)
        }
        wg.Go(func() {
            defer func() { <-sem }() // release
            errs[i] = process(ctx, item) // each goroutine writes only its own slot
        })
    }
    wg.Wait()
    return errors.Join(errs...)
}
```

## Pipeline Alternatives

### Go 1.23+ Iterators (range-over-func)

For in-process data transformations that do not need concurrency, iterators avoid the overhead of goroutines and channels:

```go
func Filter[T any](seq iter.Seq[T], pred func(T) bool) iter.Seq[T] {
    return func(yield func(T) bool) {
        for v := range seq {
            if pred(v) {
                if !yield(v) {
                    return
                }
            }
        }
    }
}

func Map[T, U any](seq iter.Seq[T], f func(T) U) iter.Seq[U] {
    return func(yield func(U) bool) {
        for v := range seq {
            if !yield(f(v)) {
                return
            }
        }
    }
}
```

Use iterators when:

- Processing is CPU-bound and does not benefit from parallelism
- You want lazy evaluation without goroutine overhead
- The data source is already sequential (slice, database cursor)

Use goroutine+channel pipelines when:

- Stages involve I/O (network, disk) that benefits from concurrency
- You need true parallelism across CPU cores
- Stages have different throughput characteristics

Dự án không dùng `samber/ro` (ngoài ADR-44); dùng vòng lặp hoặc iterator của thư viện chuẩn.

## Goroutine Leak Detection

Dự án chưa duyệt `go.uber.org/goleak`. Run pipeline tests inside `synctest.Test` (it waits for every goroutine started in the bubble and fails on deadlock), test early cancellation explicitly, and run `go test -race`.

## Common Pipeline Mistakes

| Mistake | Fix |
| --- | --- |
| Missing `ctx.Done()` in pipeline stage | Always select on context to allow cancellation |
| Not closing output channel | Producer must `defer close(out)` |
| Unbounded goroutine spawning | Use a fixed worker count or a channel semaphore |
| Sending mutable data through channel | Send copies or immutable values |
| Blocking send without select | Wrap channel sends in select with `ctx.Done()` |
