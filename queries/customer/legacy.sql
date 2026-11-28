-- Chuyển khách từ hệ cũ (kế hoạch M7, Task 542). Chạy trong tenancy.WithTenant (RLS lọc tenant). Thứ tự khóa ADR-46
-- nhóm 5: khách → danh tính → … → legacy_customers → legacy_review.

-- name: LegacyMapped :many
-- Mã khách cũ (trong @ids) đã được chuyển.
SELECT legacy_id
FROM customer.legacy_customers
WHERE legacy_id = ANY (@ids::bigint[]);

-- name: LegacyPhoneHolders :many
-- Chủ của từng SĐT (danh tính phone) trong @phones: gốc của nhóm chủ, và mã khách cũ của chính hồ sơ giữ SĐT (NULL khi
-- hồ sơ không đến từ lần chuyển).
SELECT i.value,
       c.root_id,
       l.legacy_id
FROM customer.customer_identities i
JOIN customer.customers c ON c.tenant_id = i.tenant_id AND c.id = i.customer_id
LEFT JOIN customer.legacy_customers l ON l.tenant_id = i.tenant_id AND l.customer_id = i.customer_id
WHERE i.kind = 'phone'
  AND i.scope = ''
  AND i.value = ANY (@phones::text[]);

-- name: InsertLegacyCustomer :execrows
-- Ghi mã cũ → khách; 0 dòng khi mã đã có (lượt khác vừa ghi).
INSERT INTO customer.legacy_customers (tenant_id, legacy_id, customer_id)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @legacy_id, @customer_id)
ON CONFLICT (tenant_id, legacy_id) DO NOTHING;

-- name: InsertLegacyReview :exec
-- Thêm một mục cần xem; đã có (cùng mã, cùng lý do) thì giữ nguyên, kể cả khi nhân viên đã xử lý.
INSERT INTO customer.legacy_review (tenant_id, legacy_id, reason, related_customer_ids)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @legacy_id, @reason, @related::uuid[])
ON CONFLICT (tenant_id, legacy_id, reason) DO NOTHING;
