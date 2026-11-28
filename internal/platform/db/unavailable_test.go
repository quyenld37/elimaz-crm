package db_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/testkit"
)

// Task 529: lỗi "database ngừng, quá tải, quá hạn" do pgx trả thật trên PostgreSQL thật (qua proxy cắt kết nối như
// PostgreSQL bị dừng giữa tải) đều là Unavailable, kể cả khi đã bọc; lỗi câu lệnh, sai cấu hình thì không.

// checkUnavailable đòi err khác nil, Unavailable(err) == want và Unreachable(err) == unreachable, cả khi err đã bọc %w.
func checkUnavailable(t *testing.T, name string, err error, want, unreachable bool) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: không có lỗi", name)
	}
	for _, e := range []error{err, fmt.Errorf("lượt mua flash sale: %w", err)} {
		if got := db.Unavailable(e); got != want {
			t.Errorf("%s: Unavailable(%T %v) = %v, muốn %v", name, e, e, got, want)
		}
		if got := db.Unreachable(e); got != unreachable {
			t.Errorf("%s: Unreachable(%T %v) = %v, muốn %v", name, e, e, got, unreachable)
		}
	}
}

// Không nối được: cổng không ai nghe (từ chối ngay), proxy đang ngừng (đóng kết nối lúc bắt tay); sai mật khẩu (máy
// chủ trả 28P01) là lỗi cấu hình, không phải "ngừng".
func TestUnavailable_Connect(t *testing.T) {
	t.Parallel()
	_, _, appURL := testkit.StartPostgres(t)

	_, err := pgx.Connect(t.Context(), withHost(t, appURL, closedAddr(t)))
	checkUnavailable(t, "connect refused", err, true, true)

	p, u := testkit.StartSwitchProxy(t, appURL)
	p.SetDown(true)
	_, err = pgx.Connect(t.Context(), u)
	checkUnavailable(t, "proxy down", err, true, true)

	bad, err := url.Parse(appURL)
	if err != nil {
		t.Fatal(err)
	}
	bad.User = url.UserPassword(bad.User.Username(), "sai-mat-khau")
	_, err = pgx.Connect(t.Context(), bad.String())
	checkUnavailable(t, "28P01", err, false, false)
}

// Kết nối đứt giữa lúc chờ trả lời (broken pipe, EOF), rồi dùng lại kết nối đã đóng ("conn closed").
func TestUnavailable_BrokenMidQuery(t *testing.T) {
	t.Parallel()
	adminURL, _, appURL := testkit.StartPostgres(t)
	p, u := testkit.StartSwitchProxy(t, appURL)
	conn := ownerConn(t, u)
	done := make(chan error, 1)
	go func() {
		_, err := conn.Exec(t.Context(), "SELECT pg_sleep(5)")
		done <- err
	}()
	waitRunning(t, ownerConn(t, adminURL), "SELECT pg_sleep(5)") // câu đã chạy trên máy chủ; cắt giữa lúc chờ trả lời
	p.SetDown(true)
	checkUnavailable(t, "broken mid-query", <-done, true, true)
	_, err := conn.Exec(t.Context(), "SELECT 1")
	checkUnavailable(t, "conn closed", err, true, true)
}

// Pool và RetryTx qua proxy bị cắt: lỗi là Unavailable, RetryTx không chạy lại (không ErrRetryLater, fn một lần).
func TestUnavailable_PoolRetryTx(t *testing.T) {
	t.Parallel()
	_, _, appURL := testkit.StartPostgres(t)
	p, u := testkit.StartSwitchProxy(t, appURL)
	d, err := db.Open(t.Context(), u, db.PoolOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Pool.Close)
	if err := d.Pool.Ping(t.Context()); err != nil {
		t.Fatal(err)
	}
	p.SetDown(true)
	calls := 0
	err = d.RetryTx(t.Context(), db.TxAPI, func(tx pgx.Tx) error {
		calls++
		_, err := tx.Exec(t.Context(), "SELECT 1")
		return err //nolint:wrapcheck // lỗi thử
	})
	checkUnavailable(t, "RetryTx", err, true, true)
	if errors.Is(err, db.ErrRetryLater) || calls > 1 {
		t.Errorf("RetryTx không được chạy lại lỗi nối: ErrRetryLater %v, %d lần", errors.Is(err, db.ErrRetryLater), calls)
	}
}

// Máy chủ cắt hay hủy câu: 57P01 (pg_terminate_backend như lúc PostgreSQL tắt), 57014 (statement_timeout), hạn của
// ctx (pgconn hủy câu).
func TestUnavailable_ServerSide(t *testing.T) {
	t.Parallel()
	adminURL, _, appURL := testkit.StartPostgres(t)

	conn := ownerConn(t, appURL)
	var pid int32
	if err := conn.QueryRow(t.Context(), "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatal(err)
	}
	if _, err := ownerConn(t, adminURL).Exec(t.Context(), "SELECT pg_terminate_backend($1)", pid); err != nil {
		t.Fatal(err)
	}
	_, err := conn.Exec(t.Context(), "SELECT 1")
	checkUnavailable(t, "57P01", err, true, true)

	conn = ownerConn(t, appURL)
	if _, err := conn.Exec(t.Context(), "SET statement_timeout = '20ms'"); err != nil {
		t.Fatal(err)
	}
	_, err = conn.Exec(t.Context(), "SELECT pg_sleep(1)")
	checkUnavailable(t, "57014", err, true, false)

	conn = ownerConn(t, appURL)
	qctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	_, err = conn.Exec(qctx, "SELECT pg_sleep(1)")
	checkUnavailable(t, "deadline", err, true, false)
}

// Lỗi câu lệnh thật không phải Unavailable: 22P02, 23505, ErrNoRows.
func TestUnavailable_StatementErrors(t *testing.T) {
	t.Parallel()
	_, _, appURL := testkit.StartPostgres(t)
	conn := ownerConn(t, appURL)
	_, err := conn.Exec(t.Context(), "SELECT 'x'::int")
	checkUnavailable(t, "22P02", err, false, false)
	_, err = conn.Exec(t.Context(), "CREATE TEMP TABLE u529 (id int PRIMARY KEY); INSERT INTO u529 VALUES (1), (1)")
	checkUnavailable(t, "23505", err, false, false)
	err = conn.QueryRow(t.Context(), "SELECT 1 WHERE false").Scan(new(int))
	checkUnavailable(t, "ErrNoRows", err, false, false)
}

// Lỗi tổng hợp: lớp SQLSTATE 08, 53, 57 và quá hạn là Unavailable; trong đó chỉ lớp 08, 57P01/57P02/57P03, 53300 là
// Unreachable (không tới được database), còn 53100, 57014, quá hạn ctx thì database có thể vẫn sống. Lỗi tạm của
// RetryTx (40P01…), ErrRetryLater (đã có đường riêng), hủy từ phía client, lỗi thường: không cái nào.
func TestUnavailable_Synthetic(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err                      error
		unavailable, unreachable bool
	}{
		{nil, false, false},
		{&pgconn.PgError{Code: "08006"}, true, true},
		{&pgconn.PgError{Code: "53300"}, true, true},
		{&pgconn.PgError{Code: "57P02"}, true, true},
		{&pgconn.PgError{Code: "57P03"}, true, true},
		{&pgconn.PgError{Code: "53100"}, true, false},
		{&pgconn.PgError{Code: "57014"}, true, false},
		{&pgconn.PgError{Code: "40P01"}, false, false},
		{&pgconn.PgError{Code: "23505"}, false, false},
		{context.DeadlineExceeded, true, false},
		{context.Canceled, false, false},
		{db.ErrRetryLater, false, false},
		{errors.New("hỏng"), false, false},
		{pgx.ErrNoRows, false, false},
	}
	for _, c := range cases {
		if got := db.Unavailable(c.err); got != c.unavailable {
			t.Errorf("Unavailable(%v) = %v, muốn %v", c.err, got, c.unavailable)
		}
		if got := db.Unreachable(c.err); got != c.unreachable {
			t.Errorf("Unreachable(%v) = %v, muốn %v", c.err, got, c.unreachable)
		}
	}
}

// waitRunning chờ tới khi câu q đang chạy trên máy chủ (pg_stat_activity, đọc bằng admin), chốt chống treo 10 s.
func waitRunning(t *testing.T, admin *pgx.Conn, q string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var n int
		if err := admin.QueryRow(t.Context(),
			"SELECT count(*) FROM pg_stat_activity WHERE state = 'active' AND query = $1", q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("quá 10 s chờ câu %q chạy", q)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// closedAddr trả host:port không ai nghe (cổng vừa đóng: nối bị từ chối ngay).
func closedAddr(t *testing.T) string {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

// withHost thay host:port của chuỗi kết nối rawURL.
func withHost(t *testing.T, rawURL, host string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	u.Host = host
	return u.String()
}
