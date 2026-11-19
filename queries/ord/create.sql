-- Tạo đơn (kế hoạch M2, Task M2-16; order.Writer.Create). Chạy trong tenancy.WithTenant. Thứ tự khóa ADR-46 nhóm 7:
-- đơn → dòng đơn → lịch sử (dòng mới, không khóa dòng có sẵn).

-- name: InsertOrder :exec
-- Đơn mới đủ cột M2. Đơn tạo thẳng confirmed ghi confirmed_at, confirmed_by; nháp có draft_expires_at.
INSERT INTO ord.orders
    (id, tenant_id, seller_id, code, customer_id, channel, idempotency_key, status, ship_address, ship_phone,
     ship_phone_reversed, address_complete, subtotal, line_discount_total, order_discount, coupon_discount,
     discount_total, shipping_fee_charged, cod_amount, grand_total, created_by, version, warehouse_id,
     traffic_source_id, external_ref, referral_ref, draft_expires_at, carrier_note, care_by, conversation_id,
     channel_account_id, order_type, replaced_return_id, confirmed_at, confirmed_by, created_at, updated_at)
VALUES (@id, NULLIF(current_setting('app.tenant_id', true), '')::uuid, @seller_id, @code, @customer_id, @channel,
        @idempotency_key, @status, @ship_address, @ship_phone, @ship_phone_reversed, @address_complete, @subtotal,
        @line_discount_total, @order_discount, @coupon_discount, @discount_total, @shipping_fee_charged, @cod_amount,
        @grand_total, sqlc.narg(created_by)::uuid, @version, @warehouse_id, sqlc.narg(traffic_source_id)::uuid,
        sqlc.narg(external_ref)::text, sqlc.narg(referral_ref)::jsonb, sqlc.narg(draft_expires_at)::timestamptz,
        @carrier_note, sqlc.narg(care_by)::uuid, sqlc.narg(conversation_id)::uuid,
        sqlc.narg(channel_account_id)::uuid, @order_type, sqlc.narg(replaced_return_id)::uuid,
        sqlc.narg(confirmed_at)::timestamptz, sqlc.narg(confirmed_by)::uuid, @now::timestamptz, @now::timestamptz);

-- name: OrderByIdempotencyKey :one
-- Đơn của người bán sellerID đã có khóa nghiệp vụ (04 mục 5.5). Khóa của người bán khác: không thấy ở đây, câu chèn
-- đụng unique (tenant_id, idempotency_key) → ErrDuplicateOrder.
SELECT id
FROM ord.orders
WHERE seller_id = @seller_id AND idempotency_key = @idempotency_key;

-- name: VariantExists :one
-- Biến thể có trong shop đang chạy không, ở bất kỳ người bán nào (phân biệt ErrMixedSellers với ErrVariantNotFound).
SELECT EXISTS (SELECT 1 FROM catalog.variants_v WHERE variant_id = @variant_id)::boolean AS found;

-- name: TxNow :one
-- Giờ bắt đầu transaction: lớp bọc CreateManual của M1 lấy giờ ở database (Q30), không gọi time.Now trong module.
SELECT now()::timestamptz AS now;
