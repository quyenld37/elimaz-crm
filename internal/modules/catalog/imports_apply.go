package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog/internal/barcode"
	"elimaz.com.vn/crm/internal/modules/catalog/internal/importer"
	"elimaz.com.vn/crm/internal/modules/catalog/internal/store"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// importApplyName là việc transactional áp bản nhập (Task M1-47b; không gọi mạng: plan đã nằm trong database).
const importApplyName = "catalog.import.apply"

// ImportPlannedProduct là một mẫu trong plan của bản nhập.
type ImportPlannedProduct = importer.PlannedProduct

// applyReserve là phần cuối của hạn một lần Handle giữ lại cho việc lùi savepoint, ghi failed, audit và commit: còn ít
// hơn chừng này thì lần áp dừng ghi (Task 407, B-01).
const applyReserve = 5 * time.Second

// applyTooLongAttempts: lần áp hết ngân sách thời gian ở lượt nhỏ hơn số này thì chạy lại (máy bận nhất thời); từ
// lượt này thì bản nhập failed với lỗi CodeApplyTooLong (tệp quá lớn để áp trong một transaction, Q10).
const applyTooLongAttempts = 3

// errApplyOverBudget: lần áp dừng trước khi ghi hết plan vì hạn còn lại của Handle dưới applyReserve; savepoint lùi.
var errApplyOverBudget = errors.New("catalog: lần áp bản nhập hết ngân sách thời gian")

// ImportApplyKind là việc catalog.import.apply: transactional, chính sách mặc định (8 lượt, hạn một lần 30 s).
func ImportApplyKind() queue.Kind {
	return queue.Kind{Name: importApplyName, Policy: queue.DefaultPolicy}
}

// applyPayload: Barcodes là dải barcode cấp lúc ApplyImport cho các biến thể mới để trống barcode (nil: việc xếp trước
// khi có dải, hay plan không cần mã tự sinh).
type applyPayload struct {
	ImportID uuid.UUID      `json:"importId"`
	ActorID  uuid.UUID      `json:"actorId"`
	Barcodes *barcode.Range `json:"barcodes,omitempty"`
}

// ApplyImport chuyển bản nhập id (ready, đúng version) của người bán sellerID sang applying và xếp việc
// catalog.import.apply (DedupeKey = mã bản nhập), ghi audit, trong tx của bên gọi. Khóa dòng bản nhập trước khi so.
// Cấp luôn dải barcode cho các biến thể mới để trống barcode của plan (ADR-46 nhóm 4: bản nhập rồi bộ đếm, khóa bộ
// đếm chỉ trong tx ngắn này) để việc áp không khóa bộ đếm suốt lần ghi dài.
// Lỗi: ErrNotFound, ErrImportNotReady (không ở ready), ErrVersionConflict.
func (w Writer) ApplyImport(ctx context.Context, tx pgx.Tx, sellerID, id uuid.UUID, version int64,
	actor identity.AuthUser,
) (Import, error) {
	if err := w.check(); err != nil {
		return Import{}, err
	}
	q := store.New(tx)
	cur, err := q.LockImportForApply(ctx, store.LockImportForApplyParams{SellerID: sellerID, ID: id})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Import{}, ErrNotFound
	case err != nil:
		return Import{}, fmt.Errorf("khóa bản nhập: %w", err)
	case cur.Status != ImportReady:
		return Import{}, ErrImportNotReady
	case cur.Version != version:
		return Import{}, ErrVersionConflict
	}
	if err := oneRow(q.StartApply(ctx, id)); err != nil {
		return Import{}, fmt.Errorf("chuyển bản nhập sang applying: %w", err)
	}
	codes, err := reserveImportBarcodes(ctx, tx, sellerID, id)
	if err != nil {
		return Import{}, err
	}
	tenant := actor.TenantID
	if _, _, err := w.Queue.Enqueue(ctx, tx, ImportApplyKind(), queue.NewJob{
		TenantID: &tenant, Payload: applyPayload{ImportID: id, ActorID: actor.UserID, Barcodes: codes},
		DedupeKey: id.String(),
	}); err != nil {
		return Import{}, fmt.Errorf("xếp việc áp bản nhập: %w", err)
	}
	if err := w.audit(ctx, tx, actor, "catalog.import.apply", "catalog.import", id,
		map[string]any{"status": cur.Status, "version": cur.Version},
		map[string]any{"status": ImportApplying, "version": cur.Version + 1}); err != nil {
		return Import{}, err
	}
	return GetImport(ctx, tx, sellerID, id)
}

// reserveImportBarcodes cấp dải barcode đủ cho các biến thể mới để trống barcode trong plan của bản nhập id; nil khi
// không có biến thể nào như vậy.
func reserveImportBarcodes(ctx context.Context, tx pgx.Tx, sellerID, id uuid.UUID) (*barcode.Range, error) {
	data, err := store.New(tx).GetImportPlan(ctx, store.GetImportPlanParams{SellerID: sellerID, ID: id})
	if err != nil {
		return nil, fmt.Errorf("đọc plan bản nhập: %w", err)
	}
	var plan ImportPlan
	if err := json.Unmarshal(data.Plan, &plan); err != nil {
		return nil, fmt.Errorf("đọc plan bản nhập: %w", err)
	}
	var n int64
	for _, pp := range plan.Products {
		for _, v := range pp.Variants {
			if v.Action == importer.ActionCreate && v.Barcode == "" {
				n++
			}
		}
	}
	if n == 0 {
		return nil, nil //nolint:nilnil // không cần dải
	}
	r, err := barcode.Reserve(ctx, tx, n)
	if err != nil {
		return nil, err //nolint:wrapcheck // barcode đã bọc
	}
	return &r, nil
}

// ImportApplier là việc catalog.import.apply (queue.Handler, queue.TerminalHook). Handle chạy trong một transaction:
// khóa tư vấn theo người bán, khóa bản nhập (chỉ chạy khi còn applying), tệp cùng nội dung vừa được áp → duplicate;
// khóa danh mục (chia sẻ) và mẫu có sẵn của plan theo id tăng dần, đọc lại SKU và mã mẫu rồi importer.Recheck; ghi
// bằng CreateProduct, UpdateProduct, AddVariant, UpdateVariant, SetClassification trong một savepoint. Dữ liệu đổi từ
// lúc kiểm (Recheck, hay unique của database lúc ghi) → lùi savepoint, bản nhập failed với lỗi dòng, việc xong; lỗi
// khác → cả transaction lùi, việc chạy lại theo lịch. Trần thời gian: trước mỗi mẫu, hạn còn lại của ctx dưới
// applyReserve → dừng, lùi savepoint; lượt < applyTooLongAttempts chạy lại, từ lượt đó bản nhập failed với một lỗi cả
// tệp CodeApplyTooLong (không còn chạy lại tới hàng giờ rồi dead).
type ImportApplier struct {
	w   Writer
	now func() time.Time // nil: time.Now
}

// NewImportApplier trả việc catalog.import.apply ghi bằng w (Events, Queue, Realtime của tiến trình worker).
func NewImportApplier(w Writer) *ImportApplier { return &ImportApplier{w: w} }

// Kind implements queue.Handler.
func (*ImportApplier) Kind() queue.Kind { return ImportApplyKind() }

// OnTerminal implements queue.TerminalHook: việc dead hay expired → bản nhập còn applying thành failed.
func (*ImportApplier) OnTerminal(ctx context.Context, j queue.Job, status string, tx pgx.Tx) error {
	if status != queue.StatusDead && status != queue.StatusExpired {
		return nil
	}
	pl, ok := decodeApplyPayload(j)
	if !ok {
		return nil
	}
	if _, err := store.New(tx).FailApply(ctx, store.FailApplyParams{ID: pl.ImportID}); err != nil {
		return fmt.Errorf("đặt bản nhập %s failed: %w", pl.ImportID, err)
	}
	return nil
}

func decodeApplyPayload(j queue.Job) (applyPayload, bool) {
	return decodePayload(j, func(pl applyPayload) bool { return pl.ImportID != uuid.Nil && pl.ActorID != uuid.Nil })
}

// applyCounts là số đã làm của một lần áp (audit tổng).
type applyCounts struct {
	Create, Update, Unchanged       int
	ProductsCreated, ProductsEdited int
}

// Handle implements queue.Handler.
func (a *ImportApplier) Handle(ctx context.Context, j queue.Job, tx pgx.Tx) (json.RawMessage, error) {
	pl, ok := decodeApplyPayload(j)
	if !ok {
		return nil, fmt.Errorf("payload việc áp bản nhập: %w", queue.ErrPermanent)
	}
	q := store.New(tx)
	head, err := q.GetImportForValidation(ctx, pl.ImportID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, fmt.Errorf("bản nhập %s không có: %w", pl.ImportID, queue.ErrPermanent)
	case err != nil:
		return nil, fmt.Errorf("đọc bản nhập: %w", err)
	case head.Status != ImportApplying:
		return applyResult(head.Status)
	}
	seller := head.SellerID
	if err := q.LockImportApplySeller(ctx, seller); err != nil {
		return nil, fmt.Errorf("khóa tư vấn áp bản nhập: %w", err)
	}
	cur, err := q.LockImportForApply(ctx, store.LockImportForApplyParams{SellerID: seller, ID: pl.ImportID})
	if err != nil {
		return nil, fmt.Errorf("khóa bản nhập: %w", err)
	}
	if cur.Status != ImportApplying {
		return applyResult(cur.Status)
	}
	ap := *a // mỗi lần chạy (kể cả chạy lại) dùng lại dải của payload từ đầu
	ap.w.barcodes = pl.Barcodes
	status, err := ap.apply(ctx, tx, seller, pl, j.Attempts, identity.AuthUser{UserID: pl.ActorID, TenantID: *j.TenantID})
	if err != nil {
		return nil, err
	}
	return applyResult(status)
}

// apply chạy lần áp (lượt attempts) của bản nhập đã khóa, đang applying; trả trạng thái mới.
func (a *ImportApplier) apply(ctx context.Context, tx pgx.Tx, seller uuid.UUID, pl applyPayload, attempts int,
	actor identity.AuthUser,
) (string, error) {
	q := store.New(tx)
	data, err := q.GetImportPlan(ctx, store.GetImportPlanParams{SellerID: seller, ID: pl.ImportID})
	if err != nil {
		return "", fmt.Errorf("đọc plan bản nhập: %w", err)
	}
	dup, err := q.FindAppliedImportBySHA(ctx, store.FindAppliedImportBySHAParams{SellerID: seller, FileSha256: data.FileSha256})
	switch {
	case err == nil:
		if _, err := q.MarkApplyDuplicate(ctx, store.MarkApplyDuplicateParams{DuplicateOf: &dup, ID: pl.ImportID}); err != nil {
			return "", fmt.Errorf("đặt bản nhập duplicate: %w", err)
		}
		return ImportDuplicate, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return "", fmt.Errorf("tìm bản nhập đã áp cùng tệp: %w", err)
	}
	var plan ImportPlan
	if data.Plan == nil || json.Unmarshal(data.Plan, &plan) != nil {
		return "", fmt.Errorf("plan của bản nhập %s hỏng: %w", pl.ImportID, queue.ErrPermanent)
	}
	counts, rowErrs, err := a.applyPlan(ctx, tx, seller, plan, actor)
	if errors.Is(err, errApplyOverBudget) && attempts >= applyTooLongAttempts {
		rowErrs, err = []ImportError{applyTooLongError()}, nil
	}
	if err != nil {
		return "", err
	}
	if len(rowErrs) > 0 {
		return ImportFailed, a.fail(ctx, tx, pl.ImportID, data.Summary, rowErrs, actor)
	}
	if err := oneRow(q.FinishApply(ctx, store.FinishApplyParams{AppliedBy: &actor.UserID, ID: pl.ImportID})); err != nil {
		return "", fmt.Errorf("đặt bản nhập applied: %w", err)
	}
	if err := a.w.audit(ctx, tx, actor, "catalog.import.applied", "catalog.import", pl.ImportID, nil, map[string]any{
		"create": counts.Create, "update": counts.Update, "unchanged": counts.Unchanged,
		"productsCreated": counts.ProductsCreated, "productsUpdated": counts.ProductsEdited,
	}); err != nil {
		return "", err
	}
	return ImportApplied, nil
}

// fail đặt bản nhập failed với lỗi dòng rowErrs (tóm tắt thêm số lỗi) và ghi audit.
func (a *ImportApplier) fail(ctx context.Context, tx pgx.Tx, id uuid.UUID, rawSummary []byte, rowErrs []ImportError,
	actor identity.AuthUser,
) error {
	var summary ImportSummary
	if err := json.Unmarshal(rawSummary, &summary); err != nil {
		return fmt.Errorf("đọc tóm tắt bản nhập: %w", err)
	}
	summary.Errors = len(rowErrs)
	sum, err := json.Marshal(summary)
	if err != nil {
		return fmt.Errorf("mã hóa tóm tắt: %w", err)
	}
	errs, err := json.Marshal(rowErrs[:min(len(rowErrs), MaxImportErrors)])
	if err != nil {
		return fmt.Errorf("mã hóa lỗi bản nhập: %w", err)
	}
	if err := oneRow(store.New(tx).FailApply(ctx, store.FailApplyParams{Errors: errs, Summary: sum, ID: id})); err != nil {
		return fmt.Errorf("đặt bản nhập failed: %w", err)
	}
	return a.w.audit(ctx, tx, actor, "catalog.import.apply_failed", "catalog.import", id, nil,
		map[string]any{"errors": len(rowErrs)})
}

// applyPlan khóa, kiểm lại và ghi plan. rowErrs khác rỗng: dữ liệu đã đổi từ lúc kiểm, không còn gì được ghi.
func (a *ImportApplier) applyPlan(ctx context.Context, tx pgx.Tx, seller uuid.UUID, plan ImportPlan,
	actor identity.AuthUser,
) (applyCounts, []ImportError, error) {
	if errs, err := lockPlan(ctx, tx, seller, plan); err != nil || len(errs) > 0 {
		return applyCounts{}, errs, err
	}
	skus, codes := planKeys(plan)
	var now importer.Refs
	if err := existingRefs(ctx, tx, seller, skus, codes, &now); err != nil {
		return applyCounts{}, nil, err
	}
	if errs := importer.Recheck(plan, now); len(errs) > 0 {
		return applyCounts{}, errs, nil
	}
	var counts applyCounts
	err := db.Savepoint(ctx, tx, func(sp pgx.Tx) error {
		var err error
		counts, err = a.write(ctx, sp, seller, plan, actor)
		return err
	})
	if err == nil {
		return counts, nil, nil
	}
	var step *stepError
	if !errors.As(err, &step) {
		return applyCounts{}, nil, fmt.Errorf("ghi bản nhập: %w", err)
	}
	rowErr, ok, derr := describeStep(ctx, tx, seller, step)
	switch {
	case derr != nil:
		return applyCounts{}, nil, derr
	case !ok: // lỗi hạ tầng (hết hạn, lỗi tạm của database…): cả transaction lùi, việc chạy lại theo lịch
		return applyCounts{}, nil, fmt.Errorf("ghi bản nhập: %w", err)
	}
	return applyCounts{}, []ImportError{rowErr}, nil
}

// lockPlan khóa chia sẻ các danh mục của plan (tăng dần; danh mục vừa lưu trữ hay xóa → lỗi dòng) rồi khóa các mẫu có
// sẵn của plan theo id tăng dần (ADR-46 nhóm 4: danh mục trước mẫu).
func lockPlan(ctx context.Context, tx pgx.Tx, seller uuid.UUID, plan ImportPlan) ([]ImportError, error) {
	q := store.New(tx)
	firstRow := map[uuid.UUID]int{}
	var cats, products []uuid.UUID
	for _, pp := range plan.Products {
		if pp.CategoryID != nil && len(pp.Variants) > 0 {
			if _, seen := firstRow[*pp.CategoryID]; !seen {
				firstRow[*pp.CategoryID] = pp.Variants[0].Row
				cats = append(cats, *pp.CategoryID)
			}
		}
		if pp.ExistingProductID != nil {
			products = append(products, *pp.ExistingProductID)
		}
	}
	slices.SortFunc(cats, compareUUID)
	for _, c := range cats {
		_, err := shareLockActiveCategory(ctx, q, c)
		switch {
		case errors.Is(err, ErrNotFound):
			return []ImportError{{
				Row: firstRow[c], Column: importer.ColCategory, Code: importer.CodeChangedSinceValidation,
				Message: "Danh mục vừa bị lưu trữ hay xóa sau khi kiểm tệp: hãy tải tệp lên lại để kiểm",
			}}, nil
		case err != nil:
			return nil, err
		}
	}
	if len(products) == 0 {
		return nil, nil
	}
	slices.SortFunc(products, compareUUID)
	if _, err := q.LockProductsByIDs(ctx, store.LockProductsByIDsParams{SellerID: seller, Ids: slices.Compact(products)}); err != nil {
		return nil, fmt.Errorf("khóa mẫu có sẵn của bản nhập: %w", err)
	}
	return nil, nil
}

// overBudget: ctx có hạn và hạn còn lại dưới applyReserve (ctx không hạn: không bao giờ).
func (a *ImportApplier) overBudget(ctx context.Context) bool {
	deadline, ok := ctx.Deadline()
	if !ok {
		return false
	}
	now := time.Now
	if a.now != nil {
		now = a.now
	}
	return deadline.Sub(now()) < applyReserve
}

// applyTooLongError là lỗi cả tệp khi lần áp không xong trong hạn một lần của việc dù đã chạy lại.
func applyTooLongError() ImportError {
	return ImportError{Row: 0, Column: "", Code: CodeApplyTooLong, Message: fmt.Sprintf(
		"Tệp quá lớn để áp trong một lần (mỗi lần áp tối đa %d giây): hãy chia thành nhiều tệp nhỏ hơn, khoảng "+
			"2.000 dòng mỗi tệp, rồi tải lên lại", int(ImportApplyKind().Policy.Timeout.Seconds()))}
}

func compareUUID(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) }

// errNotOneRow: câu đổi trạng thái có điều kiện của bản nhập đã khóa không chạm đúng một dòng (không xảy ra khi giữ
// khóa dòng; giữ để không ghi tiếp trên trạng thái lạ).
var errNotOneRow = errors.New("catalog: câu đổi bản nhập không chạm đúng một dòng")

func oneRow(n int64, err error) error {
	switch {
	case err != nil:
		return err
	case n != 1:
		return errNotOneRow
	}
	return nil
}

// planKeys trả mọi SKU, mã mẫu của plan (đã viết hoa, không trùng, tăng dần).
func planKeys(plan ImportPlan) (skus, codes []string) {
	for _, pp := range plan.Products {
		codes = append(codes, pp.Code)
		for _, v := range pp.Variants {
			skus = append(skus, v.SKU)
		}
	}
	slices.Sort(skus)
	slices.Sort(codes)
	return slices.Compact(skus), slices.Compact(codes)
}
