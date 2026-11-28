-- Task 269 (M7-24): view còn thiếu cho báo cáo sẵn sàng dữ liệu. Các view nguồn khác (catalog.product_readiness_v,
-- channel.channel_accounts_v, conversation.{automation_rules,reply_templates,tags}_v, ord.traffic_sources_v,
-- fulfillment.carrier_accounts_v, promotion.coupons_v, inventory.warehouses_v, inventory.stock_ledger_v) đã có.
-- catalog.images_processing_v: ảnh sản phẩm chưa xóa mà tệp còn ở bước uploaded hay processing, kèm mốc đổi trạng thái
-- cuối (since) để báo cáo đếm ảnh kẹt quá 30 phút. Đọc platform.files.status: ngoại lệ ADR-27 mục 4 như
-- catalog.product_readiness_v (0703). Chỉ thêm view, không cột, không bảng.

-- +goose Up
CREATE VIEW catalog.images_processing_v WITH (security_invoker = true) AS
SELECT i.id AS image_id, i.product_id, f.status AS file_status, f.updated_at AS since
FROM catalog.product_images AS i
JOIN platform.files AS f ON f.tenant_id = i.tenant_id AND f.id = i.file_id
WHERE i.deleted_at IS NULL AND f.deleted_at IS NULL AND f.status IN ('uploaded', 'processing');

GRANT SELECT ON catalog.images_processing_v TO crm_app;
