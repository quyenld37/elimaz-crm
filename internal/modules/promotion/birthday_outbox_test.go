package promotion_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/promotion"
)

func (e *env) outbox(now time.Time, q promotion.OutboxQuery) (promotion.OutboxPage, error) {
	var p promotion.OutboxPage
	err := e.app(func(tx pgx.Tx) error {
		var err error
		p, err = promotion.BirthdayOutbox(e.t.Context(), tx, now, q)
		return err //nolint:wrapcheck // test
	})
	return p, err
}

// outboxCodes đi hết các trang outbox (limit mỗi trang) lúc now và trả mã theo thứ tự gặp.
func (e *env) outboxCodes(now time.Time, limit int) []string {
	e.t.Helper()
	var codes []string
	q := promotion.OutboxQuery{Limit: limit}
	for range 20 {
		p, err := e.outbox(now, q)
		if err != nil {
			e.t.Fatal(err)
		}
		for _, g := range p.Items {
			codes = append(codes, g.Code)
		}
		if p.Next == "" {
			return codes
		}
		q.Cursor = p.Next
	}
	e.t.Fatal("outbox không hết trang")
	return nil
}

func (e *env) giftFailed(code string, now time.Time) (promotion.GiftSendFailure, error) {
	var f promotion.GiftSendFailure
	err := e.app(func(tx pgx.Tx) error {
		var err error
		f, err = promotion.MarkBirthdaySendFailed(e.t.Context(), tx, code, now)
		return err //nolint:wrapcheck // test
	})
	return f, err
}

// Outbox phân trang theo con trỏ (lúc phát, mã): đi hết trang ra đủ quà, không trùng; con trỏ sai → ErrInvalidInput.
// Quà gửi lỗi không chặn quà mới: lỗi lần 1, 2 → rời outbox 6 giờ rồi quay lại; lỗi lần 3 → không tự gửi nữa (vẫn ở
// "Cần nhắn"); trang đầu limit 1 là quà kế tiếp; báo lỗi thêm sau khi hết lượt → kết quả như lần 3, Changed false
// (Task 510: bên gọi không ghi audit). Quà đã trao: báo lỗi không đổi gì. Mã lạ, mã không phải quà sinh nhật
// → ErrNotFound.
func TestBirthdayOutbox_PagesAndFailedGiftDoesNotBlock(t *testing.T) {
	t.Parallel()
	e := newGiftEnv(t)
	d60 := 60 * 24 * time.Hour
	for _, phone := range []string{"0912000011", "0912000012", "0912000013"} {
		e.seedBirthday(&bdCustomer{phone: phone, day: 28, month: 11, savedAgo: d60, delivered: 1, zalo: true})
	}
	if r, err := e.runGifts(promotion.BirthdayTriggerManual, bdNow()); err != nil || r.Given != 3 {
		t.Fatalf("lượt phát: %+v %v", r, err)
	}
	all := e.outboxCodes(bdNow(), 100)
	if paged := e.outboxCodes(bdNow(), 1); len(all) != 3 || !slices.Equal(paged, all) {
		t.Fatalf("phân trang: %v / %v", paged, all)
	}
	if _, err := e.outbox(bdNow(), promotion.OutboxQuery{Cursor: "khong-phai-con-tro"}); !errors.Is(err,
		promotion.ErrInvalidInput) {
		t.Fatalf("con trỏ sai: %v", err)
	}
	head := all[0]
	at := e.failThreeTimes(all)
	if got := e.outboxCodes(at.Add(48*time.Hour), 100); !slices.Equal(got, all[1:]) {
		t.Fatalf("hết lượt: outbox %v", got)
	}
	todo := e.pendingGifts(false)
	if len(todo) != 3 {
		t.Fatalf("cần nhắn còn quà hết lượt: %d", len(todo))
	}
	if f, err := e.giftFailed(head, at); err != nil || f.Failures != 3 || f.RetryAt != nil || f.Changed || f.Given {
		t.Fatalf("báo lỗi sau khi hết lượt: %+v %v", f, err)
	}

	if err := e.app(func(tx pgx.Tx) error {
		_, err := promotion.MarkGiven(t.Context(), tx, all[1], bdNow())
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if f, err := e.giftFailed(all[1], bdNow()); err != nil || f.Failures != 0 || f.RetryAt != nil || !f.Given || f.Changed {
		t.Fatalf("quà đã trao: %+v %v", f, err)
	}
	if _, err := e.giftFailed("KHONGCO-1", bdNow()); !errors.Is(err, promotion.ErrNotFound) {
		t.Fatalf("mã lạ: %v", err)
	}
	if _, err := e.create(sale10("THUONG1")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.giftFailed("THUONG1", bdNow()); !errors.Is(err, promotion.ErrNotFound) {
		t.Fatalf("mã không phải quà sinh nhật: %v", err)
	}
}

// [WB05] Mã nhập gắn SĐT (boundPhone, chuẩn hóa +84…): áp được cho đơn của SĐT đó, SĐT khác → bound_other; nhập lại
// cùng SĐT → skipped; cùng mã mà SĐT khác hay bỏ SĐT → conflicts, mã có sẵn giữ SĐT cũ; SĐT sai → ErrInvalidPhone
// của customer, không nhập mã nào.
func TestImportCoupons_BoundPhone(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	now := time.Now()
	item := promotion.ImportItem{
		Code: "SNSDT-1", DiscountKind: promotion.KindFixed, Value: 50000, ValidFrom: now.Add(-time.Hour),
		ValidTo: now.Add(24 * time.Hour), BoundPhone: "0912 000 777",
	}
	free := item
	free.Code, free.BoundPhone = "SNSDT-2", ""
	r, err := e.importCoupons([]promotion.ImportItem{item, free})
	if err != nil || r.Inserted != 2 {
		t.Fatalf("nhập: %+v %v", r, err)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM promotion.coupons WHERE code = 'SNSDT-1'
		AND bound_phone = '+84912000777'`); n != 1 {
		t.Fatal("mã gắn SĐT")
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM promotion.coupons WHERE code = 'SNSDT-2'
		AND bound_phone IS NULL`); n != 1 {
		t.Fatal("mã không gắn SĐT")
	}
	in := e.input(e.customer(), 400000)
	in.Channel, in.Phone = "web", "+84912000777"
	if c := e.check("SNSDT-1", in); !c.OK {
		t.Fatalf("đúng SĐT: %+v", c)
	}
	in.Phone = "0912000778"
	if c := e.check("SNSDT-1", in); c.OK || c.Reason != promotion.ReasonBoundOther {
		t.Fatalf("SĐT khác: %+v", c)
	}
	again := item
	again.BoundPhone = "+84912000777"
	if r, err := e.importCoupons([]promotion.ImportItem{again}); err != nil || r.Skipped != 1 {
		t.Fatalf("nhập lại cùng SĐT: %+v %v", r, err)
	}
	other, none := item, free
	other.BoundPhone, none.Code = "0912000778", "SNSDT-1"
	other.Code = "SNSDT-2"
	if r, err := e.importCoupons([]promotion.ImportItem{none, other}); err != nil ||
		!slices.Equal(r.Conflicts, []string{"SNSDT-1", "SNSDT-2"}) {
		t.Fatalf("SĐT khác: %+v %v", r, err)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM promotion.coupons WHERE code = 'SNSDT-1'
		AND bound_phone = '+84912000777'`); n != 1 {
		t.Fatal("mã có sẵn bị đổi SĐT")
	}
	bad := item
	bad.Code, bad.BoundPhone = "SNSDT-3", "0012345678"
	if _, err := e.importCoupons([]promotion.ImportItem{bad}); !errors.Is(err, customer.ErrInvalidPhone) {
		t.Fatalf("SĐT sai: %v", err)
	}
}

// failThreeTimes báo quà all[0] lỗi ba lần (mỗi lần sau khi hết giờ chờ): lần 1, 2 → lúc thử lại now + 6 giờ, quà
// quay lại sau đó; lần 3 → hết lượt; sau mỗi lần trang đầu limit 1 là all[1]. Trả lúc của lần báo cuối.
func (e *env) failThreeTimes(all []string) time.Time {
	t := e.t
	t.Helper()
	head, at := all[0], bdNow()
	for i := 1; i <= 3; i++ {
		f, err := e.giftFailed(head, at)
		if err != nil || f.Failures != i || f.Code != head || !f.Changed {
			t.Fatalf("lỗi lần %d: %+v %v", i, f, err)
		}
		if p, _ := e.outbox(at, promotion.OutboxQuery{Limit: 1}); len(p.Items) != 1 || p.Items[0].Code != all[1] {
			t.Fatalf("lỗi lần %d: trang đầu %+v", i, p.Items)
		}
		if i == 3 {
			if f.RetryAt != nil {
				t.Fatalf("hết lượt mà còn lúc thử lại: %v", f.RetryAt)
			}
			return at
		}
		if f.RetryAt == nil || !f.RetryAt.Equal(at.Add(6*time.Hour)) {
			t.Fatalf("lỗi lần %d: lúc thử lại %v", i, f.RetryAt)
		}
		at = at.Add(6*time.Hour + time.Second)
		if got := e.outboxCodes(at, 100); !slices.Equal(got, all) {
			t.Fatalf("lỗi lần %d: hết giờ chờ, outbox %v", i, got)
		}
	}
	return at
}

// Task 521: khách có cờ "không liên hệ" (ở gốc) không có quà trong "Cần nhắn" lẫn outbox của website (gửi quà là liên
// hệ); tắt cờ thì quà quay lại, khách khác không bị ảnh hưởng.
func TestBirthdayGifts_SkipDoNotContact(t *testing.T) {
	t.Parallel()
	e := newGiftEnv(t)
	d60 := 60 * 24 * time.Hour
	var cs []*bdCustomer
	for _, phone := range []string{"0912000021", "0912000022"} {
		c := &bdCustomer{phone: phone, day: 28, month: 11, savedAgo: d60, delivered: 1, zalo: true}
		e.seedBirthday(c)
		cs = append(cs, c)
	}
	a := cs[0]
	if r, err := e.runGifts(promotion.BirthdayTriggerManual, bdNow()); err != nil || r.Given != 2 {
		t.Fatalf("lượt phát: %+v %v", r, err)
	}
	setFlag := func(on bool) {
		t.Helper()
		if err := e.app(func(tx pgx.Tx) error {
			return e.customerWriter().SetDoNotContact(t.Context(), tx, a.customerID, on,
				customer.Actor{Type: customer.ActorStaff, ID: e.user}, bdNow())
		}); err != nil {
			t.Fatal(err)
		}
	}
	if len(e.pendingGifts(false)) != 2 || len(e.outboxCodes(bdNow(), 100)) != 2 {
		t.Fatal("trước khi bật cờ phải thấy 2 quà")
	}
	setFlag(true)
	todo := e.pendingGifts(false)
	if _, ok := todo[a.customerID]; ok || len(todo) != 1 || len(e.outboxCodes(bdNow(), 100)) != 1 {
		t.Fatalf("bật cờ: Cần nhắn %d, outbox %d, muốn 1 và 1", len(todo), len(e.outboxCodes(bdNow(), 100)))
	}
	setFlag(false)
	if len(e.pendingGifts(false)) != 2 || len(e.outboxCodes(bdNow(), 100)) != 2 {
		t.Fatal("tắt cờ phải thấy lại 2 quà")
	}
}
