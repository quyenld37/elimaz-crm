-- Sổ tồn chỉ ghi thêm (kế hoạch M1, Task M1-28). Chạy trong tenancy.WithTenant.

-- name: InsertLedger :exec
INSERT INTO inventory.stock_ledger
    (tenant_id, variant_id, warehouse_id, bucket, delta, doc_type, doc_id, step, actor_type, actor_id)
VALUES (@tenant_id, @variant_id, @warehouse_id, @bucket, @delta, @doc_type, @doc_id, @step, @actor_type,
        sqlc.narg(actor_id)::uuid);
