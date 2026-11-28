-- Mở bán: điều kiện lên kệ, hiện hàng loạt, ẩn lại (kế hoạch M7, Task 261 / M7-16). Chạy trong tenancy.WithTenant.
-- Việc của cả shop (catalog.launch.publish, ScopeNone): mọi câu lọc seller_id = ANY(mọi người bán của tenant) (ADR-29,
-- tests/arch/sellerfilter_test.go; KH14: mẫu của người bán thứ hai cũng hiện được). Thứ tự khóa ADR-46 nhóm 4: chỉ
-- khóa catalog.products theo id tăng dần; biến thể, ảnh, phân loại chỉ đọc qua catalog.product_readiness_v.

-- name: LaunchSellers :many
SELECT id FROM catalog.sellers ORDER BY id;

-- name: LockLaunchProducts :many
-- Khóa mẫu chưa xóa ở trạng thái from_status của các người bán sellers: mọi mẫu (every) hoặc các mã ids. Yêu cầu chờ
-- khóa thấy bản mới của dòng (READ COMMITTED đánh giá lại điều kiện): mẫu vừa đổi trạng thái rơi khỏi kết quả.
SELECT p.id, p.seller_id
FROM catalog.products AS p
WHERE p.seller_id = ANY(@sellers::uuid[]) AND p.status = @from_status::text AND p.deleted_at IS NULL
  AND (@every::bool OR p.id = ANY(@ids::uuid[]))
ORDER BY p.id
FOR UPDATE;

-- name: SetLaunchStatus :many
-- Đổi trạng thái các mẫu ids (đã khóa) từ from_status sang to_status; published_at NULL khi ẩn lại; version tăng 1
-- (Seq của product.updated).
UPDATE catalog.products AS p
SET status = @to_status::text, published_at = sqlc.narg(published_at)::timestamptz, version = p.version + 1,
    updated_at = now()
WHERE p.seller_id = ANY(@sellers::uuid[]) AND p.id = ANY(@ids::uuid[]) AND p.status = @from_status::text
RETURNING p.id, p.seller_id, p.version;

-- name: ReadinessOf :many
-- Điều kiện lên kệ của các mã ids, hoặc (drafts) của mọi mẫu nháp; theo mã tăng dần.
SELECT r.product_id, p.code, p.name, r.status, r.ready, r.reasons::text[] AS reasons
FROM catalog.product_readiness_v AS r
JOIN catalog.products AS p ON p.id = r.product_id
WHERE p.seller_id = ANY(@sellers::uuid[])
  AND ((@drafts::bool AND r.status = 'draft') OR r.product_id = ANY(@ids::uuid[]))
ORDER BY r.product_id;

-- name: ReadinessPage :many
-- Một trang mẫu nháp có mã lớn hơn after, lọc theo ready (NULL: mọi mẫu).
SELECT r.product_id, p.code, p.name, r.status, r.ready, r.reasons::text[] AS reasons
FROM catalog.product_readiness_v AS r
JOIN catalog.products AS p ON p.id = r.product_id
WHERE p.seller_id = ANY(@sellers::uuid[]) AND r.status = 'draft' AND r.product_id > @after::uuid
  AND (sqlc.narg(ready)::bool IS NULL OR r.ready = sqlc.narg(ready)::bool)
ORDER BY r.product_id
LIMIT @lim::int;

-- name: ReadinessCounts :one
-- Số mẫu nháp đủ và chưa đủ điều kiện của cả shop.
SELECT coalesce(count(*) FILTER (WHERE r.ready), 0)::int AS ready_count,
       coalesce(count(*) FILTER (WHERE NOT r.ready), 0)::int AS not_ready_count
FROM catalog.product_readiness_v AS r
JOIN catalog.products AS p ON p.id = r.product_id
WHERE p.seller_id = ANY(@sellers::uuid[]) AND r.status = 'draft';
