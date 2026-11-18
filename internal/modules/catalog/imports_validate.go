package catalog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog/internal/importer"
	"elimaz.com.vn/crm/internal/modules/catalog/internal/store"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/platform/xlsx"
)

// Lệnh kiểm tệp nhập (Task M1-47).
const (
	importValidateName    = "catalog.import.validate"
	importValidateTimeout = 30 * time.Second

	// Lý do (last_error của lệnh) khi không đọc được tệp: chỉ là mã, không có nội dung tệp.
	reasonImportBadPayload = "bad_payload"
	reasonImportNotFound   = "import_not_found"
	reasonImportFile       = "file_unavailable"
)

// Lỗi cấp tệp (Row 0, Column rỗng) của bản nhập invalid.
const (
	CodeFileNotXlsx    = "file_not_xlsx"
	CodeFileTooLarge   = "file_too_large"
	CodeTooManyRows    = "too_many_rows"
	CodeHeaderMismatch = "header_mismatch"
)

// ImportValidateKind là lệnh catalog.import.validate: CommandKind (in_doubt khi hết giữ chỗ, 8 lượt), không hết hạn,
// hạn một lần 30 s.
func ImportValidateKind() queue.Kind {
	return commands.CommandKind(importValidateName, 0, importValidateTimeout)
}

type importPayload struct {
	ImportID uuid.UUID `json:"importId"`
}

// ImportValidatorOptions là phụ thuộc của lệnh kiểm: DB (crm_app), Store (kho tệp).
type ImportValidatorOptions struct {
	DB    *db.DB
	Store files.FileStore
}

// ImportValidator là lệnh catalog.import.validate (commands.Handler, commands.Looker, queue.TerminalHook). Execute
// chạy ngoài transaction: đọc bản nhập và Refs dùng chung (transaction đọc ngắn), tải tệp (files.ReadContent, 5 MiB),
// xlsx.Read + importer.Validate + importer.MergeCellErrors, đọc biến thể và mẫu có sẵn cho đúng SKU, mã mẫu của tệp
// (transaction đọc ngắn), rồi ghi kết quả trong một transaction ngắn: khóa bản nhập, chỉ ghi khi còn validating; tệp
// cùng nội dung đã applied cho cùng người bán → duplicate; có lỗi → invalid; không → ready + plan + summary. Ghi kết quả ngay ở đây (không
// ở DoneHook) để plan nhiều MB không nằm trong platform.jobs.result; chạy lại không đổi gì nhờ điều kiện validating.
type ImportValidator struct {
	o ImportValidatorOptions
}

// NewImportValidator trả lệnh catalog.import.validate.
func NewImportValidator(o ImportValidatorOptions) *ImportValidator { return &ImportValidator{o: o} }

// Kind implements commands.Handler.
func (*ImportValidator) Kind() queue.Kind { return ImportValidateKind() }

// AutoRetryNotSent implements commands.Handler: chạy lại an toàn (chỉ đọc, ghi có điều kiện).
func (*ImportValidator) AutoRetryNotSent() bool { return true }

// Lookup implements commands.Looker: bản nhập đã rời validating → done (kết quả đã ghi); còn validating hay không đọc
// được → not_sent (chạy lại).
func (v *ImportValidator) Lookup(ctx context.Context, j queue.Job) (string, error) {
	pl, ok := decodeImportPayload(j)
	if !ok {
		return commands.StatusNotSent, nil
	}
	var status string
	err := tenancy.WithTenant(ctx, v.o.DB, *j.TenantID, func(tx pgx.Tx) error {
		row, err := store.New(tx).GetImportForValidation(ctx, pl.ImportID)
		status = row.Status
		return err //nolint:wrapcheck // xử lý ở dưới
	})
	if err != nil || status == ImportValidating {
		return commands.StatusNotSent, nil //nolint:nilerr // không đọc được thì chạy lại: Execute quyết định
	}
	return commands.StatusDone, nil
}

// OnTerminal implements queue.TerminalHook: dead (hết lượt, tệp không đọc được) hoặc expired → bản nhập còn
// validating thành failed. in_doubt: giữ nguyên (tra lại sẽ chạy tiếp).
func (*ImportValidator) OnTerminal(ctx context.Context, j queue.Job, status string, tx pgx.Tx) error {
	if status != queue.StatusDead && status != queue.StatusExpired {
		return nil
	}
	pl, ok := decodeImportPayload(j)
	if !ok {
		return nil
	}
	if _, err := store.New(tx).FailValidation(ctx, pl.ImportID); err != nil {
		return fmt.Errorf("đặt bản nhập %s failed: %w", pl.ImportID, err)
	}
	return nil
}

func decodeImportPayload(j queue.Job) (importPayload, bool) {
	var pl importPayload
	if j.TenantID == nil || json.Unmarshal(j.Payload, &pl) != nil || pl.ImportID == uuid.Nil {
		return pl, false
	}
	return pl, true
}

// validation là kết quả kiểm của một tệp.
type validation struct {
	sum    [sha256.Size]byte
	errs   []ImportError
	plan   *ImportPlan
	models int
}

// Execute implements commands.Handler (ngoài transaction, hạn 30 s).
func (v *ImportValidator) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	pl, ok := decodeImportPayload(j)
	if !ok {
		return importRejected(reasonImportBadPayload), nil
	}
	tenant := *j.TenantID
	var (
		row  store.GetImportForValidationRow
		refs importer.Refs
	)
	err := tenancy.WithTenant(ctx, v.o.DB, tenant, func(tx pgx.Tx) error {
		var err error
		if row, err = store.New(tx).GetImportForValidation(ctx, pl.ImportID); err != nil || row.Status != ImportValidating {
			return err //nolint:wrapcheck // xử lý ở dưới
		}
		refs, err = classificationRefs(ctx, tx)
		return err
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return importRejected(reasonImportNotFound), nil
	case err != nil:
		return importNotSent("db"), nil
	case row.Status != ImportValidating:
		return importDone(row.Status)
	}
	body, err := files.ReadContent(ctx, v.o.DB, v.o.Store, tenant, row.FileID, importPurpose, xlsx.DefaultLimits().MaxBytes)
	var res validation
	switch {
	case errors.Is(err, files.ErrTooLarge):
		res = validation{errs: []ImportError{fileError(CodeFileTooLarge)}}
	case errors.Is(err, files.ErrNotFound), errors.Is(err, files.ErrNotReady), errors.Is(err, files.ErrObjectNotFound):
		return importRejected(reasonImportFile), nil
	case err != nil:
		return importNotSent("store"), nil
	default:
		if res, err = v.check(ctx, tenant, row.SellerID, body, refs); err != nil {
			return importNotSent("db"), nil //nolint:nilerr // lỗi đọc database: chạy lại theo lịch
		}
	}
	status, err := v.record(ctx, tenant, row.SellerID, pl.ImportID, res)
	if err != nil {
		return importNotSent("db"), nil //nolint:nilerr // lỗi ghi: chạy lại (ghi có điều kiện)
	}
	return importDone(status)
}

// check đọc và kiểm body; lỗi chỉ là lỗi đọc database (lỗi của tệp nằm trong validation.errs).
func (v *ImportValidator) check(ctx context.Context, tenant, sellerID uuid.UUID, body []byte, refs importer.Refs) (validation, error) {
	res := validation{sum: sha256.Sum256(body)}
	rows, cellErrs, err := xlsx.Read(bytes.NewReader(body), int64(len(body)), xlsx.DefaultLimits(), importer.RequiredColumns())
	if err != nil {
		res.errs = []ImportError{fileError(fileErrorCode(err))}
		return res, nil
	}
	skus, codes := keysOf(rows)
	if err := tenancy.WithTenant(ctx, v.o.DB, tenant, func(tx pgx.Tx) error {
		return existingRefs(ctx, tx, sellerID, skus, codes, &refs)
	}); err != nil {
		return res, fmt.Errorf("đọc dữ liệu có sẵn cho bản nhập: %w", err)
	}
	plan, rowErrs := importer.Validate(rows, refs)
	if res.errs = importer.MergeCellErrors(cellErrs, rowErrs); len(res.errs) == 0 {
		res.plan, res.models = &plan, len(plan.Products)
	}
	return res, nil
}

// keysOf trả các SKU và mã mẫu (viết hoa, không trùng) có trong rows.
func keysOf(rows []xlsx.Row) (skus, codes []string) {
	for _, r := range rows {
		if s := strings.ToUpper(strings.TrimSpace(r.Cells[importer.ColSKU])); s != "" {
			skus = append(skus, s)
		}
		if c := strings.ToUpper(strings.TrimSpace(r.Cells[importer.ColCode])); c != "" {
			codes = append(codes, c)
		}
	}
	slices.Sort(skus)
	slices.Sort(codes)
	return slices.Compact(skus), slices.Compact(codes)
}

// record ghi res vào bản nhập id khi còn validating; trả trạng thái của bản nhập sau khi ghi.
func (v *ImportValidator) record(ctx context.Context, tenant, sellerID, id uuid.UUID, res validation) (string, error) {
	var status string
	err := tenancy.WithTenant(ctx, v.o.DB, tenant, func(tx pgx.Tx) error {
		q := store.New(tx)
		cur, err := q.LockImport(ctx, id)
		if err != nil {
			return fmt.Errorf("khóa bản nhập: %w", err)
		}
		if status = cur.Status; status != ImportValidating {
			return nil
		}
		params, err := finishParams(ctx, q, sellerID, id, res)
		if err != nil {
			return err
		}
		if _, err := q.FinishValidation(ctx, params); err != nil {
			return fmt.Errorf("ghi kết quả kiểm: %w", err)
		}
		status = params.Status
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("ghi bản nhập %s: %w", id, err)
	}
	return status, nil
}

// finishParams dựng câu ghi kết quả: duplicate khi tệp cùng nội dung đã applied cho người bán sellerID, invalid khi
// có lỗi, ready khi không.
func finishParams(ctx context.Context, q *store.Queries, sellerID, id uuid.UUID, res validation) (store.FinishValidationParams, error) {
	p := store.FinishValidationParams{ID: id, FileSha256: res.sum[:], Summary: []byte("{}"), Errors: []byte("[]")}
	dup, err := q.FindAppliedImportBySHA(ctx, store.FindAppliedImportBySHAParams{SellerID: sellerID, FileSha256: res.sum[:]})
	switch {
	case err == nil:
		p.Status, p.DuplicateOf = ImportDuplicate, &dup
		return p, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return p, fmt.Errorf("tìm bản nhập đã áp cùng tệp: %w", err)
	}
	var summary ImportSummary
	if len(res.errs) > 0 {
		p.Status, summary.Errors = ImportInvalid, len(res.errs)
		if p.Errors, err = json.Marshal(res.errs[:min(len(res.errs), MaxImportErrors)]); err != nil {
			return p, fmt.Errorf("mã hóa lỗi bản nhập: %w", err)
		}
	} else {
		p.Status, summary = ImportReady, summarize(res)
		if p.Plan, err = json.Marshal(res.plan); err != nil {
			return p, fmt.Errorf("mã hóa kế hoạch bản nhập: %w", err)
		}
	}
	if p.Summary, err = json.Marshal(summary); err != nil {
		return p, fmt.Errorf("mã hóa tóm tắt bản nhập: %w", err)
	}
	return p, nil
}

func summarize(res validation) ImportSummary {
	s := ImportSummary{Models: res.models}
	for _, p := range res.plan.Products {
		for _, pv := range p.Variants {
			switch pv.Action {
			case importer.ActionCreate:
				s.Create++
			case importer.ActionUpdate:
				s.Update++
			case importer.ActionUnchanged:
				s.Unchanged++
			}
		}
	}
	return s
}

func fileErrorCode(err error) string {
	switch {
	case errors.Is(err, xlsx.ErrTooLarge):
		return CodeFileTooLarge
	case errors.Is(err, xlsx.ErrTooManyRows):
		return CodeTooManyRows
	case errors.Is(err, xlsx.ErrHeaderMismatch):
		return CodeHeaderMismatch
	}
	return CodeFileNotXlsx
}

// fileError là lỗi cấp tệp (Row 0, Column rỗng) kèm câu tiếng Việt.
func fileError(code string) ImportError {
	lim := xlsx.DefaultLimits()
	msg := map[string]string{
		CodeFileTooLarge: fmt.Sprintf("Tệp quá lớn: tối đa %d MB (giải nén tối đa %d MB)", lim.MaxBytes>>20, lim.MaxUnzipped>>20),
		CodeTooManyRows:  fmt.Sprintf("Tệp có quá %d dòng: hãy chia thành nhiều tệp", lim.MaxRows),
		CodeHeaderMismatch: "Dòng tiêu đề không khớp mẫu (thiếu hay trùng cột): hãy tải mẫu mới và chép dữ liệu " +
			"vào đúng cột",
		CodeFileNotXlsx: "Tệp không phải Excel .xlsx đọc được: hãy mở bằng Excel và lưu lại dạng .xlsx",
	}[code]
	return ImportError{Row: 0, Column: "", Code: code, Message: msg}
}

func importRejected(reason string) commands.Outcome {
	return commands.Outcome{Status: commands.StatusRejected, Reason: reason}
}

func importNotSent(reason string) commands.Outcome {
	return commands.Outcome{Status: commands.StatusNotSent, Reason: reason}
}

func importDone(status string) (commands.Outcome, error) {
	body, err := json.Marshal(map[string]string{"status": status})
	if err != nil {
		return commands.Outcome{}, fmt.Errorf("mã hóa kết quả kiểm: %w", err)
	}
	return commands.Outcome{Status: commands.StatusDone, Result: body}, nil
}
