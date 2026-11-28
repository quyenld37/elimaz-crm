-- Sự kiện hãng của vận đơn ngoài hệ thống (kế hoạch M7, Task 265 / M7-20; MO07, Q17). Pha mở rộng: chỉ CREATE TABLE,
-- index, RLS, GRANT; không chạm bảng cũ.
-- - Đơn cũ (Nhanh) còn đang giao sau ngày mở: hãng vẫn gửi webhook về tuyến của tài khoản. CRM không đọc các đơn đó
--   (ADR-11); handler vào của fulfillment ghi một dòng mỗi (hãng, mã vận đơn), đếm số lần, không cảnh báo, không tra hãng.
-- - Chỉ mã vận đơn, hãng, tài khoản, số lần, mốc giờ; không dữ liệu khách. last_raw_status để NULL khi tín hiệu không
--   mang trạng thái (tín hiệu hãng của M3 không mang, B10).
-- - Thứ tự khóa: nhóm 8 ADR-46, bảng cuối nhóm (sau mọi bảng fulfillment khác); ghi trong transaction của việc vào.

-- +goose Up
CREATE TABLE fulfillment.unmatched_carrier_events (
    id                 uuid        PRIMARY KEY DEFAULT uuidv7(),
    tenant_id          uuid        NOT NULL REFERENCES platform.tenants (id),
    carrier_key        text        NOT NULL CHECK (length(carrier_key) BETWEEN 1 AND 32),
    carrier_account_id uuid,
    tracking_code      text        NOT NULL CHECK (length(tracking_code) BETWEEN 1 AND 128),
    last_raw_status    text        CHECK (length(last_raw_status) BETWEEN 1 AND 64),
    first_seen_at      timestamptz NOT NULL,
    last_seen_at       timestamptz NOT NULL,
    event_count        int         NOT NULL DEFAULT 1 CHECK (event_count >= 1),
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, carrier_key, tracking_code),
    FOREIGN KEY (tenant_id, carrier_account_id) REFERENCES fulfillment.carrier_accounts (tenant_id, id),
    CHECK (first_seen_at <= last_seen_at)
);
-- Danh sách "mới thấy trước" (listUnmatchedCarrierEvents): thứ tự first_seen_at DESC, id DESC.
CREATE INDEX unmatched_carrier_events_first_seen
    ON fulfillment.unmatched_carrier_events (tenant_id, first_seen_at DESC, id DESC);
SELECT platform.enable_tenant_rls('fulfillment.unmatched_carrier_events');

GRANT SELECT, INSERT ON fulfillment.unmatched_carrier_events TO crm_app;
GRANT UPDATE (carrier_account_id, last_raw_status, last_seen_at, event_count)
    ON fulfillment.unmatched_carrier_events TO crm_app;
