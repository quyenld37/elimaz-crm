-- Nhận phiếu trả theo số lượng tốt, hỏng, thiếu (kế hoạch M3, Task M3-23; Q-34). Chạy trong tenancy.WithTenant, trong
-- tx của bên gọi, sau khi đã khóa đơn → phiếu → dòng phiếu (LockReturnItems, returns.sql).

-- name: SetReturnItemQty :exec
-- Ghi ba số, tình trạng (một loại: tên loại; trộn: mixed) và tiền hoàn của dòng phiếu đã khóa. CHECK
-- return_items_qty_split đòi tổng bằng quantity.
UPDATE ord.return_items
SET condition = @condition, refund_amount = @refund_amount, sellable_qty = @sellable_qty,
    damaged_qty = @damaged_qty, missing_qty = @missing_qty
WHERE id = @id;
