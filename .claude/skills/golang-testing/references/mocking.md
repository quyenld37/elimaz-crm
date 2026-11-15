# Fakes and Test Fixtures

> Bản dự án ELIMAZ CRM. Bản gốc dùng `testify/mock`, `testify/assert`, biến fixture toàn cục và `jonboulle/clockwork`; dự án dùng fake viết tay, `testing` chuẩn, hàm tạo fixture và đồng hồ tiêm qua interface (ADR-44 không có testify, gomock, mockery, clockwork). Chi tiết: `../SOURCE.md`.

## Table of Contents

- [Hand-written Fakes](#hand-written-fakes)
- [Fake Organization](#fake-organization)
- [Test Fixtures](#test-fixtures)
- [Time Control](#time-control)

## Hand-written Fakes

Declare the interface where it is consumed (in the package that needs the dependency), keep it small, then write a fake in the test file. A fake records calls and returns canned results; assertions stay in the test with the standard `testing` package.

```go
// user_service.go — the consumer declares only what it needs
type userStore interface {
    GetUser(ctx context.Context, id int64) (*User, error)
    CreateUser(ctx context.Context, user *User) error
}

// user_service_test.go
type fakeUserStore struct {
    users   map[int64]*User
    getErr  error
    created []*User
}

func (f *fakeUserStore) GetUser(ctx context.Context, id int64) (*User, error) {
    if f.getErr != nil {
        return nil, f.getErr
    }
    u, ok := f.users[id]
    if !ok {
        return nil, ErrNotFound
    }
    return u, nil
}

func (f *fakeUserStore) CreateUser(ctx context.Context, user *User) error {
    f.created = append(f.created, user)
    return nil
}

func TestService_GetUser(t *testing.T) {
    want := &User{ID: 1, Name: "John"}
    store := &fakeUserStore{users: map[int64]*User{1: want}}
    service := NewService(store)

    got, err := service.GetUser(t.Context(), 1)
    if err != nil {
        t.Fatalf("GetUser: %v", err)
    }
    if got != want {
        t.Errorf("GetUser = %+v, want %+v", got, want)
    }
}

func TestService_GetUser_NotFound(t *testing.T) {
    service := NewService(&fakeUserStore{users: map[int64]*User{}})

    user, err := service.GetUser(t.Context(), 999)
    if !errors.Is(err, ErrNotFound) {
        t.Fatalf("err = %v, want ErrNotFound", err)
    }
    if user != nil {
        t.Errorf("user = %+v, want nil", user)
    }
}
```

Dự án: code chạm database không dùng fake mà test trên PostgreSQL thật (xem `golang-database/references/testing.md`); fake chỉ dùng cho logic nghiệp vụ.

## Fake Organization

Keep each fake next to the tests that use it, in the consumer package's `_test.go` file. Do not create a shared `mocks`/`testutil` package across modules (dự án cấm gói `util`, `common`, `helpers`, `shared`); a test helper that several packages genuinely need lives in `internal/platform/<việc>` with a noun name.

```go
// user_service.go
type UserService struct {
    db    userStore
    email welcomeMailer
}

type welcomeMailer interface {
    SendWelcomeEmail(ctx context.Context, to string) error
}
```

```go
// user_service_test.go
package mypackage

type fakeMailer struct{ sentTo []string }

func (f *fakeMailer) SendWelcomeEmail(ctx context.Context, to string) error {
    f.sentTo = append(f.sentTo, to)
    return nil
}

func TestUserService_CreateUser(t *testing.T) {
    store := &fakeUserStore{users: map[int64]*User{}}
    mailer := &fakeMailer{}
    service := NewUserService(store, mailer)

    user := &User{Name: "Test", Email: "test@example.com"}
    if err := service.CreateUser(t.Context(), user); err != nil {
        t.Fatalf("CreateUser: %v", err)
    }
    if len(store.created) != 1 {
        t.Errorf("created %d users, want 1", len(store.created))
    }
    if !slices.Equal(mailer.sentTo, []string{"test@example.com"}) {
        t.Errorf("sent to %v, want [test@example.com]", mailer.sentTo)
    }
}
```

(A white-box test — `package mypackage` — is needed here because the interfaces are unexported, consumer-side types.)

## Test Fixtures

Build fixtures with functions that return a fresh value each call, so one test cannot change data another test sees (no package-level fixture variables):

```go
func newDefaultUser() *User {
    return &User{
        ID:        123,
        Name:      "Jane Doe",
        Email:     "jane@example.com",
        CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
    }
}

func newAdminUser() *User {
    u := newDefaultUser()
    u.ID = 1
    u.Name = "Admin User"
    u.Role = "admin"
    return u
}
```

Prefer fixed, readable values over random ones in example-based tests; use rapid when you want generated input.

## Time Control

Make time-dependent code testable without `time.Sleep()` by either:

1. running the test inside `synctest.Test` (Go 1.25+), where `time.Now`, timers and tickers use a fake clock that advances when every goroutine in the bubble is blocked; or
2. injecting a clock through a small consumer-side interface.

```go
// scheduler.go
type clock interface{ Now() time.Time }

type Scheduler struct {
    clock clock
    jobs  []*Job
}

// scheduler_test.go
type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time            { return c.now }
func (c *fakeClock) Advance(d time.Duration)   { c.now = c.now.Add(d) }

func TestScheduler_AddJob(t *testing.T) {
    clk := &fakeClock{now: time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)}
    scheduler := NewScheduler(clk)

    scheduler.AddJob(&Job{ID: "1", RunAt: clk.Now().Add(time.Hour)})
    if got := scheduler.PendingCount(); got != 1 {
        t.Fatalf("pending = %d, want 1", got)
    }

    clk.Advance(2 * time.Hour)
    if got := scheduler.PendingCount(); got != 0 {
        t.Errorf("pending = %d, want 0", got)
    }
}
```
