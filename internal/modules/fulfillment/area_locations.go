package fulfillment

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/locations"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/store"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng locations (kế hoạch M3, Task M3-18, Q-19): đối chiếu địa giới theo hãng. Lệnh command.carrier.locations đọc
// danh sách tỉnh của hãng rồi phường của từng tỉnh đã khớp (gọi hãng trong Execute, ngoài transaction), khớp tên với
// danh mục của CRM (customer.admin_units_v, gói internal/locations); OnDone ghi carrier_locations (upsert). Quá hạn thì
// việc dừng giữa chừng, kết quả có next và OnDone ghi việc kế tiếp từ tỉnh đó. Địa chỉ chưa khớp không bao giờ được gửi
// đoán: LocationMap trả false, bên gọi (báo phí, tạo vận đơn) trả address_unmapped.

const (
	// LocationsKindName là tên loại lệnh đồng bộ địa giới của một tài khoản hãng.
	LocationsKindName = "command.carrier.locations"
	locationsExpiry   = 30 * time.Minute
	locationsTimeout  = 25 * time.Second
	// locationsCallTimeout: hạn của mỗi lần gọi Locations của hãng.
	locationsCallTimeout = 8 * time.Second
	// locationsBudget: đã dùng chừng này (đồng hồ của module) thì không gọi tỉnh kế tiếp, trả next; cộng một lần gọi
	// vẫn dưới locationsTimeout.
	locationsBudget = 15 * time.Second
	// locationsMargin: phần hạn thật của ctx phải còn khi bắt đầu một lần gọi (ngoài hạn của chính lần gọi).
	locationsMargin = 2 * time.Second

	levelProvince = "province"
	levelWard     = "ward"

	defaultUnmappedPage = 50
	maxUnmappedPage     = 100
)

// LocationsKind là Kind của lệnh đồng bộ địa giới: hết hạn 30 phút, mỗi lần chạy tối đa 25 s.
func LocationsKind() queue.Kind {
	return commands.CommandKind(LocationsKindName, locationsExpiry, locationsTimeout)
}

// locationsPayload: tài khoản, lần đồng bộ (run, chung mọi việc kế tiếp), tỉnh của CRM bắt đầu (rỗng: từ đầu, kèm tỉnh).
type locationsPayload struct {
	AccountID uuid.UUID `json:"accountId"`
	Run       uuid.UUID `json:"run"`
	Cursor    string    `json:"cursor,omitempty"`
}

// locationsResult là Outcome.Result: mã đã khớp của phần vừa đọc, số khớp và chưa khớp của phần đó, tỉnh kế tiếp
// (next rỗng: xong). Không khóa API, không dữ liệu khách.
type locationsResult struct {
	CarrierKey         string        `json:"carrierKey"`
	Next               string        `json:"next,omitempty"`
	SyncedAt           time.Time     `json:"syncedAt"`
	ProvincesMatched   int           `json:"provincesMatched"`
	ProvincesUnmatched int           `json:"provincesUnmatched"`
	WardsMatched       int           `json:"wardsMatched"`
	WardsUnmatched     int           `json:"wardsUnmatched"`
	Rows               []locationRow `json:"rows"`
}

type locationRow struct {
	Level       string `json:"l"`
	OurCode     string `json:"o"`
	CarrierCode string `json:"c"`
	CarrierName string `json:"n"`
}

// SyncLocations ghi lệnh đồng bộ địa giới của tài khoản id (trong tx của bên gọi), trả mã việc. Lỗi: ErrAccountNotFound;
// tài khoản nhập tay hay chưa có khóa → ErrAccountTransition; không active → ErrAccountUnavailable; hãng không có
// adapter hay adapter không đọc được địa giới → ErrCarrierNotSupported. Audit do bên gọi ghi.
func (s *Service) SyncLocations(ctx context.Context, tx pgx.Tx, id uuid.UUID, a Actor) (uuid.UUID, error) {
	r, err := getAccount(ctx, store.New(tx), id)
	if err != nil {
		return uuid.Nil, err
	}
	switch {
	case r.Mode != ModeAPI || len(r.CredentialEnc) == 0:
		return uuid.Nil, fmt.Errorf("đồng bộ địa giới tài khoản %s: %w", r.Mode, ErrAccountTransition)
	case r.Status != AccountActive:
		return uuid.Nil, fmt.Errorf("đồng bộ địa giới tài khoản %s: %w", r.Status, ErrAccountUnavailable)
	}
	if ad, ok := s.adapter(r.CarrierKey); !ok || !ad.Capabilities().Locations {
		return uuid.Nil, fmt.Errorf("đồng bộ địa giới hãng %q: %w", r.CarrierKey, ErrCarrierNotSupported)
	}
	run, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, fmt.Errorf("sinh mã lần đồng bộ: %w", err)
	}
	return enqueueLocations(ctx, tx, s.queue(), a.TenantID, locationsPayload{AccountID: id, Run: run})
}

// enqueueLocations ghi một việc đồng bộ; khóa chống trùng sync:<account>:<run>:<cursor> (việc kế tiếp ghi lại trong
// OnDone chạy lại không thành hai việc; lần đồng bộ sau có run khác). Không OrderKey: hai lần đồng bộ chạy song song chỉ
// cùng upsert một bảng (thứ tự khóa dòng như nhau); việc dead không chặn lần sau.
func enqueueLocations(ctx context.Context, tx pgx.Tx, q queue.JobQueue, tenant uuid.UUID, p locationsPayload) (uuid.UUID, error) {
	job, _, err := q.Enqueue(ctx, tx, LocationsKind(), queue.NewJob{
		TenantID: &tenant, Payload: p,
		DedupeKey: "sync:" + p.AccountID.String() + ":" + p.Run.String() + ":" + p.Cursor,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("ghi lệnh đồng bộ địa giới: %w", err)
	}
	return job, nil
}

// locationsCommand là lệnh command.carrier.locations của Service s.
type locationsCommand struct{ s *Service }

func (locationsCommand) Kind() queue.Kind       { return LocationsKind() }
func (locationsCommand) AutoRetryNotSent() bool { return true }

// Lookup luôn not_sent: đồng bộ chỉ đọc hãng, chạy lại an toàn (upsert không thêm dòng).
func (locationsCommand) Lookup(context.Context, queue.Job) (string, error) {
	return commands.StatusNotSent, nil
}

// locSource là thứ Execute đọc trong transaction ngắn: tài khoản đã giải mã và danh mục của CRM theo nhóm.
type locSource struct {
	acc       Account
	provinces []locations.Unit
	wards     map[string][]locations.Unit // theo mã tỉnh của CRM
}

// Execute đọc tài khoản, danh mục (transaction ngắn), rồi gọi hãng: tỉnh, sau đó phường của từng tỉnh đã khớp theo mã
// CRM tăng dần, bắt đầu ở payload.cursor. Mỗi việc đọc ít nhất một tỉnh; đã dùng locationsBudget (đồng hồ của module)
// hay ctx còn ít hơn một lần gọi thì dừng, kết quả có next. Lỗi hãng phân loại như lệnh tra (classifyTrackError):
// 401/403 → rejected auth + MarkAuthFailed; 429 → not_sent theo Retry-After; quá giờ, mạng → not_sent; không hỗ trợ,
// từ chối → rejected. Phần đã đọc của việc lỗi bỏ đi (việc chạy lại từ cùng cursor).
func (c locationsCommand) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	var p locationsPayload
	if err := json.Unmarshal(j.Payload, &p); err != nil || p.AccountID == uuid.Nil || p.Run == uuid.Nil || j.TenantID == nil {
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonBadPayload}, nil //nolint:nilerr // payload hỏng: dừng hẳn
	}
	src, err := c.source(ctx, *j.TenantID, p.AccountID)
	switch {
	case errors.Is(err, ErrAccountNotFound), errors.Is(err, ErrAccountUnavailable):
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonUnavailable}, nil
	case errors.Is(err, ErrKeysMissing), errors.Is(err, ErrCredentialsUnreadable):
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonDecrypt}, nil
	case err != nil:
		return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonReadFailed}, nil
	}
	ad, ok := c.s.adapter(src.acc.CarrierKey)
	if !ok || !ad.Capabilities().Locations {
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonNoCarrier}, nil
	}
	res, err := c.read(ctx, ad, src, p.Cursor)
	src.acc.Creds = nil
	if err != nil {
		return c.fail(ctx, *j.TenantID, p.AccountID, err)
	}
	b, err := json.Marshal(res)
	if err != nil {
		return commands.Outcome{}, fmt.Errorf("mã hóa kết quả đồng bộ địa giới: %w", err)
	}
	return commands.Outcome{Status: commands.StatusDone, Result: b}, nil
}

// source đọc tài khoản (ResolveAccount: phải active) và danh mục trong một transaction ngắn. Lỗi của ResolveAccount
// (ErrAccountNotFound, ErrAccountUnavailable, ErrKeysMissing, ErrCredentialsUnreadable) trả nguyên để phân loại; lỗi khác
// là lỗi đọc.
func (c locationsCommand) source(ctx context.Context, tenant, id uuid.UUID) (locSource, error) {
	var src locSource
	err := tenancy.WithTenant(ctx, c.s.d.DB, tenant, func(tx pgx.Tx) error {
		src = locSource{wards: map[string][]locations.Unit{}} // RetryTx có thể chạy lại
		var err error
		if src.acc, err = c.s.ResolveAccount(ctx, tx, id); err != nil {
			return err
		}
		units, err := store.New(tx).ListAdminUnitsForMatch(ctx)
		if err != nil {
			return fmt.Errorf("đọc danh mục địa giới: %w", err)
		}
		for _, u := range units {
			switch u.Level {
			case levelProvince:
				src.provinces = append(src.provinces, locations.Unit{Code: u.Code, Name: u.Name})
			case levelWard:
				src.wards[u.ParentCode] = append(src.wards[u.ParentCode], locations.Unit{Code: u.Code, Name: u.Name})
			}
		}
		return nil
	}, db.TxWorker)
	if err != nil {
		return locSource{}, fmt.Errorf("đọc tài khoản, danh mục của lệnh đồng bộ địa giới: %w", err)
	}
	return src, nil
}

// read gọi hãng và khớp (ngoài transaction). cursor rỗng: kết quả có cả tỉnh và số phường của tỉnh chưa khớp (chỉ
// tính một lần mỗi lần đồng bộ).
func (c locationsCommand) read(ctx context.Context, ad CarrierAdapter, src locSource, cursor string) (locationsResult, error) {
	start := c.s.d.Now()
	res := locationsResult{CarrierKey: src.acc.CarrierKey, SyncedAt: start.UTC(), Rows: []locationRow{}}
	remote, err := c.call(ctx, ad, src.acc, levelProvince, "")
	if err != nil {
		return locationsResult{}, err
	}
	pairs, unmatched := locations.Match(src.provinces, remotes(remote, levelProvince, ""))
	if cursor == "" {
		res.ProvincesMatched, res.ProvincesUnmatched = len(pairs), unmatched
		matched := make(map[string]bool, len(pairs))
		for _, pp := range pairs {
			matched[pp.OurCode] = true
			res.Rows = append(res.Rows, locationRow{levelProvince, pp.OurCode, pp.CarrierCode, pp.CarrierName})
		}
		for _, u := range src.provinces {
			if !matched[u.Code] {
				res.WardsUnmatched += len(src.wards[u.Code])
			}
		}
	}
	first := true
	for _, pp := range pairs {
		ours := src.wards[pp.OurCode]
		if pp.OurCode < cursor || len(ours) == 0 {
			continue
		}
		if !first && c.overBudget(ctx, start) {
			res.Next = pp.OurCode
			break
		}
		first = false
		remote, err := c.call(ctx, ad, src.acc, levelWard, pp.CarrierCode)
		if err != nil {
			return locationsResult{}, err
		}
		wp, wu := locations.Match(ours, remotes(remote, levelWard, pp.CarrierCode))
		res.WardsMatched += len(wp)
		res.WardsUnmatched += wu
		for _, w := range wp {
			res.Rows = append(res.Rows, locationRow{levelWard, w.OurCode, w.CarrierCode, w.CarrierName})
		}
	}
	return res, nil
}

// overBudget: đã dùng locationsBudget theo đồng hồ của module, hay hạn thật của ctx không đủ cho một lần gọi nữa.
func (c locationsCommand) overBudget(ctx context.Context, start time.Time) bool {
	if c.s.d.Now().Sub(start) >= locationsBudget {
		return true
	}
	dl, ok := ctx.Deadline()
	return ok && time.Until(dl) < locationsCallTimeout+locationsMargin
}

func (locationsCommand) call(ctx context.Context, ad CarrierAdapter, acc Account, level, parent string) ([]Location, error) {
	cctx, cancel := context.WithTimeout(ctx, locationsCallTimeout)
	defer cancel()
	ls, err := ad.Locations(cctx, acc, level, parent)
	if err != nil {
		return nil, fmt.Errorf("đọc địa giới %s của hãng: %w", level, err)
	}
	return ls, nil
}

// remotes lọc đơn vị đúng cấp (cấp rỗng: coi là đúng) và đúng cha (cha rỗng: coi là cha đã hỏi) của hãng.
func remotes(ls []Location, level, parent string) []locations.Remote {
	out := make([]locations.Remote, 0, len(ls))
	for _, l := range ls {
		if (l.Level == "" || l.Level == level) && (parent == "" || l.ParentCarrierCode == "" || l.ParentCarrierCode == parent) {
			out = append(out, locations.Remote{Code: l.CarrierCode, Name: l.Name})
		}
	}
	return out
}

// fail phân loại lỗi gọi hãng; hãng từ chối khóa → MarkAuthFailed trong transaction ngắn, hạn riêng (ctx của lệnh có thể
// đã hết). Ghi lỗi → not_sent (thử lại cả lần đọc) để tài khoản bị từ chối khóa luôn được đánh dấu.
func (c locationsCommand) fail(ctx context.Context, tenant, accountID uuid.UUID, err error) (commands.Outcome, error) {
	out, authCode := classifyTrackError(err)
	if authCode == "" {
		return out, nil
	}
	mctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), markTimeout)
	defer cancel()
	if err := tenancy.WithTenant(mctx, c.s.d.DB, tenant, func(tx pgx.Tx) error {
		return MarkAuthFailed(mctx, tx, accountID, authCode)
	}, db.TxWorker); err != nil {
		return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonReadFailed}, nil //nolint:nilerr // database lỗi: đọc lại sau
	}
	return out, nil
}

// OnDone (trong transaction đổi lệnh sang done): upsert các mã đã khớp theo (level, our_code) tăng dần; next khác rỗng →
// ghi việc kế tiếp cùng run từ tỉnh next.
func (c locationsCommand) OnDone(ctx context.Context, j queue.Job, result json.RawMessage, tx pgx.Tx) error {
	if result == nil { // done do tra lại (Lookup luôn not_sent nên không xảy ra)
		return nil
	}
	var p locationsPayload
	var res locationsResult
	if err := json.Unmarshal(j.Payload, &p); err != nil {
		return fmt.Errorf("đọc lệnh đồng bộ địa giới: %w", err)
	}
	if err := json.Unmarshal(result, &res); err != nil {
		return fmt.Errorf("đọc kết quả đồng bộ địa giới: %w", err)
	}
	if err := upsertLocations(ctx, store.New(tx), res.CarrierKey, res.SyncedAt, res.Rows); err != nil {
		return fmt.Errorf("ghi địa giới theo hãng: %w", err)
	}
	if res.Next == "" {
		return nil
	}
	_, err := enqueueLocations(ctx, tx, c.s.queue(), *j.TenantID, locationsPayload{
		AccountID: p.AccountID, Run: p.Run, Cursor: res.Next,
	})
	return err
}

// SeedLocation là một mã địa giới thử cho EnsureSeedLocations: mã CRM, tên, cấp (Ward false: tỉnh).
type SeedLocation struct {
	Code, Name string
	Ward       bool
}

// EnsureSeedLocations ghi mã của hãng carrierKey cho các mã địa giới thử locs với mã hãng = mã CRM (hãng giả
// tools/fakecarrier dùng đúng mã của CRM) trong tx của bên gọi (ngữ cảnh tenant), chỉ dòng thiếu hay mã hãng khác; trả
// số dòng đã ghi (chạy lại: 0). Chỉ cho lệnh seed-web-fixture và test (Task 511): địa giới thử của bộ e2e website
// không có ở danh mục của hãng nên đồng bộ thật (command.carrier.locations) không khớp được. Khóa: chỉ dòng
// carrier_locations theo (level, our_code) tăng dần (nhóm 8).
func EnsureSeedLocations(ctx context.Context, tx pgx.Tx, carrierKey string, locs []SeedLocation) (int, error) {
	q := store.New(tx)
	have, err := q.ListCarrierLocationCodes(ctx, carrierKey)
	if err != nil {
		return 0, fmt.Errorf("đọc mã địa giới của hãng: %w", err)
	}
	cur := locations.NewMap(rowsOf(have))
	var rows []locationRow
	for _, l := range locs {
		level := levelProvince
		if l.Ward {
			level = levelWard
		}
		if c, ok := cur.Carrier(level, l.Code); !ok || c != l.Code {
			rows = append(rows, locationRow{level, l.Code, l.Code, l.Name})
		}
	}
	if err := upsertLocations(ctx, q, carrierKey, time.Now().UTC(), rows); err != nil {
		return 0, fmt.Errorf("ghi địa giới thử của hãng: %w", err)
	}
	return len(rows), nil
}

// upsertLocations ghi các dòng mã hãng (bản sao sắp theo level, our_code tăng dần: thứ tự khóa); rỗng → không ghi.
func upsertLocations(ctx context.Context, q *store.Queries, carrierKey string, at time.Time, rows []locationRow) error {
	if len(rows) == 0 {
		return nil
	}
	rows = slices.Clone(rows)
	slices.SortFunc(rows, func(a, b locationRow) int {
		return cmp.Or(strings.Compare(a.Level, b.Level), strings.Compare(a.OurCode, b.OurCode))
	})
	arg := store.UpsertCarrierLocationsParams{CarrierKey: carrierKey, SyncedAt: at}
	for _, r := range rows {
		arg.Levels = append(arg.Levels, r.Level)
		arg.OurCodes = append(arg.OurCodes, r.OurCode)
		arg.CarrierCodes = append(arg.CarrierCodes, r.CarrierCode)
		arg.CarrierNames = append(arg.CarrierNames, r.CarrierName)
	}
	return q.UpsertCarrierLocations(ctx, arg) //nolint:wrapcheck // hai bên gọi bọc theo việc của mình
}

func rowsOf(rs []store.ListCarrierLocationCodesRow) []locations.Row {
	out := make([]locations.Row, 0, len(rs))
	for _, r := range rs {
		out = append(out, locations.Row{Level: r.Level, OurCode: r.OurCode, CarrierCode: r.CarrierCode})
	}
	return out
}

// LocationMap đọc một lần mọi mã của hãng carrierKey trong tx (ngữ cảnh tenant của bên gọi) và trả bảng trong bộ nhớ
// cho mọi lần tra trong transaction đó (không giữ giữa các transaction nên không bao giờ cũ hơn tx). Mã chưa khớp →
// Carrier trả false: bên gọi trả address_unmapped (Task M3-12, M3-13).
func (*Service) LocationMap(ctx context.Context, tx pgx.Tx, carrierKey string) (LocationMap, error) { //nolint:ireturn // LocationMap là interface của adapter
	rows, err := store.New(tx).ListCarrierLocationCodes(ctx, carrierKey)
	if err != nil {
		return nil, fmt.Errorf("đọc mã địa giới của hãng: %w", err)
	}
	return locations.NewMap(rowsOf(rows)), nil
}

// UnmappedLocation là một mã địa giới của CRM chưa có mã của hãng; ParentCode rỗng với tỉnh.
type UnmappedLocation struct {
	Level, OurCode, Name, ParentCode string
}

// UnmappedFilter lọc danh sách chưa khớp: hãng (bắt buộc), cấp (rỗng: mọi cấp), sau "<level>:<mã>" của dòng cuối trang
// trước, cỡ trang (0: 50; tối đa 100).
type UnmappedFilter struct {
	CarrierKey, Level, After string
	Limit                    int
}

// UnmappedLocations trả mã của CRM chưa có mã của hãng f.CarrierKey theo (cấp, mã), và còn trang sau hay không. Lỗi:
// thiếu hãng, cấp lạ, After sai dạng, cỡ trang ngoài 0…100 → ErrInvalidPage.
func (*Service) UnmappedLocations(ctx context.Context, tx pgx.Tx, f UnmappedFilter) ([]UnmappedLocation, bool, error) {
	limit := f.Limit
	if limit == 0 {
		limit = defaultUnmappedPage
	}
	switch {
	case f.CarrierKey == "":
		return nil, false, fmt.Errorf("%w: thiếu hãng", ErrInvalidPage)
	case f.Level != "" && f.Level != levelProvince && f.Level != levelWard:
		return nil, false, fmt.Errorf("%w: cấp %q", ErrInvalidPage, f.Level)
	case limit < 1 || limit > maxUnmappedPage:
		return nil, false, fmt.Errorf("%w: cỡ trang %d", ErrInvalidPage, f.Limit)
	}
	arg := store.ListUnmappedLocationsParams{CarrierKey: f.CarrierKey, Level: f.Level, MaxRows: int32(limit + 1)}
	if f.After != "" {
		lv, code, ok := strings.Cut(f.After, ":")
		if !ok || code == "" || (lv != levelProvince && lv != levelWard) {
			return nil, false, fmt.Errorf("%w: con trỏ", ErrInvalidPage)
		}
		arg.AfterLevel, arg.AfterCode = lv, code
	}
	rows, err := store.New(tx).ListUnmappedLocations(ctx, arg)
	if err != nil {
		return nil, false, fmt.Errorf("đọc địa giới chưa khớp: %w", err)
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	out := make([]UnmappedLocation, 0, len(rows))
	for _, r := range rows {
		out = append(out, UnmappedLocation{Level: r.Level, OurCode: r.Code, Name: r.Name, ParentCode: r.ParentCode})
	}
	return out, more, nil
}

// locationsHandlers: lệnh đồng bộ địa giới khi có database và bộ adapter.
func (m *Module) locationsHandlers() []commands.Handler {
	var out []commands.Handler
	if m.d.DB != nil && m.d.Carriers != nil {
		out = append(out, locationsCommand{s: m.Service})
	}
	return out
}

func (*Module) locationsInbound() []webhooks.InboundHandler { return nil }
func (*Module) locationsSources() []webhooks.Source         { return nil }
func (*Module) locationsConsumers() []events.Consumer       { return nil }
func (*Module) locationsSchedules() []schedules.Schedule    { return nil }
func (*Module) locationsCleanups() []schedules.CleanupFunc  { return nil }
