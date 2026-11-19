package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// ListCustomerTags (GET /api/v1/customer-tags, customer.read): mọi nhãn khách của shop, theo tên (Task M2-12).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListCustomerTags(ctx context.Context) (gen.ListCustomerTagsRes, error) {
	var tags []customer.Tag
	err := h.catalogRead(ctx, func(tx pgx.Tx) error {
		var err error
		tags, err = customer.ListTags(ctx, tx)
		return err //nolint:wrapcheck // lỗi của customer đã bọc
	})
	if err != nil {
		return nil, fmt.Errorf("đọc nhãn khách: %w", err)
	}
	out := &gen.CustomerTagList{Items: make([]gen.CustomerTag, 0, len(tags))}
	for _, t := range tags {
		out.Items = append(out.Items, tagOut(t))
	}
	return out, nil
}

// CreateCustomerTag (POST /api/v1/customer-tags, customer.tags.manage): tạo nhãn; tên trùng → 409 already_exists, màu
// ngoài bảng màu của database → 400 invalid_input. Trả nhãn, 201.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CreateCustomerTag(ctx context.Context, req *gen.CreateCustomerTagRequest, _ gen.CreateCustomerTagParams) (gen.CreateCustomerTagRes, error) {
	return h.tagWrite(ctx, http.StatusCreated, func(tx pgx.Tx, w customer.Writer, a customer.Actor, now time.Time) (customer.Tag, error) {
		return w.CreateTag(ctx, tx, req.Name, string(req.Color), a, now)
	})
}

// UpdateCustomerTag (PATCH /api/v1/customer-tags/{tagId}, customer.tags.manage): đổi tên, màu khi version còn khớp
// (409 version_conflict); tên trùng → 409 already_exists.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) UpdateCustomerTag(ctx context.Context, req *gen.UpdateCustomerTagRequest, params gen.UpdateCustomerTagParams) (gen.UpdateCustomerTagRes, error) {
	var name, color *string
	if n, ok := req.Name.Get(); ok {
		name = &n
	}
	if c, ok := req.Color.Get(); ok {
		v := string(c)
		color = &v
	}
	return h.tagWrite(ctx, http.StatusOK, func(tx pgx.Tx, w customer.Writer, a customer.Actor, now time.Time) (customer.Tag, error) {
		return w.UpdateTag(ctx, tx, params.TagId, req.Version, name, color, a, now)
	})
}

// AddCustomerTag (PUT /api/v1/customers/{id}/tags/{tagId}, customer.write): gắn nhãn tay vào gốc của khách; đã có thì
// không đổi gì (vẫn 200). Trả hồ sơ theo gốc.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) AddCustomerTag(ctx context.Context, params gen.AddCustomerTagParams) (gen.AddCustomerTagRes, error) {
	return h.customerWrite(ctx, http.StatusOK, func(tx pgx.Tx, w customer.Writer, a customer.Actor, now time.Time) (uuid.UUID, error) {
		return params.ID, w.AddTag(ctx, tx, params.ID, params.TagId, customer.TagSourceStaff, a, now)
	})
}

// RemoveCustomerTag (DELETE /api/v1/customers/{id}/tags/{tagId}, customer.write): gỡ nhãn gắn tay khỏi cả nhóm; nhãn
// chỉ do quy tắc gắn → 409 version_conflict, dòng giữ nguyên. Trả hồ sơ theo gốc.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) RemoveCustomerTag(ctx context.Context, params gen.RemoveCustomerTagParams) (gen.RemoveCustomerTagRes, error) {
	return h.customerWrite(ctx, http.StatusOK, func(tx pgx.Tx, w customer.Writer, a customer.Actor, _ time.Time) (uuid.UUID, error) {
		return params.ID, w.RemoveTag(ctx, tx, params.ID, params.TagId, a)
	})
}

// tagWrite chạy change trong refusableWrite với một customer.Writer của yêu cầu và trả nhãn. Lỗi nghiệp vụ của customer
// là câu trả lời không đổi gì (không lưu vào khóa chống trùng).
func (h *handler) tagWrite(ctx context.Context, status int,
	change func(tx pgx.Tx, w customer.Writer, a customer.Actor, now time.Time) (customer.Tag, error),
) (*gen.CustomerTag, error) {
	w := customer.Writer{Events: h.d.Events, Queue: queue.NewQueue(), Realtime: h.d.Realtime}
	out, err := h.refusableWrite(ctx, status, isCustomerRefusal, "ghi nhãn khách", func(tx pgx.Tx, u identity.AuthUser) (any, error) {
		a := customer.Actor{Type: customer.ActorStaff, ID: u.UserID, RequestID: RequestID(ctx)}
		t, err := change(tx, w, a, time.Now().UTC())
		if err != nil {
			return nil, err
		}
		v := tagOut(t)
		return &v, nil
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.CustomerTag), nil //nolint:forcetypeassert // change trên trả *gen.CustomerTag
}

func tagOut(t customer.Tag) gen.CustomerTag {
	return gen.CustomerTag{ID: t.ID, Name: t.Name, Color: t.Color, Version: t.Version}
}
