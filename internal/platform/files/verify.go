package files

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/internal/store"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Đối chiếu kho tệp (kế hoạch M1, Task M1-46; spec 04 mục 7.3, ADR-36; TP12): sau khi dựng lại kho từ bản sao, tệp
// ready nào không còn đối tượng trên kho thì thành lost để màn hình hiện ô trống có ghi chú và có danh sách tệp mất.
const (
	// verifyBatch: số tệp tối đa một lần Execute (kế hoạch: lô 500).
	verifyBatch = 500
	// verifyBudget: một lần Execute dừng nhận tệp mới sau chừng này (hạn của loại là 30 s); phần còn lại sang lệnh sau.
	verifyBudget = 20 * time.Second
	// verifyParallel: số tệp hỏi kho cùng lúc.
	verifyParallel = 8
	// verifyExpiry: lượt đối chiếu chờ trong hàng tối đa một ngày.
	verifyExpiry = 24 * time.Hour
)

// VerifyKind là lệnh đối chiếu kho file.verify: như mọi lệnh, 30 s một lần, 8 lượt.
func VerifyKind() queue.Kind {
	return commands.CommandKind("file.verify", verifyExpiry, 30*time.Second)
}

// verifyPayload: lượt đối chiếu RunID, lô gồm tệp có mã lớn hơn After (uuid.Nil: từ đầu).
type verifyPayload struct {
	RunID uuid.UUID `json:"runId"`
	After uuid.UUID `json:"after"`
}

// verifyResult là Result của một lô: tệp thiếu đối tượng (kèm version lúc thấy thiếu), số tệp đã kiểm, mã tệp cuối
// đã kiểm khi còn lô sau (rỗng: hết).
type verifyResult struct {
	Missing []missingFile `json:"missing"`
	Checked int           `json:"checked"`
	Next    *uuid.UUID    `json:"next,omitempty"`
}

type missingFile struct {
	ID      uuid.UUID `json:"id"`
	Version int64     `json:"version"`
}

// EnqueueVerify xếp lô đầu của một lượt đối chiếu mới cho tenant của tx (tenancy.WithTenant) và trả mã lệnh. Mỗi lô
// xong thì OnDone xếp lô sau của cùng lượt (một lượt là một chuỗi lệnh của một tenant).
func EnqueueVerify(ctx context.Context, tx pgx.Tx, q queue.JobQueue) (uuid.UUID, error) {
	tenant, err := store.New(tx).CurrentTenant(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("xếp đối chiếu kho: đọc tenant của ngữ cảnh: %w", err)
	}
	run, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, fmt.Errorf("xếp đối chiếu kho: sinh mã lượt: %w", err)
	}
	return enqueueVerifyBatch(ctx, tx, q, tenant, verifyPayload{RunID: run})
}

func enqueueVerifyBatch(ctx context.Context, tx pgx.Tx, q queue.JobQueue, tenant uuid.UUID, pl verifyPayload) (uuid.UUID, error) {
	id, _, err := q.Enqueue(ctx, tx, VerifyKind(), queue.NewJob{
		TenantID: &tenant, Payload: pl, DedupeKey: pl.RunID.String() + ":" + pl.After.String(),
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("xếp đối chiếu kho: %w", err)
	}
	return id, nil
}

// Verifier là lệnh file.verify: Execute (ngoài transaction) đọc một lô tệp ready, hỏi kho (Head) ảnh gốc và mọi
// biến thể (tệp máy chủ ghi: chính đối tượng của nó), trả tệp thiếu; OnDone đặt lost và xếp lô sau. Chỉ đọc kho nên
// chạy lại luôn an toàn.
type Verifier struct {
	o      ProcessorOptions
	batch  int
	budget time.Duration
}

// NewVerifyHandler trả lệnh file.verify (dùng o.DB, o.Store, o.Bus).
func NewVerifyHandler(o ProcessorOptions) *Verifier {
	return &Verifier{o: o, batch: verifyBatch, budget: verifyBudget}
}

// Kind implements commands.Handler.
func (*Verifier) Kind() queue.Kind { return VerifyKind() }

// AutoRetryNotSent implements commands.Handler: chỉ đọc kho, chạy lại an toàn.
func (*Verifier) AutoRetryNotSent() bool { return true }

// Lookup implements commands.Looker: luôn not_sent (Execute không đổi gì bên ngoài).
func (*Verifier) Lookup(context.Context, queue.Job) (string, error) {
	return commands.StatusNotSent, nil
}

// target là một đối tượng phải có trên kho.
type target struct {
	b   Bucket
	key string
}

// verifyFile là một tệp của lô cùng các đối tượng phải có.
type verifyFile struct {
	id      uuid.UUID
	version int64
	targets []target
}

// Execute implements commands.Handler.
func (v *Verifier) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	var pl verifyPayload
	if j.TenantID == nil || json.Unmarshal(j.Payload, &pl) != nil || pl.RunID == uuid.Nil {
		return rejected(reasonBadPayload), nil
	}
	batch, full, err := v.load(ctx, *j.TenantID, pl.After)
	if err != nil {
		return notSent("db", 0), nil
	}
	checked, missing, err := v.check(ctx, batch)
	switch {
	case err != nil:
		return notSent("store", 0), nil
	case checked == 0 && len(batch) > 0:
		return notSent("timeout", 0), nil
	}
	res := verifyResult{Missing: []missingFile{}, Checked: checked}
	for i := range checked {
		if missing[i] {
			res.Missing = append(res.Missing, missingFile{ID: batch[i].id, Version: batch[i].version})
		}
	}
	if checked < len(batch) || full {
		res.Next = &batch[checked-1].id
	}
	body, err := json.Marshal(res)
	if err != nil {
		return commands.Outcome{}, fmt.Errorf("mã hóa kết quả đối chiếu kho: %w", err)
	}
	return commands.Outcome{Status: commands.StatusDone, Result: body}, nil
}

// load đọc một lô tệp ready sau after cùng chỗ của mọi biến thể (transaction đọc ngắn); full = lô đầy (có thể còn).
func (v *Verifier) load(ctx context.Context, tenant, after uuid.UUID) ([]verifyFile, bool, error) {
	var (
		rows []store.ListReadyFilesAfterRow
		vars []store.ListVariantKeysOfRow
	)
	err := tenancy.WithTenant(ctx, v.o.DB, tenant, func(tx pgx.Tx) error {
		s := store.New(tx)
		var err error
		if rows, err = s.ListReadyFilesAfter(ctx, store.ListReadyFilesAfterParams{After: after, Lim: int32(v.batch)}); err != nil { //nolint:gosec // G115: lô nhỏ
			return err //nolint:wrapcheck // bọc ở dưới
		}
		ids := make([]uuid.UUID, len(rows))
		for i, r := range rows {
			ids[i] = r.ID
		}
		vars, err = s.ListVariantKeysOf(ctx, ids)
		return err //nolint:wrapcheck // bọc ở dưới
	})
	if err != nil {
		return nil, false, fmt.Errorf("đối chiếu kho: đọc lô tệp: %w", err)
	}
	byFile := map[uuid.UUID][]target{}
	for _, r := range vars {
		byFile[r.FileID] = append(byFile[r.FileID], target{Bucket(r.Bucket), r.Key})
	}
	out := make([]verifyFile, len(rows))
	for i, r := range rows {
		out[i] = verifyFile{id: r.ID, version: r.Version, targets: append(mainTargets(r), byFile[r.ID]...)}
	}
	return out, len(rows) == v.batch, nil
}

// mainTargets là đối tượng chính phải có của một tệp ready: ảnh công khai → ảnh gốc orig/<sha> (khóa trong dòng là
// khóa tải lên trong crm-tmp, tự hết hạn); ảnh riêng tư → không có (R10: không giữ gốc, chỉ biến thể); tệp khác →
// chính bucket, khóa của dòng, trừ tệp tạm (crm-tmp hay có hạn: hết hạn là bị xóa đúng luật, không phải mất). Mục
// đích đã bỏ khỏi sổ → không đoán.
func mainTargets(r store.ListReadyFilesAfterRow) []target {
	p, ok := PurposeOf(r.Purpose)
	switch {
	case !ok, p.Image && p.Private:
		return nil
	case p.Image:
		if len(r.ContentSha256) != 32 {
			return nil
		}
		return []target{{BucketOriginals, OriginalKey(hex.EncodeToString(r.ContentSha256))}}
	case Bucket(r.Bucket) == BucketTmp, r.ExpiresAt.Valid:
		return nil
	}
	return []target{{Bucket(r.Bucket), r.Key}}
}

// check hỏi kho từng tệp của lô (verifyParallel tệp cùng lúc, theo thứ tự); quá v.budget thì thôi nhận tệp mới.
// checked là số tệp đầu lô đã kiểm xong liền nhau; missing[i]: tệp i thiếu ít nhất một đối tượng. Head lỗi khác
// "không có" → err (không kết luận tệp nào mất vì lỗi mạng).
func (v *Verifier) check(ctx context.Context, batch []verifyFile) (int, []bool, error) {
	deadline := time.Now().Add(v.budget)
	done := make([]bool, len(batch))
	missing := make([]bool, len(batch))
	var (
		mu    sync.Mutex
		next  int
		first error
		wg    sync.WaitGroup
	)
	take := func() (int, bool) {
		mu.Lock()
		defer mu.Unlock()
		if first != nil || next >= len(batch) || time.Now().After(deadline) || ctx.Err() != nil {
			return 0, false
		}
		next++
		return next - 1, true
	}
	for range min(verifyParallel, len(batch)) {
		wg.Go(func() {
			for i, ok := take(); ok; i, ok = take() {
				gone, err := v.missing(ctx, batch[i].targets)
				mu.Lock()
				if err != nil && first == nil {
					first = err
				}
				done[i], missing[i] = err == nil, gone
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if first != nil {
		return 0, nil, first
	}
	checked := slices.Index(done, false)
	if checked < 0 {
		checked = len(batch)
	}
	return checked, missing, nil
}

// missing: có đối tượng nào trong ts không còn trên kho.
func (v *Verifier) missing(ctx context.Context, ts []target) (bool, error) {
	for _, t := range ts {
		_, err := v.o.Store.Head(ctx, t.b, t.key)
		switch {
		case errors.Is(err, ErrObjectNotFound):
			return true, nil
		case err != nil:
			return false, fmt.Errorf("đối chiếu kho: hỏi %s/%s: %w", t.b, t.key, err)
		}
	}
	return false, nil
}

// OnDone implements commands.DoneHook: khóa tệp thiếu theo mã tăng dần (ADR-46 nhóm 12b); tệp còn ready, chưa xóa,
// cùng version lúc Execute thấy thiếu → lost, lost_at, phát báo hiệu. Rồi xếp lô sau của cùng lượt (DedupeKey theo
// lượt và mã tệp cuối: chạy lại không xếp trùng).
func (v *Verifier) OnDone(ctx context.Context, j queue.Job, result json.RawMessage, tx pgx.Tx) error {
	var pl verifyPayload
	var res verifyResult
	if j.TenantID == nil || len(result) == 0 || json.Unmarshal(j.Payload, &pl) != nil || json.Unmarshal(result, &res) != nil {
		return nil
	}
	slices.SortFunc(res.Missing, func(a, b missingFile) int { return slices.Compare(a.ID[:], b.ID[:]) })
	s := store.New(tx)
	pub := newProcessor(v.o, false)
	for _, m := range res.Missing {
		row, err := s.LockFile(ctx, m.ID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			continue
		case err != nil:
			return fmt.Errorf("khóa tệp %s: %w", m.ID, err)
		case row.Status != StatusReady || row.DeletedAt.Valid || row.Version != m.Version:
			continue
		}
		if _, err := s.SetFileLost(ctx, m.ID); err != nil {
			return fmt.Errorf("đánh dấu mất %s: %w", m.ID, err)
		}
		if err := pub.publish(ctx, tx, row.TenantID, row.ID); err != nil {
			return err
		}
	}
	if res.Next == nil {
		return nil
	}
	_, err := enqueueVerifyBatch(ctx, tx, queue.NewQueue(), *j.TenantID, verifyPayload{RunID: pl.RunID, After: *res.Next})
	return err
}

// enqueueRestore xếp file.process cho mọi tệp lost cùng tenant, cùng nội dung với tệp row vừa ready (tải lại ảnh đã
// mất: ảnh gốc orig/<sha> có lại trên kho). Chỉ ảnh công khai (ảnh riêng tư không có ảnh gốc để dựng lại, R10).
// DedupeKey = mã tệp + ":restore:" + lost_at: mỗi lần mất một lệnh.
func enqueueRestore(ctx context.Context, tx pgx.Tx, row store.PlatformFile, sum []byte) error {
	lost, err := store.New(tx).ListLostSameContent(ctx, store.ListLostSameContentParams{ContentSha256: sum, ID: row.ID})
	if err != nil {
		return fmt.Errorf("tìm tệp mất cùng nội dung với %s: %w", row.ID, err)
	}
	q := queue.NewQueue()
	for _, l := range lost {
		if p, ok := PurposeOf(l.Purpose); !ok || !p.Image || p.Private {
			continue
		}
		tenant := row.TenantID
		if _, _, err := q.Enqueue(ctx, tx, ProcessKind(), queue.NewJob{
			TenantID: &tenant, Payload: map[string]string{"fileId": l.ID.String()},
			DedupeKey: l.ID.String() + ":restore:" + strconv.FormatInt(l.LostAt.Time.UnixMicro(), 10),
		}); err != nil {
			return fmt.Errorf("xếp phục hồi tệp mất %s: %w", l.ID, err)
		}
	}
	return nil
}
