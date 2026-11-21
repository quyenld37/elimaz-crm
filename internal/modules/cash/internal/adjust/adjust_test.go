package adjust_test

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/testkit/codtest"
)

var deliver = [][]string{{"picked_up", "delivered"}} //nolint:gochecknoglobals // hằng của test

func preview(e *codtest.Env, in cash.CODAdjustmentInput) (cash.CODAdjustmentPreview, error) {
	var p cash.CODAdjustmentPreview
	err := e.App(func(tx pgx.Tx) error {
		var err error
		p, err = e.Mod.Service.PreviewCODAdjustment(e.T.Context(), tx, in)
		return err //nolint:wrapcheck // test
	})
	return p, err //nolint:wrapcheck // test
}

func mustPreview(t *testing.T, e *codtest.Env, in cash.CODAdjustmentInput) cash.CODAdjustmentPreview {
	t.Helper()
	p, err := preview(e, in)
	if err != nil {
		t.Fatalf("xem trước %+v: %v", in, err)
	}
	return p
}

func create(e *codtest.Env, in cash.CODAdjustmentInput, token string) (cash.CODTransfer, error) {
	var out cash.CODTransfer
	err := e.App(func(tx pgx.Tx) error {
		var err error
		out, err = e.Mod.Service.CreateCODAdjustment(e.T.Context(), tx, in, token)
		return err //nolint:wrapcheck // test
	})
	return out, err //nolint:wrapcheck // test
}

func bal(b cash.Balances) [5]int64 {
	return [5]int64{b.ToCollect, b.CarrierHolding, b.ShopReceived, b.CarrierFee, b.SalesClearing}
}

// remittedV1 dựng V1 630.000 đã giao, bảng kê chốt ngày 19/11/2026 (giờ Việt Nam) phí 32.000; trả vận đơn và bút toán
// remitted.
func remittedV1(t *testing.T, e *codtest.Env) (uuid.UUID, uuid.UUID) {
	t.Helper()
	v1 := e.Shipment(codtest.Ship{Tracking: "FK0001", COD: 630000, Status: fulfillment.StatusDelivered, Steps: deliver})
	remit := e.Remit(v1, "FK0001", 630000, 32000, time.Date(2026, 11, 19, 3, 0, 0, 0, time.UTC))
	if got, want := e.Bal(v1), [5]int64{0, 0, 598000, 32000, -630000}; got != want {
		t.Fatalf("V1 sau chốt %v, mong %v", got, want)
	}
	return v1, remit
}

// TI08: V1 đã chốt (shop_received 598.000); nhân viên ghi nhầm bảng kê → xem trước đảo bút toán remitted: after =
// {0, 630000, 0, 0, −630000}, cảnh báo "thuộc bảng kê đã chốt ngày 19/11/2026", không đổi sổ; ghi với token → bút
// toán reversal trỏ remitted, lý do và người làm; bút toán cũ còn nguyên ba dòng; số dư bằng bản xem trước.
func TestAdjust_TI08ReverseRemitted(t *testing.T) {
	t.Parallel()
	e := codtest.New(t)
	v1, remit := remittedV1(t, e)
	in := cash.CODAdjustmentInput{
		ShipmentID: v1, Mode: cash.AdjustReverse, ReversesTransferID: &remit, Reason: "ghi nhầm bảng kê", Actor: e.User,
	}
	p := mustPreview(t, e, in)
	if bal(p.Before) != [5]int64{0, 0, 598000, 32000, -630000} || bal(p.After) != [5]int64{0, 630000, 0, 0, -630000} {
		t.Errorf("trước %v sau %v", bal(p.Before), bal(p.After))
	}
	if p.Kind != cash.KindReversal || len(p.Entries) != 3 || p.Token == "" ||
		!slices.Contains(p.Warnings, "Bút toán này thuộc bảng kê đã chốt ngày 19/11/2026") {
		t.Errorf("bản xem trước: %+v", p)
	}
	if n := e.Count("SELECT count(*) FROM cash.cod_transfers WHERE shipment_id = $1", v1); n != 3 {
		t.Fatalf("xem trước đổi sổ: %d bút toán", n)
	}
	tr, err := create(e, in, p.Token)
	if err != nil {
		t.Fatalf("ghi: %v", err)
	}
	if tr.Kind != cash.KindReversal || tr.ReversesTransferID == nil || *tr.ReversesTransferID != remit ||
		tr.Reason != "ghi nhầm bảng kê" || tr.Actor.Type != cash.ActorStaff || tr.Actor.ID != e.User || len(tr.Entries) != 3 {
		t.Errorf("bút toán đảo: %+v", tr)
	}
	if got := e.Bal(v1); got != bal(p.After) {
		t.Errorf("số dư sau ghi %v, mong %v", got, bal(p.After))
	}
	if n := e.Count("SELECT count(*) FROM cash.cod_ledger_entries WHERE transfer_id = $1", remit); n != 3 {
		t.Errorf("bút toán remitted còn %d dòng, mong 3 (không xóa dòng cũ)", n)
	}
	if _, err := create(e, in, p.Token); !errors.Is(err, cash.ErrPreviewStale) {
		t.Errorf("ghi lại cùng token: %v, mong ErrPreviewStale", err)
	}
	if _, err := preview(e, in); !errors.Is(err, cash.ErrAdjustmentBlocked) {
		t.Errorf("đảo lần hai: %v, mong ErrAdjustmentBlocked", err)
	}
	if _, err := preview(e, cash.CODAdjustmentInput{
		ShipmentID: v1, Mode: cash.AdjustReverse, ReversesTransferID: &tr.ID, Reason: "x",
	}); !errors.Is(err, cash.ErrAdjustmentBlocked) {
		t.Errorf("đảo bút toán đảo: %v, mong ErrAdjustmentBlocked", err)
	}
}

// Token cũ sau khi vận đơn có bút toán khác → ErrPreviewStale, không ghi gì; xem trước lại rồi ghi được. Chuyển ngăn:
// kind adjustment hai vế from −amount, to +amount; ngăn sau điều chỉnh âm → cảnh báo.
func TestAdjust_StaleTokenAndMove(t *testing.T) {
	t.Parallel()
	e := codtest.New(t)
	v, _ := remittedV1(t, e)
	move := cash.CODAdjustmentInput{
		ShipmentID: v, Mode: cash.AdjustMove, From: cash.AccountCarrierFee, To: cash.AccountShopReceived, Amount: 2000,
		Reason: "phí ghi thừa", Actor: e.User,
	}
	old := mustPreview(t, e, move)
	if old.Kind != cash.KindAdjustment || len(old.Warnings) != 0 ||
		!slices.Equal(old.Entries, []cash.Entry{{Account: cash.AccountCarrierFee, Amount: -2000}, {Account: cash.AccountShopReceived, Amount: 2000}}) ||
		bal(old.After) != [5]int64{0, 0, 600000, 30000, -630000} {
		t.Fatalf("xem trước chuyển ngăn: %+v", old)
	}
	other := mustPreview(t, e, cash.CODAdjustmentInput{
		ShipmentID: v, Mode: cash.AdjustMove, From: cash.AccountShopReceived, To: cash.AccountCarrierHolding, Amount: 1,
		Reason: "khác", Actor: e.User,
	})
	if _, err := create(e, cash.CODAdjustmentInput{
		ShipmentID: v, Mode: cash.AdjustMove, From: cash.AccountShopReceived, To: cash.AccountCarrierHolding, Amount: 1,
		Reason: "khác", Actor: e.User,
	}, other.Token); err != nil {
		t.Fatalf("ghi bút toán khác: %v", err)
	}
	before := e.Count("SELECT count(*) FROM cash.cod_transfers WHERE shipment_id = $1", v)
	if _, err := create(e, move, old.Token); !errors.Is(err, cash.ErrPreviewStale) {
		t.Errorf("token cũ: %v, mong ErrPreviewStale", err)
	}
	if n := e.Count("SELECT count(*) FROM cash.cod_transfers WHERE shipment_id = $1", v); n != before {
		t.Errorf("token cũ vẫn ghi: %d → %d bút toán", before, n)
	}
	if _, err := create(e, move, "khong-phai-token"); !errors.Is(err, cash.ErrPreviewStale) {
		t.Errorf("token lạ: %v", err)
	}
	fresh := mustPreview(t, e, move)
	tr, err := create(e, move, fresh.Token)
	if err != nil || tr.Kind != cash.KindAdjustment || tr.Reason != "phí ghi thừa" {
		t.Fatalf("ghi sau khi xem lại: %+v %v", tr, err)
	}
	neg := mustPreview(t, e, cash.CODAdjustmentInput{
		ShipmentID: v, Mode: cash.AdjustMove, From: cash.AccountCarrierHolding, To: cash.AccountShopReceived, Amount: 5000,
		Reason: "thử", Actor: e.User,
	})
	if !slices.Contains(neg.Warnings, "Sau điều chỉnh, ngăn Hãng đang giữ sẽ âm") {
		t.Errorf("cảnh báo âm: %v", neg.Warnings)
	}
	if n := e.Count("SELECT coalesce(sum(amount), 0)::int FROM cash.cod_ledger_entries"); n != 0 {
		t.Errorf("tổng sổ %d", n)
	}
}

// Thiếu lý do → ErrAdjustReasonRequired; yêu cầu sai (chế độ lạ, ngăn trùng, số tiền 0, đảo thiếu mã, trường thừa, lý
// do quá 500 ký tự) → ErrAdjustmentInvalid; bút toán của vận đơn khác → ErrAdjustTargetNotFound; vận đơn không có →
// ErrShipmentNotFound; expected của vận đơn chưa kết thúc → ErrAdjustmentBlocked (Task 120: khóa expected cố định), của
// vận đơn đã giao → được đảo. Mọi ca không ghi gì, ở cả xem trước và ghi.
func TestAdjust_Refusals(t *testing.T) {
	t.Parallel()
	e := codtest.New(t)
	ship := e.Shipment(codtest.Ship{Tracking: "FK0101", COD: 450000, Status: fulfillment.StatusShipping, Steps: [][]string{{"picked_up"}}})
	done := e.Shipment(codtest.Ship{Tracking: "FK0102", COD: 300000, Status: fulfillment.StatusDelivered, Steps: deliver})
	shipExp, doneExp := e.TransferOf(ship, cash.KindExpected), e.TransferOf(done, cash.KindExpected)
	long := strings.Repeat("ạ", 501)
	for _, c := range []struct {
		name string
		in   cash.CODAdjustmentInput
		want error
	}{
		{"thiếu lý do", cash.CODAdjustmentInput{ShipmentID: ship, Mode: cash.AdjustReverse, ReversesTransferID: &shipExp, Reason: "  "}, cash.ErrAdjustReasonRequired},
		{"chế độ lạ", cash.CODAdjustmentInput{ShipmentID: ship, Mode: "delete", Reason: "x"}, cash.ErrAdjustmentInvalid},
		{"ngăn trùng", cash.CODAdjustmentInput{ShipmentID: ship, Mode: cash.AdjustMove, From: cash.AccountToCollect, To: cash.AccountToCollect, Amount: 1, Reason: "x"}, cash.ErrAdjustmentInvalid},
		{"ngăn lạ", cash.CODAdjustmentInput{ShipmentID: ship, Mode: cash.AdjustMove, From: "cash", To: cash.AccountToCollect, Amount: 1, Reason: "x"}, cash.ErrAdjustmentInvalid},
		{"số tiền 0", cash.CODAdjustmentInput{ShipmentID: ship, Mode: cash.AdjustMove, From: cash.AccountToCollect, To: cash.AccountCarrierHolding, Reason: "x"}, cash.ErrAdjustmentInvalid},
		{"đảo thiếu mã", cash.CODAdjustmentInput{ShipmentID: ship, Mode: cash.AdjustReverse, Reason: "x"}, cash.ErrAdjustmentInvalid},
		{"đảo kèm số tiền", cash.CODAdjustmentInput{ShipmentID: ship, Mode: cash.AdjustReverse, ReversesTransferID: &shipExp, Amount: 5, Reason: "x"}, cash.ErrAdjustmentInvalid},
		{"chuyển kèm mã đảo", cash.CODAdjustmentInput{ShipmentID: ship, Mode: cash.AdjustMove, ReversesTransferID: &shipExp, From: cash.AccountToCollect, To: cash.AccountCarrierHolding, Amount: 1, Reason: "x"}, cash.ErrAdjustmentInvalid},
		{"lý do 501 ký tự", cash.CODAdjustmentInput{ShipmentID: ship, Mode: cash.AdjustReverse, ReversesTransferID: &shipExp, Reason: long}, cash.ErrAdjustmentInvalid},
		{"bút toán vận đơn khác", cash.CODAdjustmentInput{ShipmentID: ship, Mode: cash.AdjustReverse, ReversesTransferID: &doneExp, Reason: "x"}, cash.ErrAdjustTargetNotFound},
		{"vận đơn không có", cash.CODAdjustmentInput{ShipmentID: uuid.New(), Mode: cash.AdjustReverse, ReversesTransferID: &shipExp, Reason: "x"}, cash.ErrShipmentNotFound},
		{"expected chưa kết thúc", cash.CODAdjustmentInput{ShipmentID: ship, Mode: cash.AdjustReverse, ReversesTransferID: &shipExp, Reason: "x"}, cash.ErrAdjustmentBlocked},
	} {
		c.in.Actor = e.User
		if _, err := preview(e, c.in); !errors.Is(err, c.want) {
			t.Errorf("%s: xem trước %v, mong %v", c.name, err, c.want)
		}
		if _, err := create(e, c.in, "t"); !errors.Is(err, c.want) {
			t.Errorf("%s: ghi %v, mong %v", c.name, err, c.want)
		}
	}
	if n := e.Count("SELECT count(*) FROM cash.cod_transfers WHERE kind IN ('adjustment', 'reversal')"); n != 0 {
		t.Errorf("ca từ chối vẫn ghi %d bút toán", n)
	}
	ok := cash.CODAdjustmentInput{ShipmentID: done, Mode: cash.AdjustReverse, ReversesTransferID: &doneExp, Reason: strings.Repeat("ạ", 500), Actor: e.User}
	if _, err := create(e, ok, mustPreview(t, e, ok).Token); err != nil {
		t.Errorf("đảo expected của vận đơn đã giao (lý do 500 ký tự): %v", err)
	}
}

// Hai lần ghi cùng một bản xem trước chạy cùng lúc → đúng một lần ghi, lần kia ErrPreviewStale (khóa sổ của vận đơn,
// tính lại token sau khóa).
func TestAdjust_ConcurrentSameToken(t *testing.T) {
	t.Parallel()
	e := codtest.New(t)
	v, remit := remittedV1(t, e)
	in := cash.CODAdjustmentInput{ShipmentID: v, Mode: cash.AdjustReverse, ReversesTransferID: &remit, Reason: "nhầm", Actor: e.User}
	tok := mustPreview(t, e, in).Token
	errs := make([]error, 4)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Go(func() { _, errs[i] = create(e, in, tok) })
	}
	wg.Wait()
	okN, stale := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			okN++
		case errors.Is(err, cash.ErrPreviewStale):
			stale++
		default:
			t.Errorf("lỗi lạ: %v", err)
		}
	}
	if okN != 1 || stale != 3 {
		t.Errorf("ghi được %d, cũ %d; mong 1, 3", okN, stale)
	}
	if n := e.Count("SELECT count(*) FROM cash.cod_transfers WHERE reverses_transfer_id = $1", remit); n != 1 {
		t.Errorf("bút toán đảo: %d", n)
	}
}
