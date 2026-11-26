package inbound

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Trạng thái của một mục đính kèm (cột messages.attachments): pending → stored, hoặc một lý do không lưu được.
const (
	attPending     = "pending"
	attStored      = "stored"
	attBlocked     = "blocked"     // địa chỉ không thuộc máy chủ của kênh, hay trỏ vào mạng nội bộ
	attUnavailable = "unavailable" // máy chủ của kênh báo tệp không còn (403, 404, 410)
	attTooLarge    = "too_large"   // quá 25 MiB
	attFailed      = "failed"      // hết lượt thử, hay kênh không cho tải
	attExpired     = "expired"     // quá hạn 1 giờ mà chưa tải được
)

// Mã lý do của kết quả lệnh (last_error; không có URL, nội dung).
const (
	reasonDB           = "db"
	reasonDownload     = "download"
	reasonStore        = "store"
	reasonNoDownloader = "no_downloader"
	reasonNoStore      = "no_file_store"
)

// octetStream là loại ghi trên kho cho tệp không phải ảnh: đường dẫn ký trả đúng loại này, kèm attachment (TP04).
const octetStream = "application/octet-stream"

// storedFile là tệp lệnh đã ghi lên kho (jobs.result, truyền cho OnDone). FileID nil: không có gì để ghi (tin không
// còn, mục không còn pending).
type storedFile struct {
	FileID      *uuid.UUID `json:"fileId,omitempty"`
	ContentType string     `json:"contentType,omitempty"`
	Size        int64      `json:"size,omitempty"`
	SHA256      string     `json:"sha256,omitempty"`
	Image       bool       `json:"image,omitempty"`
}

// fetchAttachment là lệnh command.fb.fetch_attachment: Execute ngoài transaction, OnDone và OnTerminal trong
// transaction của Runner.
type fetchAttachment struct{ d deps.Deps }

func (fetchAttachment) Kind() queue.Kind { return deps.FetchAttachmentKind() }

// AutoRetryNotSent: lệnh chỉ đọc, lỗi tạm thì tự thử lại trong hạn.
func (fetchAttachment) AutoRetryNotSent() bool { return true }

// source là thứ cần để tải một mục, đọc trong một transaction chỉ đọc.
type source struct {
	url  string
	dl   channel.AttachmentDownloader
	gone bool // tin không còn hay mục không còn pending: không làm gì
}

// Execute tải mục j.Index của tin: (1) transaction chỉ đọc: URL của mục còn pending, adapter của Page; (2) tải (tối đa
// 25 MiB, chỉ từ máy chủ của kênh, loại nhận dạng theo nội dung); (3) ảnh JPEG/PNG/WebP → kho crm-tmp ở TmpKey (ảnh
// thô có thể còn GPS, R10); loại khác (cả GIF, SVG, HTML giả ảnh) → bucket private ở ServerKey, ghi loại
// application/octet-stream. Lỗi vĩnh viễn ghi lý do vào mục rồi rejected; lỗi tạm not_sent.
func (c fetchAttachment) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	var p deps.AttachmentPayload
	if err := json.Unmarshal(j.Payload, &p); err != nil || p.MessageID == uuid.Nil || j.TenantID == nil {
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonBadPayload}, nil
	}
	tenant := *j.TenantID
	src, err := readTx(ctx, c.d, tenant, func(tx pgx.Tx) (source, error) { return c.source(ctx, tx, p) })
	switch {
	case err != nil:
		return notSentDB(), nil
	case src.gone:
		return nothingToDo(), nil
	case src.dl == nil:
		return c.refuse(ctx, tenant, p, attFailed, reasonNoDownloader), nil
	case c.d.Files == nil:
		return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonNoStore}, nil
	}
	rc, info, err := src.dl.Download(ctx, src.url, maxAttachmentBytes)
	if err != nil {
		if reason := downloadReason(err); reason != "" {
			return c.refuse(ctx, tenant, p, reason, reason), nil
		}
		return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonDownload}, nil
	}
	defer func() { _ = rc.Close() }()
	out, ok := c.put(ctx, tenant, rc, info)
	if !ok {
		return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonStore}, nil
	}
	raw, err := json.Marshal(out)
	if err != nil { // chuỗi và số, không lỗi
		return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonStore}, nil
	}
	return commands.Outcome{Status: commands.StatusDone, Result: raw}, nil
}

// readTx chạy read trong một transaction tenant và trả kết quả của lần chạy cuối (RetryTx có thể chạy lại).
//
//nolint:ireturn // S là kiểu cụ thể của bên gọi (source, profileSource)
func readTx[S any](ctx context.Context, d deps.Deps, tenant uuid.UUID, read func(pgx.Tx) (S, error)) (S, error) {
	var out S
	err := tenancy.WithTenant(ctx, d.DB, tenant, func(tx pgx.Tx) error {
		var err error
		out, err = read(tx)
		return err
	})
	return out, err //nolint:wrapcheck // lỗi tạm của database, bên gọi trả not_sent
}

// notSentDB: lỗi tạm của database trước khi gọi kênh (chưa đọc gì từ ngoài): thử lại.
func notSentDB() commands.Outcome {
	return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonDB}
}

// nothingToDo: tin, hội thoại không còn (hay mục đã xong): xong, OnDone không ghi gì.
func nothingToDo() commands.Outcome {
	return commands.Outcome{Status: commands.StatusDone, Result: json.RawMessage(`{}`)}
}

// source đọc URL của mục p.Index (còn pending) và adapter tải tệp của Page.
func (c fetchAttachment) source(ctx context.Context, tx pgx.Tx, p deps.AttachmentPayload) (source, error) {
	m, err := store.New(tx).GetMessageAttachments(ctx, p.MessageID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return source{gone: true}, nil
	case err != nil:
		return source{}, fmt.Errorf("đọc tin cần tải tệp: %w", err)
	}
	var items []deps.PendingAttachment
	if err := json.Unmarshal(m.Attachments, &items); err != nil || p.Index < 0 || p.Index >= len(items) ||
		items[p.Index].Status != attPending || items[p.Index].URL == "" {
		return source{gone: true}, nil //nolint:nilerr // cột hỏng hay mục đã xong = không còn gì để tải
	}
	acc, err := channel.Account(ctx, tx, m.ChannelAccountID)
	if err != nil {
		return source{}, err //nolint:wrapcheck // channel đã bọc việc đang làm
	}
	src := source{url: items[p.Index].URL}
	if c.d.Registry != nil {
		if ad, ok := c.d.Registry.Get(acc.Channel); ok {
			src.dl, _ = ad.(channel.AttachmentDownloader)
		}
	}
	return src, nil
}

// downloadReason là trạng thái của mục khi lỗi tải là vĩnh viễn; rỗng: lỗi tạm.
func downloadReason(err error) string {
	switch {
	case errors.Is(err, channel.ErrDownloadBlocked):
		return attBlocked
	case errors.Is(err, channel.ErrDownloadUnavailable):
		return attUnavailable
	case errors.Is(err, channel.ErrDownloadTooLarge):
		return attTooLarge
	}
	return ""
}

// put ghi tệp đã tải lên kho (ngoài transaction) với mã tệp mới; đã có đối tượng ở khóa đó (chạy lại) coi như đã ghi.
func (c fetchAttachment) put(ctx context.Context, tenant uuid.UUID, r io.Reader, info channel.DownloadInfo) (storedFile, bool) {
	id, err := uuid.NewV7()
	if err != nil {
		return storedFile{}, false
	}
	mt := mediaType(info.ContentType)
	out := storedFile{FileID: &id, ContentType: info.ContentType, Size: info.Size, SHA256: info.SHA256}
	bucket, key, ct := files.BucketPrivate, files.ServerKey(tenant, id), octetStream
	if slices.Contains(imageTypes(), mt) {
		out.Image, out.ContentType = true, mt
		bucket, key, ct = files.BucketTmp, files.TmpKey(tenant, id), mt
	}
	if err := c.d.Files.Put(ctx, bucket, key, r, info.Size, ct); err != nil && !errors.Is(err, files.ErrObjectExists) {
		return storedFile{}, false
	}
	return out, true
}

// refuse ghi trạng thái state vào mục (transaction ngắn riêng) rồi trả rejected với mã reason. Ghi lỗi thì OnTerminal
// vẫn đặt mục failed.
func (c fetchAttachment) refuse(ctx context.Context, tenant uuid.UUID, p deps.AttachmentPayload, state, reason string) commands.Outcome {
	_ = tenancy.WithTenant(ctx, c.d.DB, tenant, func(tx pgx.Tx) error {
		return c.patch(ctx, tx, tenant, p, attachmentPatch{Status: state})
	})
	return commands.Outcome{Status: commands.StatusRejected, Reason: reason}
}

// attachmentPatch là phần ghép vào mục đính kèm.
type attachmentPatch struct {
	Status       string     `json:"status"`
	FileID       *uuid.UUID `json:"fileId,omitempty"`
	DownloadOnly bool       `json:"downloadOnly,omitempty"`
}

// errNotPending: mục không còn pending (hay tin không còn) khi ghép.
var errNotPending = errors.New("mục đính kèm không còn chờ")

// patch ghép pt vào mục còn pending rồi báo hiệu hội thoại đổi; mục không còn pending → errNotPending.
func (c fetchAttachment) patch(ctx context.Context, tx pgx.Tx, tenant uuid.UUID, p deps.AttachmentPayload, pt attachmentPatch) error {
	raw, err := json.Marshal(pt)
	if err != nil {
		return fmt.Errorf("mã hóa mục đính kèm: %w", err)
	}
	row, err := store.New(tx).PatchPendingAttachment(ctx, store.PatchPendingAttachmentParams{
		Idx: int32(p.Index), Patch: raw, ID: p.MessageID, //nolint:gosec // chỉ số mục nhỏ (vài tệp một tin)
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return errNotPending
	case err != nil:
		return fmt.Errorf("ghi mục đính kèm: %w", err)
	}
	return access.Signal(ctx, tx, c.d.Bus, tenant, deps.TopicConversationChanged, row.ChannelAccountID, //nolint:wrapcheck // access đã bọc
		deps.EntityConversation, row.ConversationID)
}

// OnDone (trong transaction của Runner, ADR-46: dòng tin nhóm 6 trước, platform.files 12b, platform.jobs 13 sau): mục
// còn pending → stored kèm mã tệp; ảnh → files.RegisterPrivateImage (dòng uploaded, M1 xếp file.process_private cùng
// transaction); loại khác → files.Register (chat_attachment, ready, chỉ tải về). Tin hay mục không còn → không ghi gì
// (đối tượng đã ghi lên kho thành mồ côi, việc dọn của M1 xử lý).
func (c fetchAttachment) OnDone(ctx context.Context, j queue.Job, result json.RawMessage, tx pgx.Tx) error {
	var p deps.AttachmentPayload
	var f storedFile
	if json.Unmarshal(j.Payload, &p) != nil || json.Unmarshal(result, &f) != nil || f.FileID == nil || j.TenantID == nil {
		return nil
	}
	tenant, id := *j.TenantID, *f.FileID
	err := c.patch(ctx, tx, tenant, p, attachmentPatch{Status: attStored, FileID: &id, DownloadOnly: !f.Image})
	switch {
	case errors.Is(err, errNotPending):
		return nil
	case err != nil:
		return err
	}
	if f.Image {
		if _, err := files.RegisterPrivateImage(ctx, tx, c.d.Queue, files.NewPrivateImage{
			ID: id, Purpose: PurposeChatImage, Size: f.Size, ContentType: f.ContentType,
			FileName: fileName("anh", f.SHA256, f.ContentType),
		}); err != nil {
			return fmt.Errorf("ghi ảnh khách gửi: %w", err)
		}
		return nil
	}
	sum, _ := hex.DecodeString(f.SHA256) // adapter trả hex 64 ký tự; hỏng → nil (không lưu mã băm)
	if len(sum) != 32 {
		sum = nil
	}
	if _, err := files.Register(ctx, tx, files.NewFile{
		ID: id, Purpose: PurposeChatAttachment, Key: files.ServerKey(tenant, id), SHA256: sum, Size: f.Size,
		ContentType: f.ContentType, FileName: fileName("tep", f.SHA256, f.ContentType),
	}); err != nil {
		return fmt.Errorf("ghi tệp khách gửi: %w", err)
	}
	return nil
}

// OnTerminal: lệnh dead → mục còn pending thành failed; expired → expired (lý do cụ thể đã ghi lúc rejected thì mục
// không còn pending, giữ nguyên). in_doubt (giữ chỗ hết hạn) giữ pending: chạy lại bằng tay an toàn (chỉ đọc).
func (c fetchAttachment) OnTerminal(ctx context.Context, j queue.Job, status string, tx pgx.Tx) error {
	var p deps.AttachmentPayload
	if json.Unmarshal(j.Payload, &p) != nil || j.TenantID == nil {
		return nil
	}
	state := attFailed
	switch status {
	case queue.StatusExpired:
		state = attExpired
	case queue.StatusDead:
	default:
		return nil
	}
	if err := c.patch(ctx, tx, *j.TenantID, p, attachmentPatch{Status: state}); err != nil && !errors.Is(err, errNotPending) {
		return err
	}
	return nil
}

// mediaType là loại không kèm tham số (text/html; charset=utf-8 → text/html); hỏng → nguyên chuỗi.
func mediaType(ct string) string {
	if mt, _, err := mime.ParseMediaType(ct); err == nil {
		return mt
	}
	return ct
}

// fileName là tên tệp hiện cho nhân viên: <tiền tố>-<8 ký tự đầu mã băm>.<đuôi theo loại> (không dùng tên khách đặt).
func fileName(prefix, sha, ct string) string {
	short := sha
	if len(short) > 8 {
		short = short[:8]
	}
	return prefix + "-" + short + "." + extension(ct)
}

// extension là đuôi tệp theo loại đã nhận dạng; loại lạ → bin.
func extension(ct string) string {
	exts := map[string]string{
		"image/jpeg": "jpg", "image/png": "png", "image/webp": "webp", "image/gif": "gif", "application/pdf": "pdf",
		"text/plain": "txt", "text/html": "html", "text/xml": "xml", "image/svg+xml": "svg", "application/zip": "zip",
		"video/mp4": "mp4", "audio/mpeg": "mp3", "audio/wave": "wav", "video/webm": "webm",
	}
	if e, ok := exts[mediaType(ct)]; ok {
		return e
	}
	return "bin"
}
