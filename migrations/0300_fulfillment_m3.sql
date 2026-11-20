-- Vận chuyển M3 (kế hoạch M3, Task M3-01; spec 05 mục 2; Q-30, Q-32, Q-33). Mở rộng ba bảng của M1 (0118, 0130) bằng
-- ALTER, không CREATE lại; thêm bảng gọi lại, đổi địa chỉ, bất thường, báo giá, tem, mã địa giới của hãng; công bố view.
-- Chủ dữ liệu: module fulfillment. Khóa ngoại xuyên module mới: shipments → ord.orders (ghép người bán), ord.orders →
-- shipments (vận đơn hiện hành, M2 để lại), shipments/label_batches → platform.files (docs/views.md).
-- Pha mở rộng: chỉ thêm cột, giá trị CHECK, ràng buộc, bảng, view. SET NOT NULL ở dưới là thu chặt cột vừa thêm/điền
-- (dòng contract ngay phía trên).
-- Thứ tự khóa: docs/adr/0046-thu-tu-khoa.md nhóm 8 (carrier_accounts → shipments → shipment_events → callback_tasks →
-- address_change_requests → shipment_anomalies → label_batches → shipping_quotes → carrier_locations); migration chạy
-- một mình nên ALTER khóa bảng theo đúng thứ tự đó rồi tới ord.orders.

-- +goose Up

-- (1) Tài khoản hãng. route_key: khóa tuyến webhook 32 ký tự (như platform.webhook_deliveries.route_key); DEFAULT điền
-- dòng có sẵn (mỗi dòng một giá trị: hàm volatile tính lại từng dòng) và chèn của seed/test M1 (EnsureCarrierAccount);
-- đường tạo tài khoản của M3 tự sinh bằng crypto/rand.
ALTER TABLE fulfillment.carrier_accounts
    ADD COLUMN mode text NOT NULL DEFAULT 'api' CHECK (mode IN ('api', 'manual')),
    ADD COLUMN environment text NOT NULL DEFAULT 'sandbox' CHECK (environment IN ('sandbox', 'production')),
    ADD COLUMN route_key text NOT NULL DEFAULT replace(gen_random_uuid()::text, '-', '')
        CHECK (route_key ~ '^[A-Za-z0-9]{32}$'),
    ADD COLUMN default_service text NOT NULL DEFAULT '',
    ADD COLUMN default_options jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(default_options) = 'object'),
    ADD COLUMN is_default_for_warehouse boolean NOT NULL DEFAULT false,
    ADD COLUMN show_on_web boolean NOT NULL DEFAULT false,
    ADD COLUMN web_sort int NOT NULL DEFAULT 0,
    ADD COLUMN credential_key_id text,
    ADD COLUMN credential_updated_at timestamptz,
    ADD COLUMN last_check_at timestamptz,
    ADD COLUMN last_check_result text,
    DROP CONSTRAINT carrier_accounts_status_check,
    -- disabled giữ tới 0308 (Q-32): dòng M1 đổi sang paused ở (4), giá trị bỏ ở pha thu hẹp.
    ADD CONSTRAINT carrier_accounts_status_check
        CHECK (status IN ('draft', 'checking', 'active', 'auth_failed', 'paused', 'revoked', 'disabled')),
    ADD CONSTRAINT carrier_accounts_seller_id_key UNIQUE (tenant_id, seller_id, id);
CREATE UNIQUE INDEX carrier_accounts_route_key ON fulfillment.carrier_accounts (tenant_id, route_key);
COMMENT ON INDEX fulfillment.carrier_accounts_route_key IS
    'seller-exempt: khóa tuyến webhook duy nhất trên cả shop (định tuyến tới đúng một tài khoản, Task M3-01)';
-- Một tài khoản mặc định mỗi (người bán, kho, hãng) (ADR-29: mỗi người bán chọn tài khoản mặc định của mình).
CREATE UNIQUE INDEX carrier_accounts_default_key ON fulfillment.carrier_accounts (tenant_id, seller_id, warehouse_id,
    carrier_key) WHERE is_default_for_warehouse;

-- (2) Vận đơn. status thêm giá trị (giữ đủ năm giá trị của M1); seller_id điền ở (4) rồi thu chặt ở (5).
ALTER TABLE fulfillment.shipments
    ADD COLUMN seller_id uuid,
    ADD COLUMN mode text NOT NULL DEFAULT 'api' CHECK (mode IN ('api', 'manual')),
    ADD COLUMN service text NOT NULL DEFAULT '',
    ADD COLUMN sub_status text NOT NULL DEFAULT '',
    ADD COLUMN cancel_kind text
        CHECK (cancel_kind IN ('order_cancel', 'replace', 'create_rejected', 'carrier_cancelled', 'manual')),
    ADD COLUMN cancel_intent text CHECK (cancel_intent IN ('order_cancel', 'replace')),
    ADD COLUMN create_state text NOT NULL DEFAULT 'pending'
        CHECK (create_state IN ('pending', 'not_sent', 'unknown', 'created', 'rejected', 'manual')),
    ADD COLUMN create_attempts int NOT NULL DEFAULT 1 CHECK (create_attempts >= 0),
    ADD COLUMN create_job_id uuid,
    ADD COLUMN cod_collected_amount bigint CHECK (cod_collected_amount BETWEEN 0 AND 9007199254740991),
    ADD COLUMN declared_value bigint NOT NULL DEFAULT 0 CHECK (declared_value BETWEEN 0 AND 9007199254740991),
    ADD COLUMN fee_actual bigint CHECK (fee_actual BETWEEN 0 AND 9007199254740991),
    ADD COLUMN quote_id uuid,
    ADD COLUMN weight_grams int CHECK (weight_grams > 0),
    ADD COLUMN length_cm int CHECK (length_cm > 0),
    ADD COLUMN width_cm int CHECK (width_cm > 0),
    ADD COLUMN height_cm int CHECK (height_cm > 0),
    ADD COLUMN options jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(options) = 'object'),
    ADD COLUMN ship_to jsonb CHECK (ship_to IS NULL OR jsonb_typeof(ship_to) = 'object'),
    ADD COLUMN partial boolean NOT NULL DEFAULT false,
    ADD COLUMN picked_up_at timestamptz,
    ADD COLUMN delivered_at timestamptz,
    ADD COLUMN carrier_reported_returned_at timestamptz,
    ADD COLUMN returned_at timestamptz,
    ADD COLUMN last_carrier_event_at timestamptz,
    ADD COLUMN last_synced_at timestamptz,
    ADD COLUMN last_sync_error text,
    ADD COLUMN stuck_since timestamptz,
    ADD COLUMN needs_review boolean NOT NULL DEFAULT false,
    ADD COLUMN label_file_id uuid,
    ADD COLUMN label_printed_at timestamptz,
    ALTER COLUMN last_status_at SET DEFAULT now(),
    DROP CONSTRAINT shipments_status_check,
    ADD CONSTRAINT shipments_status_check CHECK (status IN ('creating', 'awaiting_pickup', 'shipping',
        'delivery_failed', 'delivered', 'returning', 'returned', 'cancel_requested', 'cancelled')),
    ADD CONSTRAINT shipments_label_file_fk FOREIGN KEY (tenant_id, label_file_id)
        REFERENCES platform.files (tenant_id, id);
COMMENT ON INDEX fulfillment.shipments_code_key IS
    'seller-exempt: mã vận đơn của CRM (NewShipmentCode) duy nhất trên cả shop (Task M1-31)';
COMMENT ON INDEX fulfillment.shipments_tracking_key IS
    'seller-exempt: mã hãng duy nhất theo hãng trên cả shop (VC13; tín hiệu webhook tra theo mã hãng)';
COMMENT ON INDEX fulfillment.shipments_active_order IS
    'seller-exempt: một đơn một vận đơn chưa hủy (B1); đơn đã thuộc đúng một người bán';
CREATE INDEX shipments_account_tracking ON fulfillment.shipments (tenant_id, carrier_account_id, tracking_code);
CREATE INDEX shipments_order_created ON fulfillment.shipments (tenant_id, order_id, created_at DESC);
CREATE INDEX shipments_open_status ON fulfillment.shipments (tenant_id, status, last_status_at)
    WHERE status NOT IN ('delivered', 'returned', 'cancelled');
CREATE INDEX shipments_stuck ON fulfillment.shipments (tenant_id, stuck_since) WHERE stuck_since IS NOT NULL;
CREATE INDEX shipments_needs_review ON fulfillment.shipments (tenant_id, needs_review) WHERE needs_review;
CREATE INDEX shipments_created ON fulfillment.shipments (tenant_id, created_at);

-- (3) Sự kiện vận đơn (chỉ ghi thêm; trigger và REVOKE của M1 giữ). status của M1: trạng thái vận đơn mà sự kiện chỉ
-- tới (shipstate.Target(canonical)), '' khi không chỉ tới trạng thái nào. recorded_at đã có từ M1.
ALTER TABLE fulfillment.shipment_events
    ADD COLUMN canonical text,
    ADD COLUMN source text CHECK (source IN ('carrier_api', 'carrier_webhook', 'staff', 'system')),
    ADD COLUMN sub_status text NOT NULL DEFAULT '',
    ADD COLUMN applied boolean,
    ADD COLUMN ignored_reason text
        CHECK (ignored_reason IN ('older', 'duplicate_state', 'not_current', 'invalid_edge', 'unmapped', 'review')),
    ADD COLUMN actor_id uuid,
    DROP CONSTRAINT shipment_events_status_check,
    ADD CONSTRAINT shipment_events_status_check CHECK (status IN ('', 'creating', 'awaiting_pickup', 'shipping',
        'delivery_failed', 'delivered', 'returning', 'returned', 'cancel_requested', 'cancelled'));
CREATE INDEX shipment_events_shipment_occurred ON fulfillment.shipment_events (tenant_id, shipment_id, occurred_at DESC);

-- (4) Điền dòng có sẵn của M1, từng tenant trong ngữ cảnh của tenant đó (RLS ép cả chủ bảng; không tắt RLS).
-- +goose StatementBegin
DO $backfill$
DECLARE
    t uuid;
BEGIN
    FOR t IN SELECT id FROM platform.tenants LOOP
        PERFORM set_config('app.tenant_id', t::text, true);

        UPDATE fulfillment.carrier_accounts SET status = 'paused' WHERE status = 'disabled';

        UPDATE fulfillment.shipments s
        SET seller_id = o.seller_id
        FROM ord.orders o
        WHERE o.tenant_id = s.tenant_id AND o.id = s.order_id AND s.seller_id IS NULL;

        UPDATE fulfillment.shipments
        SET create_state = 'created'
        WHERE tracking_code IS NOT NULL AND create_state = 'pending';

        UPDATE fulfillment.shipments SET cancel_kind = 'manual' WHERE status = 'cancelled' AND cancel_kind IS NULL;

        UPDATE fulfillment.shipments SET last_status_at = created_at WHERE last_status_at IS NULL;
    END LOOP;
    PERFORM set_config('app.tenant_id', '', true);
END
$backfill$;
-- +goose StatementEnd

-- (5) Thu chặt sau khi đã điền.
-- contract: không bản nào chèn vận đơn thiếu seller_id (cột mới thêm ngay trên; InsertShipment ghi từ bản này)
ALTER TABLE fulfillment.shipments ALTER COLUMN seller_id SET NOT NULL;
-- contract: không bản nào ghi last_status_at NULL (điền ở (4); cột có DEFAULT now(), mọi UPDATE trạng thái ghi now())
ALTER TABLE fulfillment.shipments ALTER COLUMN last_status_at SET NOT NULL;

ALTER TABLE fulfillment.shipments
    ADD CONSTRAINT shipments_cancel_kind_required CHECK (status <> 'cancelled' OR cancel_kind IS NOT NULL),
    ADD CONSTRAINT shipments_tracking_required CHECK (status IN ('creating', 'cancelled') OR tracking_code IS NOT NULL),
    -- KH14 cho vận đơn (ADR-29): người bán của vận đơn là người bán của đơn và của tài khoản hãng.
    ADD CONSTRAINT shipments_seller_order_fk FOREIGN KEY (tenant_id, seller_id, order_id)
        REFERENCES ord.orders (tenant_id, seller_id, id) NOT VALID,
    ADD CONSTRAINT shipments_seller_account_fk FOREIGN KEY (tenant_id, seller_id, carrier_account_id)
        REFERENCES fulfillment.carrier_accounts (tenant_id, seller_id, id) NOT VALID;
ALTER TABLE fulfillment.shipments VALIDATE CONSTRAINT shipments_seller_order_fk;
ALTER TABLE fulfillment.shipments VALIDATE CONSTRAINT shipments_seller_account_fk;

-- Khóa ngoại M2 để lại cho M3 (M2 Task 05): vận đơn hiện hành của đơn là vận đơn có thật. Đường tạo vận đơn chèn vận
-- đơn trước rồi mới gắn vào đơn (CreateForOrder).
ALTER TABLE ord.orders ADD CONSTRAINT orders_current_shipment_fk FOREIGN KEY (tenant_id, current_shipment_id)
    REFERENCES fulfillment.shipments (tenant_id, id) NOT VALID;
ALTER TABLE ord.orders VALIDATE CONSTRAINT orders_current_shipment_fk;

-- (6) Bảng mới (nhóm 8 của ADR-46, sau shipment_events).

-- Việc gọi lại khách sau một lần giao thất bại: một việc mỗi (vận đơn, lần giao).
CREATE TABLE fulfillment.callback_tasks (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id       uuid NOT NULL REFERENCES platform.tenants (id),
    shipment_id     uuid NOT NULL,
    order_id        uuid NOT NULL,
    attempt_no      int NOT NULL CHECK (attempt_no >= 1),
    failed_event_id uuid,
    failed_reason   text NOT NULL DEFAULT '',
    failed_at       timestamptz NOT NULL,
    due_at          timestamptz NOT NULL,
    status          text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'in_progress', 'done', 'closed')),
    outcome         text CHECK (outcome IN ('redeliver', 'redeliver_new_address', 'customer_cancel', 'no_answer',
        'reschedule', 'carrier_returning', 'delivered')),
    next_call_at    timestamptz,
    assignee_id     uuid,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    version         bigint NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, id),
    CONSTRAINT callback_tasks_attempt_key UNIQUE (tenant_id, shipment_id, attempt_no),
    FOREIGN KEY (tenant_id, shipment_id) REFERENCES fulfillment.shipments (tenant_id, id),
    FOREIGN KEY (tenant_id, order_id) REFERENCES ord.orders (tenant_id, id),
    FOREIGN KEY (tenant_id, failed_event_id) REFERENCES fulfillment.shipment_events (tenant_id, id)
);
SELECT platform.enable_tenant_rls('fulfillment.callback_tasks');
CREATE INDEX callback_tasks_status_due ON fulfillment.callback_tasks (tenant_id, status, due_at);

-- Nhật ký gọi lại: chỉ ghi thêm (trigger chặn cả chủ bảng, REVOKE).
CREATE TABLE fulfillment.callback_task_logs (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id    uuid NOT NULL REFERENCES platform.tenants (id),
    task_id      uuid NOT NULL,
    outcome      text NOT NULL CHECK (outcome IN ('redeliver', 'redeliver_new_address', 'customer_cancel', 'no_answer',
        'reschedule', 'carrier_returning', 'delivered')),
    note         text NOT NULL DEFAULT '' CHECK (length(note) <= 2000),
    next_call_at timestamptz,
    actor_id     uuid NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, task_id) REFERENCES fulfillment.callback_tasks (tenant_id, id)
);
SELECT platform.enable_tenant_rls('fulfillment.callback_task_logs');
CREATE INDEX callback_task_logs_task ON fulfillment.callback_task_logs (tenant_id, task_id, created_at);

-- +goose StatementBegin
CREATE FUNCTION fulfillment.callback_task_logs_append_only()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $fn$
BEGIN
    RAISE EXCEPTION 'callback_task_logs is append-only (% bị chặn)', TG_OP
        USING ERRCODE = 'insufficient_privilege';
END
$fn$;
-- +goose StatementEnd

CREATE TRIGGER callback_task_logs_append_only
    BEFORE UPDATE OR DELETE OR TRUNCATE ON fulfillment.callback_task_logs
    FOR EACH STATEMENT EXECUTE FUNCTION fulfillment.callback_task_logs_append_only();

-- Yêu cầu đổi địa chỉ giao: một yêu cầu còn mở mỗi vận đơn. new_address là dữ liệu cá nhân: không vào view.
CREATE TABLE fulfillment.address_change_requests (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id    uuid NOT NULL REFERENCES platform.tenants (id),
    shipment_id  uuid NOT NULL,
    new_address  jsonb NOT NULL CHECK (jsonb_typeof(new_address) = 'object'),
    mode         text NOT NULL CHECK (mode IN ('api', 'manual')),
    status       text NOT NULL CHECK (status IN ('pending', 'sent', 'confirmed', 'rejected', 'unknown')),
    job_id       uuid,
    requested_by uuid NOT NULL,
    resolved_by  uuid,
    resolved_at  timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    version      bigint NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, shipment_id) REFERENCES fulfillment.shipments (tenant_id, id)
);
SELECT platform.enable_tenant_rls('fulfillment.address_change_requests');
CREATE UNIQUE INDEX address_change_requests_open ON fulfillment.address_change_requests (tenant_id, shipment_id)
    WHERE status IN ('pending', 'sent', 'unknown');

-- Vận đơn cần xem: một bất thường mở mỗi (vận đơn, loại). detail chỉ mang mã, trạng thái, số tiền; không dữ liệu cá nhân.
CREATE TABLE fulfillment.shipment_anomalies (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id   uuid NOT NULL REFERENCES platform.tenants (id),
    shipment_id uuid NOT NULL,
    kind        text NOT NULL CHECK (kind IN ('in_doubt_create', 'in_doubt_cancel', 'manual_cancel', 'invalid_edge',
        'unmapped_status', 'tracking_conflict', 'cancel_conflict', 'partial_reported', 'lost_or_damaged',
        'missing_items', 'not_current_moved')),
    detail      jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(detail) = 'object'),
    status      text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'resolved')),
    resolution  text,
    resolved_by uuid,
    resolved_at timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, shipment_id) REFERENCES fulfillment.shipments (tenant_id, id)
);
SELECT platform.enable_tenant_rls('fulfillment.shipment_anomalies');
CREATE UNIQUE INDEX shipment_anomalies_open ON fulfillment.shipment_anomalies (tenant_id, shipment_id, kind)
    WHERE status = 'open';

-- Lô in tem của một tài khoản hãng (1–200 vận đơn).
CREATE TABLE fulfillment.label_batches (
    id                 uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id          uuid NOT NULL REFERENCES platform.tenants (id),
    carrier_account_id uuid NOT NULL,
    shipment_ids       uuid[] NOT NULL CHECK (cardinality(shipment_ids) BETWEEN 1 AND 200),
    status             text NOT NULL CHECK (status IN ('pending', 'ready', 'failed')),
    file_id            uuid,
    error_code         text,
    created_by         uuid NOT NULL,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, carrier_account_id) REFERENCES fulfillment.carrier_accounts (tenant_id, id),
    FOREIGN KEY (tenant_id, file_id) REFERENCES platform.files (tenant_id, id)
);
SELECT platform.enable_tenant_rls('fulfillment.label_batches');

-- Báo giá phí của một lần hỏi (request_id) cho một đơn: một dòng mỗi (tài khoản hãng, dịch vụ).
CREATE TABLE fulfillment.shipping_quotes (
    id                 uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id          uuid NOT NULL REFERENCES platform.tenants (id),
    order_id           uuid NOT NULL,
    request_id         uuid NOT NULL,
    carrier_account_id uuid NOT NULL,
    carrier_key        text NOT NULL,
    status             text NOT NULL CHECK (status IN ('pending', 'ok', 'error', 'unavailable')),
    service            text NOT NULL DEFAULT '',
    service_name       text NOT NULL DEFAULT '',
    fee_total          bigint CHECK (fee_total BETWEEN 0 AND 9007199254740991),
    fee_breakdown      jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(fee_breakdown) = 'object'),
    expected_days      int CHECK (expected_days >= 0),
    error_code         text,
    input_hash         bytea NOT NULL,
    valid_until        timestamptz,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    CONSTRAINT shipping_quotes_request_key UNIQUE (tenant_id, request_id, carrier_account_id, service),
    FOREIGN KEY (tenant_id, order_id) REFERENCES ord.orders (tenant_id, id),
    FOREIGN KEY (tenant_id, carrier_account_id) REFERENCES fulfillment.carrier_accounts (tenant_id, id)
);
SELECT platform.enable_tenant_rls('fulfillment.shipping_quotes');
CREATE INDEX shipping_quotes_order_created ON fulfillment.shipping_quotes (tenant_id, order_id, created_at DESC);
ALTER TABLE fulfillment.shipments ADD CONSTRAINT shipments_quote_fk FOREIGN KEY (tenant_id, quote_id)
    REFERENCES fulfillment.shipping_quotes (tenant_id, id);

-- Mã địa giới của hãng theo mã địa giới của CRM (đồng bộ từ hãng).
CREATE TABLE fulfillment.carrier_locations (
    tenant_id    uuid NOT NULL REFERENCES platform.tenants (id),
    carrier_key  text NOT NULL,
    level        text NOT NULL CHECK (level IN ('province', 'ward')),
    our_code     text NOT NULL,
    carrier_code text NOT NULL,
    carrier_name text NOT NULL,
    synced_at    timestamptz NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, carrier_key, level, our_code)
);
SELECT platform.enable_tenant_rls('fulfillment.carrier_locations');

-- (7) View công bố (Q-X2). shipments_v: chín cột của M1 giữ thứ tự, cột M3 nối vào cuối; không ship_to, options,
-- create_job_id. Không view nào có khóa, khóa tuyến, mã tài khoản ở hãng, payload thô, địa chỉ.
CREATE OR REPLACE VIEW fulfillment.shipments_v WITH (security_invoker = true) AS
SELECT id, order_id, code, carrier_key, carrier_account_id, tracking_code, status, cod_amount, fee,
       tenant_id, seller_id, mode, service, sub_status, cancel_kind, create_state, cod_collected_amount,
       declared_value, fee_actual, weight_grams, partial, picked_up_at, delivered_at, carrier_reported_returned_at,
       returned_at, last_status_at, last_carrier_event_at, last_synced_at, last_sync_error, stuck_since, needs_review,
       label_file_id, label_printed_at, created_at, ship_to ->> 'province_code' AS province_code
FROM fulfillment.shipments;

CREATE VIEW fulfillment.carrier_accounts_v WITH (security_invoker = true) AS
SELECT tenant_id, id, seller_id, carrier_key, mode, environment, name, warehouse_id, status, is_default_for_warehouse,
       show_on_web, web_sort, last_check_at, last_check_result, created_at, updated_at
FROM fulfillment.carrier_accounts;

CREATE VIEW fulfillment.shipment_events_v WITH (security_invoker = true) AS
SELECT tenant_id, id, shipment_id, source, raw_status, canonical, status, sub_status, occurred_at, recorded_at, applied,
       ignored_reason
FROM fulfillment.shipment_events;

CREATE VIEW fulfillment.locations_v WITH (security_invoker = true) AS
SELECT tenant_id, carrier_key, level, our_code, carrier_code, carrier_name, synced_at
FROM fulfillment.carrier_locations;

-- (8) Quyền của crm_app: chèn, đọc; sửa đúng các cột M3 đổi. Không sửa khóa (tenant_id, seller_id, carrier_key,
-- route_key, external_account_ref, mã vận đơn, đơn, tài khoản của vận đơn). Không DELETE.
GRANT INSERT ON fulfillment.carrier_accounts TO crm_app;
GRANT UPDATE (mode, environment, name, warehouse_id, credential_enc, credential_key_id, credential_updated_at, status,
    default_service, default_options, is_default_for_warehouse, show_on_web, web_sort, last_check_at,
    last_check_result, updated_at, version) ON fulfillment.carrier_accounts TO crm_app;
GRANT UPDATE (service, sub_status, cancel_kind, cancel_intent, create_state, create_attempts, create_job_id,
    cod_collected_amount, declared_value, fee_actual, quote_id, weight_grams, length_cm, width_cm, height_cm, options,
    ship_to, partial, picked_up_at, delivered_at, carrier_reported_returned_at, returned_at, last_carrier_event_at,
    last_synced_at, last_sync_error, stuck_since, needs_review, label_file_id, label_printed_at)
    ON fulfillment.shipments TO crm_app;

REVOKE ALL ON fulfillment.callback_task_logs FROM PUBLIC;
GRANT SELECT, INSERT ON fulfillment.callback_tasks, fulfillment.callback_task_logs, fulfillment.address_change_requests,
    fulfillment.shipment_anomalies, fulfillment.label_batches, fulfillment.shipping_quotes,
    fulfillment.carrier_locations TO crm_app;
GRANT UPDATE (due_at, status, outcome, next_call_at, assignee_id, updated_at, version)
    ON fulfillment.callback_tasks TO crm_app;
GRANT UPDATE (status, job_id, resolved_by, resolved_at, updated_at, version)
    ON fulfillment.address_change_requests TO crm_app;
GRANT UPDATE (detail, status, resolution, resolved_by, resolved_at, updated_at) ON fulfillment.shipment_anomalies
    TO crm_app;
GRANT UPDATE (status, file_id, error_code, updated_at) ON fulfillment.label_batches TO crm_app;
GRANT UPDATE (status, service_name, fee_total, fee_breakdown, expected_days, error_code, valid_until, updated_at)
    ON fulfillment.shipping_quotes TO crm_app;
GRANT UPDATE (carrier_code, carrier_name, synced_at, updated_at) ON fulfillment.carrier_locations TO crm_app;
GRANT SELECT ON fulfillment.shipments_v, fulfillment.carrier_accounts_v, fulfillment.shipment_events_v,
    fulfillment.locations_v TO crm_app;
