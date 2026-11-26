package inbound

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Lệnh đọc hồ sơ kênh của khách mới (Task 155; CH08): chỉ đọc, hạn 1 giờ, mỗi lần 30 s.
const (
	fetchProfileName    = "command.fb.fetch_profile"
	fetchProfileExpiry  = time.Hour
	fetchProfileTimeout = 30 * time.Second
)

// Mã lý do của lệnh đọc hồ sơ.
const (
	reasonNoProfileFetcher = "no_profile_fetcher"
	reasonNoCredentials    = "no_credentials"
	reasonProfileDenied    = "profile_denied"
	reasonProfile          = "profile"
)

// FetchProfileKind là loại lệnh đọc hồ sơ.
func FetchProfileKind() queue.Kind {
	return commands.CommandKind(fetchProfileName, fetchProfileExpiry, fetchProfileTimeout)
}

// profilePayload là payload của lệnh đọc hồ sơ: không mang PSID (Execute đọc từ hội thoại).
type profilePayload struct {
	CustomerID     uuid.UUID `json:"customerId"`
	ConversationID uuid.UUID `json:"conversationId"`
}

// profileResult là kết quả lệnh (jobs.result, truyền cho OnDone): chỉ ảnh đại diện đã ghi lên kho (nếu có). Tên khách
// (dữ liệu cá nhân) không bao giờ vào jobs.result: Execute ghi tên trong transaction riêng (setName).
type profileResult struct {
	Avatar *storedFile `json:"avatar,omitempty"`
}

// OnNewCustomer xếp lệnh đọc hồ sơ kênh (tên, ảnh đại diện) cho khách customerID vừa tạo từ tin vào của hội thoại
// conversationID (tenant tenantID); DedupeKey là mã khách (một lệnh mỗi khách).
func OnNewCustomer(ctx context.Context, tx pgx.Tx, q queue.JobQueue, tenantID, customerID, conversationID uuid.UUID) error {
	if _, _, err := q.Enqueue(ctx, tx, FetchProfileKind(), queue.NewJob{
		TenantID: &tenantID, Payload: profilePayload{CustomerID: customerID, ConversationID: conversationID},
		DedupeKey: customerID.String(),
	}); err != nil {
		return fmt.Errorf("ghi lệnh đọc hồ sơ khách: %w", err)
	}
	return nil
}

// fetchProfile là lệnh command.fb.fetch_profile.
type fetchProfile struct{ d deps.Deps }

func (fetchProfile) Kind() queue.Kind { return FetchProfileKind() }

// AutoRetryNotSent: lệnh chỉ đọc, lỗi tạm thì tự thử lại trong hạn.
func (fetchProfile) AutoRetryNotSent() bool { return true }

// profileSource là thứ cần để đọc hồ sơ, đọc trong một transaction chỉ đọc.
type profileSource struct {
	acc     channel.ChannelAccount
	adapter channel.MessagingAdapter
	pf      channel.ProfileFetcher
	cred    channel.Credentials
	user    string
	refusal string // khác rỗng: không đọc được (rejected)
	gone    bool
}

// Execute đọc hồ sơ PSID của hội thoại rồi ghi tên (setName, transaction riêng): lỗi tạm của kênh (chưa gửi, quá tải, không rõ) → not_sent; kênh từ chối (thiếu
// quyền Business Asset User Profile Access, token hỏng) → rejected, tên tạm giữ nguyên. Ảnh đại diện (≤ 2 MiB, JPEG,
// PNG, WebP) ghi lên bucket private; không tải được thì bỏ qua ảnh, vẫn đặt tên (tệp task-155, Quyết định 7).
func (c fetchProfile) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	var p profilePayload
	if err := json.Unmarshal(j.Payload, &p); err != nil || p.ConversationID == uuid.Nil || j.TenantID == nil {
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonBadPayload}, nil
	}
	tenant := *j.TenantID
	src, err := readTx(ctx, c.d, tenant, func(tx pgx.Tx) (profileSource, error) { return c.source(ctx, tx, p.ConversationID) })
	switch {
	case err != nil:
		return notSentDB(), nil
	case src.gone:
		return nothingToDo(), nil
	case src.refusal != "":
		return commands.Outcome{Status: commands.StatusRejected, Reason: src.refusal}, nil
	}
	prof, err := src.pf.FetchProfile(ctx, src.acc, src.cred, src.user)
	if err != nil {
		switch src.adapter.ClassifyError(err) {
		case channel.ErrorNotSentRetry, channel.ErrorRateLimited, channel.ErrorUnknown:
			return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonProfile}, nil
		case channel.ErrorAuth, channel.ErrorPolicy, channel.ErrorPermanent:
		}
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonProfileDenied}, nil
	}
	if err := c.setName(ctx, tenant, p.CustomerID, strings.TrimSpace(prof.Name)); err != nil {
		return notSentDB(), nil
	}
	raw, err := json.Marshal(profileResult{Avatar: c.avatar(ctx, tenant, src.adapter, prof.AvatarURL)})
	if err != nil {
		return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonProfile}, nil
	}
	return commands.Outcome{Status: commands.StatusDone, Result: raw}, nil
}

// source đọc hội thoại (PSID), tài khoản, adapter đọc hồ sơ và token của Page.
func (c fetchProfile) source(ctx context.Context, tx pgx.Tx, convID uuid.UUID) (profileSource, error) {
	conv, err := store.New(tx).GetConversationForProfile(ctx, convID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return profileSource{gone: true}, nil
	case err != nil:
		return profileSource{}, fmt.Errorf("đọc hội thoại cần hồ sơ: %w", err)
	case conv.Kind != "inbox":
		return profileSource{gone: true}, nil
	}
	acc, err := channel.Account(ctx, tx, conv.ChannelAccountID)
	if err != nil {
		return profileSource{}, err //nolint:wrapcheck // channel đã bọc việc đang làm
	}
	src := profileSource{acc: acc, user: conv.ExternalThreadRef}
	if c.d.Registry != nil {
		src.adapter, _ = c.d.Registry.Get(acc.Channel)
	}
	if src.adapter != nil {
		src.pf, _ = src.adapter.(channel.ProfileFetcher)
	}
	if src.pf == nil {
		src.refusal = reasonNoProfileFetcher
		return src, nil
	}
	src.cred, err = channel.CredentialsFor(ctx, tx, c.d.Keys, acc.ID)
	if err != nil { // chưa có token, chưa có khóa, bản mã hỏng: đọc lại cũng không được
		src.refusal = reasonNoCredentials
	}
	return src, nil
}

// avatar tải ảnh đại diện rawURL (≤ 2 MiB, JPEG/PNG/WebP) lên bucket private; mọi lỗi → nil (bỏ qua ảnh).
func (c fetchProfile) avatar(ctx context.Context, tenant uuid.UUID, ad channel.MessagingAdapter, rawURL string) *storedFile {
	dl, ok := ad.(channel.AttachmentDownloader)
	if rawURL == "" || !ok || c.d.Files == nil {
		return nil
	}
	rc, info, err := dl.Download(ctx, rawURL, maxAvatarBytes)
	if err != nil {
		return nil
	}
	defer func() { _ = rc.Close() }()
	mt := mediaType(info.ContentType)
	id, err := uuid.NewV7()
	if err != nil || !slices.Contains(imageTypes(), mt) {
		return nil
	}
	if err := c.d.Files.Put(ctx, files.BucketPrivate, files.ServerKey(tenant, id), rc, info.Size, mt); err != nil &&
		!errors.Is(err, files.ErrObjectExists) {
		return nil
	}
	return &storedFile{FileID: &id, ContentType: mt, Size: info.Size, SHA256: info.SHA256, Image: true}
}

// setName đặt tên từ kênh cho khách customerID qua customer.Writer.SetNameFromChannel trong một transaction tenant
// riêng (chỉ khóa dòng khách, nhóm 5; tên nhân viên đã sửa không bị đè; gọi lại cùng tên không đổi gì). Tên rỗng,
// khách không còn hay tên sai → bỏ qua; lỗi khác (database) → trả lỗi để lệnh thử lại.
func (c fetchProfile) setName(ctx context.Context, tenant, customerID uuid.UUID, name string) error {
	if name == "" || customerID == uuid.Nil {
		return nil
	}
	w := customer.Writer{Events: c.d.Events, Queue: c.d.Queue, Realtime: c.d.Bus}
	return tenancy.WithTenant(ctx, c.d.DB, tenant, func(tx pgx.Tx) error { //nolint:wrapcheck // bên gọi trả not_sent
		_, err := w.SetNameFromChannel(ctx, tx, customerID, name)
		if errors.Is(err, customer.ErrNotFound) || errors.Is(err, customer.ErrInvalidInput) {
			return nil
		}
		return err //nolint:wrapcheck // customer đã bọc việc đang làm
	})
}

// OnDone (ADR-46: hội thoại nhóm 6 → platform.files 12b): ảnh đại diện → conversations.avatar_file_id rồi dòng tệp
// chat_avatar (ready); báo hiệu hội thoại đổi.
func (c fetchProfile) OnDone(ctx context.Context, j queue.Job, result json.RawMessage, tx pgx.Tx) error {
	var p profilePayload
	var r profileResult
	if json.Unmarshal(j.Payload, &p) != nil || json.Unmarshal(result, &r) != nil || j.TenantID == nil {
		return nil
	}
	tenant := *j.TenantID
	if r.Avatar == nil || r.Avatar.FileID == nil {
		return nil
	}
	id := *r.Avatar.FileID
	page, err := store.New(tx).SetConversationAvatar(ctx, store.SetConversationAvatarParams{
		FileID: pgtype.UUID{Bytes: id, Valid: true}, ID: p.ConversationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("ghi ảnh đại diện của hội thoại: %w", err)
	}
	sum, _ := hex.DecodeString(r.Avatar.SHA256)
	if len(sum) != 32 {
		sum = nil
	}
	if _, err := files.Register(ctx, tx, files.NewFile{
		ID: id, Purpose: PurposeChatAvatar, Key: files.ServerKey(tenant, id), SHA256: sum, Size: r.Avatar.Size,
		ContentType: r.Avatar.ContentType, FileName: fileName("avatar", r.Avatar.SHA256, r.Avatar.ContentType),
	}); err != nil {
		return fmt.Errorf("ghi ảnh đại diện của khách: %w", err)
	}
	return access.Signal(ctx, tx, c.d.Bus, tenant, deps.TopicConversationChanged, page, deps.EntityConversation, //nolint:wrapcheck // access đã bọc
		p.ConversationID)
}
