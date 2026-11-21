-- Đổi địa chỉ sau khi đã gửi hãng (kế hoạch M3, Task M3-17). Chạy trong tenancy.WithTenant / WriteTx. Thứ tự khóa
-- ADR-46: ord.orders (nhóm 7, qua order.LockSnapshot) → shipments → address_change_requests (nhóm 8) → platform.jobs
-- (nhóm 13, qua queue/commands, cuối cùng). new_address là dữ liệu cá nhân: chỉ lệnh sửa địa chỉ và lúc xác nhận đọc.

-- name: AddressChangeOpen :one
-- Vận đơn (đã khóa) còn yêu cầu mở (pending, sent, unknown; khớp index address_change_requests_open).
SELECT EXISTS (
    SELECT 1 FROM fulfillment.address_change_requests
    WHERE shipment_id = @shipment_id AND status IN ('pending', 'sent', 'unknown')
);

-- name: InsertAddressChange :one
-- Yêu cầu mới (pending) của vận đơn đã khóa; trùng yêu cầu mở → lỗi unique (23505) của address_change_requests_open.
INSERT INTO fulfillment.address_change_requests (tenant_id, shipment_id, new_address, mode, status, requested_by, note)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @shipment_id, @new_address, @mode, 'pending',
    @requested_by, sqlc.narg(note))
RETURNING id, shipment_id, mode, status, note, created_at, resolved_at;

-- name: SetAddressChangeJob :exec
-- Gắn lệnh sửa địa chỉ vào yêu cầu vừa chèn.
UPDATE fulfillment.address_change_requests
SET job_id = @job_id, updated_at = now(), version = version + 1
WHERE id = @id;

-- name: AddressChangeShipment :one
-- Vận đơn của yêu cầu (đọc không khóa, để khóa đơn rồi vận đơn trước yêu cầu).
SELECT shipment_id FROM fulfillment.address_change_requests WHERE id = @id;

-- name: LockAddressChange :one
-- Khóa yêu cầu (sau khi đã khóa đơn, vận đơn).
SELECT id, shipment_id, new_address, mode, status, job_id, note, created_at, resolved_at
FROM fulfillment.address_change_requests
WHERE id = @id
FOR UPDATE;

-- name: SetAddressChangeStatus :one
-- Đổi yêu cầu còn mở (đã khóa) sang status; confirmed, rejected ghi người xác nhận (NULL: hãng, hệ thống) và giờ xong;
-- note khác NULL thay ghi chú. Yêu cầu đã đóng → pgx.ErrNoRows.
UPDATE fulfillment.address_change_requests
SET status = sqlc.arg(status)::text,
    resolved_by = CASE WHEN sqlc.arg(status)::text IN ('confirmed', 'rejected') THEN sqlc.narg(resolved_by)::uuid
        ELSE resolved_by END,
    resolved_at = CASE WHEN sqlc.arg(status)::text IN ('confirmed', 'rejected') THEN now() ELSE resolved_at END,
    note = coalesce(sqlc.narg(note), note), updated_at = now(), version = version + 1
WHERE id = @id AND status IN ('pending', 'sent', 'unknown')
RETURNING id, shipment_id, mode, status, note, created_at, resolved_at;

-- name: AddressChangeTarget :one
-- Yêu cầu của lệnh sửa địa chỉ kèm vận đơn, chế độ tài khoản (Execute; đọc không khóa, ngoài transaction của lệnh).
SELECT r.id, r.status, r.new_address, s.id AS shipment_id, s.code, s.carrier_key, s.carrier_account_id,
       s.status AS shipment_status, s.tracking_code, a.mode
FROM fulfillment.address_change_requests r
JOIN fulfillment.shipments s ON s.tenant_id = r.tenant_id AND s.id = r.shipment_id
JOIN fulfillment.carrier_accounts a ON a.tenant_id = s.tenant_id AND a.id = s.carrier_account_id
WHERE r.id = @id;

-- name: SetShipmentShipTo :exec
-- Địa chỉ giao của vận đơn (đã khóa) sau khi hãng nhận đổi địa chỉ; không đổi version (không phải trường nhân viên sửa).
UPDATE fulfillment.shipments
SET ship_to = @ship_to, updated_at = now()
WHERE id = @id;

-- name: OpenAddressChanges :many
-- Yêu cầu còn mở của vận đơn (nhiều nhất một: index address_change_requests_open); không đọc địa chỉ (Task 134).
SELECT id, shipment_id, mode, status, note, created_at, resolved_at
FROM fulfillment.address_change_requests
WHERE shipment_id = @shipment_id AND status IN ('pending', 'sent', 'unknown')
ORDER BY created_at DESC, id DESC;
