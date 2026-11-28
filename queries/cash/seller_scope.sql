-- Người bán của đối tượng tiền mà API gọi theo mã (ADR-29, Task 531; cash.CheckSeller): đọc theo khóa chính qua view
-- công bố của fulfillment, ord, không khóa. seller_id của tài khoản hãng, đơn không bao giờ đổi, nên kiểm trước khi
-- khóa vẫn đúng.

-- name: SellerOfStatement :one
-- Bảng kê thuộc người bán của tài khoản hãng của nó (vận đơn khớp dòng cùng tài khoản đó: khóa ngoại KH14).
SELECT a.seller_id
FROM cash.cod_statements st
JOIN fulfillment.carrier_accounts_v a ON a.id = st.carrier_account_id
WHERE st.id = @id;

-- name: SellerOfCarrierAccount :one
SELECT seller_id
FROM fulfillment.carrier_accounts_v
WHERE id = @id;

-- name: SellerOfRefundObligation :one
SELECT o.seller_id
FROM cash.refund_obligations r
JOIN ord.orders_v o ON o.id = r.order_id
WHERE r.id = @id;
