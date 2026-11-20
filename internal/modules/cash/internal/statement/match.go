package statement

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/cash/internal/postings"
	"elimaz.com.vn/crm/internal/modules/cash/internal/store"
)

// Trạng thái bảng kê sau khi đọc (Task M3-32).
const (
	StatusMatched   = "matched"
	StatusConfirmed = "confirmed"
)

// Kết quả khớp của một dòng (cột match_status).
const (
	MatchPending        = "pending"
	MatchMatched        = "matched"
	MatchAmountMismatch = "amount_mismatch"
	MatchExternal       = "external"
	MatchDuplicate      = "duplicate"
	MatchFeeOnly        = "fee_only"
)

// kindFee là loại bút toán phí hãng (cùng giá trị với cash.KindFee).
const kindFee = "fee"

// Fact là một dòng bảng kê cùng vận đơn khớp được (nil: không có vận đơn của tài khoản hãng với mã đó) và trạng thái
// sổ của vận đơn lúc đọc: Delivered = giao xong toàn bộ (không giao một phần); Holding = số dư carrier_holding;
// Remitted = đã có bút toán remitted chưa bị đảo.
type Fact struct {
	LineID     uuid.UUID
	LineNo     int32
	COD, Fee   int64
	OtherFee   int64
	Posted     bool
	ShipmentID *uuid.UUID
	Delivered  bool
	Holding    int64
	Remitted   bool
}

// Verdict là kết quả khớp của một dòng: Expected = carrier_holding lúc khớp, Diff = COD của dòng − Expected (nil khi
// external).
type Verdict struct {
	Status         string
	Expected, Diff *int64
}

// Classify xếp một dòng theo thứ tự (kế hoạch M3-32): không có vận đơn → external (TI06); COD 0 mà phí (fee + other)
// khác 0 → fee_only; vận đơn chưa giao xong (hay giao một phần) mà dòng có COD → amount_mismatch; carrier_holding 0
// mà đã có remitted → duplicate; COD = carrier_holding → matched; khác → amount_mismatch. holding, remitted là trạng
// thái sổ hiện tại của vận đơn (đã cộng các dòng trước của cùng lô).
func Classify(f Fact, holding int64, remitted bool) Verdict {
	if f.ShipmentID == nil {
		return Verdict{Status: MatchExternal}
	}
	exp, diff := holding, f.COD-holding
	v := Verdict{Expected: &exp, Diff: &diff}
	switch {
	case f.COD == 0 && f.Fee+f.OtherFee != 0:
		v.Status = MatchFeeOnly
	case !f.Delivered && f.COD > 0:
		v.Status = MatchAmountMismatch
	case holding == 0 && remitted:
		v.Status = MatchDuplicate
	case f.COD == holding:
		v.Status = MatchMatched
	default:
		v.Status = MatchAmountMismatch
	}
	return v
}

// Entry là một vế bút toán (ngăn, số đồng có dấu).
type Entry struct {
	Account string
	Amount  int64
}

// Entries trả bút toán của dòng theo kết quả khớp status: matched, amount_mismatch → remitted carrier_holding −C,
// shop_received +(C − fee − other), carrier_fee +(fee + other) với C = COD của dòng; fee_only → fee carrier_fee
// +(fee + other), shop_received −(fee + other). Phí âm (hoàn phí) giữ dấu. Bỏ vế 0 đồng; không vế nào (mọi số 0) hay
// kết quả khác → kind rỗng.
func Entries(f Fact, status string) (kind string, es []Entry) {
	fee := f.Fee + f.OtherFee
	switch status {
	case MatchMatched, MatchAmountMismatch:
		kind = postings.KindRemitted
		es = nonZero(Entry{postings.AccountCarrierHolding, -f.COD}, Entry{postings.AccountShopReceived, f.COD - fee},
			Entry{postings.AccountCarrierFee, fee})
	case MatchFeeOnly:
		kind = kindFee
		es = nonZero(Entry{postings.AccountCarrierFee, fee}, Entry{postings.AccountShopReceived, -fee})
	}
	if len(es) == 0 {
		return "", nil
	}
	return kind, es
}

func nonZero(es ...Entry) []Entry {
	out := es[:0]
	for _, e := range es {
		if e.Amount != 0 {
			out = append(out, e)
		}
	}
	return out
}

// ledger là trạng thái sổ của các vận đơn trong một lần khớp hay một lô, cộng dần theo các dòng đã xét.
type ledger map[uuid.UUID]*shipmentState

type shipmentState struct {
	holding  int64
	remitted bool
}

// verdict xếp f trên trạng thái hiện tại của vận đơn và cộng bút toán của dòng vào trạng thái đó; trả kết quả, bút
// toán sẽ ghi và carrier_holding trước, sau dòng.
func (l ledger) verdict(f Fact) (v Verdict, kind string, es []Entry, before, after int64) {
	if f.ShipmentID == nil {
		return Classify(f, 0, false), "", nil, 0, 0
	}
	st, ok := l[*f.ShipmentID]
	if !ok {
		st = &shipmentState{holding: f.Holding, remitted: f.Remitted}
		l[*f.ShipmentID] = st
	}
	before = st.holding
	v = Classify(f, st.holding, st.remitted)
	kind, es = Entries(f, v.Status)
	for _, e := range es {
		if e.Account == postings.AccountCarrierHolding {
			st.holding += e.Amount
		}
	}
	if kind == postings.KindRemitted {
		st.remitted = true
	}
	return v, kind, es, before, st.holding
}

// Match khớp mọi dòng của bảng kê id (đang parsed) trong tx của bên gọi: tìm vận đơn theo (tài khoản hãng của bảng
// kê, mã vận đơn), xếp từng dòng theo line_no bằng Classify trên số dư cộng dần (xem trước: bút toán thật ghi khi chốt),
// rồi parsed → matched. Trả version mới; bảng kê không còn parsed (sự kiện giao lại) → ok = false, không đổi gì.
func Match(ctx context.Context, tx pgx.Tx, id uuid.UUID) (version int64, ok bool, err error) {
	q := store.New(tx)
	st, err := q.LockParsedStatement(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("khóa bảng kê %s để khớp: %w", id, err)
	}
	rows, err := q.MatchFacts(ctx, store.MatchFactsParams{CarrierAccountID: st.CarrierAccountID, StatementID: id})
	if err != nil {
		return 0, false, fmt.Errorf("đọc dòng bảng kê %s để khớp: %w", id, err)
	}
	facts := make([]Fact, len(rows))
	for i, r := range rows {
		facts[i] = Fact{
			LineID: r.ID, LineNo: r.LineNo, COD: r.CodAmount, Fee: r.FeeAmount, OtherFee: r.OtherFee,
			Posted: r.PostedTransferID.Valid, ShipmentID: uuidPtr(r.ShipmentID), Delivered: r.Delivered,
			Holding: r.CarrierHolding, Remitted: r.Remitted,
		}
	}
	res := newResults(len(facts))
	l := ledger{}
	for _, f := range facts {
		v, _, _, _, _ := l.verdict(f)
		res.add(f, v, uuid.Nil)
	}
	if err := q.SetLineResults(ctx, res.p); err != nil {
		return 0, false, fmt.Errorf("ghi kết quả khớp bảng kê %s: %w", id, err)
	}
	version, err = q.MarkMatched(ctx, id)
	if err != nil {
		return 0, false, fmt.Errorf("ghi bảng kê %s đã khớp: %w", id, err)
	}
	return version, true, nil
}

// results gom kết quả của nhiều dòng thành tham số của SetLineResults.
type results struct{ p store.SetLineResultsParams }

func newResults(n int) *results {
	return &results{p: store.SetLineResultsParams{
		Ids: make([]uuid.UUID, 0, n), Statuses: make([]string, 0, n), ShipmentIds: make([]uuid.UUID, 0, n),
		HasAmounts: make([]bool, 0, n), Expected: make([]int64, 0, n), Diffs: make([]int64, 0, n),
		Posted: make([]uuid.UUID, 0, n),
	}}
}

func (r *results) add(f Fact, v Verdict, posted uuid.UUID) {
	var sh uuid.UUID
	if f.ShipmentID != nil {
		sh = *f.ShipmentID
	}
	var exp, diff int64
	if v.Expected != nil {
		exp, diff = *v.Expected, *v.Diff
	}
	r.p.Ids, r.p.Statuses, r.p.ShipmentIds = append(r.p.Ids, f.LineID), append(r.p.Statuses, v.Status), append(r.p.ShipmentIds, sh)
	r.p.HasAmounts, r.p.Expected, r.p.Diffs = append(r.p.HasAmounts, v.Expected != nil), append(r.p.Expected, exp), append(r.p.Diffs, diff)
	r.p.Posted = append(r.p.Posted, posted)
}

func nullUUID(id uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: id, Valid: id != uuid.Nil} }
