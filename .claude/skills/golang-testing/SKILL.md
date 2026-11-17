---
name: golang-testing
description: "Production-ready Golang tests with the standard testing package — table-driven tests, parallel tests, fuzzing, property tests with rapid, testing/synctest, fixtures, hand-written fakes on consumer-side interfaces, integration tests on real PostgreSQL via testcontainers-go, code coverage, benchmarks, idiomatic test naming. Use when writing or reviewing Go tests, choosing a testing approach, or debugging flaky/slow tests. ELIMAZ CRM copy: no testify, no mock generators."
user-invocable: true
license: MIT
metadata:
  author: samber
  version: "1.4.0"
  curated-for: ELIMAZ CRM (see SOURCE.md)
paths:
  - "**/*.go"
---

**Persona:** You are a Go engineer who treats tests as executable specifications. You write tests to constrain behavior, not to hit coverage targets.

**Modes:**

- **Write mode** — generating new tests for existing or new code. Work sequentially through the code under test; write table-driven tests by hand, then enrich with edge cases and error paths.
- **Review mode** — reviewing a PR's test changes. Focus on the diff: check coverage of new behaviour, assertion quality, table-driven structure, and absence of flakiness patterns. Sequential.
- **Audit mode** — auditing an existing test suite for gaps, flakiness, or bad patterns (order-dependent tests, missing `t.Parallel()`, implementation-detail coupling), split by concern: (1) unit test quality and coverage gaps, (2) integration test isolation, (3) goroutine leaks and race conditions. Dự án: chỉ chạy song song sub-agent khi người dùng yêu cầu.
- **Debug mode** — a test is failing or flaky. Work sequentially: reproduce reliably, isolate the failing assertion, trace the root cause in production code or test setup.

> **Thứ tự ưu tiên.** Luật dự án (CLAUDE.md, ADR-44, kế hoạch M0) đứng trên skill này khi hai bên khác nhau.

> **Dự án dùng `testing` chuẩn + rapid (property test) + testcontainers-go (PostgreSQL thật), không dùng testify, gomock, mockery, goleak, clockwork, gotests.** Thêm thư viện test khác cần duyệt theo ADR-44.

# Go Testing Best Practices

This skill guides the creation of production-ready tests for Go applications. Follow these principles to write maintainable, fast, and reliable tests.

## Best Practices Summary

1. Table-driven tests MUST use named subtests -- every test case needs a `name` field passed to `t.Run`
2. Integration tests MUST use build tags (`//go:build integration`) to separate from unit tests — dự án: theo cách tách mà kế hoạch M0 và `make ci` quy định; test SQL vẫn bắt buộc chạy trong CI
3. Tests MUST NOT depend on execution order -- each test MUST be independently runnable
4. Independent tests SHOULD use `t.Parallel()` when possible
5. Tests MUST assert observable behavior and public API contracts, not implementation details -- a test coupled to internals turns every refactor into a test rewrite while proving nothing about the contract
6. Packages with goroutines SHOULD prove every goroutine exits — dự án: dùng `testing/synctest`, test tắt êm (Shutdown/ctx cancel rồi chờ), và `go test -race`; goleak chưa được duyệt
7. Dự án dùng `testing` chuẩn (`t.Errorf`, `t.Fatalf`, so sánh bằng `==`, `slices.Equal`, `maps.Equal`, `reflect.DeepEqual` khi cần), không dùng testify
8. Fake interfaces, not concrete types — viết fake tay, interface khai ở phía dùng
9. Keep unit tests fast (< 1ms), use build tags for integration tests
10. Run tests with race detection in CI
11. Include examples as executable documentation
12. Test files MUST be named after the source file under test, not after the function or method being tested
13. Test functions SHOULD appear in the same order as the functions/methods they test in the source file
14. Quy luật phải luôn đúng của vùng rủi ro cao (tổng sổ bằng 0, không bán vượt) viết thành property test bằng `pgregory.net/rapid`, cộng fuzzing của `go test`

## Test Structure and Organization

### File Conventions

```go
// package_test.go - tests in same package (white-box, access unexported)
package mypackage

// mypackage_test.go - tests in test package (black-box, public API only)
package mypackage_test
```

Name the test file after the source file it tests, not after the function or method under test. Go's convention is one test file per source file (`foo.go` -> `foo_test.go`), because tools (`go test`, coverage reports, IDE "jump to test" navigation) and reviewers all resolve tests by source file, not by symbol. A source file usually declares several functions/methods; splitting its tests by symbol name scatters them across many files and breaks that file-to-file mapping.

```
// ✓ Good — one test file per source file
helloworld.go       -> helloworld_test.go   // contains TestHelloWorld, TestAbcd, TestXyz, ...

// ✗ Bad — test file named after the function/method instead of the source file
helloworld.go       -> abcd_test.go         // wrong: should be helloworld_test.go
```

Exception: very large source files MAY be split into multiple `_test.go` files by concern (e.g. `foo_test.go` + `foo_edgecases_test.go`), but each split file's name MUST still be derived from the source file name, never from an individual function name. Prefer keeping a single `_test.go` file per source file even when it grows large — splitting adds navigation overhead and is rarely worth it; reach for the exception only when a single file becomes genuinely unwieldy to browse or review. (Dự án: `*_dynsql.go` có `*_dynsql_test.go` chạy trên PostgreSQL thật.)

Within a test file, order test functions to match the order their tested functions/methods appear in the source file. A reader (human or agent) scrolling `foo.go` alongside `foo_test.go` can then find the matching test by position instead of searching; drift between the two orderings compounds every time either file grows.

### Naming Conventions

```go
func TestAdd(t *testing.T) { ... }               // function test
func TestMyStruct_MyMethod(t *testing.T) { ... } // method test
func BenchmarkAdd(b *testing.B) { ... }          // benchmark
func ExampleAdd() { ... }                        // example
func FuzzAdd(f *testing.F) { ... }               // fuzz test
```

## Table-Driven Tests

Table-driven tests are the idiomatic Go way to test multiple scenarios. Always name each test case.

```go
func TestCalculatePrice(t *testing.T) {
    tests := []struct {
        name      string
        quantity  int64
        unitPrice int64 // dự án: tiền là int64 đồng
        expected  int64
    }{
        {
            name:      "single item",
            quantity:  1,
            unitPrice: 10_000,
            expected:  10_000,
        },
        {
            name:      "bulk discount - 100 items",
            quantity:  100,
            unitPrice: 10_000,
            expected:  900_000, // 10% discount
        },
        {
            name:      "zero quantity",
            quantity:  0,
            unitPrice: 10_000,
            expected:  0,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := CalculatePrice(tt.quantity, tt.unitPrice)
            if got != tt.expected {
                t.Errorf("CalculatePrice(%d, %d) = %d, want %d",
                    tt.quantity, tt.unitPrice, got, tt.expected)
            }
        })
    }
}
```

## Common Pitfall: Using the Parent `t` Inside Subtests

Inside a `t.Run` closure, always call methods on the subtest's own `t` (the closure parameter), never on a captured parent `t`. A failure reported on the parent `t` is attributed to the parent test, so the failing subtest still reports `--- PASS`, silently hiding which case broke. Shadowing the parameter name `t` (as in the examples here) prevents this.

Verify with a deliberately-broken case: if `go test -v -run TestName` shows `--- FAIL: TestName` but every `--- PASS: TestName/subtest_name` line still says PASS, the failure is being reported on the wrong `t`.

## Property Tests with rapid

```go
import "pgregory.net/rapid"

func TestLedgerAlwaysBalances(t *testing.T) {
    rapid.Check(t, func(t *rapid.T) {
        amounts := rapid.SliceOf(rapid.Int64Range(1, 10_000_000)).Draw(t, "amounts")
        entries := PostTransfers(amounts)
        var sum int64
        for _, e := range entries {
            sum += e.Amount
        }
        if sum != 0 {
            t.Fatalf("ledger sum = %d, want 0", sum)
        }
    })
}
```

## Unit Tests

Unit tests should be fast (< 1ms), isolated (no external dependencies), and deterministic. Dự án: mọi câu SQL (sqlc và `*_dynsql.go`) được test trên PostgreSQL thật, không giả lập database.

## Testing HTTP Handlers

Use `httptest` for handler tests with table-driven patterns. See [HTTP Testing](./references/http-testing.md) for examples with request/response bodies, query parameters, headers, and status code assertions.

## Goroutine Leak Detection

Dự án chưa duyệt `go.uber.org/goleak`. Prove goroutines exit instead:

- run concurrent code inside `synctest.Test` (below): the test fails if goroutines started in the bubble are still blocked when the bubble ends;
- for long-lived components, test the shutdown path: cancel the context or call `Shutdown`, then wait for it to return within a deadline;
- `go test -race ./...` in CI.

## testing/synctest for Deterministic Goroutine Testing

`testing/synctest` (Go 1.25+) provides deterministic tests for goroutines, timers, deadlines, and context cancellation. Time advances only when all goroutines are blocked, making ordering predictable.

When to use `synctest` instead of real time:

- Testing concurrent code with time-based operations (time.Sleep, time.After, time.Ticker)
- When race conditions need to be reproducible
- When tests are flaky due to timing issues

```go
import (
    "context"
    "testing"
    "testing/synctest"
    "time"
)

func TestContextTimeout(t *testing.T) {
    synctest.Test(t, func(t *testing.T) {
        const timeout = 5 * time.Second

        ctx, cancel := context.WithTimeout(t.Context(), timeout)
        defer cancel()

        time.Sleep(timeout - time.Nanosecond)
        synctest.Wait()
        if err := ctx.Err(); err != nil {
            t.Fatalf("before timeout: %v", err)
        }

        time.Sleep(time.Nanosecond)
        synctest.Wait()
        if err := ctx.Err(); err != context.DeadlineExceeded {
            t.Fatalf("after timeout: got %v, want DeadlineExceeded", err)
        }
    })
}
```

Use `synctest.Test` in Go 1.25+ and later. Do not use the old Go 1.24 experimental `synctest.Run` API in Go 1.25+ code. If a module explicitly targets Go 1.24 and opts into `GOEXPERIMENT=synctest`, use the old API only as a compatibility fallback.

Key differences in `synctest`:

- `time.Sleep` advances synthetic time instantly when the goroutine blocks
- `time.After` fires when synthetic time reaches the duration
- All goroutines run to blocking points before time advances
- Test execution is deterministic and repeatable
- Go 1.27+ adds `synctest.Sleep(d)` as a direct helper to advance the bubble's fake clock, equivalent to `time.Sleep(d)` followed by `synctest.Wait()` but without needing a real goroutine to block on (chưa xác minh với release notes Go 1.27)

Go 1.27+ also adds `httptest.NewTestServer()`, an in-memory fake-network variant of `httptest.NewServer` that composes with `synctest` — no real socket, so server tests can run inside a `synctest.Test` bubble instead of needing `httptest.NewServer` plus real timers (chưa xác minh với release notes Go 1.27).

## Test Timeouts

For tests that may hang, use a timeout helper that panics with caller location. See [Helpers](./references/helpers.md).

## Benchmarks

Write benchmarks as sub-benchmarks (`b.Run` per variant) so each variant gets its own name in the output — that name is what comparison tooling diffs. For Go 1.24+, use `b.Loop()` rather than a `b.N` loop.

→ See [Benchmarks in a Test Suite](./references/benchmarks.md) for the code shape and size-parameterized examples.

## Go 1.26+: test artifacts

When a test, benchmark, or fuzz target needs to persist files for inspection, use `ArtifactDir()` instead of ad-hoc paths or repo-local output.

```go
func TestRenderGoldenArtifact(t *testing.T) {
    dir := t.ArtifactDir()

    out := filepath.Join(dir, "rendered.json")
    if err := os.WriteFile(out, renderedBytes, 0o644); err != nil {
        t.Fatal(err)
    }

    t.Logf("artifact written: %s", out)
}
```

Available on `*testing.T`, `*testing.B`, and `*testing.F` in Go 1.26+.

### Go 1.27+: `stdversion` runs automatically

`go test` now invokes the `stdversion` vet check by default, flagging any use of an API newer than the module's `go` directive. A CI failure from this check means either the `go` directive needs bumping or the code needs to stop using the newer API — it is not a check to silence. (Chưa xác minh với release notes Go 1.27.)

## Parallel Tests

Use `t.Parallel()` to run tests concurrently:

```go
func TestParallelOperations(t *testing.T) {
    tests := []struct {
        name string
        data []byte
    }{
        {"small data", make([]byte, 1024)},
        {"medium data", make([]byte, 1024*1024)},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()

            result := Process(tt.data)
            if result == nil {
                t.Fatal("Process returned nil")
            }
        })
    }
}
```

## Fuzzing

Use fuzzing to find edge cases and bugs:

```go
func FuzzReverse(f *testing.F) {
    f.Add("hello")
    f.Add("")
    f.Add("a")

    f.Fuzz(func(t *testing.T, input string) {
        reversed := Reverse(input)
        doubleReversed := Reverse(reversed)
        if input != doubleReversed {
            t.Errorf("Reverse(Reverse(%q)) = %q, want %q", input, doubleReversed, input)
        }
    })
}
```

## Examples as Documentation

`ExampleXxx` functions are executable documentation: `go test` compares their stdout to the `// Output:` comment, so a drifting example fails the build instead of misleading readers.

→ See [Examples as Documentation](./references/examples.md) for naming rules, `Unordered output`, and placement.

## Code Coverage

Generate a profile with `go test -coverprofile=coverage.out ./...`, then read the uncovered lines with `go tool cover -html=coverage.out`. Coverage locates untested paths; it does not measure assertion quality, so treat a percentage as a gap finder rather than a target. (Dự án đo chất lượng test vùng rủi ro cao bằng mutation testing với gremlins.)

→ See [Code Coverage](./references/coverage.md) for coverage modes, `-coverpkg`, and reporting pitfalls.

## Integration Tests

Use build tags to separate integration tests from unit tests. Dự án: PostgreSQL 18 thật dựng bằng testcontainers-go, migration goose, kết nối pgx (không `database/sql`, không docker-compose trong test):

```go
//go:build integration

package mypackage_test

func TestDatabaseIntegration(t *testing.T) {
    pool := testdb.New(t) // helper dựng PostgreSQL 18 bằng testcontainers-go và chạy migration goose
    // Test real database operations through sqlc queries or *_dynsql.go code
    _ = pool
}
```

Run integration tests separately:

```bash
go test -tags=integration ./...
```

For a testcontainers-go setup, schema via goose migrations, and test data, see [Integration Testing](./references/integration-testing.md).

## Fakes Instead of Mocks

Fake interfaces, not concrete types. Define interfaces where consumed, then write small hand-written fakes in the test file.

For fake patterns, test fixtures, and time control, see [Mocking](./references/mocking.md).

## Enforce with Linters

Many test best practices are enforced automatically by golangci-lint linters such as `thelper` and `paralleltest` — dự án: bật hay không theo cấu hình golangci-lint v2 của repo.

## Related skills in this project

- `golang-database` (references/testing.md) — database integration test patterns
- `golang-concurrency` — goroutine lifecycle and leak prevention
- `golang-context` — `t.Context()` and cancellation in tests

## Quick Reference

```bash
go test ./...                          # all tests
go test -run TestName ./...            # specific test by exact name
go test -run TestName/subtest ./...    # subtests within a test
go test -run 'Test(Add|Sub)' ./...     # multiple tests (regexp OR)
go test -run 'Test[A-Z]' ./...         # tests starting with capital letter
go test -run 'TestUser.*' ./...        # tests matching prefix
go test -run '.*Validation.*' ./...    # tests containing substring
go test -run TestName/. ./...          # all subtests of TestName
go test -run '/(unit|integration)' ./... # filter by subtest name
go test -race ./...                    # race detection
go test -cover ./...                   # coverage summary
go test -bench=. -benchmem ./...       # benchmarks
go test -fuzz=FuzzName ./...           # fuzzing
go test -tags=integration ./...        # integration tests
```
