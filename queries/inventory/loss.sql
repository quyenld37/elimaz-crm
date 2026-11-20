-- Chứng từ điều chỉnh của vận chuyển (kế hoạch M3, Task M3-05): ghi hàng thất lạc (inventory.Writer.RecordLoss) và đảo
-- phiếu xuất (ReverseConsumption) dùng doc_type adjust, doc_id = mã do bên gọi đưa. Chạy trong tenancy.WithTenant.

-- name: LedgerHasDocStep :one
-- Chứng từ doc đã có dòng sổ ở bước step chưa: cùng mã lần hai thì không ghi gì (gọi lại). Bên gọi đã khóa số dư liên
-- quan nên lần chạy song song cùng mã thấy dòng sổ của lần trước sau khi chờ khóa (READ COMMITTED).
SELECT EXISTS (
    SELECT 1 FROM inventory.stock_ledger WHERE doc_type = @doc_type AND doc_id = @doc_id::uuid AND step = @step
);

-- name: LockBalancesByKeys :many
-- Khóa các dòng số dư (variant_ids[i], warehouse_ids[i]) theo (variant_id, warehouse_id) tăng dần (ADR-46 nhóm 11)
-- trước khi kiểm sổ và đổi số dư.
SELECT b.variant_id, b.warehouse_id
FROM inventory.stock_balances AS b
WHERE (b.variant_id, b.warehouse_id) IN (
    SELECT unnest(@variant_ids::uuid[]), unnest(@warehouse_ids::uuid[])
)
ORDER BY b.variant_id, b.warehouse_id
FOR UPDATE;
