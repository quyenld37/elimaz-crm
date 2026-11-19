package realtime_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/testkit"
)

const testAUD = "aud-crm"

// Nhịp nhỏ trong test (kế hoạch: Heartbeat, Batch nhỏ).
const (
	testBatch     = 100 * time.Millisecond
	testHeartbeat = 200 * time.Millisecond
)

// rtEnv: database thật đã migrate, hub LISTEN thật (crm_app), máy chủ httpapi thật (auth Access thử, guard) có route
// GET /api/v1/stream.
type rtEnv struct {
	t        *testing.T
	staff    *testkit.Staff
	adminURL string
	keys     *testkit.AccessKeys
	hub      *realtime.PGHub
	spy      *statusSpy // Bus mà máy chủ HTTP dùng: hub, có đếm lời gọi Status của luồng SSE
	reg      *metrics.Registry
	srv      *httptest.Server
}

func newRTEnv(t *testing.T) *rtEnv {
	t.Helper()
	adminURL, ownerURL, appURL := testkit.StartPostgres(t)
	e := &rtEnv{t: t, staff: &testkit.Staff{}, adminURL: adminURL, keys: testkit.AccessKeyServer(t), reg: metrics.NewRegistry()}
	for _, p := range []struct {
		dst **db.DB
		url string
	}{{&e.staff.App, appURL}, {&e.staff.Owner, ownerURL}} {
		d, err := db.Open(t.Context(), p.url, db.PoolOptions{MaxConns: 8})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(d.Pool.Close)
		*p.dst = d
	}
	logger := slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
	e.hub = realtime.NewPGHub(realtime.PGHubOptions{URL: appURL, Metrics: e.reg, Logger: logger})
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() { e.hub.Run(ctx) })
	e.spy = &statusSpy{Bus: e.hub, idle: make(chan struct{}, 1)}
	v := e.keys.Verifier(t, testAUD)
	stop := make(chan struct{})
	e.srv = httptest.NewUnstartedServer(httpapi.NewServer(httpapi.Deps{
		Config: config.Config{}, DB: e.staff.App, Logger: logger, Access: v, Realtime: e.spy,
		SSE: realtime.SSEOptions{Batch: testBatch, Heartbeat: testHeartbeat, Stop: stop},
	}))
	e.srv.Config.RegisterOnShutdown(func() { close(stop) })
	e.srv.Start()
	t.Cleanup(func() {
		e.srv.Close()
		cancel()
		wg.Wait()
	})
	e.waitListening(true, 10*time.Second)
	return e
}

// statusSpy bọc Bus để test biết luồng SSE đã làm gì, không đoán bằng giờ ngủ. Luồng SSE chỉ gọi Status ở nhịp tim
// và ghi ": ping" ngay sau khi nhận true, trong cùng goroutine; nên với một client: số lần nhận true là số ping đã ghi
// hoặc sắp ghi, và khi nó hỏi lại mà nhận false thì mọi ping trước đó đã ghi xong.
type statusSpy struct {
	realtime.Bus
	mu    sync.Mutex
	pings int
	idle  chan struct{} // sức chứa 1: có lời gọi Status nhận false
}

func (s *statusSpy) Status() (bool, int) {
	listening, clients := s.Bus.Status()
	if listening {
		s.mu.Lock()
		s.pings++
		s.mu.Unlock()
	} else {
		select {
		case s.idle <- struct{}{}:
		default:
		}
	}
	return listening, clients
}

// pingsWritten trả số lần Status nhận true (số ping luồng SSE đã ghi hoặc sắp ghi).
func (s *statusSpy) pingsWritten() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pings
}

// waitListening chờ hub báo listening == want trong timeout.
func (e *rtEnv) waitListening(want bool, timeout time.Duration) {
	e.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if l, _ := e.hub.Status(); l == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	e.t.Fatalf("hub không về listening = %v trong %s", want, timeout)
}

// user tạo nhân viên email ở tenant với các grant (quyền, phạm vi "all" hoặc "page:<id>") và trả thẻ Access.
func (e *rtEnv) user(tenant uuid.UUID, email string, grants map[string][]string) string {
	e.t.Helper()
	uid := e.staff.User(e.t, tenant, email, "active")
	for scope, perms := range grants {
		typ, id := scope, ""
		if scope != "all" {
			typ, id = "page", scope[len("page:"):]
		}
		e.staff.Grant(e.t, tenant, uid, perms, typ, id)
	}
	return e.keys.Sign(testkit.AccessClaims{Email: email, AUD: testAUD, MFA: []string{"mfa"}})
}

var errRollback = errors.New("lùi transaction thử")

// publish phát s trong một transaction của crm_app; rollback → transaction lùi sau khi Publish.
func (e *rtEnv) publish(s realtime.Signal, rollback bool) {
	e.t.Helper()
	ctx := e.t.Context()
	err := pgx.BeginFunc(ctx, e.staff.App.Pool, func(tx pgx.Tx) error {
		if err := e.hub.Publish(ctx, tx, s); err != nil {
			return fmt.Errorf("publish: %w", err)
		}
		if rollback {
			return errRollback
		}
		return nil
	})
	if err != nil && (!rollback || !errors.Is(err, errRollback)) {
		e.t.Fatalf("publish: %v", err)
	}
}

// open mở luồng SSE và đòi khung đầu là resync.
func (e *rtEnv) open(token string) *testkit.SSEClient {
	e.t.Helper()
	c := testkit.OpenSSE(e.t, e.srv.URL, token)
	ev, data, err := c.Next(2 * time.Second)
	if err != nil || ev != "resync" || string(data) != "{}" {
		e.t.Fatalf("khung đầu: %q %s %v, mong resync {}", ev, data, err)
	}
	return c
}

// nextNonPing trả khung kế tiếp không phải ": ping" trong timeout.
func nextNonPing(c *testkit.SSEClient, timeout time.Duration) (string, []byte, error) {
	deadline := time.Now().Add(timeout)
	for {
		left := time.Until(deadline)
		if left <= 0 {
			return "", nil, testkit.ErrSSETimeout
		}
		ev, data, err := c.Next(left)
		if err != nil {
			return "", nil, fmt.Errorf("chờ khung: %w", err)
		}
		if ev != ":ping" {
			return ev, data, nil
		}
	}
}

// openSub mở luồng cho một nhân viên mới chỉ có realtime.subscribe phạm vi all.
func (e *rtEnv) openSub(tenant uuid.UUID, email string) *testkit.SSEClient {
	e.t.Helper()
	return e.open(e.user(tenant, email, map[string][]string{"all": {"realtime.subscribe"}}))
}

// sig là báo hiệu chỉ cần realtime.subscribe, đối tượng {"x", id}.
func sig(tenant uuid.UUID, topic, id string) realtime.Signal {
	return realtime.Signal{TenantID: tenant, Topic: topic, Requires: "realtime.subscribe", Entity: realtime.Entity{Type: "x", ID: id}}
}

// convP2 là báo hiệu hội thoại c9 trên page:p2, cần conversation.read.
func convP2(tenant uuid.UUID) realtime.Signal {
	return realtime.Signal{
		TenantID: tenant, Topic: "conversation.changed", Requires: "conversation.read",
		Scope: &identity.Scope{Type: identity.ScopePage, ID: "p2"}, Entity: realtime.Entity{Type: "conversation", ID: "c9"},
	}
}
