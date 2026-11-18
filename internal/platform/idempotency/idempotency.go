// Package idempotency là khóa chống trùng của yêu cầu ghi (ADR-41; vùng rủi ro cao): bên gọi gửi một khóa, Acquire
// ghi nhận khóa ở trạng thái running bằng một transaction ngắn riêng, rồi Claim.Complete ghi "đã xong" kèm kết quả
// **trong transaction nghiệp vụ**. Lần gửi lại cùng khóa nhận kết quả đã lưu, đang chạy thì ErrInProgress, khác nội
// dung thì ErrKeyReused. Dòng running quá 60 s (tiến trình trước đã chết) được nhận lại.
//
// Mọi câu chạy trong tenancy.WithTenant: bảng platform.idempotency_keys có RLS. Gói không biết HTTP; lớp httpapi đổi
// kết quả sang mã trả lời.
package idempotency

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/internal/store"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Thời hạn của ADR-41: "đang chạy" giữ 60 s, khóa giữ 72 giờ.
const (
	LockFor   = 60 * time.Second
	KeepFor   = 72 * time.Hour
	releaseIn = 5 * time.Second // hạn của Release chạy trên ctx đã tách khỏi việc hủy của bên gọi
)

var (
	// ErrInProgress báo một yêu cầu cùng khóa đang chạy (running còn hạn). Bên gọi thử lại với đúng khóa đó.
	ErrInProgress = errors.New("idempotency: yêu cầu cùng khóa đang chạy")
	// ErrKeyReused báo cùng khóa nhưng nội dung yêu cầu (mã băm) khác lần trước; không thực hiện.
	ErrKeyReused = errors.New("idempotency: khóa đã dùng cho yêu cầu khác")
	// ErrClaimLost báo Claim đã bị yêu cầu khác nhận lại (giữ quá 60 s), nên không được ghi "xong"; transaction nghiệp
	// vụ phải lùi.
	ErrClaimLost = errors.New("idempotency: khóa đã bị yêu cầu khác nhận lại")
)

// Scope là phạm vi của khóa (ADR-41): tenant, người gọi (người dùng hoặc khóa dịch vụ), route.
type Scope struct {
	TenantID uuid.UUID
	Actor    string
	Route    string
}

// Stored là kết quả cuối đã lưu của một khóa. Body là đúng từng byte đã đưa cho Claim.Complete (cột response_raw,
// Task 396); nil khi handler không trả thân hoặc route không lưu thân (no-replay của ADR-47).
type Stored struct {
	Status int
	Body   []byte
}

// Claim là quyền chạy yêu cầu của một khóa mới (hoặc vừa nhận lại).
type Claim interface {
	// Complete ghi "đã xong" kèm status và body (JSON hợp lệ, lưu nguyên từng byte; nil → NULL) trong tx của nghiệp
	// vụ; phải là câu cuối của transaction (thứ tự khóa ADR-46 nhóm 13). Claim đã bị nhận lại → ErrClaimLost.
	Complete(ctx context.Context, tx pgx.Tx, status int, body []byte) error
	// Release xóa dòng khóa (câu trả lời "thử lại" không được lưu); chạy transaction riêng, kể cả khi ctx đã hủy.
	// Claim đã bị nhận lại thì không xóa gì.
	Release(ctx context.Context) error
}

// claim giữ khóa và thẻ chủ (locked_until mà nó đặt).
type claim struct {
	d     *db.DB
	scope Scope
	key   string
	token time.Time
}

// Acquire ghi nhận khóa bằng một transaction ngắn, commit riêng (ADR-41). Khóa mới: chèn running, locked_until =
// now + 60 s, expires_at = now + 72 h, trả Claim. Đã có dòng: mã băm khác → ErrKeyReused; done → *Stored; running
// còn hạn → ErrInProgress; running quá hạn → nhận lại (Claim mới); dòng đã quá expires_at mà chưa bị dọn → coi như
// khóa mới. Lỗi tạm hết lượt bọc db.ErrRetryLater.
//
//nolint:ireturn // Interfaces của kế hoạch M0 Task 9 khai Acquire trả Claim (interface)
func Acquire(ctx context.Context, d *db.DB, s Scope, key string, reqHash []byte, now time.Time) (Claim, *Stored, error) {
	now = now.UTC().Truncate(time.Microsecond) // độ chính xác của timestamptz: thẻ chủ so bằng đúng giá trị
	token := now.Add(LockFor)
	var (
		won    bool
		stored *Stored
	)
	err := tenancy.WithTenant(ctx, d, s.TenantID, func(tx pgx.Tx) error {
		won, stored = false, nil // fn có thể chạy lại: gán lại kết quả ở mỗi lần
		var err error
		won, stored, err = begin(ctx, store.New(tx), s, key, reqHash, now, token)
		return err
	}, db.TxAPI)
	if err != nil {
		return nil, nil, fmt.Errorf("ghi nhận khóa chống trùng: %w", err)
	}
	if !won {
		return nil, stored, nil
	}
	return &claim{d: d, scope: s, key: key, token: token}, nil, nil
}

// begin là thân transaction của Acquire: won khi bên gọi giữ khóa.
func begin(ctx context.Context, q *store.Queries, s Scope, key string, reqHash []byte, now, token time.Time) (bool, *Stored, error) {
	ts := func(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }
	n, err := q.InsertIdempotencyKey(ctx, store.InsertIdempotencyKeyParams{
		TenantID: s.TenantID, Actor: s.Actor, Route: s.Route, Key: key, RequestHash: reqHash,
		LockedUntil: ts(token), ExpiresAt: now.Add(KeepFor),
	})
	if err != nil {
		return false, nil, fmt.Errorf("chèn khóa: %w", err)
	}
	if n == 1 {
		return true, nil, nil
	}
	// Câu thứ hai: ảnh chụp mới thấy dòng mà yêu cầu song song vừa commit; FOR UPDATE giữ dòng tới hết transaction.
	row, err := q.LockIdempotencyKey(ctx, store.LockIdempotencyKeyParams{TenantID: s.TenantID, Actor: s.Actor, Route: s.Route, Key: key})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil, ErrInProgress // dòng vừa bị Release giữa hai câu: bên gọi thử lại
	}
	if err != nil {
		return false, nil, fmt.Errorf("đọc khóa: %w", err)
	}
	if !row.ExpiresAt.After(now) {
		if _, err := q.ResetExpiredIdempotencyKey(ctx, store.ResetExpiredIdempotencyKeyParams{
			RequestHash: reqHash, LockedUntil: ts(token), Now: now, ExpiresAt: now.Add(KeepFor),
			TenantID: s.TenantID, Actor: s.Actor, Route: s.Route, Key: key,
		}); err != nil {
			return false, nil, fmt.Errorf("làm mới khóa hết hạn: %w", err)
		}
		return true, nil, nil
	}
	if string(row.RequestHash) != string(reqHash) {
		return false, nil, ErrKeyReused
	}
	if row.Status == "done" {
		return false, &Stored{Status: int(row.ResponseStatus.Int32), Body: row.ResponseRaw}, nil
	}
	n, err = q.ReclaimIdempotencyKey(ctx, store.ReclaimIdempotencyKeyParams{
		LockedUntil: ts(token), TenantID: s.TenantID, Actor: s.Actor, Route: s.Route, Key: key, Now: ts(now),
	})
	if err != nil {
		return false, nil, fmt.Errorf("nhận lại khóa: %w", err)
	}
	if n == 0 {
		return false, nil, ErrInProgress
	}
	return true, nil, nil
}

func (c *claim) Complete(ctx context.Context, tx pgx.Tx, status int, body []byte) error {
	n, err := store.New(tx).CompleteIdempotencyKey(ctx, store.CompleteIdempotencyKeyParams{
		ResponseStatus: pgtype.Int4{Int32: int32(status), Valid: true}, //nolint:gosec // mã HTTP 100–599
		ResponseRaw:    body,
		TenantID:       c.scope.TenantID, Actor: c.scope.Actor, Route: c.scope.Route, Key: c.key,
		LockedUntil: pgtype.Timestamptz{Time: c.token, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("ghi khóa đã xong: %w", err)
	}
	if n == 0 {
		return ErrClaimLost
	}
	return nil
}

func (c *claim) Release(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), releaseIn)
	defer cancel()
	err := tenancy.WithTenant(ctx, c.d, c.scope.TenantID, func(tx pgx.Tx) error {
		_, err := store.New(tx).ReleaseIdempotencyKey(ctx, store.ReleaseIdempotencyKeyParams{
			TenantID: c.scope.TenantID, Actor: c.scope.Actor, Route: c.scope.Route, Key: c.key,
			LockedUntil: pgtype.Timestamptz{Time: c.token, Valid: true},
		})
		return err //nolint:wrapcheck // bọc một lần bên dưới
	}, db.TxAPI)
	if err != nil {
		return fmt.Errorf("trả lại khóa chống trùng: %w", err)
	}
	return nil
}
