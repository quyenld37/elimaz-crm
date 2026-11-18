-- Lỗi ứng dụng gộp theo dấu vân tay (kế hoạch M0 bản 5, Task 20; spec 04 mục "Gom lỗi ứng dụng", 05, ADR-30, 38).
-- Pha mở rộng: một bảng nền tảng mới, không RLS và không tenant_id (Global Constraints: lỗi của cả hệ thống). Chỉ
-- internal/platform/observability đọc, ghi.
-- Thứ tự khóa: docs/adr/0046-thu-tu-khoa.md, mục "Bảng M0 ngoài thứ tự nhóm" (b): mỗi lần ghi một câu upsert trong
-- transaction riêng, không nằm trong transaction nghiệp vụ.
-- message đã qua observability.ScrubMessage (không còn chuỗi từ 8 chữ số, email; tối đa 300 ký tự).

-- +goose Up
CREATE TABLE platform.error_events (
    fingerprint       text        PRIMARY KEY CHECK (fingerprint ~ '^[0-9a-f]{64}$'),
    role              text        NOT NULL,
    name              text        NOT NULL,
    message           text        NOT NULL,
    top_frames        text        NOT NULL,
    count             bigint      NOT NULL DEFAULT 1,
    first_seen_at     timestamptz NOT NULL DEFAULT now(),
    last_seen_at      timestamptz NOT NULL DEFAULT now(),
    sample_request_id text,
    sample_job_id     uuid
);

-- Luật new-error (first_seen_at trong 60 s) và tóm tắt 24 giờ (last_seen_at).
CREATE INDEX error_events_first_seen_at ON platform.error_events (first_seen_at);
CREATE INDEX error_events_last_seen_at ON platform.error_events (last_seen_at);

GRANT SELECT, INSERT, UPDATE ON platform.error_events TO crm_app;
