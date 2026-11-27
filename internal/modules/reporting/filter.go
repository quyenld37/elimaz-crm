package reporting

import (
	"fmt"
	"regexp"

	"github.com/google/uuid"
)

// Basis là cơ sở ngày của báo cáo đơn (enum ReportBasis của hợp đồng): placed theo ngày lên đơn, completed theo ngày
// giao thành công hay hoàn về kho (Q4).
type Basis string

// Các cơ sở ngày.
const (
	BasisPlaced    Basis = "placed"
	BasisCompleted Basis = "completed"
)

// Dimension là chiều nhóm của báo cáo đơn (enum ReportDimension).
type Dimension string

// Các chiều nhóm.
const (
	DimStatus    Dimension = "status"
	DimSource    Dimension = "source"
	DimCareBy    Dimension = "care_by"
	DimCreatedBy Dimension = "created_by"
	DimChannel   Dimension = "channel"
	DimPage      Dimension = "page"
	DimCarrier   Dimension = "carrier"
	DimDay       Dimension = "day"
)

// Metric là cột số mà danh sách chi tiết mở (enum ReportMetric).
type Metric string

// Các cột số.
const (
	MetricPlaced           Metric = "placed"
	MetricPlacedActive     Metric = "placed_active"
	MetricCancelled        Metric = "cancelled"
	MetricDelivered        Metric = "delivered"
	MetricReturned         Metric = "returned"
	MetricCustomerReturned Metric = "customer_returned"
	MetricPendingConfirm   Metric = "pending_confirm"
	MetricPendingShip      Metric = "pending_ship"
	MetricCancelRequested  Metric = "cancel_requested"
)

// UnknownKey là khóa của nhóm "Không xác định": kênh, nguồn, nhân viên, Page, hãng rỗng (C03: không tự gán Facebook).
const UnknownKey = "unknown"

// OrderFilter là bộ lọc chung của báo cáo bán hàng. Mỗi lát rỗng nghĩa là không lọc theo trường đó.
type OrderFilter struct {
	Period                                Period
	Basis                                 Basis
	Channels                              []string
	SourceIDs, CareBy, CreatedBy, PageIDs []uuid.UUID
	CarrierKeys                           []string
}

// Cell là một ô được bấm: chiều và khóa của dòng (UnknownKey cho nhóm không xác định).
type Cell struct {
	Dimension Dimension
	Key       string
}

// ParseBasis đọc cơ sở ngày; giá trị lạ → ErrBadParams.
func ParseBasis(s string) (Basis, error) {
	if err := checkEnum(Basis(s), basisSet, "cơ sở ngày"); err != nil {
		return "", err
	}
	return Basis(s), nil
}

// ParseDimension đọc chiều nhóm; giá trị lạ → ErrBadParams.
func ParseDimension(s string) (Dimension, error) {
	if err := checkEnum(Dimension(s), dimensionSet, "chiều nhóm"); err != nil {
		return "", err
	}
	return Dimension(s), nil
}

// ParseMetric đọc cột số; giá trị lạ → ErrBadParams.
func ParseMetric(s string) (Metric, error) {
	if err := checkEnum(Metric(s), metricSet, "cột số"); err != nil {
		return "", err
	}
	return Metric(s), nil
}

// checkEnum trả ErrBadParams khi v không có trong set.
func checkEnum[T ~string](v T, set map[T]bool, what string) error {
	if !set[v] {
		return fmt.Errorf("%s %q: %w", what, string(v), ErrBadParams)
	}
	return nil
}

//nolint:gochecknoglobals // bảng hằng chỉ đọc
var (
	basisSet     = map[Basis]bool{BasisPlaced: true, BasisCompleted: true}
	dimensionSet = map[Dimension]bool{
		DimStatus: true, DimSource: true, DimCareBy: true, DimCreatedBy: true,
		DimChannel: true, DimPage: true, DimCarrier: true, DimDay: true,
	}
	metricSet = map[Metric]bool{
		MetricPlaced: true, MetricPlacedActive: true, MetricCancelled: true, MetricDelivered: true, MetricReturned: true,
		MetricCustomerReturned: true, MetricPendingConfirm: true, MetricPendingShip: true, MetricCancelRequested: true,
	}
	// channelSet: kênh của đơn (CHECK orders_channel_check).
	channelSet = map[string]bool{"facebook": true, "web": true, "manual": true}
	// carrierKeyRe: khóa hãng (CHECK carrier_accounts_carrier_key_check).
	carrierKeyRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
)
