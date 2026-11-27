-- Chứng từ của đợt flash sale trong sổ tồn (kế hoạch M5, Task 200 / M5-17; QĐ31, A17). Kế hoạch cấp số 0508; điều phối
-- viên cấp 0540 sau gộp (0533 trùng Task 513). Pha mở rộng: chỉ nới CHECK doc_type của inventory.stock_ledger (giữ đủ bảy giá
-- trị của 0115, thêm hai): flash_hold (lên lịch, thêm suất: held +n, doc_id = mã món) và flash_release (đóng đợt: trả
-- đơn vị còn trống, held −n). Không đụng inventory.stock_documents (M5 không tạo phiếu). Ràng buộc
-- stock_ledger_step_key của 0115 vẫn chặn áp hai lần cùng bước của cùng món.
-- CHECK mới thêm NOT VALID rồi VALIDATE (sổ đã lớn: VALIDATE chỉ giữ SHARE UPDATE EXCLUSIVE, không chặn ghi sổ).

-- +goose Up
ALTER TABLE inventory.stock_ledger
    DROP CONSTRAINT stock_ledger_doc_type_check,
    ADD CONSTRAINT stock_ledger_doc_type_check
        CHECK (doc_type IN ('import', 'export', 'order', 'return', 'stocktake', 'adjust', 'damage', 'flash_hold',
            'flash_release')) NOT VALID;
ALTER TABLE inventory.stock_ledger VALIDATE CONSTRAINT stock_ledger_doc_type_check;
