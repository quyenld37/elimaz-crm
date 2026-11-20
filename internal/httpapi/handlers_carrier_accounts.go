package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// Vùng carrier_accounts (kế hoạch M3, Task M3-09, M3-18): tài khoản hãng, khóa API, kiểm kết nối, đồng bộ địa giới.
// Đọc cần fulfillment.carrier_accounts.read (webhookUrl chỉ trả cho người có …manage), ghi cần …manage (toàn shop;
// guard kiểm trước handler). Audit ghi ở đây, cùng tx (fulfillment không nhập identity); không bao giờ có khóa API
// trong câu trả lời, audit hay log. SyncCarrierLocations, ListUnmappedLocations còn là khung của Task M3-18.

const (
	permCarrierAccountsManage = identity.Permission("fulfillment.carrier_accounts.manage")
	defaultCarrierAccountPage = 50
	entityCarrierAccount      = "carrier_account"
)

var errFulfillmentNotWired = errors.New("httpapi: chưa nối fulfillment.Service")

func (h *handler) fulfillmentService() (*fulfillment.Service, error) {
	if h.d.Fulfillment == nil {
		return nil, errFulfillmentNotWired
	}
	return h.d.Fulfillment, nil
}

// accountWrite chạy change trong refusableWrite với Service và Actor của người gọi; lỗi nghiệp vụ của fulfillment là
// câu trả lời không đổi gì.
func (h *handler) accountWrite(ctx context.Context, status int, what string,
	change func(tx pgx.Tx, s *fulfillment.Service, u identity.AuthUser, a fulfillment.Actor) (any, error),
) (any, error) {
	s, err := h.fulfillmentService()
	if err != nil {
		return nil, err
	}
	return h.refusableWrite(ctx, status, isShipmentRefusal, what, func(tx pgx.Tx, u identity.AuthUser) (any, error) {
		return change(tx, s, u, fulfillment.Actor{TenantID: u.TenantID, UserID: u.UserID})
	})
}

// accountChange đọc tài khoản id (phần trước của audit), chạy op, ghi audit action với phần trước/sau do fields dựng,
// trả tài khoản sau khi đổi (có webhookUrl: người gọi có quyền manage).
func (h *handler) accountChange(ctx context.Context, what, action string, id uuid.UUID,
	op func(tx pgx.Tx, s *fulfillment.Service, a fulfillment.Actor) (fulfillment.AccountView, error),
	fields func(before, after fulfillment.AccountView) (b, a map[string]any),
) (*gen.CarrierAccount, error) {
	out, err := h.accountWrite(ctx, http.StatusOK, what,
		func(tx pgx.Tx, s *fulfillment.Service, u identity.AuthUser, a fulfillment.Actor) (any, error) {
			before, err := s.GetAccount(ctx, tx, id)
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của fulfillment
			}
			v, err := op(tx, s, a)
			if err != nil {
				return nil, err
			}
			b, af := fields(before, v)
			return carrierAccountOut(v, true), h.audit(ctx, tx, u, action, entityCarrierAccount, v.ID, b, af)
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.CarrierAccount), nil //nolint:forcetypeassert // fn trên trả *gen.CarrierAccount
}

// ListCarrierAccounts (GET /api/v1/carrier-accounts): trang theo id; webhookUrl chỉ cho người có quyền manage.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListCarrierAccounts(ctx context.Context, params gen.ListCarrierAccountsParams) (gen.ListCarrierAccountsRes, error) {
	s, err := h.fulfillmentService()
	if err != nil {
		return nil, err
	}
	after, err := idCursor(params.Cursor, fulfillment.ErrAccountInvalid)
	if err != nil {
		return nil, err
	}
	f := fulfillment.AccountListFilter{After: after, Limit: int(params.Limit.Or(defaultCarrierAccountPage))}
	if v, ok := params.WarehouseId.Get(); ok {
		f.WarehouseID = &v
	}
	if v, ok := params.Status.Get(); ok {
		f.Status = string(v)
	}
	var items []fulfillment.AccountView
	var more, manage bool
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) (err error) {
		manage = identity.Can(u, permCarrierAccountsManage, nil)
		items, more, err = s.ListAccounts(ctx, tx, f)
		return err //nolint:wrapcheck // fulfillment đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đọc tài khoản hãng: %w", err)
	}
	out := &gen.CarrierAccountPage{Items: make([]gen.CarrierAccount, 0, len(items))}
	for _, v := range items {
		out.Items = append(out.Items, *carrierAccountOut(v, manage))
	}
	if more && len(items) > 0 {
		out.NextCursor = gen.NewOptString(items[len(items)-1].ID.String())
	}
	return out, nil
}

// CreateCarrierAccount (POST /api/v1/carrier-accounts): sellerId trống → người bán của shop (catalog.DefaultSeller).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CreateCarrierAccount(ctx context.Context, req *gen.CreateCarrierAccountRequest, _ gen.CreateCarrierAccountParams) (gen.CreateCarrierAccountRes, error) {
	in := fulfillment.AccountInput{
		CarrierKey: req.CarrierKey, Mode: string(req.Mode), Environment: string(req.Environment.Or("")), Name: req.Name,
		ExternalAccountRef: req.ExternalAccountRef.Or(""), WarehouseID: req.WarehouseId,
		DefaultService: req.DefaultService.Or(""), IsDefaultForWarehouse: req.IsDefaultForWarehouse.Or(false),
		ShowOnWeb: req.ShowOnWeb.Or(false), WebSort: int(req.WebSort.Or(0)),
		Credentials: fulfillment.Credentials(req.Credentials.Or(nil)),
	}
	if o, ok := req.DefaultOptions.Get(); ok {
		in.DefaultOptions = carrierOptionsIn(o)
	}
	out, err := h.accountWrite(ctx, http.StatusCreated, "tạo tài khoản hãng",
		func(tx pgx.Tx, s *fulfillment.Service, u identity.AuthUser, a fulfillment.Actor) (any, error) {
			seller, ok := req.SellerId.Get()
			if !ok {
				var err error
				if seller, err = catalog.DefaultSeller(ctx, tx); err != nil {
					return nil, fmt.Errorf("người bán của shop: %w", err)
				}
			}
			in.SellerID = &seller
			v, err := s.CreateAccount(ctx, tx, in, a)
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của fulfillment
			}
			return carrierAccountOut(v, true), h.audit(ctx, tx, u, "fulfillment.carrier_account.create",
				entityCarrierAccount, v.ID, nil, v.AuditFields())
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.CarrierAccount), nil //nolint:forcetypeassert // fn trên trả *gen.CarrierAccount
}

// UpdateCarrierAccount (PATCH /api/v1/carrier-accounts/{id}): chỉ trường gửi lên; version lệch → 409.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) UpdateCarrierAccount(ctx context.Context, req *gen.UpdateCarrierAccountRequest, params gen.UpdateCarrierAccountParams) (gen.UpdateCarrierAccountRes, error) {
	p := fulfillment.AccountPatch{
		Name: optPtr[string](req.Name), ExternalAccountRef: optPtr[string](req.ExternalAccountRef),
		DefaultService: optPtr[string](req.DefaultService), IsDefaultForWarehouse: optPtr[bool](req.IsDefaultForWarehouse),
		ShowOnWeb: optPtr[bool](req.ShowOnWeb),
	}
	if v, ok := req.Environment.Get(); ok {
		env := string(v)
		p.Environment = &env
	}
	if v, ok := req.WarehouseId.Get(); ok {
		p.WarehouseID = &v
	}
	if v, ok := req.WebSort.Get(); ok {
		n := int(v)
		p.WebSort = &n
	}
	if v, ok := req.DefaultOptions.Get(); ok {
		o := carrierOptionsIn(v)
		p.DefaultOptions = &o
	}
	return h.accountChange(ctx, "sửa tài khoản hãng", "fulfillment.carrier_account.update", params.ID,
		func(tx pgx.Tx, s *fulfillment.Service, a fulfillment.Actor) (fulfillment.AccountView, error) {
			return s.UpdateAccount(ctx, tx, params.ID, p, req.Version, a)
		},
		func(before, after fulfillment.AccountView) (b, a map[string]any) {
			return before.AuditFields(), after.AuditFields()
		})
}

// PutCarrierCredentials (PUT /api/v1/carrier-accounts/{id}/credentials): chỉ ghi; audit chỉ có tên trường credentials.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) PutCarrierCredentials(ctx context.Context, req *gen.PutCarrierCredentialsRequest, params gen.PutCarrierCredentialsParams) (gen.PutCarrierCredentialsRes, error) {
	creds := fulfillment.Credentials(req.Credentials)
	return h.accountChange(ctx, "ghi khóa API của tài khoản hãng", "fulfillment.carrier_account.credentials", params.ID,
		func(tx pgx.Tx, s *fulfillment.Service, a fulfillment.Actor) (fulfillment.AccountView, error) {
			return s.PutCredentials(ctx, tx, params.ID, creds, a)
		},
		func(before, after fulfillment.AccountView) (b, a map[string]any) {
			had := "none"
			if before.HasCredentials {
				had = "set"
			}
			return map[string]any{"credentials": had, "status": before.Status},
				map[string]any{"credentials": "replaced", "status": after.Status}
		})
}

// CheckCarrierAccount (POST /api/v1/carrier-accounts/{id}/check): ghi lệnh kiểm, 202 {jobId}.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CheckCarrierAccount(ctx context.Context, params gen.CheckCarrierAccountParams) (gen.CheckCarrierAccountRes, error) {
	out, err := h.accountWrite(ctx, http.StatusAccepted, "kiểm kết nối tài khoản hãng",
		func(tx pgx.Tx, s *fulfillment.Service, u identity.AuthUser, a fulfillment.Actor) (any, error) {
			job, err := s.CheckAccount(ctx, tx, params.ID, a)
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của fulfillment
			}
			return &gen.JobAccepted{JobId: job}, h.audit(ctx, tx, u, "fulfillment.carrier_account.check",
				entityCarrierAccount, params.ID, nil, map[string]any{"jobId": job.String()})
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.JobAccepted), nil //nolint:forcetypeassert // fn trên trả *gen.JobAccepted
}

// SetCarrierAccountStatus (POST /api/v1/carrier-accounts/{id}/status): active (manual hoặc đã có khóa), paused,
// revoked (cuối: không đổi lại).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) SetCarrierAccountStatus(ctx context.Context, req *gen.SetCarrierAccountStatusRequest, params gen.SetCarrierAccountStatusParams) (gen.SetCarrierAccountStatusRes, error) {
	return h.accountChange(ctx, "đổi trạng thái tài khoản hãng", "fulfillment.carrier_account.status", params.ID,
		func(tx pgx.Tx, s *fulfillment.Service, a fulfillment.Actor) (fulfillment.AccountView, error) {
			return s.SetStatus(ctx, tx, params.ID, string(req.Status), optPtr[int64](req.Version), a)
		},
		func(before, after fulfillment.AccountView) (b, a map[string]any) {
			return map[string]any{"status": before.Status}, map[string]any{"status": after.Status}
		})
}

func carrierOptionsIn(o gen.CarrierOptions) fulfillment.Options {
	return fulfillment.Options{
		Inspection: o.Inspection.Or(""), FeeOnRefuse: int64(o.FeeOnRefuse.Or(0)), Payer: o.Payer.Or(""),
		Pickup: o.Pickup.Or(false), Note: o.Note.Or(""),
	}
}

func carrierOptionsOut(o fulfillment.Options) gen.CarrierOptions {
	out := gen.CarrierOptions{}
	if o.Inspection != "" {
		out.Inspection = gen.NewOptString(o.Inspection)
	}
	if o.FeeOnRefuse != 0 {
		out.FeeOnRefuse = gen.NewOptMoney(gen.Money(o.FeeOnRefuse))
	}
	if o.Payer != "" {
		out.Payer = gen.NewOptString(o.Payer)
	}
	if o.Pickup {
		out.Pickup = gen.NewOptBool(true)
	}
	if o.Note != "" {
		out.Note = gen.NewOptString(o.Note)
	}
	return out
}

// carrierAccountOut đổi AccountView sang hợp đồng; webhookUrl chỉ khi manage.
func carrierAccountOut(v fulfillment.AccountView, manage bool) *gen.CarrierAccount {
	c := v.Capabilities
	out := &gen.CarrierAccount{
		ID: v.ID, SellerId: gen.NewOptUUID(v.SellerID), CarrierKey: v.CarrierKey, Mode: gen.CarrierAccountMode(v.Mode),
		Environment: gen.CarrierAccountEnvironment(v.Environment), Name: v.Name, Status: gen.CarrierAccountStatus(v.Status),
		WarehouseId: v.WarehouseID, DefaultService: gen.NewOptString(v.DefaultService),
		DefaultOptions:        gen.NewOptCarrierOptions(carrierOptionsOut(v.DefaultOptions)),
		IsDefaultForWarehouse: v.IsDefaultForWarehouse, ShowOnWeb: v.ShowOnWeb, WebSort: int32(v.WebSort), //nolint:gosec // 0–1000
		HasCredentials: v.HasCredentials, NeedsRewrap: v.NeedsRewrap, Version: v.Version,
		Capabilities: gen.NewOptCarrierCapabilities(gen.CarrierCapabilities{
			Quote: c.Quote, Create: c.Create, LookupByRef: gen.NewOptBool(c.LookupByRef), Cancel: c.Cancel,
			CancelIdempotent: gen.NewOptBool(c.CancelIdempotent), Track: c.Track, Webhook: c.Webhook, Labels: c.Labels,
			LabelsBatch: gen.NewOptBool(c.LabelsBatch), UpdateAddress: c.UpdateAddress, Redeliver: c.Redeliver,
			Locations: c.Locations, Statement: c.Statement, Inspection: gen.NewOptBool(c.Inspection),
			DeclaredValue: gen.NewOptBool(c.DeclaredValue), FeeOnRefuse: gen.NewOptBool(c.FeeOnRefuse),
		}),
	}
	if v.LastCheckAt != nil {
		out.LastCheckAt = gen.NewOptDateTime(*v.LastCheckAt)
	}
	if v.LastCheckResult != "" {
		out.LastCheckResult = gen.NewOptString(v.LastCheckResult)
	}
	if manage {
		out.WebhookUrl = gen.NewOptString(v.WebhookURL)
	}
	return out
}

// SyncCarrierLocations (POST /api/v1/carrier-accounts/{id}/locations/sync).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) SyncCarrierLocations(_ context.Context, _ gen.SyncCarrierLocationsParams) (gen.SyncCarrierLocationsRes, error) {
	return nil, ht.ErrNotImplemented
}

// ListUnmappedLocations (GET /api/v1/carrier-locations/unmapped).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) ListUnmappedLocations(_ context.Context, _ gen.ListUnmappedLocationsParams) (gen.ListUnmappedLocationsRes, error) {
	return nil, ht.ErrNotImplemented
}
