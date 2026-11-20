-- Mục đích tệp của M3 (kế hoạch M3, Task M3-04; G-M1-5, R5): nhãn hãng carrier_label (máy chủ ghi, không nhận tải
-- lên) và bảng kê COD cod_statement (kế toán tải lên), cả hai ở bucket private. Cơ chế duy nhất là bảng
-- platform.file_purposes của M1 (0124); Go đăng ký cùng khóa bằng fulfillment.RegisterFilePurposes(),
-- cash.RegisterFilePurposes() lúc khởi động. Số 0305 bỏ trống (Q-T91-5). Pha mở rộng: chỉ thêm dòng.

-- +goose Up
INSERT INTO platform.file_purposes (key, bucket)
VALUES ('carrier_label', 'private'), ('cod_statement', 'private')
ON CONFLICT DO NOTHING;
