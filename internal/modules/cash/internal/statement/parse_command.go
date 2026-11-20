package statement

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/cash/internal/formats"
	"elimaz.com.vn/crm/internal/modules/cash/internal/store"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

const (
	parseKindName = "command.cash.statement_parse"
	parseTimeout  = 25 * time.Second
	// batchSize: số dòng ghi trong một transaction.
	batchSize = 1000
)

// Mã lỗi đọc tệp do lệnh ghi (ngoài lỗi định dạng của Parse).
const (
	CodeFileUnavailable = "file_unavailable" // tệp không có, đã xóa, chưa tải xong hay mất trên kho
	CodeUnknownFormat   = "unknown_format"   // định dạng của bảng kê không còn trong sổ định dạng
	CodeReadFailed      = "read_failed"      // hết lượt chạy lại vì lỗi tạm (database, kho tệp)
)

// Lý do ngắn ghi vào last_error của lệnh (không nội dung tệp).
const (
	reasonBadPayload = "bad_payload"
	reasonNotWired   = "not_wired"
	reasonNoStmt     = "statement_not_found"
	reasonLoad       = "load_failed"
	reasonRead       = "read_failed"
	reasonBatch      = "batch_failed"
	reasonRecord     = "record_failed"
	reasonFailed     = "parse_failed"
)

// ParseKind là lệnh đọc tệp bảng kê command.cash.statement_parse (commands.CommandKind: giữ chỗ hết hạn thì
// in_doubt, 8 lượt; không hết hạn chờ; một lần chạy tối đa 25 s).
func ParseKind() queue.Kind { return commands.CommandKind(parseKindName, 0, parseTimeout) }

// Parser là lệnh ParseKind (commands.Handler, Looker, DoneHook, queue.TerminalHook). Execute, ngoài transaction: đọc
// bảng kê (imported → parsing), đọc tệp bằng files.ReadContent trong Limits.MaxBytes, phân tích bằng Parse, rồi ghi
// dòng theo lô 1.000 dòng mỗi transaction (dòng đã có bị bỏ qua: chạy lại an toàn). Lỗi định dạng ghi parse_failed
// kèm parse_error rồi trả rejected; lỗi tạm trả not_sent (tự chạy lại). OnDone (tx của Runner) đặt parsed, line_count,
// kỳ bảng kê. Lookup: bảng kê đã parsed trở đi → done, ngược lại not_sent (chạy lại an toàn).
type Parser struct {
	db         *db.DB
	files      files.FileStore
	reg        *formats.Registry
	purpose    string
	lim        Limits
	parsed     ParsedFunc
	afterBatch func(written int) error // test: chạy sau mỗi lô đã commit; lỗi → Execute trả lỗi (in_doubt)
}

// ParsedFunc chạy trong tx của OnDone khi bảng kê id của tenant vừa thành parsed với version mới version (xếp việc khớp
// dòng, Task M3-32).
type ParsedFunc func(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, version int64) error

// NewParser dựng lệnh đọc tệp: d (crm_app), fs (kho tệp; nil → not_sent cho tới khi nối), reg (sổ định dạng),
// purpose (mục đích tệp bảng kê), parsed (chạy khi bảng kê vừa đọc xong; nil: không làm gì).
func NewParser(d *db.DB, fs files.FileStore, reg *formats.Registry, purpose string, parsed ParsedFunc) *Parser {
	return &Parser{db: d, files: fs, reg: reg, purpose: purpose, lim: DefaultLimits(), parsed: parsed}
}

// Kind implements commands.Handler.
func (*Parser) Kind() queue.Kind { return ParseKind() }

// AutoRetryNotSent implements commands.Handler: lỗi tạm thì chạy lại (chỉ đọc kho, ghi có điều kiện).
func (*Parser) AutoRetryNotSent() bool { return true }

func notSent(reason string) commands.Outcome {
	return commands.Outcome{Status: commands.StatusNotSent, Reason: reason}
}

func rejected(reason string) commands.Outcome {
	return commands.Outcome{Status: commands.StatusRejected, Reason: reason}
}

// target đọc tenant và mã bảng kê của lệnh.
func target(j queue.Job) (uuid.UUID, uuid.UUID, bool) {
	var p parsePayload
	if j.TenantID == nil || json.Unmarshal(j.Payload, &p) != nil || p.StatementID == uuid.Nil {
		return uuid.Nil, uuid.Nil, false
	}
	return *j.TenantID, p.StatementID, true
}

// Execute implements commands.Handler.
func (p *Parser) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	tenant, id, ok := target(j)
	if !ok {
		return rejected(reasonBadPayload), nil
	}
	if p.files == nil {
		return notSent(reasonNotWired), nil
	}
	var st store.CashCodStatement
	err := tenancy.WithTenant(ctx, p.db, tenant, func(tx pgx.Tx) error {
		q := store.New(tx)
		var err error
		if st, err = q.GetStatement(ctx, id); err != nil {
			return err //nolint:wrapcheck // phân loại ở dưới
		}
		return q.StartParsing(ctx, id)
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return rejected(reasonNoStmt), nil
	case err != nil:
		return notSent(reasonLoad), nil
	case st.Status == StatusParseFailed:
		return rejected(reasonFailed), nil
	case st.Status != StatusImported && st.Status != StatusParsing:
		return commands.Outcome{Status: commands.StatusDone}, nil // đã đọc xong ở lần trước
	}
	f, ok := p.reg.Get(st.Format)
	if !ok {
		return p.fail(ctx, tenant, id, &ParseError{Code: CodeUnknownFormat})
	}
	content, err := files.ReadContent(ctx, p.db, p.files, tenant, st.FileID, p.purpose, p.lim.MaxBytes)
	switch {
	case errors.Is(err, files.ErrTooLarge):
		return p.fail(ctx, tenant, id, &ParseError{Code: CodeTooLarge})
	case errors.Is(err, files.ErrNotFound), errors.Is(err, files.ErrNotReady), errors.Is(err, files.ErrObjectNotFound):
		return p.fail(ctx, tenant, id, &ParseError{Code: CodeFileUnavailable})
	case err != nil:
		return notSent(reasonRead), nil
	}
	lines, err := Parse(content, f, p.lim)
	if err != nil {
		pe, ok := errors.AsType[*ParseError](err)
		if !ok {
			pe = &ParseError{Code: CodeBadFile}
		}
		return p.fail(ctx, tenant, id, pe)
	}
	for start := 0; start < len(lines); start += batchSize {
		batch := lines[start:min(start+batchSize, len(lines))]
		if err := tenancy.WithTenant(ctx, p.db, tenant, func(tx pgx.Tx) error {
			_, err := store.New(tx).InsertStatementLines(ctx, linesParams(id, batch))
			return err //nolint:wrapcheck // chỉ phân loại not_sent
		}); err != nil {
			return notSent(reasonBatch), nil
		}
		if p.afterBatch != nil {
			if err := p.afterBatch(start + len(batch)); err != nil {
				return commands.Outcome{}, err
			}
		}
	}
	res, _ := json.Marshal(map[string]int{"lines": len(lines)}) //nolint:errchkjson // map số
	return commands.Outcome{Status: commands.StatusDone, Result: res}, nil
}

// fail ghi parse_failed với pe (chỉ khi bảng kê chưa đọc xong) rồi trả rejected; ghi lỗi → not_sent (chạy lại, lần
// sau gặp lại cùng lỗi định dạng).
func (p *Parser) fail(ctx context.Context, tenant, id uuid.UUID, pe *ParseError) (commands.Outcome, error) {
	if err := tenancy.WithTenant(ctx, p.db, tenant, func(tx pgx.Tx) error {
		return failParse(ctx, tx, id, pe)
	}); err != nil {
		return notSent(reasonRecord), nil
	}
	return rejected(pe.Code), nil
}

func failParse(ctx context.Context, tx pgx.Tx, id uuid.UUID, pe *ParseError) error {
	b, _ := json.Marshal(pe) //nolint:errchkjson // chuỗi và số
	if _, err := store.New(tx).FailParse(ctx, store.FailParseParams{
		ParseError: pgtype.Text{String: string(b), Valid: true}, ID: id,
	}); err != nil {
		return fmt.Errorf("ghi lỗi đọc bảng kê %s: %w", id, err)
	}
	return nil
}

func linesParams(id uuid.UUID, ls []Line) store.InsertStatementLinesParams {
	p := store.InsertStatementLinesParams{
		StatementID: id, LineNos: make([]int32, len(ls)), TrackingCodes: make([]string, len(ls)),
		CodAmounts: make([]int64, len(ls)), FeeAmounts: make([]int64, len(ls)), OtherFees: make([]int64, len(ls)),
		NetAmounts: make([]int64, len(ls)), PaidOns: make([]pgtype.Date, len(ls)),
	}
	for i, l := range ls {
		p.LineNos[i], p.TrackingCodes[i], p.CodAmounts[i] = l.LineNo, l.TrackingCode, l.COD
		p.FeeAmounts[i], p.OtherFees[i], p.NetAmounts[i] = l.Fee, l.OtherFee, l.Net
		p.PaidOns[i] = pgtype.Date{Time: l.PaidOn, Valid: !l.PaidOn.IsZero()}
	}
	return p
}

// Lookup implements commands.Looker: bảng kê đã parsed (hay matched, confirmed) → done; chưa, hay không có →
// not_sent (Execute chạy lại an toàn: dòng đã ghi bị bỏ qua, bảng kê không có thì rejected).
func (p *Parser) Lookup(ctx context.Context, j queue.Job) (string, error) {
	tenant, id, ok := target(j)
	if !ok {
		return commands.StatusNotSent, nil
	}
	var status string
	err := tenancy.WithTenant(ctx, p.db, tenant, func(tx pgx.Tx) error {
		st, err := store.New(tx).GetStatement(ctx, id)
		status = st.Status
		return err //nolint:wrapcheck // phân loại ở dưới
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return commands.StatusNotSent, nil
	case err != nil:
		return commands.StatusUnknown, fmt.Errorf("tra bảng kê %s: %w", id, err)
	case status == StatusImported, status == StatusParsing, status == StatusParseFailed:
		return commands.StatusNotSent, nil
	}
	return commands.StatusDone, nil
}

// OnDone implements commands.DoneHook: parsed, line_count, kỳ bảng kê đếm từ dòng đã ghi (chỉ khi chưa đọc xong).
// Lần đầu thành parsed thì gọi parsed (xếp việc khớp dòng, Task M3-32) trong cùng tx.
func (p *Parser) OnDone(ctx context.Context, j queue.Job, _ json.RawMessage, tx pgx.Tx) error {
	tenant, id, ok := target(j)
	if !ok {
		return nil
	}
	version, err := store.New(tx).FinishParse(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // đã đọc xong ở lần trước
	}
	if err != nil {
		return fmt.Errorf("ghi bảng kê %s đã đọc: %w", id, err)
	}
	if p.parsed == nil {
		return nil
	}
	return p.parsed(ctx, tx, tenant, id, version)
}

// OnTerminal implements queue.TerminalHook: lệnh dead hay expired mà bảng kê chưa đọc xong (hết lượt vì lỗi tạm) →
// parse_failed read_failed; lỗi định dạng đã được Execute ghi trước. in_doubt không đổi gì (Lookup quyết chạy lại).
func (*Parser) OnTerminal(ctx context.Context, j queue.Job, status string, tx pgx.Tx) error {
	_, id, ok := target(j)
	if !ok || (status != queue.StatusDead && status != queue.StatusExpired) {
		return nil
	}
	return failParse(ctx, tx, id, &ParseError{Code: CodeReadFailed})
}
