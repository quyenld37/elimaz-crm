package customer_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
)

// submitFeedback gửi góp ý trong tenant A bằng khóa dịch vụ (người làm của website).
func (e *writeEnv) submitFeedback(in customer.FeedbackInput) (uuid.UUID, error) {
	var id uuid.UUID
	err := e.try(e.tenant, func(tx pgx.Tx) error {
		var err error
		id, err = e.w.SubmitFeedback(e.t.Context(), tx, in, customer.Actor{Type: customer.ActorService, ID: uuid.New()}, e.now)
		return err //nolint:wrapcheck // test
	})
	return id, err
}

func (e *writeEnv) mustFeedback(in customer.FeedbackInput) uuid.UUID {
	e.t.Helper()
	id, err := e.submitFeedback(in)
	if err != nil {
		e.t.Fatalf("gửi góp ý: %v", err)
	}
	return id
}

func (e *writeEnv) listFeedback(tenant uuid.UUID, f customer.FeedbackFilter) ([]customer.Feedback, *customer.ListCursor, error) {
	var (
		out  []customer.Feedback
		next *customer.ListCursor
	)
	err := e.try(tenant, func(tx pgx.Tx) error {
		var err error
		out, next, err = customer.ListFeedback(e.t.Context(), tx, f)
		return err //nolint:wrapcheck // test
	})
	return out, next, err
}

func (e *writeEnv) updateFeedback(tenant, id uuid.UUID, version int, state, note *string) (customer.Feedback, error) {
	var out customer.Feedback
	err := e.try(tenant, func(tx pgx.Tx) error {
		var err error
		out, err = e.w.UpdateFeedback(e.t.Context(), tx, id, version, state, note, e.actor, e.now.Add(time.Hour))
		return err //nolint:wrapcheck // test
	})
	return out, err
}

// feedbackCustomer đọc customer_id đã lưu của góp ý id (uuid.Nil khi không gắn).
func (e *writeEnv) feedbackCustomer(id uuid.UUID) uuid.UUID {
	e.t.Helper()
	var c *uuid.UUID
	e.inTx(func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT customer_id FROM customer.feedback WHERE id = $1`, id).Scan(&c)
	})
	if c == nil {
		return uuid.Nil
	}
	return *c
}

// expectFeedbackRoots đòi danh sách góp ý lọc theo khách by có đúng các góp ý trong want, mỗi góp ý kèm gốc hiện tại.
func (e *writeEnv) expectFeedbackRoots(by uuid.UUID, want map[uuid.UUID]uuid.UUID) {
	e.t.Helper()
	got, _, err := e.listFeedback(e.tenant, customer.FeedbackFilter{CustomerID: &by})
	if err != nil || len(got) != len(want) {
		e.t.Fatalf("lọc theo khách %s: %d góp ý, mong %d (%v)", by, len(got), len(want), err)
	}
	for _, f := range got {
		if root, ok := want[f.ID]; !ok || f.CustomerID == nil || *f.CustomerID != root {
			e.t.Errorf("lọc theo khách %s: góp ý %s gốc %v, mong %s", by, f.ID, f.CustomerID, root)
		}
	}
}

// auditText trả mọi dòng audit của tenant A dạng JSON.
func (e *writeEnv) auditText() string {
	e.t.Helper()
	var rows *string
	e.inTx(func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT string_agg(row_to_json(a)::text, ' ') FROM identity.audit_logs a`).Scan(&rows)
	})
	if rows == nil {
		return ""
	}
	return *rows
}

// Liên lạc là SĐT (viết kiểu nào cũng được) của khách có sẵn → gắn hồ sơ giữ SĐT; đọc trả gốc hiện tại (SĐT của hồ sơ
// đã gộp → gốc của nhóm), hoàn tác gộp → góp ý theo hồ sơ con về nhóm riêng; SĐT lạ → không gắn, không tạo khách, không
// thêm danh tính; email → không gắn. Audit customer.feedback.submit chỉ có tên trường, không nội dung, tên, SĐT.
func TestSubmitFeedback_LinksByPhone(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	a := e.create(customer.CreateInput{FullName: "Chị Lan", Phone: "0912000111"})
	b := e.create(customer.CreateInput{FullName: "Lan Nguyễn", Phone: "0987000222"})
	m := e.mustMerge(a.ID, b.ID)
	customers := e.count(`SELECT count(*) FROM customer.customers`)
	identities := e.count(`SELECT count(*) FROM customer.customer_identities`)

	byA := e.mustFeedback(customer.FeedbackInput{
		Topic: "product", Body: "Áo đẹp lắm", Contact: "+84 912 000 111",
		Name: "Lan", Mood: new(5),
	})
	byB := e.mustFeedback(customer.FeedbackInput{
		Topic: "delivery", Body: "Giao chậm nội dung-bí-mật", Contact: "0987000222",
		OrderRef: "W123", Page: "/gop-y", Device: "iPhone",
	})
	stranger := e.mustFeedback(customer.FeedbackInput{Topic: "other", Body: "Khách lạ", Contact: "0909555666"})
	mail := e.mustFeedback(customer.FeedbackInput{Topic: "website", Body: "Có email", Contact: "lan@example.com"})
	none := e.mustFeedback(customer.FeedbackInput{Topic: "service", Body: "Không để liên lạc"})

	for _, c := range []struct {
		name string
		id   uuid.UUID
		want uuid.UUID
	}{
		{"SĐT của gốc", byA, a.ID},
		{"SĐT của hồ sơ đã gộp", byB, b.ID},
		{"SĐT lạ", stranger, uuid.Nil},
		{"email", mail, uuid.Nil},
		{"không liên lạc", none, uuid.Nil},
	} {
		if got := e.feedbackCustomer(c.id); got != c.want {
			t.Errorf("%s: customer_id %s, mong %s", c.name, got, c.want)
		}
	}
	if n := e.count(`SELECT count(*) FROM customer.customers`); n != customers {
		t.Errorf("số khách đổi: %d → %d", customers, n)
	}
	e.expectFeedbackRoots(b.ID, map[uuid.UUID]uuid.UUID{byA: a.ID, byB: a.ID})
	if err := e.undo(m.ID, e.now); err != nil {
		t.Fatal(err)
	}
	e.expectFeedbackRoots(a.ID, map[uuid.UUID]uuid.UUID{byA: a.ID})
	e.expectFeedbackRoots(b.ID, map[uuid.UUID]uuid.UUID{byB: b.ID})
	if n := e.count(`SELECT count(*) FROM customer.customer_identities`); n != identities {
		t.Errorf("số danh tính đổi: %d → %d", identities, n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'customer.feedback.submit'
		AND entity_type = 'customer.feedback' AND actor_type = 'service'`); n != 5 {
		t.Errorf("audit gửi góp ý: %d", n)
	}
	audit := e.auditText()
	for _, bad := range []string{"bí-mật", "Áo đẹp", "912000111", "987000222", "909555666", "lan@example", "iPhone", "W123"} {
		if strings.Contains(audit, bad) {
			t.Errorf("audit chứa %q", bad)
		}
	}
}

// Kiểm trường: nội dung 2000 ký tự được, 2001 → ErrInvalidInput; nội dung rỗng, chủ đề lạ, điểm ngoài 1–5, tên 81,
// liên lạc 121, trang 301, thiết bị 121, mã đơn 61 ký tự → ErrInvalidInput; không lưu gì.
func TestSubmitFeedback_Validation(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	ok := customer.FeedbackInput{Topic: "product", Body: strings.Repeat("ả", 2000)}
	e.mustFeedback(ok)
	bad := map[string]customer.FeedbackInput{
		"nội dung 2001":     {Topic: "product", Body: strings.Repeat("ả", 2001)},
		"nội dung rỗng":     {Topic: "product", Body: "   "},
		"chủ đề lạ":         {Topic: "price", Body: "x"},
		"điểm 0":            {Topic: "product", Body: "x", Mood: new(0)},
		"điểm 6":            {Topic: "product", Body: "x", Mood: new(6)},
		"tên 81":            {Topic: "product", Body: "x", Name: strings.Repeat("a", 81)},
		"liên lạc 121":      {Topic: "product", Body: "x", Contact: strings.Repeat("1", 121)},
		"trang 301":         {Topic: "product", Body: "x", Page: strings.Repeat("p", 301)},
		"thiết bị 121":      {Topic: "product", Body: "x", Device: strings.Repeat("d", 121)},
		"mã đơn 61":         {Topic: "product", Body: "x", OrderRef: strings.Repeat("9", 61)},
		"băm 129":           {Topic: "product", Body: "x", ClientHash: strings.Repeat("h", 129)},
		"tên có điều khiển": {Topic: "product", Body: "x", Name: "a\x00b"},
	}
	for name, in := range bad {
		if _, err := e.submitFeedback(in); !errors.Is(err, customer.ErrInvalidInput) {
			t.Errorf("%s: %v, mong ErrInvalidInput", name, err)
		}
	}
	if n := e.count(`SELECT count(*) FROM customer.feedback`); n != 1 {
		t.Errorf("số góp ý: %d", n)
	}
}

// Sửa: đổi trạng thái rồi ghi chú (mỗi lần version + 1, người sửa là nhân viên); version cũ → ErrVersionConflict;
// góp ý lạ, của tenant khác → ErrNotFound; không trường nào, trạng thái lạ, ghi chú 1001 → ErrInvalidInput; ghi chú ""
// → xóa ghi chú. Audit customer.feedback.update không chứa ghi chú.
func TestUpdateFeedback_VersionAndAudit(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	id := e.mustFeedback(customer.FeedbackInput{Topic: "product", Body: "Hay"})
	f, err := e.updateFeedback(e.tenant, id, 1, new(customer.FeedbackSeen), nil)
	if err != nil || f.State != customer.FeedbackSeen || f.Version != 2 || f.Note != "" || f.UpdatedBy == nil ||
		*f.UpdatedBy != e.actor.ID || !f.UpdatedAt.Equal(e.now.Add(time.Hour)) || f.Body != "Hay" {
		t.Fatalf("đổi trạng thái: %+v %v", f, err)
	}
	f, err = e.updateFeedback(e.tenant, id, 2, nil, new("Đã gọi lại khách ghi-chu-rieng"))
	if err != nil || f.State != customer.FeedbackSeen || f.Version != 3 || f.Note != "Đã gọi lại khách ghi-chu-rieng" {
		t.Fatalf("ghi chú: %+v %v", f, err)
	}
	if _, err := e.updateFeedback(e.tenant, id, 2, new(customer.FeedbackAnswered), nil); !errors.Is(err, customer.ErrVersionConflict) {
		t.Errorf("version cũ: %v", err)
	}
	if _, err := e.updateFeedback(e.tenant, uuid.New(), 1, new(customer.FeedbackSeen), nil); !errors.Is(err, customer.ErrNotFound) {
		t.Errorf("góp ý lạ: %v", err)
	}
	if _, err := e.updateFeedback(e.other, id, 3, new(customer.FeedbackSeen), nil); !errors.Is(err, customer.ErrNotFound) {
		t.Errorf("tenant khác: %v", err)
	}
	for name, c := range map[string][2]*string{
		"không trường nào": {nil, nil}, "trạng thái lạ": {new("done"), nil},
		"ghi chú 1001": {nil, new(strings.Repeat("g", 1001))},
	} {
		if _, err := e.updateFeedback(e.tenant, id, 3, c[0], c[1]); !errors.Is(err, customer.ErrInvalidInput) {
			t.Errorf("%s: %v", name, err)
		}
	}
	f, err = e.updateFeedback(e.tenant, id, 3, new(customer.FeedbackAnswered), new(""))
	if err != nil || f.State != customer.FeedbackAnswered || f.Note != "" || f.Version != 4 {
		t.Fatalf("xóa ghi chú: %+v %v", f, err)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'customer.feedback.update'
		AND entity_id = $1 AND actor_type = 'staff'`, id.String()); n != 3 {
		t.Errorf("audit sửa góp ý: %d", n)
	}
	if strings.Contains(e.auditText(), "ghi-chu-rieng") {
		t.Error("audit chứa ghi chú")
	}
}

// Danh sách: mới nhất trước, trang theo con trỏ; lọc trạng thái, chủ đề; lọc theo khách (gốc hay hồ sơ con) trả mọi góp
// ý gắn với nhóm, kèm gốc hiện tại và tên gốc; khách lạ → ErrNotFound; tenant khác không thấy gì.
func TestListFeedback_FiltersAndPaging(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	a := e.create(customer.CreateInput{FullName: "Chị Hoa", Phone: "0912333444"})
	b := e.create(customer.CreateInput{FullName: "Hoa", Phone: "0912333555"})
	fb := e.mustFeedback(customer.FeedbackInput{Topic: "delivery", Body: "1", Contact: "0912333555"})
	e.now = e.now.Add(time.Minute)
	fa := e.mustFeedback(customer.FeedbackInput{Topic: "product", Body: "2", Contact: "0912333444"})
	e.now = e.now.Add(time.Minute)
	free := e.mustFeedback(customer.FeedbackInput{Topic: "product", Body: "3"})
	e.mustMerge(a.ID, b.ID)
	if _, err := e.updateFeedback(e.tenant, free, 1, new(customer.FeedbackAnswered), nil); err != nil {
		t.Fatal(err)
	}

	ids := func(fs []customer.Feedback) []uuid.UUID {
		out := make([]uuid.UUID, 0, len(fs))
		for _, f := range fs {
			out = append(out, f.ID)
		}
		return out
	}
	page1, next, err := e.listFeedback(e.tenant, customer.FeedbackFilter{Limit: 2})
	if err != nil || len(page1) != 2 || page1[0].ID != free || page1[1].ID != fa || next == nil {
		t.Fatalf("trang 1: %v %v %v", ids(page1), next, err)
	}
	page2, next2, err := e.listFeedback(e.tenant, customer.FeedbackFilter{Limit: 2, After: next})
	if err != nil || len(page2) != 1 || page2[0].ID != fb || next2 != nil {
		t.Fatalf("trang 2: %v %v %v", ids(page2), next2, err)
	}
	if f := page2[0]; f.CustomerID == nil || *f.CustomerID != a.ID || f.CustomerName != "Chị Hoa" || f.Topic != "delivery" ||
		f.State != customer.FeedbackNew || f.Version != 1 || f.Contact != "0912333555" {
		t.Errorf("góp ý của hồ sơ đã gộp: %+v", f)
	}
	for _, by := range []uuid.UUID{a.ID, b.ID} {
		got, _, err := e.listFeedback(e.tenant, customer.FeedbackFilter{CustomerID: &by})
		if err != nil || len(got) != 2 || got[0].ID != fa || got[1].ID != fb {
			t.Errorf("lọc theo khách %s: %v %v", by, ids(got), err)
		}
	}
	got, _, err := e.listFeedback(e.tenant, customer.FeedbackFilter{State: customer.FeedbackNew, Topic: "product"})
	if err != nil || len(got) != 1 || got[0].ID != fa {
		t.Errorf("lọc trạng thái, chủ đề: %v %v", ids(got), err)
	}
	if _, _, err := e.listFeedback(e.tenant, customer.FeedbackFilter{CustomerID: new(uuid.New())}); !errors.Is(err,
		customer.ErrNotFound) {
		t.Errorf("khách lạ: %v", err)
	}
	if _, _, err := e.listFeedback(e.tenant, customer.FeedbackFilter{State: "done"}); !errors.Is(err, customer.ErrInvalidInput) {
		t.Errorf("trạng thái lạ: %v", err)
	}
	if got, _, err := e.listFeedback(e.other, customer.FeedbackFilter{}); err != nil || len(got) != 0 {
		t.Errorf("tenant khác: %v %v", ids(got), err)
	}
}
