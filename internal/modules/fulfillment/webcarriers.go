package fulfillment

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/store"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Hãng giao hàng trên website (kế hoạch M5, Task 198 / M5-15). Hãng nào hiện trên web và theo thứ tự nào là cờ
// show_on_web, web_sort của carrier_accounts (M3, sửa bằng UpdateAccount); bảng fulfillment.channel_carrier_settings
// (một dòng kênh 'web' mỗi tenant) chỉ giữ tài khoản ưu tiên và phí dự phòng. Báo phí cho web đọc trong một
// transaction ngắn, ĐÓNG transaction rồi mới gọi hãng (QuoteNow, hạn 3 s mỗi hãng). Khi không hãng nào báo phí được
// (tài khoản manual lúc tích hợp hãng tạm dừng, hãng lỗi, quá hạn) thì dùng phí dự phòng shop đặt; không có phí dự
// phòng → ErrNoQuote (lớp HTTP trả 503 retry_later).

const (
	// TopicWebCarrierSettingsChanged báo cài đặt hãng trên web đổi (aggregate web_carrier_settings, mã = tenant, Seq =
	// version mới). Consumer webcache làm mới vùng carriers và shipquote của bộ đệm website.
	TopicWebCarrierSettingsChanged = "carrier.web_settings_changed"
	// MaxWebFallbackFee là phí dự phòng lớn nhất (CHECK của bảng).
	MaxWebFallbackFee int64 = 500000

	aggregateWebCarrierSettings = "web_carrier_settings"
	webSettingsSignalTopic      = "fulfillment.web_carrier_settings"
	// webQuoteTimeout là hạn mỗi hãng khi báo phí cho web (A2-12).
	webQuoteTimeout = 3 * time.Second
	// estimateWindow, estimateMinShipments: ước thời gian giao lấy vận đơn giao trong 60 ngày, tỉnh đủ 20 vận đơn.
	estimateWindow       = 60 * 24 * time.Hour
	estimateMinShipments = 20
	// statsWindow là cửa sổ đếm vận đơn theo hãng cho màn hãng trên web.
	statsWindow = 30 * 24 * time.Hour
)

// Lỗi của hãng trên web (đổi sang HTTP ở internal/httpapi/errors_fulfillment.go).
var (
	// ErrCarrierNotOnWeb: tài khoản ưu tiên không có, không active hay không hiện trên web.
	ErrCarrierNotOnWeb = errors.New("fulfillment: tài khoản hãng không hiện trên website")
	// ErrLastWebCarrier: tắt show_on_web của tài khoản active cuối cùng đang hiện trên web (luật Task 450).
	ErrLastWebCarrier = errors.New("fulfillment: phải còn ít nhất một hãng trên website")
	// ErrNoQuote: không hãng nào báo phí được và shop chưa đặt phí dự phòng.
	ErrNoQuote = errors.New("fulfillment: không báo được phí giao hàng")
)

// WebCarrierSettings là cài đặt kênh web: tài khoản ưu tiên (nil: không ưu tiên, rẻ nhất đứng đầu), phí dự phòng
// (nil: không có), Version để sửa có kiểm phiên bản (chưa lưu lần nào: 1).
type WebCarrierSettings struct {
	PreferredAccountID *uuid.UUID
	FallbackFee        *int64
	Version            int
}

// AuditFields là giá trị đưa vào audit.
func (s WebCarrierSettings) AuditFields() map[string]any {
	m := map[string]any{"preferredAccountId": nil, "fallbackFee": nil, "version": s.Version}
	if s.PreferredAccountID != nil {
		m["preferredAccountId"] = s.PreferredAccountID.String()
	}
	if s.FallbackFee != nil {
		m["fallbackFee"] = *s.FallbackFee
	}
	return m
}

// WebCarrierSettingsChange là cài đặt trước và sau một lần lưu (bên gọi ghi audit).
type WebCarrierSettingsChange struct {
	Before, After WebCarrierSettings
}

// WebCarrierSettingsChanged là payload của TopicWebCarrierSettingsChanged.
type WebCarrierSettingsChanged struct {
	Version int `json:"version"`
}

// GetWebCarrierSettings đọc cài đặt kênh web của tenant đang chạy (tx trong tenancy.WithTenant), không khóa. Chưa có
// dòng → không ưu tiên, không phí dự phòng, version 1.
func GetWebCarrierSettings(ctx context.Context, tx pgx.Tx) (WebCarrierSettings, error) {
	r, err := store.New(tx).GetWebCarrierSettings(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return WebCarrierSettings{Version: 1}, nil
	}
	if err != nil {
		return WebCarrierSettings{}, fmt.Errorf("đọc cài đặt hãng trên web: %w", err)
	}
	return webSettingsOf(r.PreferredAccountID, r.FallbackFee, r.Version), nil
}

// SaveWebCarrierSettings ghi cả bộ in (in.Version là version đang sửa) trong tx của bên gọi: phí dự phòng ngoài
// 0–MaxWebFallbackFee → ErrSettingOutOfRange; tài khoản ưu tiên khóa trước (nhóm 8 ADR-46: carrier_accounts trước
// channel_carrier_settings), phải active và hiện trên web → không thì ErrCarrierNotOnWeb; chèn dòng mặc định nếu
// chưa có, khóa, so version (lệch → ErrVersionConflict), version + 1; phát TopicWebCarrierSettingsChanged và báo hiệu
// màn hình. Audit do bên gọi ghi. Chạy lại được trong db.RetryTx.
func (s *Service) SaveWebCarrierSettings(ctx context.Context, tx pgx.Tx, in WebCarrierSettings, a Actor,
) (WebCarrierSettingsChange, error) {
	if f := in.FallbackFee; f != nil && (*f < 0 || *f > MaxWebFallbackFee) {
		return WebCarrierSettingsChange{}, &SettingOutOfRangeError{Field: "fallbackFee"}
	}
	q := store.New(tx)
	if p := in.PreferredAccountID; p != nil {
		r, err := q.LockWebAccount(ctx, *p)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return WebCarrierSettingsChange{}, ErrCarrierNotOnWeb
		case err != nil:
			return WebCarrierSettingsChange{}, fmt.Errorf("khóa tài khoản hãng ưu tiên: %w", err)
		case r.Status != AccountActive || !r.ShowOnWeb:
			return WebCarrierSettingsChange{}, ErrCarrierNotOnWeb
		}
	}
	cur, err := s.lockWebSettings(ctx, q, a.TenantID)
	if err != nil {
		return WebCarrierSettingsChange{}, err
	}
	if cur.Version != in.Version {
		return WebCarrierSettingsChange{}, ErrVersionConflict
	}
	after, err := s.writeWebSettings(ctx, tx, q, in.PreferredAccountID, in.FallbackFee, a)
	if err != nil {
		return WebCarrierSettingsChange{}, err
	}
	return WebCarrierSettingsChange{Before: cur, After: after}, nil
}

// lockWebSettings chèn dòng mặc định nếu chưa có rồi khóa dòng kênh web (bảng cuối nhóm 8).
func (*Service) lockWebSettings(ctx context.Context, q *store.Queries, tenantID uuid.UUID) (WebCarrierSettings, error) {
	if err := q.EnsureWebCarrierSettings(ctx, tenantID); err != nil {
		return WebCarrierSettings{}, fmt.Errorf("tạo dòng cài đặt hãng trên web: %w", err)
	}
	r, err := q.LockWebCarrierSettings(ctx)
	if err != nil {
		return WebCarrierSettings{}, fmt.Errorf("khóa cài đặt hãng trên web: %w", err)
	}
	return webSettingsOf(r.PreferredAccountID, r.FallbackFee, r.Version), nil
}

// writeWebSettings ghi dòng đã khóa (version + 1), phát sự kiện, báo hiệu; trả cài đặt mới.
func (s *Service) writeWebSettings(ctx context.Context, tx pgx.Tx, q *store.Queries, preferred *uuid.UUID, fee *int64,
	a Actor,
) (WebCarrierSettings, error) {
	p := store.UpdateWebCarrierSettingsParams{UpdatedBy: a.UserID}
	if preferred != nil {
		p.PreferredAccountID = pgtype.UUID{Bytes: *preferred, Valid: true}
	}
	if fee != nil {
		p.FallbackFee = pgtype.Int8{Int64: *fee, Valid: true}
	}
	r, err := q.UpdateWebCarrierSettings(ctx, p)
	if err != nil {
		return WebCarrierSettings{}, fmt.Errorf("ghi cài đặt hãng trên web: %w", err)
	}
	after := webSettingsOf(r.PreferredAccountID, r.FallbackFee, r.Version)
	if s.d.Events == nil {
		return WebCarrierSettings{}, errors.New("fulfillment: chưa nối sổ sự kiện (Deps.Events)")
	}
	if _, _, err := s.d.Events.Emit(ctx, tx, s.queue(), events.Event{
		TenantID: a.TenantID, AggregateType: aggregateWebCarrierSettings, AggregateID: a.TenantID.String(),
		Seq: int64(after.Version), Topic: TopicWebCarrierSettingsChanged, Payload: WebCarrierSettingsChanged{Version: after.Version},
	}); err != nil {
		return WebCarrierSettings{}, fmt.Errorf("phát %s: %w", TopicWebCarrierSettingsChanged, err)
	}
	if s.d.Bus != nil {
		if err := s.d.Bus.Publish(ctx, tx, realtime.Signal{
			TenantID: a.TenantID, Topic: webSettingsSignalTopic, Requires: permAccountsRead,
			Entity: realtime.Entity{Type: aggregateWebCarrierSettings, ID: a.TenantID.String()},
		}); err != nil {
			return WebCarrierSettings{}, fmt.Errorf("báo hiệu cài đặt hãng trên web: %w", err)
		}
	}
	return after, nil
}

// webCarrierHidden chạy trong UpdateAccount khi tài khoản r (đã khóa) đang hiện trên web bị tắt show_on_web (luật
// Task 450): khóa dòng cài đặt (sau carrier_accounts, nhóm 8) để hai lệnh tắt cùng lúc đếm lần lượt; tài khoản active
// mà không còn tài khoản active nào khác hiện trên web → ErrLastWebCarrier; tài khoản đang là ưu tiên → bỏ ưu tiên
// (version + 1, sự kiện) cùng transaction.
func (s *Service) webCarrierHidden(ctx context.Context, tx pgx.Tx, q *store.Queries, id uuid.UUID, active bool, a Actor) error {
	cur, err := s.lockWebSettings(ctx, q, a.TenantID)
	if err != nil {
		return err
	}
	if active {
		n, err := q.OtherWebCarriers(ctx, id)
		if err != nil {
			return fmt.Errorf("đếm hãng còn hiện trên web: %w", err)
		}
		if n == 0 {
			return ErrLastWebCarrier
		}
	}
	if cur.PreferredAccountID == nil || *cur.PreferredAccountID != id {
		return nil
	}
	_, err = s.writeWebSettings(ctx, tx, q, nil, cur.FallbackFee, a)
	return err
}

func webSettingsOf(preferred pgtype.UUID, fee pgtype.Int8, version int32) WebCarrierSettings {
	s := WebCarrierSettings{Version: int(version)}
	if preferred.Valid {
		id := uuid.UUID(preferred.Bytes)
		s.PreferredAccountID = &id
	}
	if fee.Valid {
		f := fee.Int64
		s.FallbackFee = &f
	}
	return s
}

// WebCarrier là một hãng hiện trên website: chỉ mã tài khoản, khóa hãng, tên (không khóa API).
type WebCarrier struct {
	AccountID        uuid.UUID
	CarrierKey, Name string
	Preferred        bool
}

// WebCarriers trả các tài khoản active hiện trên web: tài khoản ưu tiên đứng đầu, còn lại theo web_sort rồi id. Ưu
// tiên đã lưu mà tài khoản không còn active, không hiện trên web thì bị bỏ qua.
func (*Service) WebCarriers(ctx context.Context, tx pgx.Tx) ([]WebCarrier, error) {
	set, err := GetWebCarrierSettings(ctx, tx)
	if err != nil {
		return nil, err
	}
	rows, err := store.New(tx).WebCarrierAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("đọc hãng trên web: %w", err)
	}
	out := make([]WebCarrier, 0, len(rows))
	for _, r := range rows {
		out = append(out, WebCarrier{
			AccountID: r.ID, CarrierKey: r.CarrierKey, Name: r.Name,
			Preferred: set.PreferredAccountID != nil && *set.PreferredAccountID == r.ID,
		})
	}
	slices.SortStableFunc(out, func(a, b WebCarrier) int { return preferredFirst(a.Preferred, b.Preferred) })
	return out, nil
}

func preferredFirst(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return -1
	}
	return 1
}

// WebCarrierStat là một tài khoản hiện trên web cho màn quản lý: trạng thái, thứ tự, số vận đơn chưa hủy 30 ngày.
type WebCarrierStat struct {
	AccountID                uuid.UUID
	CarrierKey, Name, Status string
	WebSort, Shipments30d    int
	Preferred                bool
}

// WebCarrierStats trả mọi tài khoản bật show_on_web (trừ revoked; kể cả tạm dừng, để màn hình báo) theo web_sort, kèm
// số vận đơn chưa hủy tạo trong 30 ngày tới now (mọi kênh bán).
func WebCarrierStats(ctx context.Context, tx pgx.Tx, set WebCarrierSettings, now time.Time) ([]WebCarrierStat, error) {
	rows, err := store.New(tx).WebCarrierStats(ctx, now.UTC().Add(-statsWindow))
	if err != nil {
		return nil, fmt.Errorf("đọc số vận đơn theo hãng trên web: %w", err)
	}
	out := make([]WebCarrierStat, 0, len(rows))
	for _, r := range rows {
		out = append(out, WebCarrierStat{
			AccountID: r.ID, CarrierKey: r.CarrierKey, Name: r.Name, Status: r.Status, WebSort: int(r.WebSort),
			Shipments30d: int(r.Shipments30d), Preferred: set.PreferredAccountID != nil && *set.PreferredAccountID == r.ID,
		})
	}
	return out, nil
}

// WebShipQuoteInput là đầu vào báo phí cho web: chỉ mã tỉnh, phường (mã CRM), khối lượng, giá trị hàng, tiền thu hộ —
// không tên, SĐT, địa chỉ chi tiết.
type WebShipQuoteInput struct {
	ProvinceCode, WardCode string
	WeightGrams            int
	Value, COD             int64
}

// ShipOption là một lựa chọn giao hàng cho web. Estimated = phí dự phòng của shop (AccountID rỗng, không hãng).
type ShipOption struct {
	AccountID                        uuid.UUID
	CarrierKey, CarrierName, Service string
	Fee                              int64
	Preferred, Estimated             bool
}

// QuoteForWeb báo phí cho website: đọc cài đặt và tài khoản active hiện trên web (đã giải mã, kèm bảng mã địa giới khi
// adapter cần) trong một transaction ngắn của tenantID, ĐÓNG transaction, rồi gọi QuoteNow cho từng tài khoản (từ
// địa chỉ lấy hàng của kho tài khoản, hạn 3 s mỗi hãng, song song). Mỗi dịch vụ của hãng báo được là một lựa chọn;
// hãng lỗi bị bỏ. Xếp: ưu tiên trước, rồi phí, rồi thứ tự hãng. Không lựa chọn nào: có phí dự phòng → một lựa chọn
// Estimated; không → ErrNoQuote.
func (s *Service) QuoteForWeb(ctx context.Context, d *db.DB, tenantID uuid.UUID, in WebShipQuoteInput) ([]ShipOption, error) {
	var (
		set   WebCarrierSettings
		names map[uuid.UUID]string
		accs  []Account
	)
	err := tenancy.WithTenant(ctx, d, tenantID, func(tx pgx.Tx) error {
		var err error
		if set, err = GetWebCarrierSettings(ctx, tx); err != nil {
			return err
		}
		cars, err := s.WebCarriers(ctx, tx)
		if err != nil {
			return err
		}
		names = make(map[uuid.UUID]string, len(cars))
		for _, c := range cars {
			names[c.AccountID] = c.Name
		}
		if accs, err = s.ActiveAccounts(ctx, tx, AccountFilter{ShowOnWeb: new(true)}); err != nil {
			return err
		}
		return s.attachLocations(ctx, tx, accs)
	}, db.TxAPI)
	if err != nil {
		return nil, fmt.Errorf("đọc hãng để báo phí cho web: %w", err)
	}
	quotes := make([]FeeQuote, len(accs))
	var wg sync.WaitGroup
	for i, acc := range accs {
		qi := QuoteInput{
			From: acc.Pickup, To: Address{ProvinceCode: in.ProvinceCode, WardCode: in.WardCode},
			Parcel: Parcel{WeightGrams: in.WeightGrams, DeclaredValue: in.Value}, COD: in.COD,
		}
		wg.Go(func() { quotes[i] = s.QuoteNow(ctx, []Account{acc}, qi, webQuoteTimeout)[0] })
	}
	wg.Wait()
	return webOptions(quotes, names, set)
}

// attachLocations gắn bảng mã địa giới cho tài khoản có adapter khai Locations (mỗi hãng đọc một lần).
func (s *Service) attachLocations(ctx context.Context, tx pgx.Tx, accs []Account) error {
	maps := map[string]LocationMap{}
	for i := range accs {
		key := accs[i].CarrierKey
		ad, ok := s.adapter(key)
		if !ok || !ad.Capabilities().Locations {
			continue
		}
		if _, done := maps[key]; !done {
			lm, err := s.LocationMap(ctx, tx, key)
			if err != nil {
				return err
			}
			maps[key] = lm
		}
		accs[i].Locations = maps[key]
	}
	return nil
}

// webOptions dựng lựa chọn từ kết quả báo phí (thứ tự quotes là thứ tự hãng: web_sort).
func webOptions(quotes []FeeQuote, names map[uuid.UUID]string, set WebCarrierSettings) ([]ShipOption, error) {
	type ranked struct {
		ShipOption
		order int
	}
	var all []ranked
	for i, fq := range quotes {
		if fq.ErrorCode != "" {
			continue
		}
		pref := set.PreferredAccountID != nil && *set.PreferredAccountID == fq.AccountID
		for _, r := range fq.Results {
			all = append(all, ranked{ShipOption{
				AccountID: fq.AccountID, CarrierKey: fq.CarrierKey, CarrierName: names[fq.AccountID], Service: r.Service,
				Fee: r.Total, Preferred: pref,
			}, i})
		}
	}
	if len(all) == 0 {
		if set.FallbackFee == nil {
			return nil, ErrNoQuote
		}
		return []ShipOption{{Fee: *set.FallbackFee, Estimated: true}}, nil
	}
	slices.SortStableFunc(all, func(a, b ranked) int {
		return cmp.Or(preferredFirst(a.Preferred, b.Preferred), cmp.Compare(a.Fee, b.Fee), cmp.Compare(a.order, b.order))
	})
	out := make([]ShipOption, len(all))
	for i, r := range all {
		out[i] = r.ShipOption
	}
	return out, nil
}

// DeliveryEstimates trả trung vị số ngày từ lúc hãng lấy hàng tới lúc giao theo mã tỉnh của địa chỉ giao, trên vận đơn
// giao trong 60 ngày tới now; tỉnh có ít hơn 20 vận đơn bị bỏ.
func (*Service) DeliveryEstimates(ctx context.Context, tx pgx.Tx, now time.Time) (map[string]float64, error) {
	rows, err := store.New(tx).DeliveryMedianDays(ctx, store.DeliveryMedianDaysParams{
		Since: now.UTC().Add(-estimateWindow), MinCount: estimateMinShipments,
	})
	if err != nil {
		return nil, fmt.Errorf("tính thời gian giao theo tỉnh: %w", err)
	}
	out := make(map[string]float64, len(rows))
	for _, r := range rows {
		out[r.ProvinceCode] = r.MedianDays
	}
	return out, nil
}
