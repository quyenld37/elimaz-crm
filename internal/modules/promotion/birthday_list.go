package promotion

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/promotion/internal/store"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// Tám trạng thái của một khách trong tab "Tất cả khách" (như admin của website), theo thứ tự ưu tiên khi sắp.
const (
	BirthdayStateManual      = "manual"      // có quà còn hiệu lực mà chưa trao (chưa nhắn tay, website chưa gửi)
	BirthdayStateDue         = "due"         // tới ngày phát, đủ điều kiện, chưa có quà (lượt sau phát)
	BirthdayStateUnconfirmed = "unconfirmed" // tới ngày phát nhưng chưa có đơn web giao thành công
	BirthdayStateGiven       = "given"       // quà đã trao, chưa dùng, còn hạn
	BirthdayStateWaiting     = "waiting"     // chưa tới ngày phát
	BirthdayStateUsed        = "used"        // quà đã dùng
	BirthdayStateExpired     = "expired"     // quà hết hạn chưa dùng (hiện tới 30 ngày sau hạn)
	BirthdayStateStopped     = "stopped"     // khách dừng nhận tin
)

const (
	defaultBirthdayPage = 50
	maxBirthdayPage     = 200
	maxPendingGifts     = 500
	maxMessagedCodes    = 200
	expiredShownDays    = 30
	minPhoneDigits      = 3
)

// BirthdayStates là tám trạng thái theo thứ tự ưu tiên.
func BirthdayStates() []string {
	return []string{
		BirthdayStateManual, BirthdayStateDue, BirthdayStateUnconfirmed, BirthdayStateGiven, BirthdayStateWaiting,
		BirthdayStateUsed, BirthdayStateExpired, BirthdayStateStopped,
	}
}

// BirthdayGift là một quà chưa trao: khách, mã, điều khoản, kênh khách đồng ý (Mail chỉ khi có email), mã dừng nhận
// tin cho link trong tin. Phone, Email, StopToken là dữ liệu cá nhân, tự che khi log.
type BirthdayGift struct {
	CustomerID         uuid.UUID
	Year               int
	Code               string
	FullName           customer.Name
	Phone              logging.Phone
	Email              logging.Secret
	Day, Month         int
	Zalo, Mail         bool
	StopToken          logging.Secret
	Amount, MinOrder   int64
	ValidFrom, ValidTo time.Time
	CreatedAt          time.Time
	BirthdayIn         int // số ngày tới sinh nhật của năm quà (âm: đã qua)
	SendFailures       int // số lần website báo gửi lỗi (Task 491)
}

// Quà website gửi lỗi (Task 491): lỗi lần 1, 2 → quà rời outbox giftRetryDelay; lần thứ maxGiftSendFailures → không tự
// gửi nữa (vẫn ở "Cần nhắn").
const (
	maxGiftSendFailures = 3
	giftRetryDelay      = 6 * time.Hour
	defaultOutboxPage   = 100
)

// OutboxQuery là một trang outbox: Limit 1–500 (ngoài → 100), Cursor là con trỏ trang sau (rỗng: trang đầu).
type OutboxQuery struct {
	Limit  int
	Cursor string
}

// OutboxPage là một trang outbox và con trỏ trang sau (rỗng: hết).
type OutboxPage struct {
	Items []BirthdayGift
	Next  string
}

// BirthdayOutbox trả một trang quà chưa trao, còn hạn lúc now, của khách chưa dừng và đồng ý Zalo hay email, cũ nhất
// trước (lúc phát, mã): website gửi tin rồi báo MarkGiven (QĐ40). Quà đang chờ thử lại sau lỗi hay đã hết lượt tự gửi
// (MarkBirthdaySendFailed) không vào outbox. Con trỏ sai → ErrInvalidInput.
func BirthdayOutbox(ctx context.Context, tx pgx.Tx, now time.Time, q OutboxQuery) (OutboxPage, error) {
	limit := q.Limit
	if limit < 1 || limit > maxPendingGifts {
		limit = defaultOutboxPage
	}
	p := store.PendingBirthdayGiftsParams{Now: now, Outbox: true, MaxFailures: maxGiftSendFailures, RowLimit: int32(limit + 1)}
	if q.Cursor != "" {
		at, code, err := parseOutboxCursor(q.Cursor)
		if err != nil {
			return OutboxPage{}, err
		}
		p.AfterCreated, p.AfterCode = pgtype.Timestamptz{Time: at, Valid: true}, pgtype.Text{String: code, Valid: true}
	}
	gs, err := pendingGifts(ctx, tx, now, p)
	if err != nil {
		return OutboxPage{}, err
	}
	page := OutboxPage{Items: gs}
	if len(gs) > limit {
		page.Items = gs[:limit]
		last := page.Items[limit-1]
		page.Next = strconv.FormatInt(last.CreatedAt.UnixMicro(), 10) + "." + last.Code
	}
	return page, nil
}

// parseOutboxCursor đọc con trỏ "<lúc phát, micro giây Unix>.<mã>".
func parseOutboxCursor(c string) (time.Time, string, error) {
	micros, code, ok := strings.Cut(c, ".")
	n, err := strconv.ParseInt(micros, 10, 64)
	if !ok || err != nil || !codePattern.MatchString(code) {
		return time.Time{}, "", fmt.Errorf("con trỏ outbox sinh nhật: %w", ErrInvalidInput)
	}
	return time.UnixMicro(n).UTC(), code, nil
}

// ListBirthdayTodo trả tối đa limit quà chưa trao (tab "Cần nhắn": kể cả khách không đồng ý kênh nào và quà website gửi
// lỗi), còn hạn lúc now, của khách chưa dừng, sinh nhật gần nhất trước.
func ListBirthdayTodo(ctx context.Context, tx pgx.Tx, now time.Time, limit int) ([]BirthdayGift, error) {
	if limit < 1 || limit > maxPendingGifts {
		limit = defaultOutboxPage
	}
	gs, err := pendingGifts(ctx, tx, now, store.PendingBirthdayGiftsParams{Now: now, RowLimit: int32(limit)})
	if err != nil {
		return nil, err
	}
	slices.SortStableFunc(gs, func(a, b BirthdayGift) int { return cmp.Compare(a.BirthdayIn, b.BirthdayIn) })
	return gs, nil
}

func pendingGifts(ctx context.Context, tx pgx.Tx, now time.Time, p store.PendingBirthdayGiftsParams) ([]BirthdayGift, error) {
	rows, err := store.New(tx).PendingBirthdayGifts(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("đọc quà sinh nhật chưa trao: %w", err)
	}
	today := civilOf(now)
	out := make([]BirthdayGift, 0, len(rows))
	for _, r := range rows {
		out = append(out, BirthdayGift{
			CustomerID: r.CustomerID, Year: int(r.GiftYear), Code: r.Code, FullName: customer.Name(r.FullName),
			Phone: logging.Phone(r.Phone), Email: logging.Secret(r.Email), Day: int(r.Day), Month: int(r.Month),
			Zalo: r.ConsentZalo, Mail: r.ConsentMail, StopToken: logging.Secret(r.StopToken), Amount: r.Value,
			MinOrder: r.MinSubtotal, ValidFrom: r.ValidFrom, ValidTo: r.ValidTo, CreatedAt: r.CreatedAt,
			BirthdayIn:   int(birthdayIn(int(r.GiftYear), int(r.Day), int(r.Month)) - today),
			SendFailures: int(r.SendFailures),
		})
	}
	return out, nil
}

// GiftSendFailure là trạng thái lỗi gửi của một quà: số lần lỗi, lúc quay lại outbox (nil: hết lượt tự gửi hay đã
// trao), Given khi quà đã trao hay đã dùng (lần báo này không đổi gì).
type GiftSendFailure struct {
	CouponID uuid.UUID
	Code     string
	Failures int
	RetryAt  *time.Time
	Given    bool
}

// MarkBirthdaySendFailed ghi một lần website gửi quà code lỗi lúc now (không kênh nào tới khách): lỗi lần 1, 2 → quà
// rời outbox tới now + 6 giờ; lần 3 → không vào outbox nữa. Quà đã trao hay đã dùng → không đổi gì. Khóa mã trước quà
// (ADR-46 nhóm 10). Mã không phải quà sinh nhật hay không có → ErrNotFound.
func MarkBirthdaySendFailed(ctx context.Context, tx pgx.Tx, code string, now time.Time) (GiftSendFailure, error) {
	q := store.New(tx)
	g, err := q.LockBirthdayGiftByCode(ctx, code)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return GiftSendFailure{}, fmt.Errorf("quà sinh nhật %s: %w", code, ErrNotFound)
	case err != nil:
		return GiftSendFailure{}, fmt.Errorf("khóa quà sinh nhật: %w", err)
	}
	out := GiftSendFailure{CouponID: g.ID, Code: g.Code, Failures: int(g.SendFailures)}
	if g.GivenAt.Valid || g.UsedCount > 0 {
		out.Given = true
		return out, nil
	}
	if out.Failures >= maxGiftSendFailures {
		return out, nil
	}
	var retry pgtype.Timestamptz
	if out.Failures+1 < maxGiftSendFailures {
		retry = pgtype.Timestamptz{Time: now.Add(giftRetryDelay), Valid: true}
	}
	r, err := q.MarkBirthdayGiftFailed(ctx, store.MarkBirthdayGiftFailedParams{RetryAt: retry, CouponID: g.ID})
	if err != nil {
		return GiftSendFailure{}, fmt.Errorf("ghi lỗi gửi quà sinh nhật: %w", err)
	}
	out.Failures = int(r.SendFailures)
	if r.RetryAt.Valid {
		t := r.RetryAt.Time
		out.RetryAt = &t
	}
	return out, nil
}

// MarkBirthdayMessaged ghi lúc trao (at) cho các mã quà sinh nhật nhân viên đã nhắn tay (1–200 mã; ngoài →
// ErrInvalidInput); mã đã trao, không phải quà sinh nhật hay không có thì bỏ qua. Khóa mã theo id tăng dần (ADR-46 nhóm
// 10). Trả các mã vừa ghi.
func MarkBirthdayMessaged(ctx context.Context, tx pgx.Tx, codes []string, at time.Time) ([]string, error) {
	if len(codes) < 1 || len(codes) > maxMessagedCodes {
		return nil, fmt.Errorf("số mã đã nhắn: %w", ErrInvalidInput)
	}
	q := store.New(tx)
	ids, err := q.LockBirthdayCoupons(ctx, codes)
	if err != nil {
		return nil, fmt.Errorf("khóa mã quà sinh nhật: %w", err)
	}
	if len(ids) == 0 {
		return []string{}, nil
	}
	marked, err := q.MarkBirthdayCouponsGiven(ctx, store.MarkBirthdayCouponsGivenParams{
		At: pgtype.Timestamptz{Time: at, Valid: true}, Ids: ids,
	})
	if err != nil {
		return nil, fmt.Errorf("ghi lúc nhắn quà sinh nhật: %w", err)
	}
	slices.Sort(marked)
	return marked, nil
}

// BirthdayCustomer là một khách trong tab "Tất cả khách": trạng thái, số ngày tới sinh nhật đang xét, ngày phát (0:00
// giờ Việt Nam), mã quà hiện tại (rỗng: chưa có).
type BirthdayCustomer struct {
	CustomerID uuid.UUID
	FullName   customer.Name
	Phone      logging.Phone
	Email      logging.Secret
	Day, Month int
	Zalo, Mail bool
	State      string
	BirthdayIn int
	SendOn     time.Time
	Code       string
	// CouponID là mã (id) của quà hiện tại; uuid.Nil khi Code rỗng.
	CouponID uuid.UUID
	SavedAt  time.Time
	// delivered: khách đã có đơn giao thành công (đủ điều kiện nhận quà).
	delivered bool
}

// BirthdayCustomerQuery lọc danh sách: State rỗng hay "all" = mọi trạng thái, Month 0 = mọi tháng, Q tìm tên (bỏ dấu)
// hay ≥ 3 chữ số SĐT. Cursor là con trỏ trang sau (rỗng: trang đầu); Limit 1–200 (ngoài → 50); All bỏ phân trang (xuất).
type BirthdayCustomerQuery struct {
	State  string
	Month  int
	Q      string
	Cursor string
	Limit  int
	All    bool
}

// BirthdayCustomerPage là một trang: khách, con trỏ trang sau (nil: hết), số khách mỗi trạng thái (đã lọc tháng, tìm)
// và mỗi tháng (đã lọc trạng thái, tìm; chỉ số 0 = tháng 1), tổng số khách khớp mọi bộ lọc.
// UpcomingWeek (lọc như StateCounts): khách chờ ngày gửi, đã có đơn giao thành công, ngày gửi trong 7 ngày tới (sau hôm
// nay, tới hôm nay + 7). Year (năm hiện tại, giờ Việt Nam), YearIssued, YearUsed: số quà của năm sinh nhật Year đã
// phát và đã dùng, không theo bộ lọc (Task 487).
type BirthdayCustomerPage struct {
	Items        []BirthdayCustomer
	Next         *string
	StateCounts  map[string]int
	MonthCounts  [12]int
	Total        int
	UpcomingWeek int
	Year         int
	YearIssued   int
	YearUsed     int
}

// upcomingDays là cửa sổ "Sắp gửi · 7 ngày tới".
const upcomingDays = 7

// ListBirthdayCustomers trả mọi khách đã để lại sinh nhật với trạng thái quà lúc now (luật của admin website), ưu tiên
// theo BirthdayStates rồi gần sinh nhật nhất. Trạng thái, tháng, con trỏ sai → ErrInvalidInput.
func ListBirthdayCustomers(ctx context.Context, tx pgx.Tx, q BirthdayCustomerQuery, now time.Time) (BirthdayCustomerPage, error) {
	offset, err := checkCustomerQuery(&q)
	if err != nil {
		return BirthdayCustomerPage{}, err
	}
	all, year, err := birthdayCustomers(ctx, tx, now)
	if err != nil {
		return BirthdayCustomerPage{}, err
	}
	text, digits := customer.NormalizeName(strings.TrimSpace(q.Q)), onlyDigits(q.Q)
	page := BirthdayCustomerPage{
		StateCounts: map[string]int{}, Year: year.year, YearIssued: year.issued, YearUsed: year.used,
	}
	weekEnd := civilOf(now) + upcomingDays
	for _, st := range BirthdayStates() {
		page.StateCounts[st] = 0
	}
	var shown []BirthdayCustomer
	for _, c := range all {
		if !matchesText(c, text, digits) {
			continue
		}
		inMonth, inState := q.Month == 0 || c.Month == q.Month, q.State == "" || c.State == q.State
		if inMonth {
			page.StateCounts[c.State]++
			if c.State == BirthdayStateWaiting && c.delivered && civilOf(c.SendOn) <= weekEnd {
				page.UpcomingWeek++
			}
		}
		if inState {
			page.MonthCounts[c.Month-1]++
		}
		if inMonth && inState {
			shown = append(shown, c)
		}
	}
	setPageItems(&page, shown, q, offset)
	return page, nil
}

// setPageItems đặt Total, Items (mọi dòng khi q.All, không thì một trang từ offset) và con trỏ trang sau.
func setPageItems(page *BirthdayCustomerPage, shown []BirthdayCustomer, q BirthdayCustomerQuery, offset int) {
	page.Total = len(shown)
	if q.All {
		page.Items = shown
		return
	}
	start := min(offset, len(shown))
	end := min(start+q.Limit, len(shown))
	page.Items = shown[start:end]
	if end < len(shown) {
		next := strconv.Itoa(end)
		page.Next = &next
	}
}

// matchesText: chuỗi tìm rỗng, hay tên (bỏ dấu) chứa text, hay SĐT chứa digits (đủ minPhoneDigits số).
func matchesText(c BirthdayCustomer, text, digits string) bool {
	return text == "" || strings.Contains(customer.NormalizeName(string(c.FullName)), text) ||
		(len(digits) >= minPhoneDigits && phoneHas(c.Phone, digits))
}

// checkCustomerQuery chuẩn hóa q (all → rỗng, Limit) và trả vị trí bắt đầu từ con trỏ.
func checkCustomerQuery(q *BirthdayCustomerQuery) (int, error) {
	if q.State == "all" {
		q.State = ""
	}
	if q.State != "" && !slices.Contains(BirthdayStates(), q.State) {
		return 0, fmt.Errorf("trạng thái sinh nhật: %w", ErrInvalidInput)
	}
	if q.Month < 0 || q.Month > 12 {
		return 0, fmt.Errorf("tháng sinh nhật: %w", ErrInvalidInput)
	}
	if q.Limit < 1 || q.Limit > maxBirthdayPage {
		q.Limit = defaultBirthdayPage
	}
	if q.Cursor == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(q.Cursor)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("con trỏ danh sách sinh nhật: %w", ErrInvalidInput)
	}
	return n, nil
}

// phoneHas: digits nằm trong số đã lưu (+84…) hay trong dạng trong nước của nó (0…), để nhân viên gõ "0912…" cũng ra
// khách (Task 209).
func phoneHas(p logging.Phone, digits string) bool {
	raw := string(p)
	if strings.Contains(raw, digits) {
		return true
	}
	rest, ok := strings.CutPrefix(raw, "+84")
	return ok && strings.Contains("0"+rest, digits)
}

func onlyDigits(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
}

// yearGifts là số quà của năm sinh nhật year đã phát, đã dùng.
type yearGifts struct {
	year, issued, used int
}

// birthdayCustomers tính trạng thái của mọi hồ sơ lúc now, đã sắp, và số quà của năm hiện tại.
func birthdayCustomers(ctx context.Context, tx pgx.Tx, now time.Time) ([]BirthdayCustomer, yearGifts, error) {
	s, err := GetBirthdaySettings(ctx, tx)
	if err != nil {
		return nil, yearGifts{}, err
	}
	q := store.New(tx)
	today := civilOf(now)
	y, _, _ := today.date()
	profiles, err := q.BirthdayProfiles(ctx)
	if err != nil {
		return nil, yearGifts{}, fmt.Errorf("đọc hồ sơ sinh nhật: %w", err)
	}
	giftRows, err := q.BirthdayGiftsSince(ctx, int32(y-1)) //nolint:gosec // G115: năm
	if err != nil {
		return nil, yearGifts{}, fmt.Errorf("đọc quà sinh nhật: %w", err)
	}
	stats := yearGifts{year: y}
	gifts := make(map[uuid.UUID]map[int]store.BirthdayGiftsSinceRow, len(giftRows))
	for _, g := range giftRows {
		if gifts[g.CustomerID] == nil {
			gifts[g.CustomerID] = map[int]store.BirthdayGiftsSinceRow{}
		}
		gifts[g.CustomerID][int(g.GiftYear)] = g
		if int(g.GiftYear) == y {
			stats.issued++
			if g.UsedCount > 0 {
				stats.used++
			}
		}
	}
	out := make([]BirthdayCustomer, 0, len(profiles))
	for _, p := range profiles {
		c := BirthdayCustomer{
			CustomerID: p.CustomerID, FullName: customer.Name(p.FullName), Phone: logging.Phone(p.Phone),
			Email: logging.Secret(p.Email), Day: int(p.Day), Month: int(p.Month), Zalo: p.ConsentZalo,
			Mail: p.ConsentMail && p.Email != "", SavedAt: p.CreatedAt, delivered: p.Delivered,
		}
		bday, sendOn := customerState(&c, gifts[p.CustomerID], p.Delivered, civilOf(p.CreatedAt), today, y, s)
		if p.StoppedAt.Valid {
			c.State = BirthdayStateStopped
		}
		c.BirthdayIn, c.SendOn = int(bday-today), sendOn.start()
		out = append(out, c)
	}
	order := BirthdayStates()
	slices.SortStableFunc(out, func(a, b BirthdayCustomer) int {
		return cmp.Or(cmp.Compare(slices.Index(order, a.State), slices.Index(order, b.State)),
			cmp.Compare(max(a.BirthdayIn, -a.BirthdayIn), max(b.BirthdayIn, -b.BirthdayIn)), strings.Compare(a.CustomerID.String(), b.CustomerID.String()))
	})
	return out, stats, nil
}

// customerState đặt c.State, c.Code theo quà gần nhất (còn hiệu lực hay hết hạn chưa quá 30 ngày) hoặc lần phát kế
// tiếp; trả ngày sinh nhật đang xét và ngày phát.
func customerState(c *BirthdayCustomer, gifts map[int]store.BirthdayGiftsSinceRow, delivered bool, saved, today civil,
	y int, s BirthdaySettings,
) (civil, civil) {
	for _, yr := range []int{y + 1, y, y - 1} {
		g, ok := gifts[yr]
		if !ok {
			continue
		}
		last := civilOf(g.ValidTo.Add(-time.Nanosecond))
		if today > last+expiredShownDays {
			break
		}
		c.Code, c.CouponID = g.Code, g.CouponID
		switch {
		case g.UsedCount > 0:
			c.State = BirthdayStateUsed
		case last < today:
			c.State = BirthdayStateExpired
		case !g.GivenAt.Valid:
			c.State = BirthdayStateManual
		default:
			c.State = BirthdayStateGiven
		}
		return birthdayIn(yr, c.Day, c.Month), civilOf(g.ValidFrom)
	}
	year := y + 1
	for _, yr := range []int{y, y + 1} {
		bd := birthdayIn(yr, c.Day, c.Month)
		if _, has := gifts[yr]; bd >= today && !has && saved <= bd-civil(s.MinLeadDays) {
			year = yr
			break
		}
	}
	bd := birthdayIn(year, c.Day, c.Month)
	sendOn := bd - civil(s.DaysBefore)
	switch {
	case sendOn > today:
		c.State = BirthdayStateWaiting
	case delivered:
		c.State = BirthdayStateDue
	default:
		c.State = BirthdayStateUnconfirmed
	}
	return bd, sendOn
}
