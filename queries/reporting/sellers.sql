-- Người bán của báo cáo (kế hoạch M6, Task 227). Chạy trong tenancy.WithTenant, transaction chỉ đọc của báo cáo.

-- name: OwnSeller :one
-- Người bán của chính shop: kind own, active, id nhỏ nhất (cùng luật với GetDefaultSeller của catalog, G3 sau Task
-- 223); đọc qua view công bố của catalog.
SELECT seller_id FROM catalog.sellers_v
WHERE kind = 'own' AND status = 'active'
ORDER BY seller_id
LIMIT 1;
