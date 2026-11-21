package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/shiptest"
)

// manualWireEnv: tenant A có cửa hàng (shiptest.NewShop) và một nhân viên, để tạo tài khoản hãng bằng Service thật
// của wireM3.
type manualWireEnv struct {
	s            *testkit.Staff
	tenant, user uuid.UUID
	shop         *shiptest.Shop
}

// create tạo tài khoản key/mode bằng Service của p (không khóa, mã ở hãng local:<id>) trong tx crm_app.
func (e *manualWireEnv) create(t *testing.T, p m3Parts, key, mode string) (fulfillment.AccountView, error) {
	t.Helper()
	seller := e.shop.Seller
	var v fulfillment.AccountView
	err := tenancy.WithTenant(t.Context(), e.s.App, e.tenant, func(tx pgx.Tx) (err error) {
		v, err = p.Fulfillment.Service.CreateAccount(t.Context(), tx, fulfillment.AccountInput{
			SellerID: &seller, CarrierKey: key, Mode: mode, Name: key + " " + uuid.NewString()[:8], WarehouseID: e.shop.Warehouse,
		}, fulfillment.Actor{TenantID: e.tenant, UserID: e.user})
		return err //nolint:wrapcheck // test
	})
	if err != nil {
		return v, fmt.Errorf("tạo tài khoản %s %s: %w", key, mode, err)
	}
	return v, nil
}

// Task 448: wireM3 nối danh mục hãng nhập tay cả ở production (bộ hãng rỗng khi không có hãng giả): tài khoản mode manual
// của SPX, VTP, BEST tạo được (active) với Service thật của tiến trình; mode api của hãng chưa nối vẫn 422
// (ErrCarrierNotSupported) ở production; hãng ngoài danh mục luôn bị từ chối; ở dev (hãng giả nối) manual vẫn tạo được.
func TestWireM3_ManualAccountsWithoutCarrierAPI(t *testing.T) {
	e := &manualWireEnv{s: testkit.OpenStaff(t)}
	e.tenant = e.s.Tenant(t, "A")
	e.user = e.s.User(t, e.tenant, "kho@elimaz.test", "active")
	e.s.Exec(t, e.tenant, func(tx pgx.Tx) error {
		e.shop = shiptest.NewShop(t, tx, e.tenant)
		return nil
	})
	for _, c := range []struct {
		name   string
		cfg    config.Config
		refuse []string // mode api bị từ chối
	}{
		{"production", config.Config{Env: "production", FakeCarrierURL: "http://127.0.0.1:1"}, []string{"spx", "vtp", "best"}},
		{"production không URL", config.Config{Env: "production"}, []string{"spx", "vtp", "best"}},
		{"dev hãng giả", config.Config{Env: "development", FakeCarrierURL: "http://127.0.0.1:1"}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			p, err := wireM3(config.RoleAPI, c.cfg, m3Deps{})
			if err != nil {
				t.Fatal(err)
			}
			e.check(t, p, c.refuse)
		})
	}
}

// check đòi: manual của spx, vtp, best tạo được (active); api của từng khóa trong refuse và manual của hãng ngoài danh
// mục (ghn) bị ErrCarrierNotSupported.
func (e *manualWireEnv) check(t *testing.T, p m3Parts, refuse []string) {
	t.Helper()
	for _, key := range []string{"spx", "vtp", "best"} {
		v, err := e.create(t, p, key, fulfillment.ModeManual)
		if err != nil || v.Status != fulfillment.AccountActive || v.Mode != fulfillment.ModeManual {
			t.Errorf("%s manual: %v %+v, muốn active", key, err, v)
		}
	}
	for _, key := range refuse {
		if _, err := e.create(t, p, key, fulfillment.ModeAPI); !errors.Is(err, fulfillment.ErrCarrierNotSupported) {
			t.Errorf("%s api: %v, muốn ErrCarrierNotSupported", key, err)
		}
	}
	if _, err := e.create(t, p, "ghn", fulfillment.ModeManual); !errors.Is(err, fulfillment.ErrCarrierNotSupported) {
		t.Errorf("ghn manual: %v, muốn ErrCarrierNotSupported", err)
	}
}
