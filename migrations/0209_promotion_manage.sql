-- Quản lý mã giảm giá (kế hoạch M2, Task M2-14): crm_app được sửa các cột điều khoản, trạng thái, version của mã.
-- 0206 chỉ cấp UPDATE (used_count) cho đường dùng mã. Số 0209: 0207, 0208 đã cấp cho Task M2-10, M2-13 (Q-T71-1).
-- Pha mở rộng: chỉ GRANT. Không cấp sửa code, batch_id, seller_id, bound_*, used_count (đã có), created_*.

-- +goose Up
GRANT UPDATE (name, discount_kind, value, max_discount, min_subtotal, channels, max_uses, per_customer_limit,
    release_on_cancel, valid_from, valid_to, status, updated_at, version) ON promotion.coupons TO crm_app;
