-- Đợt mã và quà sinh nhật của website, mã nhập từ website (kế hoạch M5, Task 197 / M5-14; A18, B9, B10, QĐ42).
-- Pha mở rộng (Q-X2): chỉ ALTER … ADD cột vào hai bảng của M2 (0206), thêm index, GRANT; không CREATE bảng, không sửa
-- view. Bảng của M2 còn nhỏ, CRM chưa chạy dữ liệu thật: index không CONCURRENTLY (như 0211, 0509).
-- - coupon_batches.purpose: manual (nhân viên tạo, M2), birthday (website xin phát quà sinh nhật), imported (mã còn
--   hạn nhập từ tệp của website). Lô của website trùng (purpose, tên) là cùng lô: unique một phần, lô manual không bị
--   ràng buộc mới. Index (tenant_id, purpose, id) cho listCouponBatches (mới nhất trước, lọc purpose).
-- - coupons.given_at: lúc website trao mã cho khách (lần đầu, không đổi); coupons.origin: crm (CRM sinh) hay
--   web_import (website đặt mã từ trước, nhập một lần).
-- - GRANT UPDATE: purpose (đặt sau khi CreateBatch của M2 ghi lô), quantity (lô nhập đếm lại số mã), given_at.
-- Thứ tự khóa không đổi: ADR-46 nhóm 10 (coupon_batches → coupons).

-- +goose Up
ALTER TABLE promotion.coupon_batches
    ADD COLUMN purpose text NOT NULL DEFAULT 'manual' CHECK (purpose IN ('manual', 'birthday', 'imported'));
CREATE UNIQUE INDEX coupon_batches_web_name_key ON promotion.coupon_batches (tenant_id, purpose, name)
    WHERE purpose <> 'manual';
CREATE INDEX coupon_batches_purpose ON promotion.coupon_batches (tenant_id, purpose, id);

ALTER TABLE promotion.coupons
    ADD COLUMN given_at timestamptz,
    ADD COLUMN origin text NOT NULL DEFAULT 'crm' CHECK (origin IN ('crm', 'web_import'));

GRANT UPDATE (purpose, quantity) ON promotion.coupon_batches TO crm_app;
GRANT UPDATE (given_at) ON promotion.coupons TO crm_app;
