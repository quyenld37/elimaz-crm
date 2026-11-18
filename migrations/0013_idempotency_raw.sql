-- Gửi lại cùng khóa nhận đúng từng byte câu trả lời lần đầu (kế hoạch M0 bản 5, Task 396; ADR-41).
-- Pha mở rộng: thêm cột response_raw (byte nguyên của thân đã trả). response_body (jsonb, 0005) bị PostgreSQL chuẩn
-- hóa (thứ tự khóa, khoảng trắng) nên không phát lại đúng byte; từ bản này code ghi cả hai cột, đọc response_raw (dòng
-- do bản cũ ghi, response_raw NULL, thì đọc response_body). Cột response_body thu hẹp ở bản sau với dòng contract.
-- response_raw NULL khi handler không trả thân hoặc route no-replay (như response_body).

-- +goose Up
ALTER TABLE platform.idempotency_keys ADD COLUMN response_raw bytea;
