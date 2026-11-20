-- Phát hiện do consumer sổ COD mở theo sự kiện vận đơn (kế hoạch M3, Task M3-29): remitted_then_returned (Q-1),
-- unexpected_cancel_after_pickup. Phát hiện này không thuộc lần đối chiếu nào nên run_id được rỗng (lần đối chiếu
-- đêm, Task M3-33, vẫn ghi run_id). source_key là khóa chống trùng của phát hiện theo sự kiện
-- ("<loại>:<vận đơn>:<seq>"): sự kiện giao lại không mở phát hiện thứ hai; phát hiện của lần đối chiếu để rỗng (NULL
-- không trùng nhau). Pha mở rộng: nới NOT NULL, thêm cột cho phép rỗng và unique; bảng còn rỗng ngoài môi trường thử
-- (M3 chưa phát hành) nên tạo index thường. Thứ tự khóa không đổi (bảng chỉ chèn).

-- +goose Up
ALTER TABLE cash.reconcile_findings ALTER COLUMN run_id DROP NOT NULL;
ALTER TABLE cash.reconcile_findings ADD COLUMN source_key text CHECK (length(source_key) BETWEEN 1 AND 200);
ALTER TABLE cash.reconcile_findings
    ADD CONSTRAINT reconcile_findings_source_key UNIQUE (tenant_id, source_key);
