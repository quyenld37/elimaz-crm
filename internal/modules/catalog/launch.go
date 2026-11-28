package catalog

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/catalog/internal/store"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// Mở bán (kế hoạch M7, Task 261 / M7-16; MO01): mẫu nhập sẵn ở trạng thái draft (ẩn với khách), điều kiện lên kệ đọc
// từ view catalog.product_readiness_v, hiện hàng loạt (draft → active) và ẩn lại (active → draft). Việc của cả shop:
// mọi người bán (KH14), quyền catalog.launch.publish kiểm ở API.

// MaxLaunchIDs là số mã tối đa của một lần Publish, Unpublish theo mã (hợp đồng: productIds maxItems 500).
const MaxLaunchIDs = 500

// MaxReadinessPage là cỡ trang lớn nhất của ReadinessPage (tham số limit của hợp đồng: 1–100).
const MaxReadinessPage = 100

// ProductReadiness là điều kiện lên kệ của một mẫu: Ready khi Reasons rỗng. Mã lý do (thứ tự cố định): no_variant,
// no_price, no_image, image_processing, no_category, missing_attr:<mã thuộc tính>, no_tag.
type ProductReadiness struct {
	ProductID  uuid.UUID
	Code, Name string
	Status     ProductStatus
	Ready      bool
	Reasons    []string
}

// ReadinessFilter lọc trang điều kiện lên kệ: mọi mẫu nháp, chỉ mẫu đủ, chỉ mẫu còn lý do.
type ReadinessFilter string

// Các bộ lọc của ReadinessPage (giá trị là tham số status của getCatalogLaunchReadiness).
const (
	ReadinessAll      ReadinessFilter = ""
	ReadinessReady    ReadinessFilter = "ready"
	ReadinessNotReady ReadinessFilter = "not_ready"
)

// ReadinessPageResult là một trang mẫu nháp kèm điều kiện; Next là mã mẫu cuối khi trang đủ limit (uuid.Nil: hết);
// hai số đếm là của cả shop, không theo trang hay bộ lọc.
type ReadinessPageResult struct {
	Items                     []ProductReadiness
	Next                      uuid.UUID
	ReadyCount, NotReadyCount int
}

// PublishInput chọn mẫu để hiện: các mã IDs (1–MaxLaunchIDs) hoặc AllReady (mọi mẫu nháp đủ điều kiện); đúng một
// trong hai.
type PublishInput struct {
	IDs      []uuid.UUID
	AllReady bool
}

// PublishResult là mã các mẫu vừa hiện (tăng dần) và các mẫu nháp được chọn mà chưa đủ điều kiện (đánh giá dưới khóa).
// Mã gửi lên không phải mẫu nháp (đã bán, ngừng bán, đã xóa, không có) không ở danh sách nào.
type PublishResult struct {
	Published []uuid.UUID
	Skipped   []ProductReadiness
}

// Readiness trả điều kiện lên kệ của các mẫu ids (mẫu nháp hoặc đang bán chưa xóa; mã khác không có dòng), hoặc của
// mọi mẫu nháp khi ids nil; theo mã tăng dần.
func Readiness(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) ([]ProductReadiness, error) {
	q := store.New(tx)
	sellers, err := q.LaunchSellers(ctx)
	if err != nil {
		return nil, fmt.Errorf("đọc người bán: %w", err)
	}
	return readinessOf(ctx, q, sellers, ids == nil, ids)
}

func readinessOf(ctx context.Context, q *store.Queries, sellers []uuid.UUID, drafts bool, ids []uuid.UUID) ([]ProductReadiness, error) {
	rows, err := q.ReadinessOf(ctx, store.ReadinessOfParams{Sellers: sellers, Drafts: drafts, Ids: ids})
	if err != nil {
		return nil, fmt.Errorf("đọc điều kiện lên kệ: %w", err)
	}
	out := make([]ProductReadiness, len(rows))
	for i, r := range rows {
		out[i] = readinessFrom(store.ReadinessPageRow(r))
	}
	return out, nil
}

func readinessFrom(r store.ReadinessPageRow) ProductReadiness {
	reasons := r.Reasons
	if reasons == nil {
		reasons = []string{}
	}
	return ProductReadiness{
		ProductID: r.ProductID, Code: r.Code, Name: r.Name, Status: ProductStatus(r.Status), Ready: r.Ready, Reasons: reasons,
	}
}

// ReadinessPage trả một trang (limit 1–MaxReadinessPage, mã lớn hơn after) mẫu nháp theo bộ lọc f; limit ngoài khoảng
// hay bộ lọc lạ → ErrInvalidInput.
func ReadinessPage(ctx context.Context, tx pgx.Tx, f ReadinessFilter, after uuid.UUID, limit int) (ReadinessPageResult, error) {
	var ready pgtype.Bool
	switch f {
	case ReadinessAll:
	case ReadinessReady, ReadinessNotReady:
		ready = pgtype.Bool{Bool: f == ReadinessReady, Valid: true}
	default:
		return ReadinessPageResult{}, ErrInvalidInput
	}
	if limit < 1 || limit > MaxReadinessPage {
		return ReadinessPageResult{}, ErrInvalidInput
	}
	q := store.New(tx)
	sellers, err := q.LaunchSellers(ctx)
	if err != nil {
		return ReadinessPageResult{}, fmt.Errorf("đọc người bán: %w", err)
	}
	rows, err := q.ReadinessPage(ctx, store.ReadinessPageParams{Sellers: sellers, After: after, Ready: ready, Lim: int32(limit)})
	if err != nil {
		return ReadinessPageResult{}, fmt.Errorf("đọc trang điều kiện lên kệ: %w", err)
	}
	counts, err := q.ReadinessCounts(ctx, sellers)
	if err != nil {
		return ReadinessPageResult{}, fmt.Errorf("đếm điều kiện lên kệ: %w", err)
	}
	out := ReadinessPageResult{
		Items: make([]ProductReadiness, len(rows)), ReadyCount: int(counts.ReadyCount), NotReadyCount: int(counts.NotReadyCount),
	}
	for i, r := range rows {
		out.Items[i] = readinessFrom(r)
	}
	if len(rows) == limit {
		out.Next = rows[len(rows)-1].ProductID
	}
	return out, nil
}

// Publish hiện các mẫu nháp in chọn: khóa chúng theo mã tăng dần (ADR-46 nhóm 4), đọc lại điều kiện lên kệ sau khi
// giữ khóa, đưa mẫu đủ điều kiện sang active (published_at = now, version + 1), mỗi mẫu một product.updated (Seq =
// version) và một dòng audit catalog.product.publish trong tx của bên gọi. Mẫu chưa đủ → Skipped kèm lý do. Đầu vào
// sai (thiếu cả hai, có cả hai, quá MaxLaunchIDs mã) → ErrInvalidInput.
func (w Writer) Publish(ctx context.Context, tx pgx.Tx, in PublishInput, actor identity.AuthUser, now time.Time) (PublishResult, error) {
	if err := w.check(); err != nil {
		return PublishResult{}, err
	}
	if in.AllReady == (len(in.IDs) > 0) || len(in.IDs) > MaxLaunchIDs {
		return PublishResult{}, ErrInvalidInput
	}
	q := store.New(tx)
	sellers, locked, err := lockLaunch(ctx, q, StatusDraft, in.AllReady, in.IDs)
	if err != nil || len(locked) == 0 {
		return PublishResult{Published: []uuid.UUID{}, Skipped: []ProductReadiness{}}, err
	}
	rs, err := readinessOf(ctx, q, sellers, false, locked)
	if err != nil {
		return PublishResult{}, err
	}
	out := PublishResult{Published: []uuid.UUID{}, Skipped: []ProductReadiness{}}
	var ready []uuid.UUID
	for _, r := range rs {
		if r.Ready {
			ready = append(ready, r.ProductID)
		} else {
			out.Skipped = append(out.Skipped, r)
		}
	}
	if out.Published, err = w.setLaunchStatus(ctx, tx, actor, sellers, ready, StatusDraft, StatusActive, now); err != nil {
		return PublishResult{}, err
	}
	return out, nil
}

// Unpublish ẩn lại các mẫu đang bán ids (1–MaxLaunchIDs): khóa theo mã, active → draft, published_at NULL, mỗi mẫu một
// product.updated và một audit catalog.product.unpublish. Trả mã các mẫu đã ẩn (tăng dần); mã không đang bán bị bỏ
// qua. Đơn đã có giữ tên, giá lúc đặt (dòng đơn chép lại, không đọc mẫu).
func (w Writer) Unpublish(ctx context.Context, tx pgx.Tx, ids []uuid.UUID, actor identity.AuthUser, now time.Time) ([]uuid.UUID, error) {
	if err := w.check(); err != nil {
		return nil, err
	}
	if len(ids) == 0 || len(ids) > MaxLaunchIDs {
		return nil, ErrInvalidInput
	}
	q := store.New(tx)
	sellers, locked, err := lockLaunch(ctx, q, StatusActive, false, ids)
	if err != nil {
		return nil, err
	}
	return w.setLaunchStatus(ctx, tx, actor, sellers, locked, StatusActive, StatusDraft, now)
}

// lockLaunch khóa mẫu trạng thái from (mọi mẫu khi every, hay các mã ids) của mọi người bán, theo mã tăng dần.
func lockLaunch(ctx context.Context, q *store.Queries, from ProductStatus, every bool, ids []uuid.UUID) (sellers, locked []uuid.UUID, err error) {
	if sellers, err = q.LaunchSellers(ctx); err != nil {
		return nil, nil, fmt.Errorf("đọc người bán: %w", err)
	}
	rows, err := q.LockLaunchProducts(ctx, store.LockLaunchProductsParams{
		Sellers: sellers, FromStatus: string(from), Every: every, Ids: ids,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("khóa mẫu để mở bán: %w", err)
	}
	locked = make([]uuid.UUID, len(rows))
	for i, r := range rows {
		locked[i] = r.ID
	}
	return sellers, locked, nil
}

// setLaunchStatus đổi các mẫu ids (đã khóa) từ from sang to (published_at = now khi hiện, NULL khi ẩn), rồi theo mã
// tăng dần phát product.updated và ghi audit catalog.product.publish / catalog.product.unpublish (after có "at": now);
// trả mã đã đổi (tăng dần, rỗng khi ids rỗng).
func (w Writer) setLaunchStatus(ctx context.Context, tx pgx.Tx, actor identity.AuthUser, sellers, ids []uuid.UUID,
	from, to ProductStatus, now time.Time,
) ([]uuid.UUID, error) {
	if len(ids) == 0 {
		return []uuid.UUID{}, nil
	}
	var publishedAt pgtype.Timestamptz
	if to == StatusActive {
		publishedAt = pgtype.Timestamptz{Time: now.UTC(), Valid: true}
	}
	rows, err := store.New(tx).SetLaunchStatus(ctx, store.SetLaunchStatusParams{
		ToStatus: string(to), PublishedAt: publishedAt, Sellers: sellers, Ids: ids, FromStatus: string(from),
	})
	if err != nil {
		return nil, fmt.Errorf("đổi trạng thái mẫu %s → %s: %w", from, to, err)
	}
	slices.SortFunc(rows, func(x, y store.SetLaunchStatusRow) int { return slices.Compare(x.ID[:], y.ID[:]) })
	action := "catalog.product.publish"
	if to == StatusDraft {
		action = "catalog.product.unpublish"
	}
	out := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		out[i] = r.ID
		if err := w.productChanged(ctx, tx, actor.TenantID, r.SellerID, r.ID, r.Version); err != nil {
			return nil, err
		}
		if err := w.audit(ctx, tx, actor, action, "catalog.product", r.ID,
			map[string]any{"status": string(from), "version": r.Version - 1},
			map[string]any{"status": string(to), "version": r.Version, "at": now.UTC()}); err != nil {
			return nil, err
		}
	}
	return out, nil
}
