package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/queries"
)

// errNoFileStore: Deps.Files chưa nối (lỗi dựng; cmd/crm luôn nối cho api).
var errNoFileStore = errors.New("httpapi: chưa có kho tệp (Deps.Files)")

// CreateUpload (POST /api/v1/files/uploads, files.upload): kiểm giới hạn của mục đích, tạo tệp awaiting_upload và trả
// đường dẫn PUT ký sẵn. Bị từ chối (mục đích, loại, dung lượng, tên) thì không lưu vào khóa chống trùng.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CreateUpload(ctx context.Context, req *gen.CreateUploadRequest, _ gen.CreateUploadParams) (gen.CreateUploadRes, error) {
	u, ok := authUser(ctx)
	switch {
	case !ok:
		return nil, errNoAuthUser
	case h.d.Files == nil:
		return nil, errNoFileStore
	}
	in := files.UploadInput{TenantID: u.TenantID, Purpose: req.Purpose, FileName: req.FileName, ContentType: req.ContentType, SizeBytes: req.SizeBytes}
	var (
		out     *gen.Upload
		refused error
	)
	err := WriteTx(ctx, h.d, u.TenantID, func(tx pgx.Tx) (int, any, bool, error) {
		out, refused = nil, nil // fn có thể chạy lại (ADR-46)
		f, put, err := files.CreateUpload(ctx, tx, h.d.Files, in, u.UserID)
		switch {
		case isFileRefusal(err):
			refused = err
			return 0, nil, false, nil
		case err != nil:
			return 0, nil, false, err //nolint:wrapcheck // files đã bọc
		}
		out = &gen.Upload{FileId: f.ID, PutUrl: put.URL, Headers: gen.UploadHeaders(put.Headers), ExpiresAt: put.ExpiresAt}
		return http.StatusCreated, out, true, nil
	})
	switch {
	case err != nil:
		return nil, err
	case refused != nil:
		return nil, refused
	}
	return out, nil
}

// CompleteUpload (POST /api/v1/files/{id}/complete, files.upload): hỏi kho ngoài transaction (files.InspectUpload,
// ADR-46), rồi ghi kết quả cùng khóa chống trùng (files.ApplyUpload trong WriteTx). Kho chưa có tệp → 409
// upload_missing, không lưu (gửi lại sau khi tải xong); tenant khác → 404.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CompleteUpload(ctx context.Context, params gen.CompleteUploadParams) (gen.CompleteUploadRes, error) {
	u, ok := authUser(ctx)
	switch {
	case !ok:
		return nil, errNoAuthUser
	case h.d.Files == nil:
		return nil, errNoFileStore
	}
	check, err := files.InspectUpload(ctx, h.d.DB, h.d.Files, u.TenantID, params.ID)
	if err != nil {
		return nil, err //nolint:wrapcheck // files đã bọc; errors.go đổi ErrNotFound
	}
	q := queue.NewQueue()
	var (
		out     *gen.FileInfo
		refused error
	)
	err = WriteTx(ctx, h.d, u.TenantID, func(tx pgx.Tx) (int, any, bool, error) {
		out, refused = nil, nil
		f, err := files.ApplyUpload(ctx, tx, q, check)
		switch {
		case errors.Is(err, files.ErrUploadMissing), errors.Is(err, files.ErrNotFound):
			refused = err
			return 0, nil, false, nil
		case err != nil:
			return 0, nil, false, err //nolint:wrapcheck // files đã bọc
		}
		info, err := files.Describe(ctx, tx, f.ID, h.d.Config.FilesPublicBaseURL)
		if err != nil {
			return 0, nil, false, err //nolint:wrapcheck // files đã bọc
		}
		out = fileInfoOut(info)
		return http.StatusOK, out, true, nil
	})
	switch {
	case err != nil:
		return nil, err
	case refused != nil:
		return nil, refused
	}
	return out, nil
}

// GetFile (GET /api/v1/files/{id}, files.upload): trạng thái và biến thể công khai của tệp trong tenant người gọi.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetFile(ctx context.Context, params gen.GetFileParams) (gen.GetFileRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	var info files.Info
	err := tenancy.WithTenant(ctx, h.d.DB, u.TenantID, func(tx pgx.Tx) error {
		var err error
		info, err = files.Describe(ctx, tx, params.ID, h.d.Config.FilesPublicBaseURL)
		return err //nolint:wrapcheck // files đã bọc
	}, db.TxAPI)
	if err != nil {
		return nil, fmt.Errorf("đọc tệp: %w", err)
	}
	return fileInfoOut(info), nil
}

// isFileRefusal: lỗi nghiệp vụ của files mà câu trả lời không đổi gì (không lưu vào khóa chống trùng).
func isFileRefusal(err error) bool {
	for _, e := range []error{
		files.ErrUnknownPurpose, files.ErrUploadNotAllowed, files.ErrTooLarge,
		files.ErrTypeNotAllowed, files.ErrInvalidFile,
	} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

func fileInfoOut(info files.Info) *gen.FileInfo {
	out := &gen.FileInfo{ID: info.ID, Purpose: info.Purpose, Status: gen.FileInfoStatus(info.Status), Variants: []gen.FileVariant{}}
	if info.RejectReason != "" {
		out.RejectReason = gen.NewOptString(info.RejectReason)
	}
	if info.Width > 0 && info.Height > 0 {
		out.Width = gen.NewOptInt32(int32(min(info.Width, 1<<31-1)))
		out.Height = gen.NewOptInt32(int32(min(info.Height, 1<<31-1)))
	}
	for _, v := range info.Variants {
		out.Variants = append(out.Variants, gen.FileVariant{
			Width: int32(min(v.Width, 1<<31-1)), Format: gen.FileVariantFormat(v.Format), URL: v.URL, //nolint:gosec // đã chặn
		})
	}
	return out
}

// RegenerateImages (POST /api/v1/platform/files/regenerate, platform.files.manage): xếp một lệnh file.regenerate cho
// mỗi tệp ảnh ready của mục đích standard trong tenant người gọi chưa có bộ kích thước presetVersion (TP07), ghi
// audit, trả số lệnh mới xếp. Bộ kích thước chưa có → 422 unknown_preset, không lưu vào khóa chống trùng.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) RegenerateImages(ctx context.Context, req *gen.RegenerateImagesRequest, _ gen.RegenerateImagesParams) (gen.RegenerateImagesRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	preset := int(req.PresetVersion)
	q := queue.NewQueue()
	var (
		out     *gen.RegenerateImagesResult
		refused error
	)
	err := WriteTx(ctx, h.d, u.TenantID, func(tx pgx.Tx) (int, any, bool, error) {
		out, refused = nil, nil // fn có thể chạy lại (ADR-46)
		n, err := files.EnqueueRegenerate(ctx, tx, q, preset)
		switch {
		case errors.Is(err, files.ErrUnknownPreset):
			refused = err
			return 0, nil, false, nil
		case err != nil:
			return 0, nil, false, err //nolint:wrapcheck // files đã bọc
		}
		if err := identity.RecordAudit(ctx, tx, identity.AuditEntry{
			TenantID: u.TenantID, ActorType: "staff", ActorID: u.UserID.String(), Action: "platform.files.regenerate",
			EntityType: "platform.file_preset", EntityID: strconv.Itoa(preset),
			After: map[string]any{"presetVersion": preset, "enqueued": n}, RequestID: RequestID(ctx),
		}); err != nil {
			return 0, nil, false, fmt.Errorf("ghi audit platform.files.regenerate: %w", err)
		}
		out = &gen.RegenerateImagesResult{Enqueued: int32(min(n, 1<<31-1))} //nolint:gosec // đã chặn
		return http.StatusOK, out, true, nil
	})
	switch {
	case err != nil:
		return nil, err
	case refused != nil:
		return nil, refused
	}
	return out, nil
}

// VerifyFiles (POST /api/v1/platform/files/verify, platform.files.manage): xếp một lượt đối chiếu kho (lệnh
// file.verify, Task M1-46, TP12) cho tenant người gọi, ghi audit, trả 202 {jobId} (mã lệnh lô đầu). Tệp ready thiếu
// đối tượng trên kho thành lost khi lệnh chạy xong.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) VerifyFiles(ctx context.Context, _ gen.VerifyFilesParams) (gen.VerifyFilesRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	q := queue.NewQueue()
	var out *gen.VerifyFilesAccepted
	err := WriteTx(ctx, h.d, u.TenantID, func(tx pgx.Tx) (int, any, bool, error) {
		out = nil // fn có thể chạy lại (ADR-46)
		id, err := files.EnqueueVerify(ctx, tx, q)
		if err != nil {
			return 0, nil, false, err //nolint:wrapcheck // files đã bọc
		}
		if err := identity.RecordAudit(ctx, tx, identity.AuditEntry{
			TenantID: u.TenantID, ActorType: "staff", ActorID: u.UserID.String(), Action: "platform.files.verify",
			EntityType: "platform.job", EntityID: id.String(), RequestID: RequestID(ctx),
		}); err != nil {
			return 0, nil, false, fmt.Errorf("ghi audit platform.files.verify: %w", err)
		}
		out = &gen.VerifyFilesAccepted{JobId: id}
		return http.StatusAccepted, out, true, nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ListLostFiles (GET /api/v1/platform/files/lost, platform.files.manage): một trang tệp lost của tenant người gọi, mất
// gần nhất trước, kèm mẫu dùng tệp làm ảnh (queries.FileProducts qua catalog.products_v). Con trỏ hỏng → 400.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListLostFiles(ctx context.Context, params gen.ListLostFilesParams) (gen.ListLostFilesRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	var after *files.LostCursor
	if params.Cursor.Set {
		c, err := queries.ParseCursor(params.Cursor.Value)
		if err != nil {
			return nil, err //nolint:wrapcheck // queries đã bọc; errors.go đổi ErrInvalidInput
		}
		after = &files.LostCursor{At: c.UpdatedAt, ID: c.ID}
	}
	limit := int(params.Limit.Or(50))
	var (
		lost     []files.LostFile
		products map[uuid.UUID]queries.FileProduct
	)
	err := tenancy.WithTenant(ctx, h.d.DB, u.TenantID, func(tx pgx.Tx) error {
		var err error
		if lost, err = files.ListLost(ctx, tx, after, limit+1); err != nil {
			return err //nolint:wrapcheck // files đã bọc
		}
		ids := make([]uuid.UUID, 0, len(lost))
		for _, f := range lost {
			ids = append(ids, f.ID)
		}
		products, err = queries.FileProducts(ctx, tx, ids)
		return err //nolint:wrapcheck // queries đã bọc
	}, db.TxAPI)
	if err != nil {
		return nil, fmt.Errorf("liệt kê tệp mất: %w", err)
	}
	out := &gen.LostFilePage{Items: []gen.LostFile{}}
	if len(lost) > limit {
		last := lost[limit-1]
		out.NextCursor = gen.NewOptString(queries.Cursor{UpdatedAt: last.LostAt, ID: last.ID}.String())
		lost = lost[:limit]
	}
	for _, f := range lost {
		item := gen.LostFile{FileId: f.ID, Purpose: f.Purpose, LostAt: f.LostAt}
		if p, ok := products[f.ID]; ok {
			item.ProductId, item.ProductCode = gen.NewOptUUID(p.ProductID), gen.NewOptString(p.Code)
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}
