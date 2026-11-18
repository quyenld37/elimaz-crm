package files

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/internal/store"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// ErrUnknownPreset là lỗi khi bộ kích thước chưa có trong bảng (Preset).
var ErrUnknownPreset = errors.New("files: bộ kích thước chưa có")

// reasonUnknownPreset: lệnh file.regenerate nhắc bộ kích thước không có trong bảng của worker.
const reasonUnknownPreset = "unknown_preset"

// regenerateExpiry: lệnh tạo lại chờ trong hàng tối đa một ngày (cả kho ảnh của shop xếp một lần, imager làm 2 ảnh
// cùng lúc).
const regenerateExpiry = 24 * time.Hour

// RegenerateKind là lệnh tạo lại kích thước file.regenerate (TP07): như file.process, hạn 30 s một lần (ADR-45), 3 lần.
func RegenerateKind() queue.Kind {
	k := commands.CommandKind("file.regenerate", regenerateExpiry, 30*time.Second)
	k.Policy.MaxAttempts = 3
	return k
}

// regeneratePayload là payload của file.regenerate.
type regeneratePayload struct {
	FileID uuid.UUID `json:"fileId"`
	Preset int       `json:"preset"`
}

// EnqueueRegenerate xếp một lệnh file.regenerate cho mỗi tệp ảnh ready, chưa xóa, của mục đích chế độ standard trong
// tenant của tx (tenancy.WithTenant) chưa có biến thể nào của bộ kích thước preset (DedupeKey = mã tệp + ":v" + bản:
// gọi lại không xếp trùng). Ảnh riêng tư không giữ ảnh gốc nên không bao giờ được xếp (R10). Trả số lệnh mới tạo.
// preset không có trong bảng → ErrUnknownPreset.
func EnqueueRegenerate(ctx context.Context, tx pgx.Tx, q queue.JobQueue, preset int) (int, error) {
	if _, ok := Preset(preset); !ok {
		return 0, fmt.Errorf("tạo lại kích thước v%d: %w", preset, ErrUnknownPreset)
	}
	rows, err := store.New(tx).ListFilesMissingPreset(ctx, int32(preset)) //nolint:gosec // G115: bản đã kiểm có trong bảng
	if err != nil {
		return 0, fmt.Errorf("tạo lại kích thước v%d: liệt kê tệp: %w", preset, err)
	}
	kind, n := RegenerateKind(), 0
	for _, r := range rows {
		if p, ok := PurposeOf(r.Purpose); !ok || !p.Image || p.Private {
			continue
		}
		tenant := r.TenantID
		_, created, err := q.Enqueue(ctx, tx, kind, queue.NewJob{
			TenantID: &tenant, Payload: regeneratePayload{FileID: r.ID, Preset: preset},
			DedupeKey: r.ID.String() + ":v" + strconv.Itoa(preset),
		})
		if err != nil {
			return 0, fmt.Errorf("tạo lại kích thước v%d: xếp lệnh cho %s: %w", preset, r.ID, err)
		}
		if created {
			n++
		}
	}
	return n, nil
}

// Regenerator là lệnh file.regenerate: tạo biến thể của một bộ kích thước mới cho tệp đã ready, từ ảnh gốc
// orig/<sha> (không tải lên lại, không băm lại; dùng chung phần gọi imager với file.process). Tệp giữ nguyên trạng
// thái và bản preset_version; chỉ thêm dòng file_variants.
type Regenerator struct {
	o       ProcessorOptions
	timeout time.Duration // 0: hạn của loại (30 s)
}

// NewRegenerateHandler trả lệnh file.regenerate (o.PresetVersion không dùng: bản lấy từ payload của lệnh).
func NewRegenerateHandler(o ProcessorOptions) *Regenerator {
	if o.Signer == nil {
		o.Signer = o.Store
	}
	return &Regenerator{o: o}
}

// Kind implements commands.Handler.
func (r *Regenerator) Kind() queue.Kind {
	k := RegenerateKind()
	if r.timeout > 0 {
		k.Policy.Timeout = r.timeout
	}
	return k
}

// AutoRetryNotSent implements commands.Handler: chạy lại an toàn (khóa đầu ra cố định, PUT không ghi đè).
func (*Regenerator) AutoRetryNotSent() bool { return true }

// Lookup implements commands.Looker: luôn not_sent (như file.process: chạy lại rẻ và an toàn nhờ bản kê).
func (*Regenerator) Lookup(context.Context, queue.Job) (string, error) {
	return commands.StatusNotSent, nil
}

// Execute implements commands.Handler (ngoài transaction): đọc tệp (transaction đọc ngắn), kiểm ảnh gốc còn trên kho,
// rồi tạo biến thể như file.process với nguồn là ảnh gốc. Tệp không còn ready, đã xóa, đã có bản này → done bỏ qua;
// ảnh gốc mất → rejected source_missing (tệp vẫn ready với bản cũ).
func (r *Regenerator) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	var pl regeneratePayload
	if j.TenantID == nil || json.Unmarshal(j.Payload, &pl) != nil || pl.FileID == uuid.Nil {
		return rejected(reasonBadPayload), nil
	}
	if _, ok := Preset(pl.Preset); !ok {
		return rejected(reasonUnknownPreset), nil
	}
	jb := job{tenant: *j.TenantID, fileID: pl.FileID}
	var have bool
	err := tenancy.WithTenant(ctx, r.o.DB, jb.tenant, func(tx pgx.Tx) error {
		s := store.New(tx)
		var err error
		if jb.row, err = s.GetFile(ctx, jb.fileID); err != nil {
			return err //nolint:wrapcheck // xử lý ở dưới
		}
		vs, err := s.ListFileVariantsFull(ctx, jb.fileID)
		for _, v := range vs {
			have = have || int(v.PresetVersion) == pl.Preset
		}
		return err //nolint:wrapcheck // xử lý ở dưới
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return rejected(reasonNotFound), nil
	case err != nil:
		return notSent("db", 0), nil
	}
	purpose, ok := PurposeOf(jb.row.Purpose)
	switch {
	case !ok:
		return rejected(reasonUnknownPurpose), nil
	case purpose.Private:
		return rejected(reasonPurposePrivate), nil
	case !purpose.Image:
		return rejected(reasonNotImagePurpose), nil
	case jb.row.DeletedAt.Valid || jb.row.Status != StatusReady || have:
		return commands.Outcome{Status: commands.StatusDone, Result: json.RawMessage(`{"skipped":"not_needed"}`)}, nil
	case len(jb.row.ContentSha256) != 32:
		return rejected(reasonNotProcessable), nil
	}
	jb.purpose = purpose
	sum := hex.EncodeToString(jb.row.ContentSha256)
	switch _, err := r.o.Store.Head(ctx, BucketOriginals, OriginalKey(sum)); {
	case errors.Is(err, ErrObjectNotFound):
		return rejected(reasonSourceMissing), nil
	case err != nil:
		return notSent("store", 0), nil
	}
	o := r.o
	o.PresetVersion = pl.Preset
	return newProcessor(o, false).makeVariants(ctx, jb, sum, BucketOriginals, OriginalKey(sum))
}

// OnDone implements commands.DoneHook: khóa tệp (ADR-46 nhóm 12b); còn ready, chưa xóa, cùng mã băm → chèn biến thể
// của bộ kích thước mới (ON CONFLICT DO NOTHING), phát báo hiệu. Không đổi trạng thái hay preset_version của tệp.
func (r *Regenerator) OnDone(ctx context.Context, j queue.Job, result json.RawMessage, tx pgx.Tx) error {
	var pl regeneratePayload
	var res processResult
	if j.TenantID == nil || len(result) == 0 || json.Unmarshal(j.Payload, &pl) != nil || json.Unmarshal(result, &res) != nil ||
		res.Skipped != "" || res.Preset != pl.Preset {
		return nil
	}
	s := store.New(tx)
	row, err := s.LockFile(ctx, pl.FileID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("khóa tệp %s: %w", pl.FileID, err)
	case row.DeletedAt.Valid || row.Status != StatusReady || hex.EncodeToString(row.ContentSha256) != res.SHA256:
		return nil
	}
	for _, v := range res.Variants {
		if err := s.InsertFileVariant(ctx, store.InsertFileVariantParams{
			TenantID: row.TenantID, FileID: row.ID, PresetVersion: int32(res.Preset), Width: int32(v.Width), //nolint:gosec // G115: bản, chiều rộng nhỏ
			Format: v.Format, Bucket: string(BucketPublic), Key: v.Key, SizeBytes: v.Size,
		}); err != nil {
			return fmt.Errorf("ghi biến thể v%d của %s: %w", res.Preset, row.ID, err)
		}
	}
	return newProcessor(r.o, false).publish(ctx, tx, row.TenantID, row.ID)
}
