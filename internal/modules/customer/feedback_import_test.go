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

func (e *writeEnv) importFeedback(tenant uuid.UUID, in customer.ImportedFeedback) (bool, error) {
	var created bool
	err := e.try(tenant, func(tx pgx.Tx) error {
		var err error
		created, err = e.w.ImportFeedback(e.t.Context(), tx, in, customer.Actor{Type: customer.ActorSystem}, e.now)
		return err //nolint:wrapcheck // test
	})
	return created, err
}

// Nhập góp ý cũ của website (Task 467): giữ id, giờ tạo/sửa, trạng thái, ghi chú; liên lạc là SĐT của khách có sẵn →
// gắn khách như SubmitFeedback; nhập lại cùng id → không thêm; trạng thái lạ, ghi chú quá dài → ErrInvalidInput; id đã
// có ở tenant khác → ErrInvalidInput, tenant khác không đổi. Audit customer.feedback.import không nội dung, SĐT.
func TestImportFeedback(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	a := e.create(customer.CreateInput{FullName: "Chị Lan", Phone: "0912000111"})
	at := time.Date(2026, 9, 3, 7, 30, 0, 0, time.UTC)
	in := customer.ImportedFeedback{
		ID: uuid.New(), State: customer.FeedbackAnswered, Note: "đã gọi lại ghi-chú-kín", CreatedAt: at,
		UpdatedAt: at.Add(time.Hour), Input: customer.FeedbackInput{
			Topic: "product", Body: "Áo đẹp nội-dung-kín", Contact: "0912 000 111", Name: "Lan", Mood: new(4),
		},
	}
	if ok, err := e.importFeedback(e.tenant, in); err != nil || !ok {
		t.Fatalf("nhập lần đầu: %v %v", ok, err)
	}
	got, _, err := e.listFeedback(e.tenant, customer.FeedbackFilter{})
	if err != nil || len(got) != 1 {
		t.Fatalf("đọc lại: %d %v", len(got), err)
	}
	f := got[0]
	if f.ID != in.ID || f.State != customer.FeedbackAnswered || string(f.Note) != in.Note || !f.CreatedAt.Equal(at) ||
		!f.UpdatedAt.Equal(in.UpdatedAt) || f.CustomerID == nil || *f.CustomerID != a.ID || f.Mood == nil || *f.Mood != 4 {
		t.Fatalf("góp ý đã nhập = %+v", f)
	}
	if ok, err := e.importFeedback(e.tenant, in); err != nil || ok {
		t.Fatalf("nhập lại: %v %v", ok, err)
	}
	for name, bad := range map[string]customer.ImportedFeedback{
		"trạng thái lạ": {ID: uuid.New(), State: "done", CreatedAt: at, UpdatedAt: at, Input: in.Input},
		"ghi chú dài":   {ID: uuid.New(), State: "new", Note: strings.Repeat("a", 1001), CreatedAt: at, UpdatedAt: at, Input: in.Input},
		"thiếu giờ":     {ID: uuid.New(), State: "new", Input: in.Input},
		"thiếu id":      {State: "new", CreatedAt: at, UpdatedAt: at, Input: in.Input},
	} {
		if _, err := e.importFeedback(e.tenant, bad); !errors.Is(err, customer.ErrInvalidInput) {
			t.Errorf("%s: lỗi %v", name, err)
		}
	}
	if _, err := e.importFeedback(e.other, in); !errors.Is(err, customer.ErrInvalidInput) {
		t.Errorf("id của tenant khác: lỗi %v", err)
	}
	if n := e.count(`SELECT count(*) FROM customer.feedback`); n != 1 {
		t.Fatalf("số góp ý = %d", n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'customer.feedback.import' AND actor_type = 'system'`); n != 1 {
		t.Fatalf("audit nhập = %d", n)
	}
	audit := e.auditText()
	for _, bad := range []string{"nội-dung-kín", "ghi-chú-kín", "912000111", "912 000 111"} {
		if strings.Contains(audit, bad) {
			t.Errorf("audit chứa %q", bad)
		}
	}
}
