---
name: golang-concurrency
description: "Golang concurrency design — goroutine lifecycle and leak prevention, channels and `select`, channel ownership and direction, `sync.Mutex`/`RWMutex`/`sync.Map`/`sync.Once`/atomics, `sync.WaitGroup`, bounded worker pools, and fan-out/fan-in pipelines. Use when writing or reviewing concurrent Go code, when choosing between channels and mutexes, when protecting a shared map or counter, or when a goroutine has no clear exit. Not for defensive coding unrelated to concurrency (nil panics, slice aliasing, numeric overflow), and not for debugging a specific hung or racing program after the fact. ELIMAZ CRM copy: every goroutine owned; golang.org/x/sync needs approval."
user-invocable: true
license: MIT
metadata:
  author: samber
  version: "1.2.1"
  curated-for: ELIMAZ CRM (see SOURCE.md)
paths:
  - "**/*.go"
---

**Persona:** You are a Go concurrency engineer. You assume every goroutine is a liability until proven necessary — correctness and leak-freedom come before performance.

**Modes:**

- **Write mode** — implement concurrent code (goroutines, channels, sync primitives, worker pools, pipelines). Follow the sequential instructions below.
- **Review mode** — reviewing a PR's concurrent code changes. Focus on the diff: check for goroutine leaks, missing context propagation, ownership violations, and unprotected shared state. Sequential.
- **Audit mode** — auditing existing concurrent code across a codebase, split by the checks in the "Concurrency Audits" section. Dự án: chỉ chạy song song sub-agent khi người dùng yêu cầu.

> **Thứ tự ưu tiên.** Luật dự án (CLAUDE.md, ADR-44) đứng trên skill này khi hai bên khác nhau.

> **Dự án:** mọi goroutine phải có chủ: một thành phần khởi động nó, có cách bảo nó dừng (ctx hoặc `Shutdown`) và chờ nó thoát. `golang.org/x/sync` (`errgroup`, `singleflight`, `semaphore`) **không có trong ADR-44**: muốn dùng thì xin duyệt và ghi `docs/phu-thuoc.md`; mặc định dùng `sync.WaitGroup` + channel/ctx của thư viện chuẩn. Không biến toàn cục có thể đổi (counter, cache, pool, `sync.Once` ở mức gói) và không `init()`.

# Go Concurrency Best Practices

Go's concurrency model is built on goroutines and channels. Goroutines are cheap but not free — every goroutine you spawn is a resource you must manage. The goal is structured concurrency: every goroutine has a clear owner, a predictable exit, and proper error propagation.

## Core Principles

1. **Every goroutine must have a clear exit** — without a shutdown mechanism (context, done channel, WaitGroup), they leak and accumulate until the process crashes
2. **Share memory by communicating** — channels transfer ownership explicitly; mutexes protect shared state but make ownership implicit
3. **Send copies, not pointers** on channels — sending pointers creates invisible shared memory, defeating the purpose of channels
4. **Only the sender closes a channel** — closing from the receiver side panics if the sender writes after close
5. **Specify channel direction** (`chan<-`, `<-chan`) — the compiler prevents misuse at build time
6. **Default to unbuffered channels** — larger buffers mask backpressure; use them only with measured justification
7. **Always include `ctx.Done()` in select** — without it, goroutines leak after caller cancellation
8. **Avoid repeated `time.After` in hot loops** — each call allocates a timer and creates unnecessary churn; use `time.NewTimer` + `Reset` for long-running loops
9. **Prove goroutines exit in tests** — dự án: `testing/synctest`, test đường tắt êm, `go test -race` (goleak chưa được duyệt)

For detailed channel/select code examples, see [Channels and Select Patterns](references/channels-and-select.md).

## Channel vs Mutex vs Atomic

| Scenario | Use | Why |
| --- | --- | --- |
| Passing data between goroutines | Channel | Communicates ownership transfer |
| Coordinating goroutine lifecycle | Channel + context | Clean shutdown with select |
| Protecting shared struct fields | `sync.Mutex` / `sync.RWMutex` | Simple critical sections |
| Simple counters, flags | `sync/atomic` | Lock-free, lower overhead |
| Many readers, few writers on a map | `sync.Map` | Optimized for read-heavy workloads. **Concurrent map read/write causes a hard crash** |
| Caching expensive computations | `sync.Once` (field of the owning struct) / `singleflight` (cần duyệt, ngoài ADR-44) | Execute once or deduplicate |

## WaitGroup vs errgroup

`errgroup` thuộc `golang.org/x/sync`, chưa có trong ADR-44 (cần duyệt). Cột "stdlib" là cách làm mặc định của dự án.

| Need | stdlib (mặc định) | errgroup (cần duyệt) |
| --- | --- | --- |
| Wait for goroutines, errors not needed | `sync.WaitGroup` (`wg.Go`) | — |
| Wait + collect errors | `wg.Go` + each goroutine writes its own slot of an `[]error`, then `errors.Join` | `errgroup.Group` |
| Wait + cancel siblings on first error | `context.WithCancelCause` + `wg.Go`, call `cancel(err)` on first error | `errgroup.WithContext` |
| Wait + limit concurrency | buffered-channel semaphore or N worker goroutines reading one channel | `errgroup.SetLimit(n)` |

## Sync Primitives Quick Reference

| Primitive | Use case | Key notes |
| --- | --- | --- |
| `sync.Mutex` | Protect shared state | Keep critical sections short; never hold across I/O |
| `sync.RWMutex` | Many readers, few writers | Never upgrade RLock to Lock (deadlock) |
| `sync/atomic` | Simple counters, flags | Prefer typed atomics (Go 1.19+): `atomic.Int64`, `atomic.Bool` |
| `sync.Map` | Concurrent map, read-heavy | No explicit locking; use `RWMutex`+map when writes dominate |
| `sync.Pool` | Reuse temporary objects | Always `Reset()` before `Put()`; reduces GC pressure |
| `sync.Once` | One-time initialization | Go 1.21+: `OnceFunc`, `OnceValue`, `OnceValues` |
| `sync.WaitGroup` | Waiting for simple goroutines | Go 1.25+: prefer `wg.Go(func(){ ... })` for fire-and-wait tasks that do not panic. For Go <1.25 use `Add`/`Done`. For errors/cancellation/limits, combine with context and an error slice (see table above). |
| `x/sync/singleflight` | Deduplicate concurrent calls | Cache stampede prevention — cần duyệt (ngoài ADR-44) |
| `x/sync/errgroup` | Goroutine group + errors | `SetLimit(n)` — cần duyệt (ngoài ADR-44) |

For detailed examples and anti-patterns, see [Sync Primitives Deep Dive](references/sync-primitives.md).

## Concurrency Checklist

Before spawning a goroutine, answer:

- [ ] **How will it exit?** — context cancellation, channel close, or explicit signal
- [ ] **Can I signal it to stop?** — pass `context.Context` or done channel
- [ ] **Can I wait for it?** — `sync.WaitGroup` held by the owning component
- [ ] **Who owns it?** — dự án: thành phần nào khởi động thì thành phần đó dừng và chờ nó khi tắt
- [ ] **Who owns the channels?** — creator/sender owns and closes
- [ ] **Should this be synchronous instead?** — don't add concurrency without measured need

## Pipelines and Worker Pools

For pipeline patterns (fan-out/fan-in, bounded workers, generator chains, Go 1.23+ iterators), see [Pipelines and Worker Pools](references/pipelines.md).

## Concurrency Audits

When auditing concurrency across a large codebase, check:

1. Find all goroutine spawns (`go func`, `go method`) and verify shutdown mechanisms
2. Search for mutable globals and shared state without synchronization
3. Audit channel usage — ownership, direction, closure, buffer sizes
4. Find `time.After` in loops, missing `ctx.Done()` in select, unbounded spawning
5. Check mutex usage, `sync.Map`, atomics, and thread-safety documentation

## Common Mistakes

| Mistake | Fix |
| --- | --- |
| Fire-and-forget goroutine | Provide stop mechanism (context, done channel) |
| Closing channel from receiver | Only the sender closes |
| `time.After` in hot loop | Reuse `time.NewTimer` + `Reset` |
| Missing `ctx.Done()` in select | Always select on context to allow cancellation |
| Unbounded goroutine spawning | Use a fixed worker count or a channel semaphore |
| Sharing pointer via channel | Send copies or immutable values |
| `wg.Add` inside goroutine | Call `Add` before `go` — `Wait` may return early otherwise |
| Forgetting `-race` in CI | Always run `go test -race ./...` |
| Mutex held across I/O | Keep critical sections short |

## Related skills in this project

- `golang-context` — cancellation propagation and timeout patterns
- `golang-testing` — `testing/synctest` and shutdown tests

### Goroutine leak profile

The goroutine leak profile (experimental behind `GOEXPERIMENT=goroutineleakprofile` in Go 1.26) is generally available in `runtime/pprof` since Go 1.27 — no build flag required. It is a useful production-oriented leak signal alongside the existing tools below (chưa xác minh với release notes Go 1.27). Dự án: pprof chỉ mở trên listener nội bộ, không bao giờ ra Internet (xem `golang-security/references/network.md`).

```bash
curl http://localhost:6060/debug/pprof/goroutineleak?debug=2
go tool pprof http://localhost:6060/debug/pprof/goroutineleak
```

Keep existing tools:

- tests: `testing/synctest`, shutdown tests (goleak chưa được duyệt)
- runtime count: `runtime.NumGoroutine()`
- stack dump: `/debug/pprof/goroutine?debug=2`
- race checks: `go test -race ./...`

## References

- [Go Concurrency Patterns: Pipelines](https://go.dev/blog/pipelines)
- [Effective Go: Concurrency](https://go.dev/doc/effective_go#concurrency)
