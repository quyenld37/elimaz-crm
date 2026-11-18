-- Sự kiện nội bộ (kế hoạch M0, Task 13; spec 04 mục 5.4). Chỉ internal/platform/events dùng. Bảng bật RLS: câu chạy
-- trong WithTenant; điều kiện tenant_id ghi thêm cho rõ.

-- name: InsertDomainEvent :one
INSERT INTO platform.domain_events (tenant_id, aggregate_type, aggregate_id, seq, topic, payload)
VALUES (@tenant_id, @aggregate_type, @aggregate_id, @seq, @topic, @payload)
RETURNING id;

-- name: GetDomainEvent :one
SELECT id, tenant_id, aggregate_type, aggregate_id, seq, topic, payload, created_at
FROM platform.domain_events
WHERE tenant_id = @tenant_id AND id = @id;
