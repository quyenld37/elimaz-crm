// Package adjust là điều chỉnh sổ COD có xem trước (kế hoạch M3, Task M3-33; spec 05 mục 2 "Sửa sai"; TI08): từ trạng
// thái sổ của một vận đơn và yêu cầu (đảo một bút toán, hay chuyển một số tiền giữa hai ngăn) dựng bút toán sẽ ghi,
// số dư trước, sau, cảnh báo và token của bản xem trước. Không đọc, không ghi database: module cash đọc trạng thái
// (sau khóa sổ của vận đơn khi ghi) và ghi bằng cash.Reverse, cash.PostWithID.
package adjust

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/cash/internal/postings"
)

// Chế độ điều chỉnh (CodAdjustmentRequest.mode).
const (
	ModeReverse = "reverse"
	ModeMove    = "move"
)

// Loại bút toán sẽ ghi (khớp hằng của cash).
const (
	KindAdjustment = "adjustment"
	KindReversal   = "reversal"
)

const (
	// MaxReason là số ký tự lý do lớn nhất (CHECK length(reason) <= 500 của cash.cod_transfers).
	MaxReason = 500
	// maxAmount là 2^53 − 1 (khớp CHECK của dòng sổ).
	maxAmount = 1<<53 - 1
)

var (
	// ErrInvalid báo yêu cầu sai: chế độ lạ, thiếu bút toán cần đảo, ngăn lạ hay trùng, số tiền ngoài 1..2^53−1, trường
	// thừa của chế độ khác, lý do quá 500 ký tự.
	ErrInvalid = errors.New("cash: yêu cầu điều chỉnh không hợp lệ")
	// ErrReasonRequired báo thiếu lý do điều chỉnh.
	ErrReasonRequired = errors.New("cash: điều chỉnh cần lý do")
	// ErrTargetNotFound báo bút toán cần đảo không có trong sổ của vận đơn.
	ErrTargetNotFound = errors.New("cash: vận đơn không có bút toán cần đảo")
	// ErrBlocked báo bút toán không được đảo: đã bị đảo, là bút toán đảo, hay là expected của vận đơn chưa kết thúc
	// (khóa expected:<vận đơn> cố định: đảo xong, sự kiện hãng sau đó không ghi lại được).
	ErrBlocked = errors.New("cash: không được đảo bút toán này")
)

// Transfer là một bút toán đã có của vận đơn: đã bị đảo chưa, các vế, lúc chốt của bảng kê đã chốt sinh ra nó (nil:
// không thuộc bảng kê đã chốt).
type Transfer struct {
	ID          uuid.UUID
	Kind        string
	Reversed    bool
	Entries     []postings.Entry
	ConfirmedAt *time.Time
}

// State là sổ COD hiện tại của một vận đơn: trạng thái vận đơn, số dư, bút toán theo thứ tự ghi.
type State struct {
	ShipmentID uuid.UUID
	Status     string
	Balances   postings.Balances
	Transfers  []Transfer
}

// Request là yêu cầu điều chỉnh: reverse cần ReversesTransferID; move cần From, To, Amount; Reason bắt buộc.
type Request struct {
	Mode               string
	ReversesTransferID *uuid.UUID
	From, To           string
	Amount             int64
	Reason             string
}

// Plan là bản xem trước: bút toán sẽ ghi (Kind, Entries, ReversesTransferID với reversal), số dư trước, sau, cảnh báo,
// Token (băm của trạng thái sổ và bút toán; trạng thái đổi thì token đổi).
type Plan struct {
	Before, After      postings.Balances
	Kind               string
	Entries            []postings.Entry
	ReversesTransferID *uuid.UUID
	Warnings           []string
	Token              string
}

// vnZone là giờ Việt Nam (UTC+7, không đổi giờ) cho ngày chốt trong cảnh báo.
var vnZone = time.FixedZone("ICT", 7*3600) //nolint:gochecknoglobals // hằng múi giờ

// Preview dựng bản xem trước của r trên sổ st. Sai yêu cầu → ErrInvalid, ErrReasonRequired; bút toán cần đảo không thuộc
// vận đơn → ErrTargetNotFound; không được đảo → ErrBlocked.
func Preview(st State, r Request) (Plan, error) {
	if err := r.validate(); err != nil {
		return Plan{}, err
	}
	p := Plan{Before: st.Balances}
	switch r.Mode {
	case ModeReverse:
		t, err := target(st, *r.ReversesTransferID)
		if err != nil {
			return Plan{}, err
		}
		p.Kind, p.ReversesTransferID = KindReversal, new(t.ID)
		for _, e := range t.Entries {
			p.Entries = append(p.Entries, postings.Entry{Account: e.Account, Amount: -e.Amount})
		}
		if t.ConfirmedAt != nil {
			p.Warnings = append(p.Warnings, "Bút toán này thuộc bảng kê đã chốt ngày "+
				t.ConfirmedAt.In(vnZone).Format("02/01/2006"))
		}
	case ModeMove:
		p.Kind = KindAdjustment
		p.Entries = []postings.Entry{{Account: r.From, Amount: -r.Amount}, {Account: r.To, Amount: r.Amount}}
	}
	p.After = st.Balances
	for _, e := range p.Entries {
		add(&p.After, e)
	}
	for _, a := range []struct {
		v     int64
		label string
	}{
		{p.After.ToCollect, "Đang đi thu"},
		{p.After.CarrierHolding, "Hãng đang giữ"},
		{p.After.ShopReceived, "Đã về shop"},
		{p.After.CarrierFee, "Phí hãng"},
	} {
		if a.v < 0 {
			p.Warnings = append(p.Warnings, "Sau điều chỉnh, ngăn "+a.label+" sẽ âm")
		}
	}
	p.Token = token(st, p)
	return p, nil
}

func (r Request) validate() error {
	if strings.TrimSpace(r.Reason) == "" {
		return ErrReasonRequired
	}
	if utf8.RuneCountInString(r.Reason) > MaxReason {
		return fmt.Errorf("%w: lý do quá %d ký tự", ErrInvalid, MaxReason)
	}
	switch r.Mode {
	case ModeReverse:
		if r.ReversesTransferID == nil || *r.ReversesTransferID == uuid.Nil || r.From != "" || r.To != "" || r.Amount != 0 {
			return fmt.Errorf("%w: đảo cần đúng mã bút toán cần đảo, không ngăn, số tiền", ErrInvalid)
		}
	case ModeMove:
		switch {
		case r.ReversesTransferID != nil:
			return fmt.Errorf("%w: chuyển ngăn không có bút toán cần đảo", ErrInvalid)
		case !isAccount(r.From) || !isAccount(r.To) || r.From == r.To:
			return fmt.Errorf("%w: ngăn %q → %q", ErrInvalid, r.From, r.To)
		case r.Amount <= 0 || r.Amount > maxAmount:
			return fmt.Errorf("%w: số tiền %d", ErrInvalid, r.Amount)
		}
	default:
		return fmt.Errorf("%w: chế độ %q", ErrInvalid, r.Mode)
	}
	return nil
}

// target trả bút toán id của st khi được đảo.
func target(st State, id uuid.UUID) (Transfer, error) {
	for _, t := range st.Transfers {
		if t.ID != id {
			continue
		}
		switch {
		case t.Reversed:
			return Transfer{}, fmt.Errorf("%w: bút toán đã bị đảo", ErrBlocked)
		case t.Kind == KindReversal:
			return Transfer{}, fmt.Errorf("%w: không đảo một bút toán đảo (ghi chuyển ngăn)", ErrBlocked)
		case t.Kind == postings.KindExpected && !finished(st.Status):
			return Transfer{}, fmt.Errorf("%w: expected của vận đơn chưa kết thúc (%s)", ErrBlocked, st.Status)
		}
		return t, nil
	}
	return Transfer{}, ErrTargetNotFound
}

// finished báo vận đơn đã kết thúc: không còn sự kiện hãng nào cần ghi lại expected.
func finished(status string) bool {
	return status == "delivered" || status == "returned" || status == "cancelled"
}

func isAccount(a string) bool {
	return slices.Contains([]string{
		postings.AccountToCollect, postings.AccountCarrierHolding, postings.AccountShopReceived,
		postings.AccountCarrierFee, postings.AccountSalesClearing,
	}, a)
}

func add(b *postings.Balances, e postings.Entry) {
	switch e.Account {
	case postings.AccountToCollect:
		b.ToCollect += e.Amount
	case postings.AccountCarrierHolding:
		b.CarrierHolding += e.Amount
	case postings.AccountShopReceived:
		b.ShopReceived += e.Amount
	case postings.AccountCarrierFee:
		b.CarrierFee += e.Amount
	case postings.AccountSalesClearing:
		b.SalesClearing += e.Amount
	}
}

// token băm vận đơn, trạng thái, mọi bút toán (mã, đã bị đảo) và bút toán sẽ ghi: có bút toán mới, bút toán bị đảo,
// vận đơn đổi trạng thái hay yêu cầu khác → token khác. Lý do không vào token (sửa lý do không làm bản xem trước cũ).
func token(st State, p Plan) string {
	b := fmt.Appendf(nil, "%s|%s|", st.ShipmentID, st.Status)
	for _, t := range st.Transfers {
		b = fmt.Appendf(b, "%s:%t;", t.ID, t.Reversed)
	}
	b = fmt.Appendf(b, "|%s|", p.Kind)
	if p.ReversesTransferID != nil {
		b = fmt.Appendf(b, "%s", *p.ReversesTransferID)
	}
	for _, e := range p.Entries {
		b = fmt.Appendf(b, "|%s=%d", e.Account, e.Amount)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
