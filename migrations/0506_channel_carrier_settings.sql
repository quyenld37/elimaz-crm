-- Hãng giao hàng trên website (kế hoạch M5, Task 198 / M5-15). Pha mở rộng: chỉ CREATE TABLE, RLS, GRANT.
-- - fulfillment.channel_carrier_settings: một dòng mỗi (tenant, kênh); hiện chỉ kênh 'web'. Hãng nào hiện trên web và
--   theo thứ tự nào là cờ show_on_web, web_sort của fulfillment.carrier_accounts (M3) — bảng này KHÔNG có danh sách
--   hãng bật (một nguồn). Chỉ có tài khoản ưu tiên (đứng đầu khi báo phí) và phí dự phòng (khi không hãng nào báo
--   phí được, ví dụ tài khoản manual lúc tích hợp hãng tạm dừng).
-- - Chưa có dòng = không ưu tiên, không phí dự phòng, version 1 (fulfillment.GetWebCarrierSettings); lần ghi đầu chèn
--   dòng rồi khóa (Service.SaveWebCarrierSettings).
-- - Thứ tự khóa: nhóm 8 ADR-46, bảng cuối nhóm (sau carrier_locations): khóa carrier_accounts trước rồi mới tới đây.

-- +goose Up
CREATE TABLE fulfillment.channel_carrier_settings (
    tenant_id            uuid NOT NULL REFERENCES platform.tenants (id),
    channel              text NOT NULL CHECK (channel IN ('web')),
    preferred_account_id uuid,
    fallback_fee         bigint CHECK (fallback_fee BETWEEN 0 AND 500000),
    version              int NOT NULL DEFAULT 1,
    updated_by           uuid,
    updated_at           timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, channel),
    FOREIGN KEY (tenant_id, preferred_account_id) REFERENCES fulfillment.carrier_accounts (tenant_id, id)
);
SELECT platform.enable_tenant_rls('fulfillment.channel_carrier_settings');

GRANT SELECT, INSERT ON fulfillment.channel_carrier_settings TO crm_app;
GRANT UPDATE (preferred_account_id, fallback_fee, version, updated_by, updated_at)
    ON fulfillment.channel_carrier_settings TO crm_app;
