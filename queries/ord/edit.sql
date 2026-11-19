-- Sửa đơn (kế hoạch M2, Task M2-20; order.Writer.Edit). Chạy trong tenancy.WithTenant, trong tx của bên gọi, sau khi
-- đã khóa đơn (LockOrder, ADR-46 nhóm 7). Không gán status (đổi trạng thái chỉ ở transition.sql).

-- name: LockOrderItems :many
-- Khóa mọi dòng của đơn đã khóa theo id tăng dần (ADR-46 nhóm 7: orders → order_items), trước mọi nhóm sau (mã giảm
-- giá, tồn); các câu sửa, xóa dòng sau đó chỉ chạm dòng đã khóa.
SELECT id
FROM ord.order_items
WHERE seller_id = @seller_id AND order_id = @order_id
ORDER BY id
FOR UPDATE;

-- name: UpdateOrderEdit :one
-- Ghi lần sửa khi version còn khớp; trả version mới (không dòng nào → pgx.ErrNoRows). Địa chỉ, người duyệt giảm giá
-- chỉ ghi khi set_address, record_approval; tiền là tổng mới bên gọi tính (pricing.Compute, trigger check_order_totals
-- kiểm lúc commit); COD còn thu = max(grand_total - prepaid_total, 0); payment_status bên gọi tính lại theo tổng mới.
UPDATE ord.orders
SET ship_address         = CASE WHEN @set_address::boolean THEN sqlc.arg(new_address)::jsonb ELSE ship_address END,
    ship_phone           = CASE WHEN @set_address::boolean THEN @ship_phone::text ELSE ship_phone END,
    ship_phone_reversed  = CASE WHEN @set_address::boolean THEN @ship_phone_reversed::text ELSE ship_phone_reversed END,
    address_complete     = CASE WHEN @set_address::boolean THEN @address_complete::boolean ELSE address_complete END,
    subtotal             = @subtotal,
    line_discount_total  = @line_discount_total,
    order_discount       = @order_discount,
    coupon_discount      = @coupon_discount,
    discount_total       = @discount_total,
    shipping_fee_charged = @shipping_fee_charged,
    grand_total          = @grand_total,
    cod_amount           = GREATEST(sqlc.arg(grand_total)::bigint - prepaid_total, 0),
    payment_status       = @payment_status::text,
    carrier_note         = @carrier_note,
    care_by              = sqlc.narg(care_by)::uuid,
    traffic_source_id    = sqlc.narg(traffic_source_id)::uuid,
    discount_approved_by = CASE WHEN @record_approval::boolean THEN sqlc.narg(approved_by)::uuid
                                ELSE discount_approved_by END,
    discount_approved_at = CASE WHEN @record_approval::boolean THEN @now::timestamptz ELSE discount_approved_at END,
    version              = version + 1,
    updated_at           = @now::timestamptz
WHERE seller_id = @seller_id AND id = @id AND version = @version
RETURNING version;

-- name: UpdateEditedItems :execrows
-- Số lượng, giảm dòng, phần giảm chia về dòng của các dòng giữ nguyên (ids, … cùng thứ tự; dòng đã khóa bằng
-- LockOrderItems). Đơn giá, ảnh chụp giữ nguyên (KH11).
UPDATE ord.order_items i
SET quantity                 = u.quantity,
    line_discount            = u.line_discount,
    allocated_order_discount = u.allocated,
    net_amount               = u.net,
    updated_at               = @now::timestamptz
FROM (SELECT unnest(@ids::uuid[]) AS id, unnest(@quantities::int[]) AS quantity,
             unnest(@line_discounts::bigint[]) AS line_discount, unnest(@allocated::bigint[]) AS allocated,
             unnest(@net::bigint[]) AS net) AS u
WHERE i.seller_id = @seller_id AND i.order_id = @order_id AND i.id = u.id;

-- name: DeleteOrderItems :execrows
-- Xóa dòng bị bỏ (đã khóa; giữ hàng của chúng đã nhả trước, khóa ngoại SET NULL giữ lại dòng giữ hàng released).
DELETE FROM ord.order_items
WHERE seller_id = @seller_id AND order_id = @order_id AND id = ANY (@ids::uuid[]);
