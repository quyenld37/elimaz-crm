package httpapi

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/cash"
)

// Vùng cod_balances (kế hoạch M3, Task M3-29): tiền COD của vận đơn. Task của vùng thay thân hàm trong tệp này, không
// sửa tệp của vùng khác.

// actorName là tên hiện của tác nhân bút toán: nhân viên theo names; hãng, hệ thống có tên cố định (Actor của hợp
// đồng: id rỗng uuid 0).
func actorName(a cash.Actor, names map[uuid.UUID]string) string {
	switch a.Type {
	case cash.ActorCarrier:
		return "Hãng vận chuyển"
	case cash.ActorSystem:
		return "Hệ thống"
	}
	return names[a.ID]
}

// GetShipmentCod (GET /api/v1/shipments/{id}/cod, cash.cod.read): số dư năm ngăn và các bút toán của vận đơn theo
// thứ tự ghi. Vận đơn không có hay của tenant khác → 404.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetShipmentCod(ctx context.Context, params gen.GetShipmentCodParams) (gen.GetShipmentCodRes, error) {
	if h.d.Cash == nil {
		return nil, errNoCash
	}
	var out *gen.ShipmentCod
	err := h.catalogRead(ctx, func(tx pgx.Tx) error {
		if err := sellerShipmentCOD(ctx, tx, params.ID); err != nil {
			return err
		}
		c, err := h.d.Cash.ShipmentCOD(ctx, tx, params.ID)
		if err != nil {
			return err //nolint:wrapcheck // cash đã bọc; sentinel đổi ở errors.go
		}
		var staff []uuid.UUID
		for _, t := range c.Transfers {
			if t.Actor.Type == cash.ActorStaff && t.Actor.ID != uuid.Nil {
				staff = append(staff, t.Actor.ID)
			}
		}
		names, err := staffNames(ctx, tx, staff)
		if err != nil {
			return err
		}
		out = shipmentCodOut(c, names)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func shipmentCodOut(c cash.ShipmentCOD, names map[uuid.UUID]string) *gen.ShipmentCod {
	b := c.Balances
	out := &gen.ShipmentCod{
		Balances: gen.CodBalances{
			ToCollect: gen.SignedMoney(b.ToCollect), CarrierHolding: gen.SignedMoney(b.CarrierHolding),
			ShopReceived: gen.SignedMoney(b.ShopReceived), CarrierFee: gen.SignedMoney(b.CarrierFee),
			SalesClearing: gen.SignedMoney(b.SalesClearing),
		},
		Transfers: make([]gen.CodTransfer, 0, len(c.Transfers)),
	}
	for _, t := range c.Transfers {
		tr := gen.CodTransfer{
			ID: t.ID, Kind: gen.CodTransferKind(t.Kind), OccurredAt: t.OccurredAt,
			Entries: make([]gen.CodEntry, 0, len(t.Entries)),
		}
		for _, en := range t.Entries {
			tr.Entries = append(tr.Entries, gen.CodEntry{Account: gen.CodAccount(en.Account), Amount: gen.SignedMoney(en.Amount)})
		}
		if t.Reason != "" {
			tr.Reason = gen.NewOptString(t.Reason)
		}
		tr.Actor = gen.NewOptActor(gen.Actor{ID: t.Actor.ID, Name: actorName(t.Actor, names)})
		if t.StatementLineID != nil {
			tr.StatementLineId = gen.NewOptUUID(*t.StatementLineID)
		}
		if t.ReversesTransferID != nil {
			tr.ReversesTransferId = gen.NewOptUUID(*t.ReversesTransferID)
		}
		out.Transfers = append(out.Transfers, tr)
	}
	return out
}
