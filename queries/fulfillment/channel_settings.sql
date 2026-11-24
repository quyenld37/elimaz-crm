-- Hãng giao hàng trên website (kế hoạch M5, Task 198). Một dòng mỗi (tenant, kênh 'web'); chạy trong
-- tenancy.WithTenant (RLS). Thứ tự khóa nhóm 8 ADR-46: carrier_accounts trước, channel_carrier_settings sau.

-- name: GetWebCarrierSettings :one
-- Cài đặt kênh web, không khóa (đường đọc, báo phí cho web).
SELECT preferred_account_id, fallback_fee, version
FROM fulfillment.channel_carrier_settings
WHERE channel = 'web';

-- name: EnsureWebCarrierSettings :exec
-- Chèn dòng mặc định (không ưu tiên, không phí dự phòng, version 1) nếu chưa có; RLS (WITH CHECK) từ chối tenant khác.
INSERT INTO fulfillment.channel_carrier_settings (tenant_id, channel)
VALUES (@tenant_id, 'web')
ON CONFLICT (tenant_id, channel) DO NOTHING;

-- name: LockWebCarrierSettings :one
-- Khóa dòng kênh web (bảng cuối nhóm 8) trước khi so version hay bỏ ưu tiên.
SELECT preferred_account_id, fallback_fee, version
FROM fulfillment.channel_carrier_settings
WHERE channel = 'web'
FOR UPDATE;

-- name: UpdateWebCarrierSettings :one
-- Bên gọi đã khóa dòng (LockWebCarrierSettings) và so version.
UPDATE fulfillment.channel_carrier_settings
SET preferred_account_id = sqlc.narg(preferred_account_id)::uuid,
    fallback_fee         = sqlc.narg(fallback_fee)::bigint,
    updated_by           = @updated_by::uuid,
    updated_at           = now(),
    version              = version + 1
WHERE channel = 'web'
RETURNING preferred_account_id, fallback_fee, version;

-- name: LockWebAccount :one
-- Khóa tài khoản hãng (đầu nhóm 8) được chọn làm ưu tiên để kiểm active và show_on_web trước khi khóa cài đặt.
SELECT status, show_on_web
FROM fulfillment.carrier_accounts
WHERE id = @id
FOR UPDATE;

-- name: OtherWebCarriers :one
-- Số tài khoản active, hiện trên web, khác id (luật "ít nhất một hãng bật trên website", Task 450). Bên gọi đã khóa
-- dòng cài đặt nên hai lệnh tắt hãng cùng lúc đếm lần lượt.
SELECT count(*)::int AS n
FROM fulfillment.carrier_accounts
WHERE status = 'active' AND show_on_web AND id <> @id;

-- name: WebCarrierAccounts :many
-- Tài khoản active, hiện trên web, xếp web_sort rồi id; không có khóa API (danh sách cho website).
SELECT id, carrier_key, name
FROM fulfillment.carrier_accounts
WHERE status = 'active' AND show_on_web
ORDER BY web_sort, id;

-- name: WebCarrierStats :many
-- Mọi tài khoản hiện trên web (mọi trạng thái trừ revoked) kèm số vận đơn chưa hủy tạo từ @since (màn hãng trên web,
-- M5-25). Xếp web_sort rồi id.
SELECT a.id, a.carrier_key, a.name, a.status, a.web_sort,
       (SELECT count(*)::int
        FROM fulfillment.shipments s
        WHERE s.carrier_account_id = a.id AND s.status <> 'cancelled' AND s.created_at >= @since::timestamptz) AS shipments_30d
FROM fulfillment.carrier_accounts a
WHERE a.show_on_web AND a.status <> 'revoked'
ORDER BY a.web_sort, a.id;

-- name: DeliveryMedianDays :many
-- Trung vị số ngày từ lúc hãng lấy hàng tới lúc giao của vận đơn giao trong [@since, now), theo mã tỉnh của địa chỉ
-- giao (khóa JSON provinceCode của bản chụp ship_to); tỉnh có ít hơn @min_count vận đơn bị bỏ.
SELECT (ship_to ->> 'provinceCode')::text AS province_code,
       (percentile_cont(0.5) WITHIN GROUP (
           ORDER BY extract(epoch FROM delivered_at - picked_up_at) / 86400.0))::float8 AS median_days
FROM fulfillment.shipments
WHERE delivered_at >= @since::timestamptz
  AND picked_up_at IS NOT NULL
  AND delivered_at >= picked_up_at
  AND coalesce(ship_to ->> 'provinceCode', '') <> ''
GROUP BY 1
HAVING count(*) >= @min_count::int
ORDER BY 1;
