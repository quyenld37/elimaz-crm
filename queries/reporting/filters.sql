-- Lựa chọn của bộ lọc báo cáo bán hàng trong phạm vi người xem (kế hoạch M6, Task 230; QĐ-230-5). Chạy trong
-- tenancy.WithTenant, transaction chỉ đọc của báo cáo. all_pages: phạm vi report.sales.read toàn shop; không thì chỉ
-- đơn của page_ids. Đơn "gần đây": tạo trong 366 ngày, của người bán own (ADR-29).

-- name: FilterSources :many
-- Nguồn từng gắn với đơn trong phạm vi trong 366 ngày; người toàn shop thêm mọi nguồn đang bật.
SELECT ts.id, ts.name FROM ord.traffic_sources_v ts
WHERE (@all_pages::boolean AND ts.active)
   OR ts.id IN (
       SELECT o.traffic_source_id FROM ord.orders_v o
       WHERE o.seller_id = @seller_id AND o.created_at >= now() - interval '366 days'
         AND o.traffic_source_id IS NOT NULL
         AND (@all_pages::boolean OR o.channel_account_id = ANY(@page_ids::uuid[])))
ORDER BY ts.position, ts.name, ts.id;

-- name: FilterStaff :many
-- Nhân viên đang làm, cộng người từng phụ trách hay tạo đơn trong phạm vi trong 366 ngày.
SELECT u.id, u.display_name FROM identity.users_v u
WHERE u.status = 'active'
   OR u.id IN (
       SELECT x.id FROM ord.orders_v o
       CROSS JOIN LATERAL (VALUES (o.care_by), (o.created_by)) AS x(id)
       WHERE o.seller_id = @seller_id AND o.created_at >= now() - interval '366 days' AND x.id IS NOT NULL
         AND (@all_pages::boolean OR o.channel_account_id = ANY(@page_ids::uuid[])))
ORDER BY u.display_name, u.id;

-- name: FilterPages :many
-- Page của người bán own trong phạm vi.
SELECT a.id, a.name FROM channel.channel_accounts_v a
WHERE a.seller_id = @seller_id AND (@all_pages::boolean OR a.id = ANY(@page_ids::uuid[]))
ORDER BY a.name, a.id;

-- name: FilterCarrierKeys :many
-- Khóa hãng của tài khoản hãng của người bán own.
SELECT DISTINCT a.carrier_key FROM fulfillment.carrier_accounts_v a
WHERE a.seller_id = @seller_id
ORDER BY a.carrier_key;
