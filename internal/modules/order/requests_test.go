package order_test

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/files/filestest"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
)

// reqEnv là orderEnv kèm bus ghi báo hiệu và khóa dịch vụ giả của website.
type reqEnv struct {
	*orderEnv
	bus *realtimetest.RecordingBus
	key order.Actor
}

func newReqEnv(t *testing.T) *reqEnv {
	t.Helper()
	order.RegisterFilePurposes()
	e := &reqEnv{orderEnv: newM2Env(t), bus: &realtimetest.RecordingBus{}}
	e.key = order.Actor{TenantID: e.tenant, UserID: uuid.New(), Type: order.ActorService}
	return e
}

// newReqWithExchange: đơn web W-1 đã giao 3 chiếc biến thể v và một yêu cầu đổi 1 chiếc (received).
func newReqWithExchange(t *testing.T) (*reqEnv, uuid.UUID, order.Request) {
	t.Helper()
	e := newReqEnv(t)
	v := e.variant(200000, 10)
	e.webOrder("W-1", true, ln(v, 3, 0))
	return e, v, e.mustRequest("W-1", exchange(v, 1, "FIT"), e.now())
}

func (e *reqEnv) w() order.Writer {
	return order.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue(), Realtime: e.bus}
}

// webOrder tạo đơn kênh web có mã ngoài ref (dòng lines), confirmed hoặc đã giao (lúc e.now()).
func (e *reqEnv) webOrder(ref string, delivered bool, lines ...order.LineInput) order.Order {
	e.t.Helper()
	in := e.m2Input(30000, lines...)
	in.Channel, in.ExternalRef = order.ChannelWeb, ref
	if delivered {
		return e.deliveredFrom(in)
	}
	return e.mustCreateM2(in).Order
}

// inApp chạy fn bằng crm_app trong ngữ cảnh tenant của e và trả kết quả của fn.
func inApp[T any](e *reqEnv, fn func(tx pgx.Tx) (T, error)) (T, error) { //nolint:ireturn // T là kiểu cụ thể của bên gọi
	var out T
	err := e.app(func(tx pgx.Tx) error {
		var err error
		out, err = fn(tx)
		return err
	})
	return out, err
}

func (e *reqEnv) create(ref string, in order.CreateRequestInput, at time.Time) (order.Request, error) {
	return inApp(e, func(tx pgx.Tx) (order.Request, error) {
		return e.w().CreateRequestFromWeb(e.t.Context(), tx, e.seller, ref, in, e.key, at)
	})
}

func (e *reqEnv) mustRequest(ref string, in order.CreateRequestInput, at time.Time) order.Request {
	e.t.Helper()
	r, err := e.create(ref, in, at)
	if err != nil {
		e.t.Fatalf("tạo yêu cầu: %v", err)
	}
	return r
}

func (e *reqEnv) setStatus(id uuid.UUID, ch order.StatusChange, version int) (order.RequestChange, error) {
	return inApp(e, func(tx pgx.Tx) (order.RequestChange, error) {
		return e.w().SetRequestStatus(e.t.Context(), tx, id, ch, version, e.actor, e.now())
	})
}

func (e *reqEnv) get(id uuid.UUID) order.RequestDetail {
	e.t.Helper()
	var d order.RequestDetail
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		d, err = order.GetRequest(e.t.Context(), tx, e.seller, id)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	return d
}

// file tạo tệp của tenant theo kind: uploaded, ready, rejected (mục đích order_request_photo).
func (e *reqEnv) file(tenant uuid.UUID, kind string) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	e.staff.Exec(e.t, tenant, func(tx pgx.Tx) error {
		switch kind {
		case "ready":
			id = filestest.Image(e.t, tx, tenant, order.RequestPhotoPurposeKey)
		case "rejected":
			id = filestest.Rejected(e.t, tx, tenant, order.RequestPhotoPurposeKey)
		case "other":
			id = filestest.Uploaded(e.t, tx, tenant, "product_image")
		default:
			id = filestest.Uploaded(e.t, tx, tenant, order.RequestPhotoPurposeKey)
		}
		return nil
	})
	return id
}

func exchange(v uuid.UUID, qty int, reason string, files ...uuid.UUID) order.CreateRequestInput {
	return order.CreateRequestInput{
		Kind: order.RequestExchange, ReasonCode: "SIZE", Note: "Áo hơi chật, đổi size lớn hơn", ConditionConfirmed: true,
		Lines: []order.RequestLine{{VariantID: v, Qty: qty, Reason: reason}}, FileIDs: files,
	}
}

func cancelIn() order.CreateRequestInput {
	return order.CreateRequestInput{Kind: order.RequestCancel, ReasonCode: "CHANGED_MIND", Note: "Em đổi ý không mua nữa"}
}

func wantErr(t *testing.T, name string, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("%s: lỗi %v, mong %v", name, err, want)
	}
}

// Đơn giao lúc D (e.now()): đổi lúc D+7 ngày được; D+8 → ErrWindowClosed. Đơn chưa giao → ErrRequestNotAllowed; mã
// ngoài lạ → ErrNotFound. Báo hiệu order_request.changed cho order.requests.read, chỉ mã yêu cầu.
func TestRequest_ExchangeWindow(t *testing.T) {
	t.Parallel()
	e := newReqEnv(t)
	v := e.variant(200000, 10)
	e.webOrder("W-1", true, ln(v, 2, 0))
	d := e.now()
	r := e.mustRequest("W-1", exchange(v, 1, "TOO_SMALL"), d.Add(7*24*time.Hour))
	if r.Status != order.RequestReceived || r.Version != 1 || !strings.HasPrefix(r.Ref, "YC-") || len(r.Ref) != 11 {
		t.Fatalf("yêu cầu: %+v", r)
	}
	_, err := e.create("W-1", exchange(v, 1, "TOO_SMALL"), d.Add(8*24*time.Hour))
	wantErr(t, "D+8", err, order.ErrWindowClosed)

	e.webOrder("W-2", false, ln(v, 1, 0))
	_, err = e.create("W-2", exchange(v, 1, "TOO_SMALL"), d)
	wantErr(t, "chưa giao", err, order.ErrRequestNotAllowed)
	_, err = e.create("W-LA", exchange(v, 1, "TOO_SMALL"), d)
	wantErr(t, "đơn lạ", err, order.ErrNotFound)

	sigs := e.bus.Signals()
	if len(sigs) != 1 || sigs[0].Topic != order.TopicOrderRequestSignal || sigs[0].Requires != "order.requests.read" ||
		sigs[0].Entity.Type != "order_request" || sigs[0].Entity.ID != r.ID.String() {
		t.Fatalf("báo hiệu: %+v", sigs)
	}
}

// Đơn giao 2 chiếc; yêu cầu mở đổi 1; yêu cầu mới 2 → ErrQtyExceeded; yêu cầu cũ bị từ chối → 2 chiếc được. Biến thể
// không có trong đơn → ErrInvalidInput.
func TestRequest_QtyHeld(t *testing.T) {
	t.Parallel()
	e := newReqEnv(t)
	v, other := e.variant(200000, 10), e.variant(100000, 10)
	e.webOrder("W-1", true, ln(v, 2, 0))
	old := e.mustRequest("W-1", exchange(v, 1, "TOO_SMALL"), e.now())
	_, err := e.create("W-1", exchange(v, 2, "TOO_SMALL"), e.now())
	wantErr(t, "2 khi 1 đang giữ", err, order.ErrQtyExceeded)
	_, err = e.create("W-1", exchange(other, 1, "TOO_SMALL"), e.now())
	wantErr(t, "biến thể lạ", err, order.ErrInvalidInput)
	if _, err := e.setStatus(old.ID, order.StatusChange{
		Status: order.RequestRejected, Outcome: &order.Outcome{Reason: "Hàng đã qua sử dụng"},
	}, 1); err != nil {
		t.Fatal(err)
	}
	e.mustRequest("W-1", exchange(v, 2, "TOO_SMALL"), e.now())
}

// Hai yêu cầu đổi 2 chiếc song song trên đơn giao 2 → đúng một thành công, một ErrQtyExceeded (khóa dòng đơn).
func TestRequest_ConcurrentExchange(t *testing.T) {
	t.Parallel()
	e := newReqEnv(t)
	v := e.variant(200000, 10)
	e.webOrder("W-1", true, ln(v, 2, 0))
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Go(func() { _, errs[i] = e.create("W-1", exchange(v, 2, "FIT"), e.now()) })
	}
	wg.Wait()
	ok, exceeded := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, order.ErrQtyExceeded):
			exceeded++
		default:
			t.Fatalf("lỗi lạ: %v", err)
		}
	}
	if ok != 1 || exceeded != 1 {
		t.Fatalf("thắng %d, vượt %d", ok, exceeded)
	}
}

// Ảnh: DEFECT không ảnh → ErrPhotoRequired; 6 ảnh → ErrTooManyFiles; ảnh tenant khác, sai mục đích, đã gắn yêu cầu
// khác → ErrFileNotFound; ảnh imager từ chối → ErrFileRejected; uploaded, ready → được, giữ thứ tự.
func TestRequest_Files(t *testing.T) {
	t.Parallel()
	e := newReqEnv(t)
	v := e.variant(200000, 10)
	e.webOrder("W-1", true, ln(v, 5, 0))
	_, err := e.create("W-1", exchange(v, 1, "DEFECT"), e.now())
	wantErr(t, "DEFECT không ảnh", err, order.ErrPhotoRequired)
	six := make([]uuid.UUID, 6)
	for i := range six {
		six[i] = uuid.New()
	}
	_, err = e.create("W-1", exchange(v, 1, "DEFECT", six...), e.now())
	wantErr(t, "6 ảnh", err, order.ErrTooManyFiles)

	b := e.staff.Tenant(t, "B")
	for name, id := range map[string]uuid.UUID{
		"tenant khác": e.file(b, "uploaded"), "sai mục đích": e.file(e.tenant, "other"), "không có": uuid.New(),
	} {
		_, err = e.create("W-1", exchange(v, 1, "DEFECT", id), e.now())
		wantErr(t, name, err, order.ErrFileNotFound)
	}
	_, err = e.create("W-1", exchange(v, 1, "WRONG_ITEM", e.file(e.tenant, "rejected")), e.now())
	wantErr(t, "imager từ chối", err, order.ErrFileRejected)

	f1, f2 := e.file(e.tenant, "ready"), e.file(e.tenant, "uploaded")
	r := e.mustRequest("W-1", exchange(v, 1, "DEFECT", f1, f2), e.now())
	if d := e.get(r.ID); len(d.FileIDs) != 2 || d.FileIDs[0] != f1 || d.FileIDs[1] != f2 {
		t.Fatalf("ảnh: %v", d.FileIDs)
	}
	_, err = e.create("W-1", exchange(v, 1, "DEFECT", f1), e.now())
	wantErr(t, "ảnh đã gắn", err, order.ErrFileNotFound)
}

// Hủy: hai yêu cầu hủy mở cho cùng đơn → lần hai ErrDuplicateOpen (cả CreateCancelRequest); đơn đã giao →
// ErrRequestNotAllowed; quá 5 yêu cầu trong 24 giờ → ErrRequestLimit.
func TestRequest_CancelAndLimit(t *testing.T) {
	t.Parallel()
	e := newReqEnv(t)
	v := e.variant(200000, 10)
	o := e.webOrder("W-1", false, ln(v, 1, 0))
	e.mustRequest("W-1", cancelIn(), e.now())
	_, err := e.create("W-1", cancelIn(), e.now())
	wantErr(t, "hủy lần hai", err, order.ErrDuplicateOpen)
	err = e.app(func(tx pgx.Tx) error {
		_, err := e.w().CreateCancelRequest(t.Context(), tx, e.seller, o.ID, "", e.key, e.now())
		return err //nolint:wrapcheck // test
	})
	wantErr(t, "CreateCancelRequest", err, order.ErrDuplicateOpen)

	e.webOrder("W-2", true, ln(v, 1, 0))
	_, err = e.create("W-2", cancelIn(), e.now())
	wantErr(t, "hủy đơn đã giao", err, order.ErrRequestNotAllowed)

	q := order.CreateRequestInput{Kind: order.RequestQuestion, ReasonCode: "OTHER", Note: "Khi nào có hàng lại ạ?"}
	for range 4 {
		e.mustRequest("W-1", q, e.now())
	}
	_, err = e.create("W-1", q, e.now())
	wantErr(t, "yêu cầu thứ 6", err, order.ErrRequestLimit)
	e.mustRequest("W-1", q, e.now().Add(25*time.Hour))
}

// Đầu vào sai → ErrInvalidInput (không chạm database).
func TestRequest_InvalidInput(t *testing.T) {
	t.Parallel()
	e := newReqEnv(t)
	v := e.variant(200000, 10)
	e.webOrder("W-1", true, ln(v, 2, 0))
	noCond := exchange(v, 1, "FIT")
	noCond.ConditionConfirmed = false
	short := cancelIn()
	short.Note = "ừ"
	badReason := cancelIn()
	badReason.ReasonCode = "SIZE"
	cancelLines := cancelIn()
	cancelLines.Lines = []order.RequestLine{{VariantID: v, Qty: 1}}
	dupFile := exchange(v, 1, "FIT")
	f := uuid.New()
	dupFile.FileIDs = []uuid.UUID{f, f}
	for name, in := range map[string]order.CreateRequestInput{
		"chưa xác nhận tình trạng": noCond, "lời nhắn ngắn": short, "lý do sai loại": badReason,
		"hủy có dòng": cancelLines, "ảnh trùng": dupFile, "loại lạ": {Kind: "refund", ReasonCode: "OTHER", Note: "abcdef"},
		"dòng không lý do": exchange(v, 1, ""), "số 0": exchange(v, 0, "FIT"),
	} {
		_, err := e.create("W-1", in, e.now())
		wantErr(t, name, err, order.ErrInvalidInput)
	}
}

// Sơ đồ trạng thái: mọi cặp (from, to) của nhân viên — cạnh có trong sơ đồ qua, cạnh khác ErrInvalidTransition;
// confirmed cho yêu cầu không phải đổi → ErrInvalidTransition.
func TestRequest_Transitions(t *testing.T) {
	t.Parallel()
	e := newReqEnv(t)
	v := e.variant(200000, 100)
	e.webOrder("W-1", true, ln(v, 60, 0))
	all := []string{
		order.RequestReceived, order.RequestInReview, order.RequestNeedsInfo, order.RequestConfirmed,
		order.RequestItemsReceived, order.RequestResolved, order.RequestRejected, order.RequestCancelled,
	}
	targets := all[1:7]
	for _, from := range all {
		for _, to := range targets {
			// mỗi ca một yêu cầu mới ở trạng thái from (chủ bảng đặt thẳng); giới hạn 5/24 giờ: lùi giờ tạo một ngày.
			r := e.mustRequest("W-1", exchange(v, 1, "FIT"), e.now())
			e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
				_, err := tx.Exec(t.Context(), `UPDATE ord.order_requests SET status = $2,
					created_at = created_at - interval '1 day',
					ship_back = CASE WHEN $2 IN ('confirmed','items_received') THEN '{}'::jsonb END WHERE id = $1`, r.ID, from)
				return err //nolint:wrapcheck // test
			})
			_, err := e.setStatus(r.ID, changeTo(to), 1)
			if order.CanTransitionRequest(from, to) {
				if err != nil {
					t.Errorf("%s → %s: %v", from, to, err)
				}
			} else if !errors.Is(err, order.ErrInvalidTransition) {
				t.Errorf("%s → %s: %v, mong ErrInvalidTransition", from, to, err)
			}
		}
	}
	e.webOrder("W-2", false, ln(v, 1, 0))
	c := e.mustRequest("W-2", cancelIn(), e.now())
	_, err := e.setStatus(c.ID, changeTo(order.RequestConfirmed), 1)
	wantErr(t, "xác nhận đổi cho yêu cầu hủy", err, order.ErrInvalidTransition)
}

// changeTo dựng lần đổi hợp lệ sang to (tin khi needs_info, hướng dẫn khi confirmed, lý do khi rejected).
func changeTo(to string) order.StatusChange {
	ch := order.StatusChange{Status: to}
	switch to {
	case order.RequestNeedsInfo:
		ch.Message = "Chị gửi thêm ảnh mặt sau giúp em"
	case order.RequestConfirmed:
		ch.ShipBack = &order.ShipBackInput{
			Address: "12 Lê Lợi, Quận 1, TP.HCM", PackageCode: "DOI-01",
			Deadline: time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC),
		}
	case order.RequestRejected:
		ch.Outcome = &order.Outcome{Reason: "Quá hạn đổi"}
	case order.RequestResolved:
		ch.Outcome = &order.Outcome{TrackingCode: "GHN123"}
	}
	return ch
}

// Luật của từng trạng thái: version cũ → 409; needs_info thiếu tin → ErrInvalidInput; rejected không lý do →
// ErrReasonRequired; lý do 501 ký tự, mã vận đơn 61 ký tự, hạn hôm qua, địa chỉ ngắn → ErrInvalidInput; hạn lưu
// 23:59:59 giờ VN; tin kèm thành tin hiện cho khách; resolved ghi người, giờ đóng.
func TestRequest_StatusRules(t *testing.T) {
	t.Parallel()
	e, _, r := newReqWithExchange(t)
	_, err := e.setStatus(r.ID, order.StatusChange{Status: order.RequestInReview}, 2)
	wantErr(t, "version cũ", err, order.ErrVersionConflict)
	_, err = e.setStatus(r.ID, order.StatusChange{Status: order.RequestNeedsInfo}, 1)
	wantErr(t, "needs_info thiếu tin", err, order.ErrInvalidInput)
	_, err = e.setStatus(r.ID, order.StatusChange{Status: order.RequestRejected}, 1)
	wantErr(t, "từ chối không lý do", err, order.ErrReasonRequired)
	today := e.now()
	for name, ch := range map[string]order.StatusChange{
		"lý do 501":  {Status: order.RequestRejected, Outcome: &order.Outcome{Reason: strings.Repeat("ạ", 501)}},
		"vận đơn 61": {Status: order.RequestResolved, Outcome: &order.Outcome{TrackingCode: strings.Repeat("A", 61)}},
		"hạn hôm qua": {Status: order.RequestConfirmed, ShipBack: &order.ShipBackInput{
			Address: "12 Lê Lợi, Quận 1", PackageCode: "DOI-01", Deadline: today.AddDate(0, 0, -1),
		}},
		"địa chỉ ngắn": {Status: order.RequestConfirmed, ShipBack: &order.ShipBackInput{
			Address: "Q1", PackageCode: "DOI-01", Deadline: today,
		}},
		"kết quả khi review": {Status: order.RequestInReview, Outcome: &order.Outcome{Reason: "x"}},
		"thiếu hướng dẫn":    {Status: order.RequestConfirmed},
	} {
		_, err = e.setStatus(r.ID, ch, 1)
		wantErr(t, name, err, order.ErrInvalidInput)
	}
	ch, err := e.setStatus(r.ID, order.StatusChange{
		Status: order.RequestConfirmed, Message: "Chị gửi hàng về giúp em",
		ShipBack: &order.ShipBackInput{Address: "12 Lê Lợi, Quận 1", PackageCode: "DOI-01", Deadline: today},
	}, 1)
	if err != nil || ch.Before.Status != order.RequestReceived || ch.After.Status != order.RequestConfirmed ||
		ch.After.Version != 2 {
		t.Fatalf("xác nhận: %+v, %v", ch, err)
	}
	d := e.get(r.ID)
	want := time.Date(2026, 11, 19, 23, 59, 59, 0, time.FixedZone("ICT", 7*3600)).UTC()
	if d.ShipBack == nil || !d.ShipBack.Deadline.Equal(want) || d.ShipBack.PackageCode != "DOI-01" ||
		len(d.Messages) != 1 || !d.Messages[0].VisibleToCustomer || d.Messages[0].AuthorType != order.AuthorStaff {
		t.Fatalf("sau xác nhận: %+v", d)
	}
	if _, err := e.setStatus(r.ID, order.StatusChange{Status: order.RequestItemsReceived}, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := e.setStatus(r.ID, changeTo(order.RequestResolved), 3); err != nil {
		t.Fatal(err)
	}
	d = e.get(r.ID)
	if d.Status != order.RequestResolved || d.Outcome == nil || d.Outcome.TrackingCode != "GHN123" ||
		d.ResolvedAt == nil || d.ResolvedBy == nil || *d.ResolvedBy != e.actor.UserID || d.ShipBack == nil {
		t.Fatalf("sau đóng: %+v", d)
	}
}

// Nhân viên trả lời visible → website thấy; ghi chú nội bộ → website không thấy [WB04]; tin hiện cho khách khi đã
// đóng → ErrInvalidTransition, ghi chú vẫn được. Khách trả lời khi needs_info → in_review; khi khác → không được. Khách
// rút khi received → cancelled (gửi lại như cũ); khi in_review → không được.
func TestRequest_MessagesAndCustomer(t *testing.T) {
	t.Parallel()
	e, v, r := newReqWithExchange(t)
	reply := func(id uuid.UUID, body string, visible bool) error {
		return e.app(func(tx pgx.Tx) error {
			_, err := e.w().ReplyRequest(t.Context(), tx, id, body, visible, e.actor, e.now())
			return err //nolint:wrapcheck // test
		})
	}
	if err := reply(r.ID, "Dạ shop đã nhận yêu cầu", true); err != nil {
		t.Fatal(err)
	}
	if err := reply(r.ID, "Khách quen, ưu tiên", false); err != nil {
		t.Fatal(err)
	}
	wantErr(t, "tin ngắn", reply(r.ID, "ok", true), order.ErrInvalidInput)
	var web []order.RequestDetail
	listWeb := func() {
		if err := e.app(func(tx pgx.Tx) error {
			var err error
			web, err = order.ListWebRequests(t.Context(), tx, e.seller, "W-1")
			return err //nolint:wrapcheck // test
		}); err != nil {
			t.Fatal(err)
		}
	}
	listWeb()
	if len(web) != 1 || len(web[0].Messages) != 1 || web[0].Messages[0].Body != "Dạ shop đã nhận yêu cầu" ||
		web[0].CustomerNote == "" {
		t.Fatalf("website thấy: %+v", web)
	}
	if d := e.get(r.ID); len(d.Messages) != 2 || d.Version != 1 {
		t.Fatalf("nhân viên thấy: %+v", d)
	}

	custReply := func(body string) (order.RequestChange, error) {
		return inApp(e, func(tx pgx.Tx) (order.RequestChange, error) {
			return e.w().CustomerReplyRequest(t.Context(), tx, e.seller, "W-1", r.Ref, body, e.key, e.now()) //nolint:lll // test
		})
	}
	_, err := custReply("Em gửi thêm thông tin")
	wantErr(t, "khách trả lời khi received", err, order.ErrRequestNotAllowed)
	if _, err := e.setStatus(r.ID, changeTo(order.RequestNeedsInfo), 1); err != nil {
		t.Fatal(err)
	}
	c, err := custReply("Em gửi thêm thông tin")
	if err != nil || c.After.Status != order.RequestInReview || c.After.Version != 3 {
		t.Fatalf("khách trả lời: %+v %v", c, err)
	}
	custCancel := func(ref string) (order.RequestChange, error) {
		return inApp(e, func(tx pgx.Tx) (order.RequestChange, error) {
			return e.w().CustomerCancelRequest(t.Context(), tx, e.seller, "W-1", ref, e.key, e.now())
		})
	}
	_, err = custCancel(r.Ref)
	wantErr(t, "rút khi in_review", err, order.ErrRequestNotAllowed)
	_, err = custCancel("YC-KHONGCO1")
	wantErr(t, "rút yêu cầu lạ", err, order.ErrRequestNotFound)

	r2 := e.mustRequest("W-1", exchange(v, 1, "FIT"), e.now())
	for range 2 {
		if c, err := custCancel(r2.Ref); err != nil || c.After.Status != order.RequestCancelled || c.After.Version != 2 {
			t.Fatalf("rút: %+v %v", c, err)
		}
	}
	wantErr(t, "tin cho khách khi đã đóng", reply(r2.ID, "Dạ shop nhận được rồi", true), order.ErrInvalidTransition)
	if err := reply(r2.ID, "Khách tự rút", false); err != nil {
		t.Fatal(err)
	}
}

// ord.order_request_messages chỉ ghi thêm: chủ bảng sửa, xóa → lỗi append-only; crm_app không có quyền sửa.
func TestRequest_MessagesAppendOnly(t *testing.T) {
	t.Parallel()
	e := newReqEnv(t)
	v := e.variant(200000, 10)
	e.webOrder("W-1", true, ln(v, 1, 0))
	r := e.mustRequest("W-1", exchange(v, 1, "FIT"), e.now())
	if err := e.app(func(tx pgx.Tx) error {
		_, err := e.w().ReplyRequest(t.Context(), tx, r.ID, "Dạ shop đã nhận", true, e.actor, e.now())
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	e.staff.CheckOwnerAppendOnly(t, e.tenant, `UPDATE ord.order_request_messages SET body = 'x'`,
		`DELETE FROM ord.order_request_messages`)
	if err := e.app(func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE ord.order_request_messages SET body = 'x'`)
		return err //nolint:wrapcheck // test
	}); err == nil {
		t.Fatal("crm_app sửa được tin")
	}
}

// Danh sách cho nhân viên: lọc trạng thái, loại, q (mã yêu cầu, mã đơn, tên khách không dấu), phân trang; người bán
// khác và tenant B không thấy, đọc yêu cầu của A → ErrRequestNotFound.
func TestRequest_ListAndTenant(t *testing.T) {
	t.Parallel()
	e := newReqEnv(t)
	v := e.variant(200000, 10)
	o := e.webOrder("W-1", true, ln(v, 5, 0))
	var made []order.Request
	for i := range 3 {
		made = append(made, e.mustRequest("W-1", exchange(v, 1, "FIT"), e.now().Add(time.Duration(i)*time.Minute)))
	}
	e.webOrder("W-2", false, ln(v, 1, 0))
	cr := e.mustRequest("W-2", cancelIn(), e.now().Add(time.Hour))
	list := func(f order.RequestFilter) ([]order.RequestSummary, *order.Cursor) {
		t.Helper()
		var out []order.RequestSummary
		var next *order.Cursor
		if err := e.app(func(tx pgx.Tx) error {
			var err error
			out, next, err = order.ListRequests(t.Context(), tx, e.seller, f)
			return err //nolint:wrapcheck // test
		}); err != nil {
			t.Fatal(err)
		}
		return out, next
	}
	all, _ := list(order.RequestFilter{})
	if len(all) != 4 || all[0].ID != cr.ID || all[3].ID != made[0].ID {
		t.Fatalf("mới trước: %+v", all)
	}
	page, next := list(order.RequestFilter{Limit: 3})
	rest, end := list(order.RequestFilter{Limit: 3, After: next})
	if len(page) != 3 || next == nil || len(rest) != 1 || rest[0].ID != made[0].ID || end != nil {
		t.Fatalf("phân trang: %d %v %d %v", len(page), next, len(rest), end)
	}
	for name, c := range map[string]struct {
		f    order.RequestFilter
		want int
	}{
		"loại hủy":       {order.RequestFilter{Kind: order.RequestCancel}, 1},
		"received":       {order.RequestFilter{Status: order.RequestReceived}, 4},
		"resolved":       {order.RequestFilter{Status: order.RequestResolved}, 0},
		"mã yêu cầu":     {order.RequestFilter{Q: strings.ToLower(made[1].Ref)}, 1},
		"mã đơn":         {order.RequestFilter{Q: strings.ToLower(o.Code)}, 3},
		"tên không dấu":  {order.RequestFilter{Q: "chi lan"}, 4},
		"tên không khớp": {order.RequestFilter{Q: "Trần Văn Bình"}, 0},
	} {
		if got, _ := list(c.f); len(got) != c.want {
			t.Errorf("%s: %d dòng, mong %d", name, len(got), c.want)
		}
	}
	err := e.app(func(tx pgx.Tx) error {
		_, _, err := order.ListRequests(t.Context(), tx, e.seller, order.RequestFilter{Status: "open"})
		return err //nolint:wrapcheck // test
	})
	wantErr(t, "trạng thái lạ", err, order.ErrInvalidInput)
	// Người bán khác (ADR-29): không thấy, không đọc được.
	err = e.app(func(tx pgx.Tx) error {
		if rows, _, err := order.ListRequests(t.Context(), tx, e.partner, order.RequestFilter{}); err != nil || len(rows) != 0 {
			return fmt.Errorf("người bán khác thấy %d dòng, %w", len(rows), err)
		}
		_, err := order.GetRequest(t.Context(), tx, e.partner, made[0].ID)
		return err //nolint:wrapcheck // test
	})
	wantErr(t, "người bán khác đọc", err, order.ErrRequestNotFound)

	b := e.staff.Tenant(t, "B")
	e.staff.Exec(t, b, func(tx pgx.Tx) error {
		if rows, _, err := order.ListRequests(t.Context(), tx, e.seller, order.RequestFilter{}); err != nil || len(rows) != 0 {
			return fmt.Errorf("tenant B thấy %d dòng, %w", len(rows), err)
		}
		if _, err := order.GetRequest(t.Context(), tx, e.seller, made[0].ID); !errors.Is(err, order.ErrRequestNotFound) {
			return fmt.Errorf("tenant B đọc yêu cầu của A: %w", err)
		}
		return nil
	})
}

// Cài đặt đơn có hạn đổi mặc định 7 ngày; đổi thành 3 thì đơn giao D, đổi D+4 → ErrWindowClosed.
func TestRequest_ExchangeWindowSetting(t *testing.T) {
	t.Parallel()
	e := newReqEnv(t)
	if s := e.settingsNow(); s.ExchangeWindowDays != 7 {
		t.Fatalf("mặc định: %d", s.ExchangeWindowDays)
	}
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE ord.order_settings SET exchange_window_days = 3`)
		return err //nolint:wrapcheck // test
	})
	v := e.variant(200000, 10)
	e.webOrder("W-1", true, ln(v, 1, 0))
	_, err := e.create("W-1", exchange(v, 1, "FIT"), e.now().Add(4*24*time.Hour))
	wantErr(t, "D+4 khi hạn 3", err, order.ErrWindowClosed)
}

func (e *reqEnv) settingsNow() order.Settings {
	e.t.Helper()
	var s order.Settings
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		s, err = order.LoadSettings(e.t.Context(), tx)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	return s
}

// Mục đích order_request_photo chế độ private (R10): đường tệp máy chủ files.Register → ErrPurposeMode (ảnh chỉ qua
// RegisterPrivateImage → file.process_private).
func TestRequestPhotoPurpose_PrivateOnly(t *testing.T) {
	t.Parallel()
	e := newReqEnv(t)
	if p := order.RequestPhotoPurpose(); !p.Private || !p.Image || p.Upload || p.Bucket != files.BucketPrivate {
		t.Fatalf("mục đích: %+v", p)
	}
	id := uuid.New()
	err := e.app(func(tx pgx.Tx) error {
		_, err := files.Register(t.Context(), tx, files.NewFile{
			ID: id, Purpose: order.RequestPhotoPurposeKey, Key: files.ServerKey(e.tenant, id), Size: 10,
			ContentType: "image/jpeg", FileName: "a.jpg",
		})
		return err //nolint:wrapcheck // test
	})
	wantErr(t, "files.Register", err, files.ErrPurposeMode)
}
