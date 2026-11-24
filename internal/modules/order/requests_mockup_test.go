package order_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/order"
)

// Dữ liệu theo mockup M5 (Task 486).

func (e *reqEnv) list(f order.RequestFilter) ([]order.RequestSummary, error) {
	return inApp(e, func(tx pgx.Tx) ([]order.RequestSummary, error) {
		rows, _, err := order.ListRequests(e.t.Context(), tx, e.seller, f)
		return rows, err //nolint:wrapcheck // test
	})
}

func (e *reqEnv) counts() order.RequestCounts {
	e.t.Helper()
	c, err := inApp(e, func(tx pgx.Tx) (order.RequestCounts, error) {
		return order.CountRequests(e.t.Context(), tx)
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return c
}

// [Task 486] Dòng danh sách có tên khách (gốc), số ảnh, hạn gửi trả khi đã xác nhận đổi; lọc nhiều trạng thái
// (Statuses); Status và Statuses cùng lúc, trạng thái lạ trong Statuses → ErrInvalidInput.
func TestRequest_ListMockupFields(t *testing.T) {
	t.Parallel()
	e := newReqEnv(t)
	v := e.variant(200000, 10)
	e.webOrder("W-1", true, ln(v, 5, 0))
	f1, f2 := e.file(e.tenant, "ready"), e.file(e.tenant, "uploaded")
	withPhotos := e.mustRequest("W-1", exchange(v, 1, "DEFECT", f1, f2), e.now())
	confirmed := e.mustRequest("W-1", exchange(v, 1, "FIT"), e.now().Add(time.Minute))
	if _, err := e.setStatus(confirmed.ID, changeTo(order.RequestConfirmed), 1); err != nil {
		t.Fatal(err)
	}
	rejected := e.mustRequest("W-1", exchange(v, 1, "FIT"), e.now().Add(2*time.Minute))
	if _, err := e.setStatus(rejected.ID, changeTo(order.RequestRejected), 1); err != nil {
		t.Fatal(err)
	}
	rows, err := e.list(order.RequestFilter{})
	if err != nil || len(rows) != 3 {
		t.Fatalf("danh sách: %+v %v", rows, err)
	}
	by := map[uuid.UUID]order.RequestSummary{}
	for _, r := range rows {
		by[r.ID] = r
		if r.CustomerName != "Chị Lan" {
			t.Errorf("%s: tên khách %q", r.Ref, r.CustomerName)
		}
	}
	if got := by[withPhotos.ID]; got.PhotoCount != 2 || got.ShipBackDeadline != nil {
		t.Errorf("yêu cầu có ảnh: %+v", got)
	}
	wantDeadline := time.Date(2026, 11, 25, 23, 59, 59, 0, time.FixedZone("ICT", 7*3600))
	if got := by[confirmed.ID]; got.PhotoCount != 0 || got.ShipBackDeadline == nil ||
		!got.ShipBackDeadline.Equal(wantDeadline) {
		t.Errorf("đã xác nhận đổi: %+v", got)
	}

	open, err := e.list(order.RequestFilter{Statuses: order.OpenRequestStatuses()})
	if err != nil || len(open) != 2 || open[0].ID != confirmed.ID || open[1].ID != withPhotos.ID {
		t.Fatalf("đang mở: %+v %v", open, err)
	}
	two, err := e.list(order.RequestFilter{Statuses: []string{order.RequestRejected, order.RequestConfirmed}})
	if err != nil || len(two) != 2 {
		t.Fatalf("hai trạng thái: %+v %v", two, err)
	}
	_, err = e.list(order.RequestFilter{Status: order.RequestReceived, Statuses: []string{order.RequestRejected}})
	wantErr(t, "status và statuses", err, order.ErrInvalidInput)
	_, err = e.list(order.RequestFilter{Statuses: []string{order.RequestReceived, "open"}})
	wantErr(t, "trạng thái lạ", err, order.ErrInvalidInput)
}

// [Task 486] Số yêu cầu theo trạng thái (mọi trạng thái, kể cả 0), số đang mở, tổng; tenant khác không tính.
func TestRequest_Counts(t *testing.T) {
	t.Parallel()
	e := newReqEnv(t)
	if c := e.counts(); c.Total != 0 || c.Open != 0 || len(c.ByStatus) != 8 {
		t.Fatalf("rỗng: %+v", c)
	}
	v := e.variant(200000, 10)
	e.webOrder("W-1", true, ln(v, 5, 0))
	for range 2 {
		e.mustRequest("W-1", exchange(v, 1, "FIT"), e.now())
	}
	r := e.mustRequest("W-1", exchange(v, 1, "FIT"), e.now())
	if _, err := e.setStatus(r.ID, changeTo(order.RequestRejected), 1); err != nil {
		t.Fatal(err)
	}
	e.webOrder("W-2", false, ln(v, 1, 0))
	e.mustRequest("W-2", cancelIn(), e.now())
	c := e.counts()
	if c.Total != 4 || c.Open != 3 || c.ByStatus[order.RequestReceived] != 3 || c.ByStatus[order.RequestRejected] != 1 ||
		c.ByStatus[order.RequestResolved] != 0 || len(c.ByStatus) != 8 {
		t.Fatalf("đếm: %+v", c)
	}
	b := e.staff.Tenant(t, "B")
	e.staff.Exec(t, b, func(tx pgx.Tx) error {
		got, err := order.CountRequests(t.Context(), tx)
		if err != nil || got.Total != 0 {
			t.Errorf("tenant B: %+v %v", got, err)
		}
		return nil
	})
}

// [Task 486] Dòng đổi có biến thể muốn đổi sang: mã, tên mẫu, màu, cỡ, số còn bán được ở kho của đơn và tên kho. Website
// không nhận phần này (ListWebRequests).
func TestRequest_WantVariant(t *testing.T) {
	t.Parallel()
	e := newReqEnv(t)
	v, want := e.variant(200000, 10), e.variant(200000, 4)
	e.webOrder("W-1", true, ln(v, 2, 0))
	in := exchange(v, 1, "TOO_SMALL")
	in.Lines[0].WantVariantID = &want
	r := e.mustRequest("W-1", in, e.now())
	d := e.get(r.ID)
	if len(d.Lines) != 1 || d.Lines[0].Want == nil {
		t.Fatalf("dòng: %+v", d.Lines)
	}
	w := d.Lines[0].Want
	if w.SKU == "" || w.ProductName == "" || w.Color != "Đỏ" || w.Size == "" || w.Available == nil || *w.Available != 4 ||
		w.WarehouseName == "" {
		t.Fatalf("muốn đổi sang: %+v", w)
	}
	web, err := inApp(e, func(tx pgx.Tx) ([]order.RequestDetail, error) {
		return order.ListWebRequests(t.Context(), tx, e.seller, "W-1")
	})
	if err != nil || len(web) != 1 || web[0].Lines[0].Want != nil || web[0].History != nil {
		t.Fatalf("website: %+v %v", web, err)
	}
}

// [Task 486] Lịch sử: mỗi lần đổi trạng thái (nhân viên, khách trả lời qua website, khách rút) một dòng từ → sang, loại
// người làm, mã người làm, lúc; theo thời gian. Bảng chỉ ghi thêm.
func TestRequest_History(t *testing.T) {
	t.Parallel()
	e, v, r := newReqWithExchange(t)
	if h := e.get(r.ID).History; len(h) != 0 {
		t.Fatalf("mới tạo: %+v", h)
	}
	if _, err := e.setStatus(r.ID, changeTo(order.RequestNeedsInfo), 1); err != nil {
		t.Fatal(err)
	}
	if _, err := inApp(e, func(tx pgx.Tx) (order.RequestChange, error) {
		return e.w().CustomerReplyRequest(t.Context(), tx, e.seller, "W-1", r.Ref, "Em gửi thêm ảnh", e.key, e.now())
	}); err != nil {
		t.Fatal(err)
	}
	h := e.get(r.ID).History
	if len(h) != 2 ||
		h[0].From != order.RequestReceived || h[0].To != order.RequestNeedsInfo || h[0].ActorType != order.AuthorStaff ||
		h[0].ActorID != e.actor.UserID.String() ||
		h[1].From != order.RequestNeedsInfo || h[1].To != order.RequestInReview ||
		h[1].ActorType != order.AuthorCustomer || h[1].ActorID != e.key.UserID.String() || h[1].At.IsZero() {
		t.Fatalf("lịch sử: %+v", h)
	}
	r2 := e.mustRequest("W-1", exchange(v, 1, "FIT"), e.now())
	if _, err := inApp(e, func(tx pgx.Tx) (order.RequestChange, error) {
		return e.w().CustomerCancelRequest(t.Context(), tx, e.seller, "W-1", r2.Ref, e.key, e.now())
	}); err != nil {
		t.Fatal(err)
	}
	if h := e.get(r2.ID).History; len(h) != 1 || h[0].To != order.RequestCancelled || h[0].ActorType != order.AuthorCustomer {
		t.Fatalf("khách rút: %+v", h)
	}
	e.staff.CheckOwnerAppendOnly(t, e.tenant, `UPDATE ord.order_request_events SET to_status = 'resolved'`,
		`DELETE FROM ord.order_request_events`)
}
