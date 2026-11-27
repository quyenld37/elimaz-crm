-- Số đo của mẫu có một chữ số thập phân (Task 517; website, Nhanh có số lẻ như 72,5 cm). Pha THU HẸP (đổi tên cột của
-- bảng và view): cột cm số nguyên của 0535 (Task 516) chỉ có ở main, chưa lên máy thật, không bản mã nào ngoài bản này
-- đọc hay ghi nó sau lần triển khai.
-- - catalog.products.length_cm, width_cm, height_cm → length_mm, width_mm, height_mm: mm số nguyên 1–10000
--   (= 0,1–1000,0 cm, không mất chính xác, không cần numeric). Số có sẵn nhân 10. Quyền UPDATE cột (0535) đi theo cột.
-- - RLS bị ép cả với chủ bảng (FORCE): tạm bỏ ép trong transaction của migration để chủ bảng thấy mọi tenant khi nhân
--   10, rồi ép lại ngay (như 0533).
-- - catalog.products_v: đổi tên ba cột cuối (giữ thứ tự, kiểu, security_invoker, GRANT).

-- +goose Up
ALTER TABLE catalog.products
    DROP CONSTRAINT products_length_cm_check,
    DROP CONSTRAINT products_width_cm_check,
    DROP CONSTRAINT products_height_cm_check;

-- contract: cột length_cm chỉ có từ 0535 (Task 516), chưa lên máy thật; từ bản này mã đọc, ghi length_mm
ALTER TABLE catalog.products RENAME COLUMN length_cm TO length_mm;
-- contract: cột width_cm chỉ có từ 0535 (Task 516), chưa lên máy thật; từ bản này mã đọc, ghi width_mm
ALTER TABLE catalog.products RENAME COLUMN width_cm TO width_mm;
-- contract: cột height_cm chỉ có từ 0535 (Task 516), chưa lên máy thật; từ bản này mã đọc, ghi height_mm
ALTER TABLE catalog.products RENAME COLUMN height_cm TO height_mm;

ALTER TABLE catalog.products NO FORCE ROW LEVEL SECURITY;
UPDATE catalog.products
SET length_mm = length_mm * 10, width_mm = width_mm * 10, height_mm = height_mm * 10
WHERE length_mm IS NOT NULL OR width_mm IS NOT NULL OR height_mm IS NOT NULL;
ALTER TABLE catalog.products FORCE ROW LEVEL SECURITY;

ALTER TABLE catalog.products
    ADD CONSTRAINT products_length_mm_check CHECK (length_mm BETWEEN 1 AND 10000),
    ADD CONSTRAINT products_width_mm_check CHECK (width_mm BETWEEN 1 AND 10000),
    ADD CONSTRAINT products_height_mm_check CHECK (height_mm BETWEEN 1 AND 10000);

-- contract: cột length_cm của view chỉ có từ 0535 (Task 516), chưa lên máy thật; webcatalog đọc length_mm từ bản này
ALTER VIEW catalog.products_v RENAME COLUMN length_cm TO length_mm;
-- contract: cột width_cm của view chỉ có từ 0535 (Task 516), chưa lên máy thật; webcatalog đọc width_mm từ bản này
ALTER VIEW catalog.products_v RENAME COLUMN width_cm TO width_mm;
-- contract: cột height_cm của view chỉ có từ 0535 (Task 516), chưa lên máy thật; webcatalog đọc height_mm từ bản này
ALTER VIEW catalog.products_v RENAME COLUMN height_cm TO height_mm;
