package web

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Hãng giao hàng cho website (kế hoạch M5, Task 198): danh sách hãng (đệm 60 s: đổi show_on_web ở M3 không phát báo
// hiệu cho bộ đệm), báo phí (đệm 10 phút theo mức làm tròn và version cài đặt; lưu cài đặt làm mới sớm hơn qua sự kiện
// carrier.web_settings_changed), ước thời gian giao (đệm 1 giờ).
const (
	carriersTTL  = 60 * time.Second
	carriersKey  = "list"
	shipQuoteTTL = 10 * time.Minute
	estimatesTTL = time.Hour
	estimatesKey = "all"
	// weightStep, valueStep: bước làm tròn lên của khối lượng (g) và giá trị, COD (đồng) trong khóa đệm báo phí.
	weightStep = 250
	valueStep  = 500000
)

// Shipping là phần hãng giao hàng của website.
type Shipping struct {
	db  *db.DB
	rc  cache.ReadCache
	svc *fulfillment.Service
	now func() time.Time
}

// NewShipping trả Shipping đọc d qua rc, báo phí bằng svc; now nil → time.Now.
func NewShipping(d *db.DB, rc cache.ReadCache, svc *fulfillment.Service, now func() time.Time) *Shipping {
	if now == nil {
		now = time.Now
	}
	return &Shipping{db: d, rc: rc, svc: svc, now: now}
}

// Carriers trả hãng hiện trên website (ưu tiên đầu, còn lại theo web_sort).
func (s *Shipping) Carriers(ctx context.Context, tenant uuid.UUID) (*gen.WebShippingCarriers, error) {
	raw, _, err := s.rc.GetOrLoad(ctx, tenant, cache.SpaceCarriers, carriersKey, carriersTTL, func(ctx context.Context) ([]byte, error) {
		var cars []fulfillment.WebCarrier
		err := tenancy.WithTenant(ctx, s.db, tenant, func(tx pgx.Tx) error {
			var err error
			cars, err = s.svc.WebCarriers(ctx, tx)
			return err //nolint:wrapcheck // fulfillment đã bọc
		}, db.TxAPI)
		if err != nil {
			return nil, fmt.Errorf("đọc hãng trên web: %w", err)
		}
		out := gen.WebShippingCarriers{Carriers: make([]gen.WebShippingCarrier, len(cars))}
		for i, c := range cars {
			out.Carriers[i] = gen.WebShippingCarrier{AccountId: c.AccountID, CarrierKey: c.CarrierKey, Name: c.Name, Preferred: c.Preferred}
		}
		return out.MarshalJSON()
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // ReadCache đã bọc; ErrBusy giữ nguyên để đổi 503
	}
	var out gen.WebShippingCarriers
	if err := out.UnmarshalJSON(raw); err != nil {
		return nil, fmt.Errorf("đọc hãng trên web trong bộ đệm: %w", err)
	}
	return &out, nil
}

// Quote báo phí cho req: làm tròn lên khối lượng (250 g), giá trị và COD (500.000), đọc version cài đặt, rồi lấy từ bộ
// đệm hay báo phí mới (fulfillment.QuoteForWeb, ngoài transaction). ErrNoQuote không được đệm.
func (s *Shipping) Quote(ctx context.Context, tenant uuid.UUID, req *gen.WebShippingQuoteRequest) (*gen.WebShippingQuote, error) {
	in := fulfillment.WebShipQuoteInput{
		ProvinceCode: req.ProvinceCode, WardCode: req.WardCode,
		WeightGrams: int(roundUp(int64(req.WeightGrams), weightStep)),
		Value:       roundUp(req.Value, valueStep), COD: roundUp(req.Cod, valueStep),
	}
	var set fulfillment.WebCarrierSettings
	if err := tenancy.WithTenant(ctx, s.db, tenant, func(tx pgx.Tx) error {
		var err error
		set, err = fulfillment.GetWebCarrierSettings(ctx, tx)
		return err //nolint:wrapcheck // fulfillment đã bọc
	}, db.TxAPI); err != nil {
		return nil, fmt.Errorf("đọc cài đặt hãng trên web: %w", err)
	}
	key := quoteKey(in, set.Version)
	raw, _, err := s.rc.GetOrLoad(ctx, tenant, cache.SpaceShipQuote, key, shipQuoteTTL, func(ctx context.Context) ([]byte, error) {
		opts, err := s.svc.QuoteForWeb(ctx, s.db, tenant, in)
		if err != nil {
			return nil, err //nolint:wrapcheck // fulfillment đã bọc; ErrNoQuote giữ nguyên để đổi 503
		}
		out := gen.WebShippingQuote{Options: make([]gen.WebShipOption, len(opts))}
		for i, o := range opts {
			g := gen.WebShipOption{Fee: o.Fee, Preferred: o.Preferred, Estimated: o.Estimated}
			if !o.Estimated {
				g.AccountId = gen.NewOptUUID(o.AccountID)
				g.CarrierKey, g.CarrierName = gen.NewOptString(o.CarrierKey), gen.NewOptString(o.CarrierName)
				g.Service = gen.NewOptString(o.Service)
			}
			out.Options[i] = g
		}
		return out.MarshalJSON()
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // ErrNoQuote, ErrBusy giữ nguyên để đổi mã
	}
	var out gen.WebShippingQuote
	if err := out.UnmarshalJSON(raw); err != nil {
		return nil, fmt.Errorf("đọc báo phí trong bộ đệm: %w", err)
	}
	return &out, nil
}

// quoteKey là khóa đệm báo phí: chỉ mã địa giới (chữ, số theo hợp đồng), mức đã làm tròn, version cài đặt, nối bằng
// "_" (khớp phần khóa của cache) — không dữ liệu cá nhân.
func quoteKey(in fulfillment.WebShipQuoteInput, version int) string {
	return strings.Join([]string{
		"v" + strconv.Itoa(version), in.ProvinceCode, in.WardCode, strconv.Itoa(in.WeightGrams),
		strconv.FormatInt(in.Value, 10), strconv.FormatInt(in.COD, 10),
	}, "_")
}

// roundUp làm tròn v ≥ 0 lên bội số của step (0 giữ 0).
func roundUp(v, step int64) int64 {
	return (v + step - 1) / step * step
}

// Estimates trả số ngày giao dự kiến theo tỉnh (làm tròn 0,1 ngày), xếp theo mã tỉnh.
func (s *Shipping) Estimates(ctx context.Context, tenant uuid.UUID) (*gen.WebDeliveryEstimates, error) {
	raw, _, err := s.rc.GetOrLoad(ctx, tenant, cache.SpaceEstimates, estimatesKey, estimatesTTL, func(ctx context.Context) ([]byte, error) {
		var m map[string]float64
		now := s.now().UTC()
		err := tenancy.WithTenant(ctx, s.db, tenant, func(tx pgx.Tx) error {
			var err error
			m, err = s.svc.DeliveryEstimates(ctx, tx, now)
			return err //nolint:wrapcheck // fulfillment đã bọc
		}, db.TxAPI)
		if err != nil {
			return nil, fmt.Errorf("đọc ước thời gian giao: %w", err)
		}
		out := gen.WebDeliveryEstimates{Provinces: make([]gen.WebDeliveryEstimate, 0, len(m))}
		for code, days := range m {
			out.Provinces = append(out.Provinces, gen.WebDeliveryEstimate{ProvinceCode: code, Days: math.Round(days*10) / 10})
		}
		slices.SortFunc(out.Provinces, func(a, b gen.WebDeliveryEstimate) int {
			return cmp.Compare(a.ProvinceCode, b.ProvinceCode)
		})
		return out.MarshalJSON()
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // ReadCache đã bọc
	}
	var out gen.WebDeliveryEstimates
	if err := out.UnmarshalJSON(raw); err != nil {
		return nil, fmt.Errorf("đọc ước thời gian giao trong bộ đệm: %w", err)
	}
	return &out, nil
}

// CheckChoice đối chiếu lựa chọn giao hàng c website gửi kèm báo giá hay đơn (Task 490), NGOÀI transaction (có thể gọi
// hãng khi bộ đệm báo phí hết hạn), và trả lựa chọn kèm vấn đề cho order (order áp luật gửi thẳng/hàng đợi):
//   - không accountId (phí dự phòng): shop không còn phí dự phòng → carrier_unavailable; khác phí dự phòng đang lưu →
//     fee_mismatch;
//   - tài khoản không active hay không hiện trên web (đọc PostgreSQL, không qua bộ đệm) → carrier_unavailable;
//   - addr (địa chỉ giao của đơn; nil ở báo giá) khác mã tỉnh, phường của c.Quote → fee_mismatch;
//   - báo phí của đúng thân c.Quote (Quote: bộ đệm 10 phút như webShippingQuote) không có lựa chọn cùng tài khoản,
//     dịch vụ (không gửi: dịch vụ nào cũng được) và phí → fee_mismatch; không báo được (ErrNoQuote, cache.ErrBusy) →
//     unverified.
//
// Lỗi khác (database) trả nguyên.
func (s *Shipping) CheckChoice(ctx context.Context, tenant uuid.UUID, c *gen.WebShippingChoice, addr *order.ShipAddress,
) (*order.WebShipChoice, error) {
	out := &order.WebShipChoice{Fee: c.Fee, Service: c.Service.Or("")}
	acc, hasAcc := c.AccountId.Get()
	var (
		set  fulfillment.WebCarrierSettings
		cars []fulfillment.WebCarrier
	)
	if err := tenancy.WithTenant(ctx, s.db, tenant, func(tx pgx.Tx) error {
		var err error
		if set, err = fulfillment.GetWebCarrierSettings(ctx, tx); err != nil {
			return err //nolint:wrapcheck // fulfillment đã bọc
		}
		cars, err = s.svc.WebCarriers(ctx, tx)
		return err //nolint:wrapcheck // như trên
	}, db.TxAPI); err != nil {
		return nil, fmt.Errorf("kiểm hãng khách chọn: %w", err)
	}
	if !hasAcc {
		switch {
		case set.FallbackFee == nil:
			out.Problem = order.ShipProblemCarrierUnavailable
		case *set.FallbackFee != c.Fee:
			out.Problem = order.ShipProblemFeeMismatch
		}
		return out, nil
	}
	out.AccountID = &acc
	i := slices.IndexFunc(cars, func(w fulfillment.WebCarrier) bool { return w.AccountID == acc })
	if i < 0 {
		out.Problem = order.ShipProblemCarrierUnavailable
		return out, nil
	}
	out.CarrierKey, out.CarrierName = cars[i].CarrierKey, cars[i].Name
	if addr != nil && (addr.ProvinceCode != c.Quote.ProvinceCode || addr.WardCode != c.Quote.WardCode) {
		out.Problem = order.ShipProblemFeeMismatch
		return out, nil
	}
	q, err := s.Quote(ctx, tenant, &c.Quote)
	switch {
	case errors.Is(err, fulfillment.ErrNoQuote), errors.Is(err, cache.ErrBusy):
		out.Problem = order.ShipProblemUnverified
		return out, nil
	case err != nil:
		return nil, err
	}
	for _, o := range q.Options {
		id, ok := o.AccountId.Get()
		svc := o.Service.Or("")
		if ok && id == acc && o.Fee == c.Fee && (out.Service == "" || svc == out.Service) {
			out.Service = svc
			return out, nil
		}
	}
	out.Problem = order.ShipProblemFeeMismatch
	return out, nil
}
