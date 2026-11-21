-- Đối chiếu sổ COD hằng đêm (kế hoạch M3, Task M3-33; spec 05 mục 2 "Sổ COD"; TI03). Pha mở rộng: thêm cột cho phép
-- rỗng, unique, index, dòng lịch. reconcile_runs còn rỗng ngoài môi trường thử (M3 chưa phát hành) nên tạo index
-- thường. Thứ tự khóa không đổi (hai bảng chỉ chèn; ADR-46 nhóm 9, reconcile_runs cuối nhóm).

-- +goose Up

-- (1) Kỳ của lịch sinh lần chạy: một lần chạy mỗi (tenant, kỳ). Lần chạy không theo lịch (nếu có sau này) để rỗng.
ALTER TABLE cash.reconcile_runs ADD COLUMN slot timestamptz;
ALTER TABLE cash.reconcile_runs ADD CONSTRAINT reconcile_runs_slot_key UNIQUE (tenant_id, slot);

-- (2) Đọc phát hiện theo lần chạy (listReconcileFindings), trang theo mã tăng dần; đọc phát hiện theo vận đơn, loại
-- (bỏ qua vận đơn có remitted_then_returned còn mở).
CREATE INDEX reconcile_findings_run ON cash.reconcile_findings (tenant_id, run_id, id);
CREATE INDEX reconcile_findings_shipment_kind ON cash.reconcile_findings (tenant_id, shipment_id, kind);

-- (3) Lịch đối chiếu: kỳ đầu 19:30 UTC kế tiếp (02:30 giờ Việt Nam); Ticker gặp dòng đã có giữ next_run_at nên các kỳ
-- sau neo theo giờ này. Đảo ngược: xóa dòng.
INSERT INTO platform.schedules (name, every_seconds, per_tenant, next_run_at)
SELECT 'cash.reconcile', 86400, true, CASE WHEN s.t > now() THEN s.t ELSE s.t + interval '1 day' END
FROM (SELECT date_trunc('day', now(), 'UTC') + interval '19 hours 30 minutes' AS t) AS s
ON CONFLICT (name) DO NOTHING;
