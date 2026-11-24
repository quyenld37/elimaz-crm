package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Yêu cầu về đơn hàng — phía website (kế hoạch M5, Task 195; x-auth: service_key). Người làm là khóa dịch vụ; audit ghi
// ở đây với ActorType service, cùng tx. Lời nhắn, nội dung tin không vào audit.

// WebUploadRequestFile (POST /api/v1/web/files, web.requests.write, x-body-limit 6 MiB): kiểm loại, cỡ theo mục đích
// order_request_photo; Put ảnh thô lên crm-tmp (files.TmpKey) **ngoài** transaction (lỗi → 503 retry_later, không
// lưu); rồi một transaction: files.RegisterPrivateImage (dòng uploaded + lệnh file.process_private của M1) và audit.
// Không tự kiểm nội dung (R10: việc của imager trong file.process_private).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) WebUploadRequestFile(ctx context.Context, req *gen.WebUploadRequestFileReqWithContentType,
	_ gen.WebUploadRequestFileParams,
) (gen.WebUploadRequestFileRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	p, ok := files.PurposeOf(order.RequestPhotoPurposeKey)
	if !ok {
		return nil, fmt.Errorf("ảnh kèm yêu cầu: %w", files.ErrUnknownPurpose)
	}
	ct, _, err := mime.ParseMediaType(req.ContentType)
	if err != nil || !slices.Contains(p.AllowedTypes, ct) {
		return nil, fmt.Errorf("ảnh kèm yêu cầu: %w", files.ErrTypeNotAllowed)
	}
	data, err := io.ReadAll(io.LimitReader(req.Content.Data, p.MaxBytes+1))
	switch {
	case err != nil:
		return nil, fmt.Errorf("đọc ảnh kèm yêu cầu: %w", err)
	case int64(len(data)) > p.MaxBytes:
		return nil, fmt.Errorf("ảnh kèm yêu cầu: %w", files.ErrTooLarge)
	case len(data) == 0:
		return nil, fmt.Errorf("ảnh kèm yêu cầu: %w: rỗng", files.ErrInvalidFile)
	case h.d.Files == nil:
		return nil, errNoFileStore
	}
	id := uuid.Must(uuid.NewV7())
	size := int64(len(data))
	if err := h.d.Files.Put(ctx, files.BucketTmp, files.TmpKey(u.TenantID, id), bytes.NewReader(data), size, ct); err != nil {
		h.d.Logger.LogAttrs(ctx, slog.LevelWarn, "không ghi được ảnh kèm yêu cầu lên kho tạm",
			slog.String("request_id", RequestID(ctx)), logging.Err(err))
		return nil, fmt.Errorf("ghi ảnh kèm yêu cầu lên kho tạm: %w", db.ErrRetryLater)
	}
	out, err := h.refusableWrite(ctx, http.StatusCreated, isUploadRefusal, "ghi ảnh kèm yêu cầu",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			fid, err := files.RegisterPrivateImage(ctx, tx, queue.NewQueue(), files.NewPrivateImage{
				ID: id, Purpose: order.RequestPhotoPurposeKey, Size: size, ContentType: ct, FileName: "request-photo",
			})
			if err != nil {
				return nil, err //nolint:wrapcheck // files đã bọc
			}
			return &gen.WebRequestFile{FileId: fid}, auditService(ctx, tx, u, "order.request_photo.upload", "file", fid,
				nil, map[string]any{"purpose": order.RequestPhotoPurposeKey, "contentType": ct, "size": size})
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.WebRequestFile), nil //nolint:forcetypeassert // fn trên trả *gen.WebRequestFile
}

func isUploadRefusal(err error) bool {
	return errors.Is(err, files.ErrTypeNotAllowed) || errors.Is(err, files.ErrTooLarge) ||
		errors.Is(err, files.ErrInvalidFile)
}

// WebCreateRequest (POST /api/v1/web/orders/{externalRef}/requests, web.requests.write): yêu cầu của khách cho đơn
// web; audit order.request.create (không lời nhắn). Từ chối nghiệp vụ trả lời, không lưu theo khóa.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) WebCreateRequest(ctx context.Context, req *gen.WebCreateRequestBody, params gen.WebCreateRequestParams,
) (gen.WebCreateRequestRes, error) {
	in := order.CreateRequestInput{
		Kind: string(req.Kind), ReasonCode: string(req.Reason), Note: req.Message, FileIDs: req.FileIds,
		ConditionConfirmed: req.ConditionConfirmed.Or(false),
	}
	for _, l := range req.Lines {
		rl := order.RequestLine{VariantID: l.VariantId, Qty: int(l.Qty), Reason: string(l.Reason), Note: l.Note.Or("")}
		if w, ok := l.WantVariantId.Get(); ok {
			rl.WantVariantID = &w
		}
		in.Lines = append(in.Lines, rl)
	}
	out, err := h.webRequestWrite(ctx, http.StatusCreated, "gửi yêu cầu về đơn",
		func(tx pgx.Tx, u identity.AuthUser, seller uuid.UUID, w order.Writer) (any, error) {
			r, err := w.CreateRequestFromWeb(ctx, tx, seller, params.ExternalRef, in, webActor(u), h.now())
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của order
			}
			after := r.AuditFields()
			after["changed_fields"], after["files"] = []string{"customer_note"}, len(in.FileIDs)
			return &gen.WebRequestRef{Ref: r.Ref, Status: gen.OrderRequestStatus(r.Status)},
				auditService(ctx, tx, u, "order.request.create", "order_request", r.ID, nil, after)
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.WebRequestRef), nil //nolint:forcetypeassert // fn trên trả *gen.WebRequestRef
}

// WebReplyRequest (POST …/requests/{ref}/messages, web.requests.write): khách trả lời khi needs_info → in_review.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) WebReplyRequest(ctx context.Context, req *gen.WebReplyRequestBody, params gen.WebReplyRequestParams,
) (gen.WebReplyRequestRes, error) {
	return h.webRequestChange(ctx, "order.request.customer_reply", []string{"status", "body"},
		func(tx pgx.Tx, seller uuid.UUID, w order.Writer, a order.Actor) (order.RequestChange, error) {
			return w.CustomerReplyRequest(ctx, tx, seller, params.ExternalRef, params.Ref, req.Message, a, h.now()) //nolint:lll // sentinel
		})
}

// WebCancelRequest (POST …/requests/{ref}/cancel, web.requests.write): khách rút yêu cầu còn received.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) WebCancelRequest(ctx context.Context, params gen.WebCancelRequestParams) (gen.WebCancelRequestRes, error) {
	return h.webRequestChange(ctx, "order.request.customer_cancel", []string{"status"},
		func(tx pgx.Tx, seller uuid.UUID, w order.Writer, a order.Actor) (order.RequestChange, error) {
			return w.CustomerCancelRequest(ctx, tx, seller, params.ExternalRef, params.Ref, a, h.now())
		})
}

// webRequestChange chạy một lần đổi yêu cầu của khách và ghi audit action (chỉ khi trạng thái thật sự đổi).
func (h *handler) webRequestChange(ctx context.Context, action string, fields []string,
	change func(tx pgx.Tx, seller uuid.UUID, w order.Writer, a order.Actor) (order.RequestChange, error),
) (*gen.WebRequestRef, error) {
	out, err := h.webRequestWrite(ctx, http.StatusOK, "đổi yêu cầu về đơn của khách",
		func(tx pgx.Tx, u identity.AuthUser, seller uuid.UUID, w order.Writer) (any, error) {
			c, err := change(tx, seller, w, webActor(u))
			if err != nil {
				return nil, err
			}
			body := &gen.WebRequestRef{Ref: c.After.Ref, Status: gen.OrderRequestStatus(c.After.Status)}
			if c.After.Version == c.Before.Version {
				return body, nil
			}
			after := c.After.AuditFields()
			after["changed_fields"] = fields
			return body, auditService(ctx, tx, u, action, "order_request", c.After.ID, c.Before.AuditFields(), after)
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.WebRequestRef), nil //nolint:forcetypeassert // fn trên trả *gen.WebRequestRef
}

// WebListRequests (GET /api/v1/web/orders/{externalRef}/requests, web.orders.read): yêu cầu của đơn kèm tin hiện cho
// khách (không ghi chú nội bộ, không tên nhân viên) và hạn đổi của shop.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) WebListRequests(ctx context.Context, params gen.WebListRequestsParams) (gen.WebListRequestsRes, error) {
	out := &gen.WebCustomerRequestList{}
	if err := h.catalogRead(ctx, func(tx pgx.Tx) error {
		seller, err := catalog.DefaultSeller(ctx, tx)
		if err != nil {
			return fmt.Errorf("người bán của đơn web: %w", err)
		}
		ds, err := order.ListWebRequests(ctx, tx, seller, params.ExternalRef)
		if err != nil {
			return err //nolint:wrapcheck // sentinel của order
		}
		s, err := order.LoadSettings(ctx, tx)
		if err != nil {
			return err //nolint:wrapcheck // order đã bọc
		}
		out.ExchangeWindowDays = int32(s.ExchangeWindowDays) //nolint:gosec // 1–60
		out.Items = make([]gen.WebCustomerRequest, 0, len(ds))
		for _, d := range ds {
			out.Items = append(out.Items, webRequestOut(d))
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("đọc yêu cầu của đơn web: %w", err)
	}
	return out, nil
}

func webRequestOut(d order.RequestDetail) gen.WebCustomerRequest {
	r := gen.WebCustomerRequest{
		Ref: d.Ref, Kind: gen.OrderRequestKind(d.Kind), Reason: d.ReasonCode, Message: d.CustomerNote,
		Status: gen.OrderRequestStatus(d.Status), Lines: requestLinesOut(d.Lines, false),
		FileCount: int32(len(d.FileIDs)), //nolint:gosec // ≤ 5
		Messages:  make([]gen.WebCustomerRequestMessage, 0, len(d.Messages)), CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt,
	}
	setRequestShipBackOutcome(&r.ShipBack, &r.Outcome, d)
	for _, m := range d.Messages {
		from := gen.WebCustomerRequestMessageFromStaff
		if m.AuthorType == order.AuthorCustomer {
			from = gen.WebCustomerRequestMessageFromCustomer
		}
		r.Messages = append(r.Messages, gen.WebCustomerRequestMessage{ID: m.ID, From: from, Body: m.Body, CreatedAt: m.CreatedAt})
	}
	return r
}

// webRequestWrite chạy fn (người bán mặc định của đơn web, order.Writer đủ sự kiện, hàng đợi, báo hiệu — khách hủy
// đơn cần nhả hàng) trong refusableWrite: từ chối của yêu cầu về đơn, khách hủy đơn trả lời, không lưu theo khóa.
func (h *handler) webRequestWrite(ctx context.Context, status int, what string,
	fn func(tx pgx.Tx, u identity.AuthUser, seller uuid.UUID, w order.Writer) (any, error),
) (any, error) {
	w := h.orderWriter()
	return h.refusableWrite(ctx, status, isOrderRequestRefusal, what, func(tx pgx.Tx, u identity.AuthUser) (any, error) {
		seller, err := catalog.DefaultSeller(ctx, tx)
		if err != nil {
			return nil, fmt.Errorf("người bán của đơn web: %w", err)
		}
		return fn(tx, u, seller, w)
	})
}

// webActor: người làm của website là khóa dịch vụ.
func webActor(u identity.AuthUser) order.Actor {
	return order.Actor{TenantID: u.TenantID, UserID: u.ServiceKeyID, Type: order.ActorService}
}

// auditService ghi audit của khóa dịch vụ u (website) trong tx.
func auditService(ctx context.Context, tx pgx.Tx, u identity.AuthUser, action, entityType string, id uuid.UUID,
	before, after map[string]any,
) error {
	return recordAudit(ctx, tx, identity.AuditEntry{
		TenantID: u.TenantID, ActorType: order.ActorService, ActorID: u.ServiceKeyID.String(), Action: action,
		EntityType: entityType, EntityID: id.String(), Before: before, After: after, RequestID: RequestID(ctx),
	})
}
