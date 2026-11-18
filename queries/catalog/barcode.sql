-- Bộ đếm barcode nội bộ theo tenant (kế hoạch M1, Task M1-27, Q5). Một câu: chèn dòng khi chưa có, tăng dưới khóa
-- dòng khi đã có; tenant lấy từ ngữ cảnh của tenancy.WithTenant (thiếu ngữ cảnh → NULL → lỗi NOT NULL).

-- name: NextBarcodeNumber :one
INSERT INTO catalog.barcode_counters (tenant_id, next)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, 1)
ON CONFLICT (tenant_id) DO UPDATE SET next = catalog.barcode_counters.next + 1
RETURNING next;
