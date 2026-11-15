# Testing Database Code

> Bản dự án ELIMAZ CRM. Bản gốc dùng `testify/mock`, `go-sqlmock`, sqlx và testify `suite`; dự án dùng fake viết tay cho logic nghiệp vụ và PostgreSQL 18 thật (testcontainers-go) cho mọi câu SQL. sqlmock bị bỏ vì nó không kiểm SQL trên schema thật, trái luật "mọi câu SQL có test trên PostgreSQL thật". Chi tiết: `../SOURCE.md`.

## Table of Contents

- [Business Logic with Fakes](#business-logic-with-fakes)
- [Every Query on Real PostgreSQL](#every-query-on-real-postgresql)
  - [Test database with testcontainers-go](#test-database-with-testcontainers-go)
- [What to Test](#what-to-test)

## Business Logic with Fakes

Declare a small interface where the service consumes it, so business logic can be tested without a database. Implement it with a hand-written fake in the test file:

```go
// user_service.go — consumer-side interface
type userStore interface {
    GetByID(ctx context.Context, id int64) (User, bool, error)
}

// user_service_test.go
type fakeUserStore struct {
    users map[int64]User
}

func (f *fakeUserStore) GetByID(ctx context.Context, id int64) (User, bool, error) {
    u, ok := f.users[id]
    return u, ok, nil
}

func TestUserService_GetUser(t *testing.T) {
    want := User{ID: 1, Name: "Alice", Email: "alice@example.com"}
    svc := NewUserService(&fakeUserStore{users: map[int64]User{1: want}})

    got, err := svc.GetUser(t.Context(), 1)
    if err != nil {
        t.Fatalf("GetUser: %v", err)
    }
    if got != want {
        t.Errorf("GetUser = %+v, want %+v", got, want)
    }
}

func TestUserService_GetUser_NotFound(t *testing.T) {
    svc := NewUserService(&fakeUserStore{users: map[int64]User{}})

    _, err := svc.GetUser(t.Context(), 999)
    if !errors.Is(err, ErrUserNotFound) {
        t.Errorf("err = %v, want ErrUserNotFound", err)
    }
}
```

Unit tests verify business logic, not SQL correctness. They run fast and without external dependencies.

## Every Query on Real PostgreSQL

Every sqlc query and every `*_dynsql.go` builder is tested against a real PostgreSQL 18 with the real goose migrations applied; static queries are additionally checked by `sqlc vet` in CI. Behaviour that only a real database shows — RLS policies, tenant context set with `set_config(..., true)`, row locks, `SKIP LOCKED`, constraint violations — must be tested this way.

```go
//go:build integration

package store_test

func TestCreateAndGetUser(t *testing.T) {
    pool := testdb.New(t) // see golang-testing/references/integration-testing.md
    ctx := t.Context()

    tx, err := pool.Begin(ctx)
    if err != nil {
        t.Fatalf("begin: %v", err)
    }
    defer tx.Rollback(ctx) // each test leaves no data behind

    q := store.New(tx)
    created, err := q.CreateUser(ctx, store.CreateUserParams{Name: "Alice", Email: "alice@example.com"})
    if err != nil {
        t.Fatalf("CreateUser: %v", err)
    }
    if created.ID == 0 {
        t.Fatal("CreateUser returned zero ID")
    }

    got, err := q.GetUser(ctx, created.ID)
    if err != nil {
        t.Fatalf("GetUser: %v", err)
    }
    if got.Name != "Alice" {
        t.Errorf("Name = %q, want Alice", got.Name)
    }
}
```

The rollback-per-test pattern is fast, but it cannot test what needs a commit or a second connection (lock contention, `SKIP LOCKED`, LISTEN/NOTIFY, RLS for a different role); give those tests their own database or truncate between tests.

Run integration tests:

```bash
go test -tags=integration -race ./internal/...
```

### Test database with testcontainers-go

```go
ctr, err := postgres.Run(ctx, "postgres:18-alpine",
    postgres.WithDatabase("testdb"),
    postgres.WithUsername("test"),
    postgres.WithPassword("test"),
    postgres.BasicWaitStrategies(),
)
testcontainers.CleanupContainer(t, ctr)
if err != nil {
    t.Fatalf("starting postgres: %v", err)
}

connStr, err := ctr.ConnectionString(ctx, "sslmode=disable")
if err != nil {
    t.Fatalf("connection string: %v", err)
}
pool, err := pgxpool.New(ctx, connStr)
```

## What to Test

| What                      | Unit test (fake) | Real PostgreSQL |
| ------------------------- | :--------------: | :-------------: |
| Business logic            |        ✓         |                 |
| SQL correctness           |                  |        ✓        |
| Error paths (not found)   |        ✓         |        ✓        |
| Transaction boundaries    |                  |        ✓        |
| NULL handling round-trips |                  |        ✓        |
| Constraint violations     |                  |        ✓        |
| RLS / tenant isolation    |                  |        ✓        |
| Locks, `SKIP LOCKED`      |                  |        ✓        |
| Query performance         |                  | ✓ (with EXPLAIN) |

- Business-logic unit tests use fakes — no database connections.
- Every query is tested on real PostgreSQL; no SQL mocks (go-sqlmock is not used).
- Integration tests use testcontainers-go for reproducible database environments in CI.
- NEVER test against production databases.

→ See the `golang-testing` skill for general test patterns.
