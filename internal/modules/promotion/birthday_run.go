package promotion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/promotion/internal/store"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/schedules"
)

// Quà sinh nhật trong CRM (kế hoạch M5, Task 468 / M5-56; QĐ40). Luật ngày chép từ website
// (storefront src/server/birthday/rules.ts): ngày theo lịch Việt Nam (UTC+7, không đổi giờ), 29/2 năm không nhuận tính
// 28/2; quà đến hạn từ (sinh nhật − DaysBefore) tới sinh nhật, chỉ khi đăng ký trước sinh nhật ít nhất MinLeadDays ngày;
// mã dùng từ hôm nay tới hết ngày (ngày phát + ValidDays − 1), không ngắn hơn hết hôm nay.

// Người kích hoạt một lượt phát (cột birthday_runs.trigger).
const (
	BirthdayTriggerSchedule = "schedule"
	BirthdayTriggerManual   = "manual"
)

// BirthdayRunSchedule là tên lịch phát quà sinh nhật (mỗi giờ, mỗi tenant; chỉ phát trong 9:00–21:00 giờ Việt Nam).
const BirthdayRunSchedule = "promotion.birthday_run"

const (
	birthdayRunEvery  = time.Hour
	birthdayFirstHour = 9
	birthdayLastHour  = 21
	vnOffset          = 7 * 3600
	secondsPerDay     = 86400
	birthdayChannel   = "web"
)

// civil là một ngày theo lịch Việt Nam, đếm từ 1970-01-01.
type civil int

func civilOf(t time.Time) civil {
	s := t.Unix() + vnOffset
	d := s / secondsPerDay
	if s%secondsPerDay < 0 {
		d--
	}
	return civil(d)
}

func civilDate(y int, m time.Month, d int) civil {
	return civil(time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix() / secondsPerDay)
}

func (c civil) date() (int, time.Month, int) {
	return time.Unix(int64(c)*secondsPerDay, 0).UTC().Date()
}

// start là 0:00 giờ Việt Nam của ngày c.
func (c civil) start() time.Time { return time.Unix(int64(c)*secondsPerDay-vnOffset, 0).UTC() }

// dmy là "d/m/yyyy" cho tên lô.
func (c civil) dmy() string {
	y, m, d := c.date()
	return fmt.Sprintf("%d/%d/%d", d, m, y)
}

func leapYear(y int) bool { return y%4 == 0 && (y%100 != 0 || y%400 == 0) }

// birthdayIn là ngày sinh nhật (day, month) trong năm year (29/2 năm không nhuận → 28/2).
func birthdayIn(year, day, month int) civil {
	if month == 2 && day == 29 && !leapYear(year) {
		day = 28
	}
	return civilDate(year, time.Month(month), day)
}

// dueGift là quà đến hạn: năm sinh nhật, ngày sinh nhật, hạn dùng [ValidFrom, ValidTo) (ValidTo là 0:00 sau ngày cuối).
type dueGift struct {
	Year               int
	birthday           civil
	ValidFrom, ValidTo time.Time
}

// dueGiftAt trả quà đến hạn lúc now của sinh nhật (day, month) đăng ký lúc saved; không có → false.
func dueGiftAt(day, month int, saved, now time.Time, s BirthdaySettings) (dueGift, bool) {
	today, sv := civilOf(now), civilOf(saved)
	y, _, _ := today.date()
	for _, yr := range []int{y, y + 1} {
		bd := birthdayIn(yr, day, month)
		send := bd - civil(s.DaysBefore)
		if today < send || today > bd || sv > bd-civil(s.MinLeadDays) {
			continue
		}
		last := max(send+civil(s.ValidDays)-1, today)
		return dueGift{Year: yr, birthday: bd, ValidFrom: today.start(), ValidTo: (last + 1).start()}, true
	}
	return dueGift{}, false
}

// BirthdaySettings là cài đặt quà sinh nhật của shop. Version 1 và UpdatedBy nil khi chưa lưu lần nào.
type BirthdaySettings struct {
	Enabled                            bool
	Amount, MinOrder                   int64
	DaysBefore, ValidDays, MinLeadDays int
	Version                            int
	UpdatedBy                          *uuid.UUID
	UpdatedAt                          *time.Time
}

// DefaultBirthdaySettings trả cài đặt mặc định: tắt (nhân viên bật khi website ngừng tự phát), các số như mặc định của website.
func DefaultBirthdaySettings() BirthdaySettings {
	return BirthdaySettings{Amount: 50000, DaysBefore: 7, ValidDays: 14, MinLeadDays: 30, Version: 1}
}

func (s BirthdaySettings) valid() bool {
	return s.Amount >= 10000 && s.Amount <= 1000000 && s.Amount%1000 == 0 && s.MinOrder >= 0 &&
		s.MinOrder <= 100000000 && s.DaysBefore >= 0 && s.DaysBefore <= 30 && s.ValidDays >= 1 && s.ValidDays <= 60 &&
		s.MinLeadDays >= 0 && s.MinLeadDays <= 120
}

// AuditFields là các trường của cài đặt cho audit (không có dữ liệu cá nhân).
func (s BirthdaySettings) AuditFields() map[string]any {
	return map[string]any{
		"enabled": s.Enabled, "amount": s.Amount, "minOrder": s.MinOrder, "daysBefore": s.DaysBefore,
		"validDays": s.ValidDays, "minLeadDays": s.MinLeadDays, "version": s.Version,
	}
}

type settingsRow = store.GetBirthdaySettingsRow

func settingsOf(r settingsRow) BirthdaySettings {
	s := BirthdaySettings{
		Enabled: r.Enabled, Amount: r.Amount, MinOrder: r.MinOrder, DaysBefore: int(r.DaysBefore),
		ValidDays: int(r.ValidDays), MinLeadDays: int(r.MinLeadDays), Version: int(r.Version), UpdatedBy: r.UpdatedBy,
	}
	if r.UpdatedAt.Valid {
		t := r.UpdatedAt.Time
		s.UpdatedAt = &t
	}
	return s
}

// GetBirthdaySettings đọc cài đặt (không khóa); chưa lưu → DefaultBirthdaySettings.
func GetBirthdaySettings(ctx context.Context, tx pgx.Tx) (BirthdaySettings, error) {
	r, err := store.New(tx).GetBirthdaySettings(ctx)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return DefaultBirthdaySettings(), nil
	case err != nil:
		return BirthdaySettings{}, fmt.Errorf("đọc cài đặt quà sinh nhật: %w", err)
	}
	return settingsOf(r), nil
}

// SaveBirthdaySettings lưu cài đặt in khi version còn khớp (chưa có dòng: chèn mặc định version 1 rồi khóa như dòng có
// sẵn); trả cài đặt trước và sau. Ngoài giới hạn → ErrInvalidGiftSettings; version lệch → ErrVersionConflict.
func (Writer) SaveBirthdaySettings(ctx context.Context, tx pgx.Tx, in BirthdaySettings, version int, a Actor,
	now time.Time,
) (before, after BirthdaySettings, err error) {
	if !in.valid() {
		return BirthdaySettings{}, BirthdaySettings{}, ErrInvalidGiftSettings
	}
	q := store.New(tx)
	d := DefaultBirthdaySettings()
	if err := q.InsertBirthdaySettings(ctx, store.InsertBirthdaySettingsParams{
		Enabled: d.Enabled, Amount: d.Amount, MinOrder: d.MinOrder, DaysBefore: int32(d.DaysBefore), //nolint:gosec // G115: hằng nhỏ
		ValidDays: int32(d.ValidDays), MinLeadDays: int32(d.MinLeadDays), //nolint:gosec // G115: hằng nhỏ
	}); err != nil {
		return BirthdaySettings{}, BirthdaySettings{}, fmt.Errorf("ghi cài đặt quà sinh nhật mặc định: %w", err)
	}
	locked, err := q.LockBirthdaySettings(ctx)
	if err != nil {
		return BirthdaySettings{}, BirthdaySettings{}, fmt.Errorf("khóa cài đặt quà sinh nhật: %w", err)
	}
	before = settingsOf(settingsRow(locked))
	if before.Version != version {
		return BirthdaySettings{}, BirthdaySettings{}, ErrVersionConflict
	}
	by := a.UserID
	r, err := q.UpdateBirthdaySettings(ctx, store.UpdateBirthdaySettingsParams{
		Enabled: in.Enabled, Amount: in.Amount, MinOrder: in.MinOrder, DaysBefore: int32(in.DaysBefore), //nolint:gosec // G115: đã kiểm ≤ 120
		ValidDays: int32(in.ValidDays), MinLeadDays: int32(in.MinLeadDays), UpdatedBy: &by, //nolint:gosec // G115: đã kiểm
		Now: pgtype.Timestamptz{Time: now, Valid: true}, Version: int32(version), //nolint:gosec // G115: version int4 của bảng
	})
	if err != nil {
		return BirthdaySettings{}, BirthdaySettings{}, fmt.Errorf("lưu cài đặt quà sinh nhật: %w", err)
	}
	return before, settingsOf(settingsRow(r)), nil
}

// BirthdayRun là một lượt phát quà: ai kích hoạt (StartedBy nil: lịch), số quà đã phát, số khách không phát được (lô bị
// từ chối; thử lại ở lượt sau).
type BirthdayRun struct {
	ID            uuid.UUID
	Trigger       string
	StartedBy     *uuid.UUID
	StartedAt     time.Time
	FinishedAt    *time.Time
	Given, Failed int
}

func runOf(r store.FinishBirthdayRunRow) BirthdayRun {
	out := BirthdayRun{
		ID: r.ID, Trigger: r.Trigger, StartedBy: r.StartedBy, StartedAt: r.StartedAt, Given: int(r.Given),
		Failed: int(r.Failed),
	}
	if r.FinishedAt.Valid {
		t := r.FinishedAt.Time
		out.FinishedAt = &t
	}
	return out
}

// LastBirthdayRun trả lượt phát gần nhất (nil: chưa có lượt nào).
func LastBirthdayRun(ctx context.Context, tx pgx.Tx) (*BirthdayRun, error) {
	r, err := store.New(tx).LastBirthdayRun(ctx)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil //nolint:nilnil // chưa có lượt nào
	case err != nil:
		return nil, fmt.Errorf("đọc lượt phát quà sinh nhật gần nhất: %w", err)
	}
	run := runOf(store.FinishBirthdayRunRow(r))
	return &run, nil
}

// birthdayPick là một khách đến hạn quà trong một lượt.
type birthdayPick struct {
	customerID uuid.UUID
	phone      string
	year       int
}

// RunBirthdayGifts chạy một lượt phát quà sinh nhật lúc now trong tx của bên gọi: khóa tư vấn theo tenant (lượt khác
// đang chạy → ErrRunInProgress, không chờ), ghi lượt, rồi (khi cài đặt bật) phát mã cho mọi khách đến hạn — hồ sơ chưa
// dừng nhận tin, có ≥ 1 đơn giao (của gốc) hoặc cờ đơn cũ, đăng ký đủ sớm, chưa có quà năm đó. Mỗi nhóm cùng hạn một lô
// (EnsureWebBatch, purpose birthday, mã gắn SĐT, một lượt, kênh web) cùng dòng birthday_gifts trong một savepoint; lô bị
// từ chối → đếm Failed, nhóm khác vẫn phát. Mỗi nhóm tối đa 5.000 khách (phần còn lại lượt sau). a.UserID rỗng = lịch.
func (w Writer) RunBirthdayGifts(ctx context.Context, tx pgx.Tx, trigger string, a Actor, now time.Time) (BirthdayRun, error) {
	if trigger != BirthdayTriggerSchedule && trigger != BirthdayTriggerManual {
		return BirthdayRun{}, fmt.Errorf("người kích hoạt lượt phát: %w", ErrInvalidInput)
	}
	q := store.New(tx)
	ok, err := q.TryBirthdayRunLock(ctx)
	if err != nil {
		return BirthdayRun{}, fmt.Errorf("khóa lượt phát quà sinh nhật: %w", err)
	}
	if !ok {
		return BirthdayRun{}, ErrRunInProgress
	}
	s, err := GetBirthdaySettings(ctx, tx)
	if err != nil {
		return BirthdayRun{}, err
	}
	var startedBy *uuid.UUID
	if a.UserID != uuid.Nil {
		startedBy = &a.UserID
	}
	runID, err := q.InsertBirthdayRun(ctx, store.InsertBirthdayRunParams{Trigger: trigger, StartedBy: startedBy, StartedAt: now})
	if err != nil {
		return BirthdayRun{}, fmt.Errorf("ghi lượt phát quà sinh nhật: %w", err)
	}
	given, failed := 0, 0
	if s.Enabled {
		if given, failed, err = w.issueBirthdayGifts(ctx, tx, runID, s, a, now); err != nil {
			return BirthdayRun{}, err
		}
	}
	r, err := q.FinishBirthdayRun(ctx, store.FinishBirthdayRunParams{
		ID: runID, Given: int32(given), Failed: int32(failed),
	})
	if err != nil {
		return BirthdayRun{}, fmt.Errorf("ghi kết quả lượt phát quà sinh nhật: %w", err)
	}
	return runOf(r), nil
}

// issueBirthdayGifts phát quà cho khách đến hạn, theo nhóm hạn dùng; trả số quà đã phát và số khách bị từ chối.
func (w Writer) issueBirthdayGifts(ctx context.Context, tx pgx.Tx, runID uuid.UUID, s BirthdaySettings, a Actor,
	now time.Time,
) (given, failed int, err error) {
	groups, err := dueBirthdays(ctx, store.New(tx), s, now)
	if err != nil {
		return 0, 0, err
	}
	keys := make([]time.Time, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(x, y time.Time) int { return x.Compare(y) })
	for _, validTo := range keys {
		list := groups[validTo]
		err := db.Savepoint(ctx, tx, func(sp pgx.Tx) error {
			return w.giveBirthdayGroup(ctx, sp, runID, s, now, validTo, list, a)
		})
		switch {
		case errors.Is(err, ErrBatchConflict) || errors.Is(err, ErrInvalidInput):
			failed += len(list)
		case err != nil:
			return 0, 0, fmt.Errorf("phát nhóm quà sinh nhật: %w", err)
		default:
			given += len(list)
		}
	}
	return given, failed, nil
}

// dueBirthdays chọn khách đến hạn quà lúc now, nhóm theo hạn dùng (ValidTo); mỗi nhóm không trùng SĐT, tối đa
// maxWebBatchQuantity khách.
func dueBirthdays(ctx context.Context, q *store.Queries, s BirthdaySettings, now time.Time) (map[time.Time][]birthdayPick, error) {
	today := civilOf(now)
	var months []int32
	for d := today; d <= today+civil(s.DaysBefore); d++ {
		_, m, _ := d.date()
		if !slices.Contains(months, int32(m)) { //nolint:gosec // G115: tháng 1–12
			months = append(months, int32(m)) //nolint:gosec // G115: tháng 1–12
		}
	}
	cands, err := q.BirthdayCandidates(ctx, months)
	if err != nil {
		return nil, fmt.Errorf("đọc khách có thể đến hạn quà sinh nhật: %w", err)
	}
	groups := map[time.Time][]birthdayPick{}
	phones := map[time.Time]map[string]bool{}
	for _, c := range cands {
		g, ok := dueGiftAt(int(c.Day), int(c.Month), c.CreatedAt, now, s)
		if !ok || slices.Contains(c.GiftYears, int32(g.Year)) { //nolint:gosec // G115: năm 4 chữ số
			continue
		}
		if phones[g.ValidTo] == nil {
			phones[g.ValidTo] = map[string]bool{}
		}
		if phones[g.ValidTo][c.Phone] || len(groups[g.ValidTo]) >= maxWebBatchQuantity {
			continue
		}
		phones[g.ValidTo][c.Phone] = true
		groups[g.ValidTo] = append(groups[g.ValidTo], birthdayPick{customerID: c.CustomerID, phone: c.Phone, year: g.Year})
	}
	return groups, nil
}

// giveBirthdayGroup phát một lô cho list (cùng hạn validTo) và ghi dòng quà của từng khách.
func (w Writer) giveBirthdayGroup(ctx context.Context, tx pgx.Tx, runID uuid.UUID, s BirthdaySettings, now,
	validTo time.Time, list []birthdayPick, a Actor,
) error {
	today := civilOf(now)
	phones := make([]string, len(list))
	for i, p := range list {
		phones[i] = p.phone
	}
	run := strings.ReplaceAll(runID.String(), "-", "")
	name := fmt.Sprintf("Quà sinh nhật %s – hạn %s · %s", today.dmy(), (civilOf(validTo) - 1).dmy(), run[len(run)-8:])
	b, _, _, err := w.EnsureWebBatch(ctx, tx, WebBatchInput{
		Name: name, Purpose: PurposeBirthday, Quantity: len(list), BoundPhones: phones,
		Template: CouponTemplate{
			Name: "Quà sinh nhật", DiscountKind: KindFixed, Value: s.Amount, MinSubtotal: s.MinOrder,
			Channels: []string{birthdayChannel}, ValidFrom: today.start(), ValidTo: validTo,
		},
	}, a)
	if err != nil {
		return fmt.Errorf("phát lô quà sinh nhật: %w", err)
	}
	q := store.New(tx)
	rows, err := q.BatchCouponIDs(ctx, &b.ID)
	if err != nil {
		return fmt.Errorf("đọc mã của lô quà sinh nhật: %w", err)
	}
	byPhone := make(map[string]uuid.UUID, len(rows))
	for _, r := range rows {
		byPhone[r.BoundPhone] = r.ID
	}
	p := store.InsertBirthdayGiftsParams{RunID: runID, Now: now}
	for _, pk := range list {
		id, ok := byPhone[pk.phone]
		if !ok {
			return fmt.Errorf("lô quà sinh nhật thiếu mã của một SĐT: %w", ErrBatchConflict)
		}
		p.CustomerIds, p.Years = append(p.CustomerIds, pk.customerID), append(p.Years, int32(pk.year)) //nolint:gosec // G115: năm
		p.CouponIds = append(p.CouponIds, id)
	}
	if err := q.InsertBirthdayGifts(ctx, p); err != nil {
		return fmt.Errorf("ghi quà sinh nhật: %w", err)
	}
	return nil
}

// birthdayRunKind: việc của lịch phát quà; quá một kỳ chưa chạy thì hết hạn (kỳ sau chạy lại, lượt idempotent theo năm).
func birthdayRunKind() queue.Kind {
	return queue.Kind{
		Name: "task." + BirthdayRunSchedule, Policy: queue.Policy{MaxAttempts: 3, ExpiresAfter: birthdayRunEvery},
	}
}

func birthdayRunSchedule() schedules.Schedule {
	return schedules.Schedule{Name: BirthdayRunSchedule, Every: birthdayRunEvery, PerTenant: true, Kind: birthdayRunKind()}
}

// birthdayRunHandler chạy lượt phát của lịch: ngoài 9:00–21:00 giờ Việt Nam bỏ qua; lượt khác đang chạy (nhân viên bấm
// "Tạo mã ngay") → xong, không lỗi. Kết quả chỉ có số.
type birthdayRunHandler struct {
	w   Writer
	now func() time.Time
}

func (birthdayRunHandler) Kind() queue.Kind { return birthdayRunKind() }

// Handle chạy một lượt; kết quả {"given","failed"} hay {"skipped": lý do}.
func (h birthdayRunHandler) Handle(ctx context.Context, j queue.Job, tx pgx.Tx) (json.RawMessage, error) {
	if j.TenantID == nil {
		return nil, fmt.Errorf("việc %s: thiếu tenant: %w", j.ID, queue.ErrPermanent)
	}
	now := h.now()
	result := map[string]any{}
	switch hour := (now.Unix() + vnOffset) % secondsPerDay / 3600; {
	case hour < birthdayFirstHour || hour >= birthdayLastHour:
		result["skipped"] = "outside_hours"
	default:
		r, err := h.w.RunBirthdayGifts(ctx, tx, BirthdayTriggerSchedule, Actor{TenantID: *j.TenantID}, now)
		switch {
		case errors.Is(err, ErrRunInProgress):
			result["skipped"] = "another_run_in_progress"
		case err != nil:
			return nil, err
		default:
			result["given"], result["failed"] = r.Given, r.Failed
		}
	}
	out, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("mã hóa kết quả lượt phát quà sinh nhật: %w", err)
	}
	return out, nil
}
