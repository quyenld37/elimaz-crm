-- Tìm-hoặc-tạo khách theo danh tính kênh (kế hoạch M4, Task 152; 05 mục 1). Chạy trong tenancy.WithTenant.

-- name: LockChannelIdentity :exec
-- Khóa tư vấn theo (tenant, loại, phạm vi, giá trị) tới hết transaction, lấy trước mọi khóa dòng
-- (docs/adr/0046-thu-tu-khoa.md mục "Khóa tư vấn"): tin nhắn và comment đầu tiên của cùng một người chạy lần lượt.
SELECT pg_advisory_xact_lock(hashtextextended('customer.channel_identity:' || current_setting('app.tenant_id') || ':'
    || @kind::text || ':' || @scope::text || ':' || @value::text, 0));

-- name: SetNameSource :exec
-- Nguồn tên của hồ sơ vừa tạo trong cùng transaction (chưa ai thấy dòng; không tăng version).
UPDATE customer.customers
SET name_source = @name_source
WHERE id = @id;
