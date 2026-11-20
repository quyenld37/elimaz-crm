package statement

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/cash/internal/store"
)

// ErrChanged báo bảng kê đã đổi (version khác, hay không còn matched) từ lúc người chốt đọc (TI07).
var ErrChanged = errors.New("cash: bảng kê đã đổi")

// PostBatchSize là số dòng có vận đơn của một lô ghi tiền (một transaction): ≤ 200 khóa tư vấn mỗi transaction.
const PostBatchSize = 200

// Confirmed là bảng kê vừa chốt và tổng thực nhận của các dòng sẽ ghi tiền.
type Confirmed struct {
	Statement   Statement
	PostableNet int64
}

// Confirm chốt bảng kê id khi version còn khớp và đang matched, người chốt by, trong tx của bên gọi: không có →
// ErrNotFound; version khác hay không còn matched → ErrChanged. Không ghi bút toán: lô ghi tiền (PostBatch) chạy sau
// commit.
func Confirm(ctx context.Context, tx pgx.Tx, id uuid.UUID, version int64, by uuid.UUID) (Confirmed, error) {
	q := store.New(tx)
	row, err := q.ConfirmStatement(ctx, store.ConfirmStatementParams{
		ConfirmedBy: nullUUID(by), ID: id, Version: version,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := Get(ctx, tx, id); err != nil {
			return Confirmed{}, err
		}
		return Confirmed{}, fmt.Errorf("chốt bảng kê %s: %w", id, ErrChanged)
	}
	if err != nil {
		return Confirmed{}, fmt.Errorf("chốt bảng kê %s: %w", id, err)
	}
	net, err := q.PostableNet(ctx, id)
	if err != nil {
		return Confirmed{}, fmt.Errorf("đọc tổng thực nhận bảng kê %s: %w", id, err)
	}
	return Confirmed{Statement: statementOf(row), PostableNet: net}, nil
}

// Posting là một bút toán cần ghi cho một dòng bảng kê (khóa chống trùng remit:<dòng>, tác nhân người chốt).
type Posting struct {
	ShipmentID, LineID, ActorID uuid.UUID
	Kind, Key                   string
	Entries                     []Entry
}

// PostFunc ghi p trong tx (cash.PostWithID) và trả mã bút toán.
type PostFunc func(ctx context.Context, tx pgx.Tx, p Posting) (uuid.UUID, error)

// LockFunc lấy khóa tư vấn sổ COD của vận đơn đến hết tx (cùng khóa với consumer sổ COD, ADR-46).
type LockFunc func(ctx context.Context, tx pgx.Tx, shipmentID uuid.UUID) error

// Settled là vận đơn có carrier_holding về 0 nhờ dòng LineID.
type Settled struct{ ShipmentID, LineID uuid.UUID }

// Batch là kết quả một lô: Version = version mới của bảng kê (số thứ tự sự kiện lô sau), Last = line_no cuối của lô,
// More = còn lô sau; Settled theo thứ tự dòng. Done = false khi bảng kê không còn confirmed (không làm gì).
type Batch struct {
	Done     bool
	Version  int64
	Last     int32
	More     bool
	Settled  []Settled
	Postings int
}

// PostBatch ghi tiền cho lô dòng có vận đơn kế tiếp sau line_no after của bảng kê id đã chốt, trong tx của bên gọi
// (ADR-46): đọc lô (không khóa) → khóa tư vấn từng vận đơn theo mã tăng dần → khóa bảng kê (tăng version) → khóa dòng
// theo line_no → đọc lại số dư mới nhất → xếp lại từng dòng bằng Classify trên số dư cộng dần (dòng đã khớp mà nay
// vận đơn đã được trả đủ bởi bảng kê khác → duplicate, không ghi) → ghi bút toán của dòng chưa có bút toán bằng post
// (khóa remit:<dòng>) → ghi kết quả và posted_transfer_id. Dòng đã có bút toán (sự kiện giao lại) bị bỏ qua.
func PostBatch(ctx context.Context, tx pgx.Tx, id uuid.UUID, after int32, lock LockFunc, post PostFunc) (Batch, error) {
	q := store.New(tx)
	lines, err := lockBatchShipments(ctx, tx, id, after, lock)
	if err != nil {
		return Batch{}, err
	}
	head, err := q.TouchConfirmed(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Batch{}, nil
	}
	if err != nil {
		return Batch{}, fmt.Errorf("khóa bảng kê %s để ghi tiền: %w", id, err)
	}
	b := Batch{Done: true, Version: head.Version}
	if len(lines) == 0 {
		return b, nil
	}
	from, to := lines[0].LineNo, lines[len(lines)-1].LineNo
	b.Last, b.More = to, len(lines) == PostBatchSize
	if _, err := q.LockLineRange(ctx, store.LockLineRangeParams{StatementID: id, FromLineNo: from, ToLineNo: to}); err != nil {
		return Batch{}, fmt.Errorf("khóa dòng bảng kê %s: %w", id, err)
	}
	rows, err := q.PostFacts(ctx, store.PostFactsParams{StatementID: id, FromLineNo: from, ToLineNo: to})
	if err != nil {
		return Batch{}, fmt.Errorf("đọc số dư cho lô bảng kê %s: %w", id, err)
	}
	var actor uuid.UUID
	if head.ConfirmedBy.Valid {
		actor = head.ConfirmedBy.Bytes
	}
	res, err := postLines(ctx, tx, id, rows, actor, post, &b)
	if err != nil {
		return Batch{}, err
	}
	if err := q.SetLineResults(ctx, res.p); err != nil {
		return Batch{}, fmt.Errorf("ghi kết quả lô bảng kê %s: %w", id, err)
	}
	return b, nil
}

// lockBatchShipments đọc lô dòng có vận đơn kế tiếp sau after (không khóa) rồi lấy khóa tư vấn từng vận đơn của lô
// theo mã tăng dần (ADR-46: trước mọi khóa dòng nhóm 9).
func lockBatchShipments(ctx context.Context, tx pgx.Tx, id uuid.UUID, after int32, lock LockFunc) ([]store.PostBatchLinesRow, error) {
	lines, err := store.New(tx).PostBatchLines(ctx, store.PostBatchLinesParams{StatementID: id, AfterLineNo: after, Lim: PostBatchSize})
	if err != nil {
		return nil, fmt.Errorf("đọc lô dòng bảng kê %s: %w", id, err)
	}
	ships := make([]uuid.UUID, 0, len(lines))
	for _, l := range lines {
		ships = append(ships, l.ShipmentID)
	}
	slices.SortFunc(ships, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	for _, s := range slices.Compact(ships) {
		if err := lock(ctx, tx, s); err != nil {
			return nil, fmt.Errorf("khóa sổ COD của vận đơn: %w", err)
		}
	}
	return lines, nil
}

// postLines xếp lại từng dòng chưa có bút toán của lô trên số dư cộng dần, ghi bút toán bằng post (tác nhân actor),
// đếm vào b số bút toán và vận đơn về 0; trả kết quả cần ghi lại vào dòng.
func postLines(ctx context.Context, tx pgx.Tx, id uuid.UUID, rows []store.PostFactsRow, actor uuid.UUID, post PostFunc, b *Batch) (*results, error) {
	res := newResults(len(rows))
	l := ledger{}
	for _, r := range rows {
		if r.PostedTransferID.Valid {
			continue // sự kiện giao lại: dòng đã ghi, số dư đã có trong sổ
		}
		f := Fact{
			LineID: r.ID, LineNo: r.LineNo, COD: r.CodAmount, Fee: r.FeeAmount, OtherFee: r.OtherFee,
			ShipmentID: &r.ShipmentID, Delivered: r.Delivered, Holding: r.CarrierHolding, Remitted: r.Remitted,
		}
		v, kind, es, before, afterHolding := l.verdict(f)
		var posted uuid.UUID
		if kind != "" {
			var err error
			posted, err = post(ctx, tx, Posting{
				ShipmentID: r.ShipmentID, LineID: r.ID, ActorID: actor, Kind: kind, Key: "remit:" + r.ID.String(), Entries: es,
			})
			if err != nil {
				return nil, fmt.Errorf("ghi tiền dòng %d của bảng kê %s: %w", r.LineNo, id, err)
			}
			b.Postings++
			if before != 0 && afterHolding == 0 {
				b.Settled = append(b.Settled, Settled{ShipmentID: r.ShipmentID, LineID: r.ID})
			}
		}
		res.add(f, v, posted)
	}
	return res, nil
}
