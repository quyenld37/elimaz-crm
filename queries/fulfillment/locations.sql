-- Địa giới theo hãng (kế hoạch M3, Task M3-18, Q-19). Mọi câu trong tenancy.WithTenant hay tx của Runner (crm_app);
-- RLS lọc carrier_locations theo tenant. Danh mục của CRM đọc qua view công bố customer.admin_units_v (không tenant_id).
-- Khóa: chỉ dòng carrier_locations (ADR-46 nhóm 8, bảng cuối), ghi theo (level, our_code) tăng dần.

-- name: ListAdminUnitsForMatch :many
-- Mọi tỉnh, xã của danh mục (tỉnh trước, theo mã) để khớp tên với địa giới của hãng.
SELECT code::text AS code, level::text AS level, coalesce(parent_code, '')::text AS parent_code, name::text AS name
FROM customer.admin_units_v
ORDER BY level, code;

-- name: UpsertCarrierLocations :exec
-- Ghi mã hãng của các mã CRM (mảng cùng độ dài, đã sắp theo level, our_code): có rồi thì đổi mã, tên hãng, mốc đồng bộ.
INSERT INTO fulfillment.carrier_locations (tenant_id, carrier_key, level, our_code, carrier_code, carrier_name, synced_at)
SELECT NULLIF(current_setting('app.tenant_id', true), '')::uuid, @carrier_key::text, r.level, r.our_code, r.carrier_code,
    r.carrier_name, @synced_at::timestamptz
FROM (SELECT unnest(@levels::text[]) AS level, unnest(@our_codes::text[]) AS our_code,
          unnest(@carrier_codes::text[]) AS carrier_code, unnest(@carrier_names::text[]) AS carrier_name) AS r
ORDER BY r.level, r.our_code
ON CONFLICT (tenant_id, carrier_key, level, our_code) DO UPDATE
SET carrier_code = EXCLUDED.carrier_code,
    carrier_name = EXCLUDED.carrier_name,
    synced_at    = EXCLUDED.synced_at,
    updated_at   = now();

-- name: ListCarrierLocationCodes :many
-- Bảng đổi mã của một hãng (LocationMap).
SELECT level, our_code, carrier_code
FROM fulfillment.carrier_locations
WHERE carrier_key = @carrier_key;

-- name: ListUnmappedLocations :many
-- Mã của CRM chưa có mã của hãng carrier_key, theo (level, code) sau (after_level, after_code); level rỗng = mọi cấp.
SELECT u.level::text AS level, u.code::text AS code, u.name::text AS name,
    coalesce(u.parent_code, '')::text AS parent_code
FROM customer.admin_units_v u
WHERE (@level::text = '' OR u.level = @level::text)
  AND (u.level, u.code) > (@after_level::text, @after_code::text)
  AND NOT EXISTS (
      SELECT 1
      FROM fulfillment.carrier_locations l
      WHERE l.carrier_key = @carrier_key::text
        AND l.level = u.level
        AND l.our_code = u.code)
ORDER BY u.level, u.code
LIMIT @max_rows::int;
