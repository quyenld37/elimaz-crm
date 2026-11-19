-- Bộ đếm barcode nội bộ theo tenant (kế hoạch M1, Task M1-27, Q5). Một câu: chèn dòng khi chưa có, tăng dưới khóa
-- dòng khi đã có; tenant lấy từ ngữ cảnh của tenancy.WithTenant (thiếu ngữ cảnh → NULL → lỗi NOT NULL).

-- name: ReserveBarcodeNumbers :one
-- Cấp n số liền nhau (n ≥ 1): trả số lớn nhất của dải, dải là [next − n + 1, next]. Khóa dòng của tenant tới khi tx
-- kết thúc (ADR-46 nhóm 4: bộ đếm khóa cuối cùng).
INSERT INTO catalog.barcode_counters (tenant_id, next)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @n::bigint)
ON CONFLICT (tenant_id) DO UPDATE SET next = catalog.barcode_counters.next + @n::bigint
RETURNING next;
