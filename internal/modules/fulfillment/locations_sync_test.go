package fulfillment_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/carrier/manual"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/fieldcrypt"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
)

// Danh mục thử của CRM (kế hoạch M3, Task M3-18, bước 1): 3 tỉnh, 12 phường (mã có chữ L: không trùng mẫu khác).
const (
	locHN, locGL, locHCM = "L01", "L64", "L79"
	locOddWard           = "L79004" // "Phường Ghềnh Ráng": hãng gọi tên khác hẳn → chưa khớp
)

func seedLocUnits(e *shipEnv) {
	e.t.Helper()
	e.ownerExec(`INSERT INTO customer.admin_units (code, level, parent_code, name, name_search, effective_from, source)
		SELECT c, l, p, n, lower(n), DATE '2025-07-01', 'test' FROM (VALUES
		('L01', 'province', NULL, 'Thành phố Hà Nội'), ('L64', 'province', NULL, 'Tỉnh Gia Lai'),
		('L79', 'province', NULL, 'Thành phố Hồ Chí Minh'),
		('L01001', 'ward', 'L01', 'Phường Ba Đình'), ('L01002', 'ward', 'L01', 'Phường Hoàn Kiếm'),
		('L01003', 'ward', 'L01', 'Phường Cầu Giấy'), ('L01004', 'ward', 'L01', 'Xã Sóc Sơn'),
		('L64001', 'ward', 'L64', 'Phường Pleiku'), ('L64002', 'ward', 'L64', 'Phường Hội Phú'),
		('L64003', 'ward', 'L64', 'Xã Ia Grai'), ('L64004', 'ward', 'L64', 'Xã Chư Sê'),
		('L79001', 'ward', 'L79', 'Phường Sài Gòn'), ('L79002', 'ward', 'L79', 'Phường Bến Thành'),
		('L79003', 'ward', 'L79', 'Phường Tân Định'), ('L79004', 'ward', 'L79', 'Phường Ghềnh Ráng')
		) AS v (c, l, p, n)`)
}

// Địa giới của hãng giả: tên viết kiểu của hãng (có/không dấu, "TP.", "P."), mã khác mã CRM.
func locRemote() map[string][]fulfillment.Location {
	p := func(code, name string) fulfillment.Location {
		return fulfillment.Location{Level: "province", CarrierCode: code, Name: name}
	}
	w := func(parent, code, name string) fulfillment.Location {
		return fulfillment.Location{Level: "ward", CarrierCode: code, Name: name, ParentCarrierCode: parent}
	}
	return map[string][]fulfillment.Location{
		"": {p("HCM", "TP. Hồ Chí Minh"), p("HN", "Hà Nội"), p("GL", "Tỉnh Gia Lai"), p("QT", "Tỉnh Quảng Trị")},
		"HN": {
			w("HN", "H1", "Phuong Ba Dinh"), w("HN", "H2", "Phường Hoàn Kiếm"), w("HN", "H3", "Cau Giay"),
			w("HN", "H4", "Xa Soc Son"),
		},
		"GL": {w("GL", "G1", "Pleiku"), w("GL", "G2", "Phuong Hoi Phu"), w("GL", "G3", "Xã Ia Grai"), w("GL", "G4", "Chu Se")},
		"HCM": {
			w("HCM", "S1", "Phường Sài Gòn"), w("HCM", "S2", "Ben Thanh"), w("HCM", "S3", "P. Tân Định"),
			w("HCM", "S4", "Phường Xuân Hòa"),
		},
	}
}

// stepClock là đồng hồ giả của module: chỉ tiến khi locAdapter gọi phường (giả lập mỗi lần gọi hãng tốn thời gian).
type stepClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *stepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *stepClock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// locAdapter là hãng giả fake (checkHook) với Locations trả locRemote; mỗi lần gọi phường đồng hồ tiến step; err (nếu
// đặt) là lỗi của lần gọi tỉnh kế tiếp.
type locAdapter struct {
	*checkHook
	clock *stepClock
	mu    sync.Mutex
	step  time.Duration
	err   error
	calls int
}

func (a *locAdapter) Locations(_ context.Context, _ fulfillment.Account, level, parent string) ([]fulfillment.Location, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	if level == "province" {
		err := a.err
		a.err = nil
		if err != nil {
			return nil, err
		}
		return locRemote()[""], nil
	}
	a.clock.add(a.step)
	return locRemote()[parent], nil
}

func (a *locAdapter) set(step time.Duration, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.step, a.err = step, err
}

type locEnv struct {
	*accEnv
	ad    *locAdapter
	clock *stepClock
	acc   uuid.UUID
}

// newLocEnv: accEnv với adapter locAdapter, đồng hồ giả, danh mục thử, một tài khoản fake mode api đã kiểm (active).
func newLocEnv(t *testing.T) *locEnv {
	t.Helper()
	e := &locEnv{accEnv: newAccEnv(t), clock: &stepClock{now: time.Date(2026, 11, 21, 8, 0, 0, 0, time.UTC)}}
	e.ad = &locAdapter{checkHook: e.hook, clock: e.clock}
	reg, err := fulfillment.NewRegistry(e.ad, manual.New(fulfillment.SelfCarrierKey))
	if err != nil {
		t.Fatal(err)
	}
	keys, err := fieldcrypt.ParseKeyring(logging.Secret(keySpec("k1", 1)))
	if err != nil {
		t.Fatal(err)
	}
	m := fulfillment.New(fulfillment.Deps{
		DB: e.staff.App, Queue: queue.NewQueue(), Carriers: reg, Keys: keys, Bus: &realtimetest.RecordingBus{}, Now: e.clock.Now,
	})
	e.svc = m.Service
	e.worker = queue.NewWorker(e.staff.App, nil, queue.WorkerOptions{
		WorkerID: "test/" + uuid.NewString(), Commands: commands.NewRunner(e.staff.App, m.CommandHandlers()),
	})
	seedLocUnits(e.shipEnv)
	v := e.mustCreate(e.input(fulfillment.ModeAPI, fulfillment.Credentials{"token": liveToken}))
	e.check(v.ID)
	if got := e.get(v.ID); got.Status != fulfillment.AccountActive {
		t.Fatalf("tài khoản sau kiểm: %s", got.Status)
	}
	e.acc = v.ID
	return e
}

func (e *locEnv) sync() uuid.UUID {
	e.t.Helper()
	var job uuid.UUID
	if err := e.app(func(tx pgx.Tx) (err error) {
		job, err = e.svc.SyncLocations(e.t.Context(), tx, e.acc, e.actor)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("SyncLocations: %v", err)
	}
	return job
}

func (e *locEnv) unmapped(f fulfillment.UnmappedFilter) ([]fulfillment.UnmappedLocation, bool) {
	e.t.Helper()
	var out []fulfillment.UnmappedLocation
	var more bool
	if err := e.app(func(tx pgx.Tx) (err error) {
		out, more, err = e.svc.UnmappedLocations(e.t.Context(), tx, f)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("UnmappedLocations: %v", err)
	}
	return out, more
}

// carrierOf trả hàm tra mã hãng "fake" (LocationMap đọc một lần trong một tx của crm_app).
func (e *locEnv) carrierOf() func(level, our string) (string, bool) {
	e.t.Helper()
	var m fulfillment.LocationMap
	if err := e.app(func(tx pgx.Tx) (err error) {
		m, err = e.svc.LocationMap(e.t.Context(), tx, "fake")
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("LocationMap: %v", err)
	}
	return m.Carrier
}

const locJobs = `SELECT count(*) FROM platform.jobs WHERE kind = 'command.carrier.locations'`

// sums trả tổng số khớp, chưa khớp (tỉnh, phường) ghi trong kết quả mọi lệnh đồng bộ đã done.
func (e *locEnv) sums() [4]int {
	e.t.Helper()
	var s [4]int
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT coalesce(sum((result->>'provincesMatched')::int), 0),
			coalesce(sum((result->>'provincesUnmatched')::int), 0), coalesce(sum((result->>'wardsMatched')::int), 0),
			coalesce(sum((result->>'wardsUnmatched')::int), 0)
			FROM platform.jobs WHERE kind = 'command.carrier.locations' AND status = 'done'`).Scan(&s[0], &s[1], &s[2], &s[3])
	})
	return s
}

// checkSynced: 3 tỉnh và 11 phường có mã hãng; phường lạ chỉ có ở danh sách chưa khớp; LocationMap trả đúng mã.
func (e *locEnv) checkSynced() {
	e.t.Helper()
	if n := e.count(`SELECT count(*) FROM fulfillment.carrier_locations WHERE carrier_key = 'fake' AND level = 'province'`); n != 3 {
		e.t.Errorf("tỉnh có mã hãng = %d, muốn 3", n)
	}
	if n := e.count(`SELECT count(*) FROM fulfillment.carrier_locations WHERE carrier_key = 'fake' AND level = 'ward'`); n != 11 {
		e.t.Errorf("phường có mã hãng = %d, muốn 11", n)
	}
	items, more := e.unmapped(fulfillment.UnmappedFilter{CarrierKey: "fake"})
	var mine []fulfillment.UnmappedLocation
	for _, it := range items {
		if len(it.OurCode) > 0 && it.OurCode[0] == 'L' {
			mine = append(mine, it)
		}
	}
	if more || len(mine) != 1 || mine[0] != (fulfillment.UnmappedLocation{
		Level: "ward", OurCode: locOddWard, Name: "Phường Ghềnh Ráng", ParentCode: locHCM,
	}) {
		e.t.Errorf("chưa khớp = %+v (còn trang sau %v), muốn đúng phường %s", items, more, locOddWard)
	}
	carrier := e.carrierOf()
	for _, c := range []struct{ level, our, want string }{
		{"province", locHCM, "HCM"},
		{"province", locHN, "HN"},
		{"province", locGL, "GL"},
		{"ward", "L01001", "H1"},
		{"ward", "L01003", "H3"},
		{"ward", "L64004", "G4"},
		{"ward", "L79003", "S3"},
	} {
		if got, ok := carrier(c.level, c.our); !ok || got != c.want {
			e.t.Errorf("Carrier(%s, %s) = %q %v, muốn %q", c.level, c.our, got, ok, c.want)
		}
	}
	if got, ok := carrier("ward", locOddWard); ok {
		e.t.Errorf("Carrier(ward, %s) = %q, muốn false (address_unmapped)", locOddWard, got)
	}
}

// Đồng bộ → 3 tỉnh khớp, 11 phường khớp, 1 chưa khớp ở danh sách chưa khớp; LocationMap của phường chưa khớp → false.
// Chạy lại → không thêm dòng, synced_at mới (kế hoạch M3, Task M3-18, bước 1).
func TestLocations_SyncAndRerun(t *testing.T) {
	e := newLocEnv(t)
	e.sync()
	e.drain()
	if n := e.count(locJobs + ` AND status = 'done'`); n != 1 {
		t.Fatalf("lệnh đồng bộ done = %d, muốn 1", n)
	}
	if got := e.sums(); got != [4]int{3, 0, 11, 1} {
		t.Fatalf("kết quả (tỉnh khớp, chưa, phường khớp, chưa) = %v, muốn [3 0 11 1]", got)
	}
	e.checkSynced()
	first := e.clock.Now()
	if n := e.count(`SELECT count(*) FROM fulfillment.carrier_locations WHERE synced_at = $1`, first); n != 14 {
		t.Fatalf("dòng có synced_at của lần đầu = %d, muốn 14", n)
	}

	e.clock.add(time.Hour)
	e.sync()
	e.drain()
	if n := e.count(`SELECT count(*) FROM fulfillment.carrier_locations`); n != 14 {
		t.Fatalf("chạy lại: %d dòng, muốn 14", n)
	}
	if n := e.count(`SELECT count(*) FROM fulfillment.carrier_locations WHERE synced_at = $1 AND updated_at >= created_at`,
		first.Add(time.Hour)); n != 14 {
		t.Fatalf("chạy lại: %d dòng có synced_at mới, muốn 14", n)
	}
	e.checkSynced()
}

// Hạn 25 s: mỗi lần gọi phường tốn 10 s (đồng hồ giả) → việc đầu dừng sau hai tỉnh, ghi việc kế tiếp có cursor; tổng cuối
// như ca đầu.
func TestLocations_CursorContinues(t *testing.T) {
	e := newLocEnv(t)
	e.ad.set(10*time.Second, nil)
	e.sync()
	e.drain()
	if n := e.count(locJobs + ` AND status = 'done'`); n != 2 {
		t.Fatalf("lệnh đồng bộ done = %d, muốn 2", n)
	}
	if n := e.count(locJobs+` AND payload->>'cursor' = $1 AND dedupe_key LIKE 'sync:' || $2 || ':%:' || $1`, locHCM, e.acc.String()); n != 1 {
		t.Fatalf("việc kế tiếp có cursor %s = %d, muốn 1", locHCM, n)
	}
	if n := e.count(locJobs+` AND result->>'next' = $1`, locHCM); n != 1 {
		t.Fatalf("việc đầu phải trả next = %s", locHCM)
	}
	if got := e.sums(); got != [4]int{3, 0, 11, 1} {
		t.Fatalf("tổng (tỉnh khớp, chưa, phường khớp, chưa) = %v, muốn [3 0 11 1]", got)
	}
	e.checkSynced()

	// Đồng bộ lần hai (cùng tài khoản) không bị khóa chống trùng của lần đầu gộp mất.
	e.clock.add(time.Hour)
	e.sync()
	e.drain()
	if n := e.count(locJobs + ` AND status = 'done'`); n != 4 {
		t.Fatalf("sau lần hai: lệnh done = %d, muốn 4", n)
	}
	if n := e.count(`SELECT count(*) FROM fulfillment.carrier_locations WHERE synced_at >= $1`, e.clock.Now().Add(-time.Minute)); n != 14 {
		t.Fatalf("lần hai: %d dòng có synced_at mới, muốn 14", n)
	}
}

// Lỗi của hãng khi đọc tỉnh: 401 → dead lý do auth, tài khoản auth_failed; 429 → chờ theo Retry-After rồi chạy lại xong;
// lỗi mạng → thử lại xong; không hỗ trợ → dead. Không lần nào ghi dòng địa giới khi lỗi.
func TestLocations_CarrierErrors(t *testing.T) {
	t.Run("auth", func(t *testing.T) {
		e := newLocEnv(t)
		e.ad.set(0, &fulfillment.CarrierError{Class: fulfillment.ClassAuth, Code: "token_revoked"})
		e.sync()
		e.drain()
		if n := e.count(locJobs + ` AND status = 'dead' AND last_error LIKE '%auth%'`); n != 1 {
			t.Fatalf("lệnh dead auth = %d, muốn 1", n)
		}
		if got := e.get(e.acc); got.Status != fulfillment.AccountAuthFailed {
			t.Fatalf("tài khoản = %s, muốn auth_failed", got.Status)
		}
		if n := e.count(`SELECT count(*) FROM fulfillment.carrier_locations`); n != 0 {
			t.Fatalf("dòng địa giới = %d, muốn 0", n)
		}
	})
	t.Run("rate_limited", func(t *testing.T) {
		e := newLocEnv(t)
		e.ad.set(0, &fulfillment.CarrierError{Class: fulfillment.ClassRateLimited, RetryAfter: time.Hour})
		e.sync()
		e.drain()
		if n := e.count(locJobs + ` AND status = 'pending' AND run_at > now() + interval '50 minutes'`); n != 1 {
			t.Fatalf("lệnh chờ theo Retry-After = %d, muốn 1", n)
		}
		e.ownerExec(`UPDATE platform.jobs SET run_at = now() WHERE kind = 'command.carrier.locations'`)
		e.drain()
		e.checkSynced()
	})
	t.Run("network", func(t *testing.T) {
		e := newLocEnv(t)
		e.ad.set(0, errors.New("dial tcp: connection refused"))
		e.sync()
		e.drain()
		if n := e.count(locJobs + ` AND status = 'pending' AND attempts = 1`); n != 1 {
			t.Fatalf("lệnh chờ thử lại = %d, muốn 1", n)
		}
		e.ownerExec(`UPDATE platform.jobs SET run_at = now() WHERE kind = 'command.carrier.locations'`)
		e.drain()
		e.checkSynced()
	})
	t.Run("not_supported", func(t *testing.T) {
		e := newLocEnv(t)
		e.ad.set(0, fulfillment.ErrNotSupported)
		e.sync()
		e.drain()
		if n := e.count(locJobs + ` AND status = 'dead'`); n != 1 {
			t.Fatalf("lệnh dead = %d, muốn 1", n)
		}
	})
}

// Ghi lệnh đồng bộ: tài khoản không có (hay tenant khác) → ErrAccountNotFound; tài khoản nhập tay → ErrAccountTransition;
// hãng không đọc được địa giới (self) → không ghi lệnh. Danh sách chưa khớp: thiếu hãng, cấp lạ, cursor hỏng → ErrInvalidPage;
// phân trang theo (cấp, mã) không sót không lặp.
func TestLocations_Refusals(t *testing.T) {
	e := newLocEnv(t)
	tryStart := func(id uuid.UUID) error {
		return e.app(func(tx pgx.Tx) error {
			_, err := e.svc.SyncLocations(e.t.Context(), tx, id, e.actor)
			return err //nolint:wrapcheck // test
		})
	}
	if err := tryStart(uuid.New()); !errors.Is(err, fulfillment.ErrAccountNotFound) {
		t.Errorf("tài khoản lạ: %v", err)
	}
	man := e.mustCreate(e.input(fulfillment.ModeManual, nil))
	if err := tryStart(man.ID); !errors.Is(err, fulfillment.ErrAccountTransition) {
		t.Errorf("tài khoản nhập tay: %v", err)
	}
	if n := e.count(locJobs); n != 0 {
		t.Fatalf("lệnh đồng bộ = %d, muốn 0", n)
	}
	for _, f := range []fulfillment.UnmappedFilter{
		{}, {CarrierKey: "fake", Level: "district"}, {CarrierKey: "fake", After: "ward"}, {CarrierKey: "fake", After: "x:L01"},
	} {
		err := e.app(func(tx pgx.Tx) error {
			_, _, err := e.svc.UnmappedLocations(e.t.Context(), tx, f)
			return err //nolint:wrapcheck // test
		})
		if !errors.Is(err, fulfillment.ErrInvalidPage) {
			t.Errorf("UnmappedLocations(%+v): %v, muốn ErrInvalidPage", f, err)
		}
	}
	// Chưa đồng bộ: cả 15 mã thử chưa khớp; trang 4 dòng, đọc hết không sót không lặp, tỉnh trước phường.
	var all []fulfillment.UnmappedLocation
	after := ""
	for range 20 {
		page, more := e.unmapped(fulfillment.UnmappedFilter{CarrierKey: "fake", After: after, Limit: 4})
		all = append(all, page...)
		if !more {
			break
		}
		last := page[len(page)-1]
		after = last.Level + ":" + last.OurCode
	}
	seen := map[string]bool{}
	var mine int
	for i, it := range all {
		k := it.Level + ":" + it.OurCode
		if seen[k] || (i > 0 && all[i-1].Level+":"+all[i-1].OurCode >= k) {
			t.Fatalf("trang lặp hay sai thứ tự ở %s", k)
		}
		seen[k] = true
		if it.OurCode[0] == 'L' {
			mine++
		}
	}
	if mine != 15 {
		t.Fatalf("chưa khớp (mã thử) = %d, muốn 15", mine)
	}
	if page, _ := e.unmapped(fulfillment.UnmappedFilter{CarrierKey: "fake", Level: "province"}); len(page) == 0 || page[0].Level != "province" {
		t.Fatalf("lọc cấp tỉnh: %+v", page)
	}
}
