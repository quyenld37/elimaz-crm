-- Ghi chú khách: chỉ ghi thêm (kế hoạch M2, Task M2-12; trigger của 0201 chặn UPDATE, DELETE). Chạy trong
-- tenancy.WithTenant. Không khóa (ADR-46 mục e).

-- name: InsertNote :one
INSERT INTO customer.customer_notes (tenant_id, customer_id, body, author_id, created_at)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @customer_id, @body, @author_id, @now)
RETURNING tenant_id, id, customer_id, body, author_id, created_at;
