package httpapi

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/promotion"
)

// Quà sinh nhật — phía nhân viên (kế hoạch M5, Task 468 / M5-56; QĐ40). Đọc cần promotion.read, ghi cần
// promotion.birthday.manage (bảng route). Audit ghi ở đây, cùng tx (promotion không nhập identity), chỉ số đếm và cài
// đặt, không SĐT, mã dừng, chuỗi tìm.

// csvBOM để Excel mở tệp CSV UTF-8 đúng dấu tiếng Việt.
const csvBOM = "\xef\xbb\xbf"

// GetBirthdaySettings (GET /api/v1/birthday-settings): cài đặt kèm tên người sửa và lượt phát gần nhất.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetBirthdaySettings(ctx context.Context) (gen.GetBirthdaySettingsRes, error) {
	var out *gen.BirthdaySettings
	if err := h.catalogRead(ctx, func(tx pgx.Tx) error {
		s, err := promotion.GetBirthdaySettings(ctx, tx)
		if err != nil {
			return err //nolint:wrapcheck // promotion đã bọc
		}
		out, err = birthdaySettingsOut(ctx, tx, s)
		return err
	}); err != nil {
		return nil, fmt.Errorf("đọc cài đặt quà sinh nhật: %w", err)
	}
	return out, nil
}

// SaveBirthdaySettings (PUT /api/v1/birthday-settings): lưu khi version khớp; audit promotion.birthday.settings.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) SaveBirthdaySettings(ctx context.Context, req *gen.SaveBirthdaySettingsRequest,
	_ gen.SaveBirthdaySettingsParams,
) (gen.SaveBirthdaySettingsRes, error) {
	in := promotion.BirthdaySettings{
		Enabled: req.Enabled, Amount: req.Amount, MinOrder: req.MinOrder, DaysBefore: int(req.DaysBefore),
		ValidDays: int(req.ValidDays), MinLeadDays: int(req.MinLeadDays),
	}
	out, err := h.refusableWrite(ctx, http.StatusOK, isPromotionRefusal, "lưu cài đặt quà sinh nhật",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			before, after, err := promotion.Writer{}.SaveBirthdaySettings(ctx, tx, in, int(req.Version),
				promotionActor(u), h.now())
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của promotion
			}
			body, err := birthdaySettingsOut(ctx, tx, after)
			if err != nil {
				return nil, err
			}
			return body, h.audit(ctx, tx, u, "promotion.birthday.settings", "promotion.birthday_settings", u.TenantID,
				before.AuditFields(), after.AuditFields())
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.BirthdaySettings), nil //nolint:forcetypeassert // fn trên trả *gen.BirthdaySettings
}

// RunBirthdayGifts (POST /api/v1/birthday-runs): "Tạo mã ngay"; lượt khác đang chạy → 409 another_run_in_progress.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) RunBirthdayGifts(ctx context.Context, _ gen.RunBirthdayGiftsParams) (gen.RunBirthdayGiftsRes, error) {
	w := promotion.Writer{Realtime: h.d.Realtime}
	out, err := h.refusableWrite(ctx, http.StatusOK, isPromotionRefusal, "chạy lượt phát quà sinh nhật",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			r, err := w.RunBirthdayGifts(ctx, tx, promotion.BirthdayTriggerManual, promotionActor(u), h.now())
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của promotion
			}
			return birthdayRunOut(r), h.audit(ctx, tx, u, "promotion.birthday.run", "promotion.birthday_run", r.ID, nil,
				map[string]any{"trigger": r.Trigger, "given": r.Given, "failed": r.Failed})
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.BirthdayRun), nil //nolint:forcetypeassert // fn trên trả *gen.BirthdayRun
}

// ListBirthdayTodo (GET /api/v1/birthday-gifts/todo): tab "Cần nhắn".
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListBirthdayTodo(ctx context.Context, params gen.ListBirthdayTodoParams) (gen.ListBirthdayTodoRes, error) {
	out := &gen.BirthdayGiftList{Items: []gen.BirthdayGift{}}
	if err := h.catalogRead(ctx, func(tx pgx.Tx) error {
		gs, err := promotion.ListBirthdayTodo(ctx, tx, h.now(), int(params.Limit.Or(0)))
		out.Items = out.Items[:0]
		for _, g := range gs {
			out.Items = append(out.Items, birthdayGiftOut(g))
		}
		return err //nolint:wrapcheck // promotion đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đọc quà sinh nhật cần nhắn: %w", err)
	}
	return out, nil
}

// MarkBirthdayMessaged (POST /api/v1/birthday-gifts/messaged): "Đã nhắn"; audit promotion.birthday.messaged (số mã) khi
// có mã vừa ghi.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) MarkBirthdayMessaged(ctx context.Context, req *gen.BirthdayMessagedRequest,
	_ gen.MarkBirthdayMessagedParams,
) (gen.MarkBirthdayMessagedRes, error) {
	out, err := h.refusableWrite(ctx, http.StatusOK, isPromotionRefusal, "ghi đã nhắn quà sinh nhật",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			marked, err := promotion.MarkBirthdayMessaged(ctx, tx, req.Codes, h.now())
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của promotion
			}
			body := &gen.BirthdayMessagedResult{Marked: marked}
			if len(marked) == 0 {
				return body, nil
			}
			return body, h.audit(ctx, tx, u, "promotion.birthday.messaged", "promotion.birthday_gift",
				uuid.Must(uuid.NewV7()), nil, map[string]any{"codes": len(req.Codes), "marked": len(marked)})
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.BirthdayMessagedResult), nil //nolint:forcetypeassert // fn trên trả *gen.BirthdayMessagedResult
}

// ListBirthdayCustomers (GET /api/v1/birthday-customers): tab "Tất cả khách".
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListBirthdayCustomers(ctx context.Context, params gen.ListBirthdayCustomersParams,
) (gen.ListBirthdayCustomersRes, error) {
	q := promotion.BirthdayCustomerQuery{
		State: string(params.State.Or("")), Month: int(params.Month.Or(0)), Q: params.Q.Or(""),
		Cursor: params.Cursor.Or(""), Limit: int(params.Limit.Or(0)),
	}
	var out *gen.BirthdayCustomerPage
	if err := h.catalogRead(ctx, func(tx pgx.Tx) error {
		p, err := promotion.ListBirthdayCustomers(ctx, tx, q, h.now())
		if err != nil {
			return err //nolint:wrapcheck // sentinel của promotion
		}
		out = birthdayPageOut(p)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("đọc khách sinh nhật: %w", err)
	}
	return out, nil
}

// ExportBirthdayCustomers (POST /api/v1/birthday-customers/export): mọi khách khớp bộ lọc thành CSV (UTF-8 có BOM);
// audit promotion.birthday.export có số dòng và bộ lọc (không ghi chuỗi tìm: có thể là SĐT).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ExportBirthdayCustomers(ctx context.Context, req *gen.BirthdayExportRequest,
	_ gen.ExportBirthdayCustomersParams,
) (gen.ExportBirthdayCustomersRes, error) {
	q := promotion.BirthdayCustomerQuery{
		State: string(req.State.Or("")), Month: int(req.Month.Or(0)), Q: req.Q.Or(""), All: true,
	}
	now := h.now()
	out, err := h.refusableWrite(ctx, http.StatusOK, isPromotionRefusal, "xuất khách sinh nhật",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			p, err := promotion.ListBirthdayCustomers(ctx, tx, q, now)
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của promotion
			}
			text, err := birthdayCSV(p.Items)
			if err != nil {
				return nil, err
			}
			body := &gen.BirthdayExport{
				Filename: "khach-sinh-nhat-" + now.In(vnZone()).Format("20060102-1504") + ".csv",
				Rows:     int32(len(p.Items)), Csv: text, //nolint:gosec // G115: số khách của shop
			}
			return body, h.audit(ctx, tx, u, "promotion.birthday.export", "promotion.birthday_export",
				uuid.Must(uuid.NewV7()), nil, map[string]any{
					"rows": len(p.Items), "state": q.State, "month": q.Month, "searched": q.Q != "",
				})
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.BirthdayExport), nil //nolint:forcetypeassert // fn trên trả *gen.BirthdayExport
}

func vnZone() *time.Location { return time.FixedZone("ICT", 7*3600) }

// birthdayCSV: một dòng tiêu đề, mỗi khách một dòng; ngày theo giờ Việt Nam.
func birthdayCSV(items []promotion.BirthdayCustomer) (string, error) {
	var buf bytes.Buffer
	buf.WriteString(csvBOM)
	w := csv.NewWriter(&buf)
	w.UseCRLF = true
	rows := [][]string{{
		"Tên", "SĐT", "Email", "Ngày sinh", "Trạng thái", "Ngày phát", "Mã quà", "Zalo", "Email",
		"Đăng ký lúc",
	}}
	yes := map[bool]string{true: "có", false: "không"}
	for _, c := range items {
		rows = append(rows, []string{
			csvSafe(string(c.FullName)), string(c.Phone), csvSafe(string(c.Email)),
			strconv.Itoa(c.Day) + "/" + strconv.Itoa(c.Month), c.State, c.SendOn.In(vnZone()).Format("02/01/2006"), c.Code,
			yes[c.Zalo], yes[c.Mail], c.SavedAt.In(vnZone()).Format("02/01/2006 15:04"),
		})
	}
	if err := w.WriteAll(rows); err != nil {
		return "", fmt.Errorf("ghi CSV khách sinh nhật: %w", err)
	}
	return buf.String(), nil
}

// csvSafe chặn công thức khi mở bằng bảng tính (ô bắt đầu bằng = + - @ thêm dấu nháy).
func csvSafe(s string) string {
	if s != "" && (s[0] == '=' || s[0] == '+' || s[0] == '-' || s[0] == '@') {
		return "'" + s
	}
	return s
}

func birthdaySettingsOut(ctx context.Context, tx pgx.Tx, s promotion.BirthdaySettings) (*gen.BirthdaySettings, error) {
	out := &gen.BirthdaySettings{
		Enabled: s.Enabled, Amount: s.Amount, MinOrder: s.MinOrder, DaysBefore: int32(s.DaysBefore), //nolint:gosec // G115: ≤ 120
		ValidDays: int32(s.ValidDays), MinLeadDays: int32(s.MinLeadDays), Version: int32(s.Version), //nolint:gosec // G115
	}
	if s.UpdatedAt != nil {
		out.UpdatedAt = gen.NewOptDateTime(*s.UpdatedAt)
	}
	if s.UpdatedBy != nil {
		out.UpdatedBy = gen.NewOptUUID(*s.UpdatedBy)
		names, err := staffNames(ctx, tx, []uuid.UUID{*s.UpdatedBy})
		if err != nil {
			return nil, err
		}
		if n := names[*s.UpdatedBy]; n != "" {
			out.UpdatedByName = gen.NewOptString(n)
		}
	}
	last, err := promotion.LastBirthdayRun(ctx, tx)
	if err != nil {
		return nil, err //nolint:wrapcheck // promotion đã bọc
	}
	if last != nil {
		out.LastRun = gen.NewOptBirthdayRun(*birthdayRunOut(*last))
	}
	return out, nil
}

func birthdayRunOut(r promotion.BirthdayRun) *gen.BirthdayRun {
	out := &gen.BirthdayRun{
		ID: r.ID, Trigger: gen.BirthdayRunTrigger(r.Trigger), StartedAt: r.StartedAt,
		Given: int32(r.Given), Failed: int32(r.Failed), //nolint:gosec // G115: số khách của shop
	}
	if r.StartedBy != nil {
		out.StartedBy = gen.NewOptUUID(*r.StartedBy)
	}
	if r.FinishedAt != nil {
		out.FinishedAt = gen.NewOptDateTime(*r.FinishedAt)
	}
	return out
}

func birthdayGiftOut(g promotion.BirthdayGift) gen.BirthdayGift {
	out := gen.BirthdayGift{
		CustomerId: g.CustomerID, Code: g.Code, FullName: string(g.FullName), Phone: string(g.Phone),
		Day: int32(g.Day), Month: int32(g.Month), Zalo: g.Zalo, Mail: g.Mail, Amount: g.Amount, //nolint:gosec // G115
		MinOrder: g.MinOrder, ValidFrom: g.ValidFrom, ValidTo: g.ValidTo, BirthdayIn: int32(g.BirthdayIn), //nolint:gosec // G115
	}
	if g.Email != "" {
		out.Email = gen.NewOptString(string(g.Email))
	}
	return out
}

func birthdayPageOut(p promotion.BirthdayCustomerPage) *gen.BirthdayCustomerPage {
	out := &gen.BirthdayCustomerPage{
		Items: make([]gen.BirthdayCustomer, 0, len(p.Items)), Total: int32(p.Total), //nolint:gosec // G115
		MonthCounts: make([]int32, 0, len(p.MonthCounts)),
		StateCounts: gen.BirthdayStateCounts{
			Manual: int32(p.StateCounts[promotion.BirthdayStateManual]), Due: int32(p.StateCounts[promotion.BirthdayStateDue]), //nolint:gosec // G115
			Unconfirmed: int32(p.StateCounts[promotion.BirthdayStateUnconfirmed]), Given: int32(p.StateCounts[promotion.BirthdayStateGiven]), //nolint:gosec // G115
			Waiting: int32(p.StateCounts[promotion.BirthdayStateWaiting]), Used: int32(p.StateCounts[promotion.BirthdayStateUsed]), //nolint:gosec // G115
			Expired: int32(p.StateCounts[promotion.BirthdayStateExpired]), Stopped: int32(p.StateCounts[promotion.BirthdayStateStopped]), //nolint:gosec // G115
		},
	}
	for _, n := range p.MonthCounts {
		out.MonthCounts = append(out.MonthCounts, int32(n)) //nolint:gosec // G115
	}
	if p.Next != nil {
		out.NextCursor = gen.NewOptString(*p.Next)
	}
	for _, c := range p.Items {
		it := gen.BirthdayCustomer{
			CustomerId: c.CustomerID, FullName: string(c.FullName), Phone: string(c.Phone), Day: int32(c.Day), //nolint:gosec // G115
			Month: int32(c.Month), Zalo: c.Zalo, Mail: c.Mail, State: gen.BirthdayCustomerState(c.State), //nolint:gosec // G115
			BirthdayIn: int32(c.BirthdayIn), SendOn: c.SendOn, SavedAt: c.SavedAt, //nolint:gosec // G115
		}
		if c.Email != "" {
			it.Email = gen.NewOptString(string(c.Email))
		}
		if c.Code != "" {
			it.Code = gen.NewOptString(c.Code)
		}
		out.Items = append(out.Items, it)
	}
	return out
}
