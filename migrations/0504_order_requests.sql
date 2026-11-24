-- Yêu cầu về đơn hàng (kế hoạch M5, Task 195 / M5-12, ADR-28; gồm "Bổ sung Task 450 — luồng đủ như admin"). Pha mở
-- rộng: CREATE TABLE ×3, RLS, GRANT, ADD COLUMN có mặc định, một dòng mục đích tệp.
-- - ord.order_requests: khách gửi từ trang đơn trên website (source web) hoặc nhân viên ghi hộ (staff, chưa dùng).
--   Loại cancel, exchange, return, question. Trạng thái theo luồng của admin: received → in_review | needs_info |
--   confirmed | resolved | rejected; in_review → needs_info | confirmed | resolved | rejected; needs_info → in_review |
--   rejected; confirmed (chỉ exchange) → items_received | rejected; items_received → resolved | rejected; resolved,
--   rejected, cancelled là cuối (khách tự rút khi còn received). lines là các dòng đổi (biến thể, số, lý do, biến thể
--   muốn đổi sang, ghi chú); ship_back là hướng dẫn gửi trả khi confirmed (địa chỉ của shop, mã gói, hạn 23:59:59 giờ VN);
--   outcome là mã vận đơn (resolved) hay lý do từ chối (rejected).
-- - Mỗi đơn tối đa một yêu cầu hủy đang mở: index unique một phần (chốt ở database, ngoài khóa dòng đơn).
-- - ord.order_request_messages: tin hai phía, chỉ ghi thêm (trigger chặn UPDATE, DELETE, TRUNCATE của mọi role);
--   visible_to_customer = false là ghi chú nội bộ. body là dữ liệu cá nhân: audit chỉ ghi tên trường.
-- - ord.order_request_files: ảnh khách gửi kèm (mục đích order_request_photo, chế độ private, R10); một tệp chỉ thuộc
--   một yêu cầu.
-- - ord.order_settings.exchange_window_days: số ngày đổi hàng tính từ lúc giao (mặc định 7, như website hiện tại).
-- - Thứ tự khóa (ADR-46 nhóm 7): orders → order_settings → order_requests → order_request_messages, order_request_files
--   (chỉ chèn, không khóa).

-- +goose Up
CREATE TABLE ord.order_requests (
    id                  uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id           uuid NOT NULL REFERENCES platform.tenants (id),
    order_id            uuid NOT NULL,
    ref                 text NOT NULL CHECK (ref ~ '^YC-[0-9A-Z]{8}$'),
    kind                text NOT NULL CHECK (kind IN ('cancel', 'exchange', 'return', 'question')),
    status              text NOT NULL DEFAULT 'received' CHECK (status IN ('received', 'in_review', 'needs_info',
                            'confirmed', 'items_received', 'resolved', 'rejected', 'cancelled')),
    reason_code         text NOT NULL CHECK (reason_code ~ '^[A-Z][A-Z_]{1,29}$'),
    lines               jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(lines) = 'array'),
    source              text NOT NULL CHECK (source IN ('web', 'staff')),
    customer_note       text CHECK (length(customer_note) <= 2000),
    condition_confirmed boolean NOT NULL DEFAULT false,
    ship_back           jsonb CHECK (ship_back IS NULL OR jsonb_typeof(ship_back) = 'object'),
    outcome             jsonb CHECK (outcome IS NULL OR jsonb_typeof(outcome) = 'object'),
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    resolved_at         timestamptz,
    resolved_by         uuid,
    version             int NOT NULL DEFAULT 1 CHECK (version >= 1),
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, ref),
    FOREIGN KEY (tenant_id, order_id) REFERENCES ord.orders (tenant_id, id),
    CONSTRAINT order_requests_exchange_only CHECK (kind = 'exchange' OR status NOT IN ('confirmed', 'items_received')),
    CONSTRAINT order_requests_ship_back CHECK (status NOT IN ('confirmed', 'items_received') OR ship_back IS NOT NULL)
);
SELECT platform.enable_tenant_rls('ord.order_requests');
CREATE INDEX order_requests_status ON ord.order_requests (tenant_id, status, created_at DESC, id DESC);
CREATE INDEX order_requests_created ON ord.order_requests (tenant_id, created_at DESC, id DESC);
CREATE INDEX order_requests_order ON ord.order_requests (tenant_id, order_id);
CREATE UNIQUE INDEX order_requests_one_open_cancel ON ord.order_requests (tenant_id, order_id)
    WHERE kind = 'cancel' AND status IN ('received', 'in_review', 'needs_info');

CREATE TABLE ord.order_request_messages (
    id                  uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id           uuid NOT NULL REFERENCES platform.tenants (id),
    request_id          uuid NOT NULL,
    author_type         text NOT NULL CHECK (author_type IN ('customer', 'staff', 'system')),
    author_id           text,
    body                text NOT NULL CHECK (length(body) BETWEEN 1 AND 4000),
    visible_to_customer boolean NOT NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, request_id) REFERENCES ord.order_requests (tenant_id, id)
);
SELECT platform.enable_tenant_rls('ord.order_request_messages');
CREATE INDEX order_request_messages_request ON ord.order_request_messages (tenant_id, request_id, created_at, id);

-- Trigger theo câu lệnh như ord.order_status_history (0117): chặn UPDATE, DELETE, TRUNCATE của mọi role.
-- +goose StatementBegin
CREATE FUNCTION ord.order_request_messages_append_only()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $fn$
BEGIN
    RAISE EXCEPTION 'order_request_messages is append-only (% bị chặn)', TG_OP
        USING ERRCODE = 'insufficient_privilege';
END
$fn$;
-- +goose StatementEnd

CREATE TRIGGER order_request_messages_append_only
    BEFORE UPDATE OR DELETE OR TRUNCATE ON ord.order_request_messages
    FOR EACH STATEMENT EXECUTE FUNCTION ord.order_request_messages_append_only();

CREATE TABLE ord.order_request_files (
    tenant_id  uuid NOT NULL REFERENCES platform.tenants (id),
    request_id uuid NOT NULL,
    file_id    uuid NOT NULL,
    position   int NOT NULL CHECK (position BETWEEN 1 AND 5),
    PRIMARY KEY (tenant_id, request_id, file_id),
    UNIQUE (tenant_id, file_id),
    FOREIGN KEY (tenant_id, request_id) REFERENCES ord.order_requests (tenant_id, id)
);
SELECT platform.enable_tenant_rls('ord.order_request_files');

ALTER TABLE ord.order_settings
    ADD COLUMN exchange_window_days int NOT NULL DEFAULT 7;
ALTER TABLE ord.order_settings
    ADD CONSTRAINT order_settings_exchange_window_check CHECK (exchange_window_days BETWEEN 1 AND 60) NOT VALID;
ALTER TABLE ord.order_settings VALIDATE CONSTRAINT order_settings_exchange_window_check;

GRANT SELECT, INSERT ON ord.order_requests TO crm_app;
GRANT UPDATE (status, ship_back, outcome, updated_at, resolved_at, resolved_by, version) ON ord.order_requests TO crm_app;
GRANT SELECT, INSERT ON ord.order_request_messages TO crm_app;
GRANT SELECT, INSERT ON ord.order_request_files TO crm_app;

INSERT INTO platform.file_purposes (key, bucket, mode)
VALUES ('order_request_photo', 'private', 'private')
ON CONFLICT DO NOTHING;
