-- Vận chuyển tối thiểu — lát mỏng (kế hoạch M1, Task M1-31; spec 05 mục 2; Q-X1, B1, B4). Tên cột theo 05 và tên
-- M3 dùng; M3 mở rộng bằng ALTER (trạng thái delivery_failed, returning, returned, cancel_requested; cột service,
-- mode…) và bảng mới, không CREATE lại ba bảng này. Pha mở rộng: CREATE, GRANT.
-- Thứ tự khóa: docs/adr/0046-thu-tu-khoa.md nhóm 8 (carrier_accounts → shipments → shipment_events). Đường bắt đầu từ
-- vận đơn mà đổi đơn khóa ord.orders (nhóm 7) trước.

-- +goose Up
CREATE TABLE fulfillment.carrier_accounts (
    id                   uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id            uuid NOT NULL REFERENCES platform.tenants (id),
    seller_id            uuid NOT NULL,
    carrier_key          text NOT NULL CHECK (carrier_key ~ '^[a-z][a-z0-9_]{0,31}$'),
    name                 text NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    external_account_ref text NOT NULL CHECK (length(external_account_ref) BETWEEN 1 AND 120),
    warehouse_id         uuid NOT NULL,
    credential_enc       bytea NULL,
    status               text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    version              bigint NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, id),
    CONSTRAINT carrier_accounts_ref_key UNIQUE (tenant_id, carrier_key, external_account_ref),
    FOREIGN KEY (tenant_id, seller_id) REFERENCES catalog.sellers (tenant_id, id),
    FOREIGN KEY (tenant_id, warehouse_id) REFERENCES inventory.warehouses (tenant_id, id)
);
SELECT platform.enable_tenant_rls('fulfillment.carrier_accounts');
COMMENT ON INDEX fulfillment.carrier_accounts_ref_key IS
    'seller-exempt: một tài khoản ở hãng chỉ thuộc một người bán trong shop (Task M1-31)';

-- Mã vận đơn của CRM (NewShipmentCode): E + yyMMdd giờ Việt Nam + 6 ký tự base32 Crockford.
CREATE TABLE fulfillment.shipments (
    id                 uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id          uuid NOT NULL REFERENCES platform.tenants (id),
    code               text NOT NULL CHECK (code ~ '^E[0-9]{6}[0-9A-Z]{6}$'),
    order_id           uuid NOT NULL,
    carrier_key        text NOT NULL,
    carrier_account_id uuid NOT NULL,
    tracking_code      text NULL CHECK (length(tracking_code) BETWEEN 1 AND 64),
    status             text NOT NULL CHECK (status IN ('creating', 'awaiting_pickup', 'shipping', 'delivered', 'cancelled')),
    cod_amount         bigint NOT NULL CHECK (cod_amount BETWEEN 0 AND 9007199254740991),
    fee                bigint NULL CHECK (fee BETWEEN 0 AND 9007199254740991),
    last_status_at     timestamptz NULL,
    created_by         uuid NULL,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    version            bigint NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, id),
    CONSTRAINT shipments_code_key UNIQUE (tenant_id, code),
    CONSTRAINT shipments_tracking_key UNIQUE (tenant_id, carrier_key, tracking_code),
    FOREIGN KEY (tenant_id, carrier_account_id) REFERENCES fulfillment.carrier_accounts (tenant_id, id),
    -- Khóa ngoại xuyên module (ADR-27 mục 4, đánh đổi có chủ ý như stock_reservations): vận đơn luôn thuộc một đơn có thật.
    FOREIGN KEY (tenant_id, order_id) REFERENCES ord.orders (tenant_id, id)
);
SELECT platform.enable_tenant_rls('fulfillment.shipments');
-- Một đơn chỉ một vận đơn chưa hủy (B1).
CREATE UNIQUE INDEX shipments_active_order ON fulfillment.shipments (tenant_id, order_id) WHERE status <> 'cancelled';

-- Sự kiện vận đơn chỉ ghi thêm; event_key chống trùng theo vận đơn.
CREATE TABLE fulfillment.shipment_events (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id   uuid NOT NULL REFERENCES platform.tenants (id),
    shipment_id uuid NOT NULL,
    event_key   text NOT NULL CHECK (length(event_key) BETWEEN 1 AND 200),
    raw_status  text NOT NULL CHECK (length(raw_status) BETWEEN 1 AND 64),
    status      text NOT NULL CHECK (status IN ('creating', 'awaiting_pickup', 'shipping', 'delivered', 'cancelled')),
    occurred_at timestamptz NOT NULL,
    raw         jsonb NOT NULL CHECK (jsonb_typeof(raw) = 'object'),
    recorded_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    CONSTRAINT shipment_events_key UNIQUE (tenant_id, shipment_id, event_key),
    FOREIGN KEY (tenant_id, shipment_id) REFERENCES fulfillment.shipments (tenant_id, id)
);
SELECT platform.enable_tenant_rls('fulfillment.shipment_events');

-- +goose StatementBegin
CREATE FUNCTION fulfillment.shipment_events_append_only()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $fn$
BEGIN
    RAISE EXCEPTION 'shipment_events is append-only (% bị chặn)', TG_OP
        USING ERRCODE = 'insufficient_privilege';
END
$fn$;
-- +goose StatementEnd

CREATE TRIGGER shipment_events_append_only
    BEFORE UPDATE OR DELETE OR TRUNCATE ON fulfillment.shipment_events
    FOR EACH STATEMENT EXECUTE FUNCTION fulfillment.shipment_events_append_only();

CREATE VIEW fulfillment.shipments_v WITH (security_invoker = true) AS
SELECT id, order_id, code, carrier_key, carrier_account_id, tracking_code, status, cod_amount, fee
FROM fulfillment.shipments;

-- crm_app: tài khoản hãng chỉ đọc (seed và M3 ghi bằng chủ bảng/đường quản trị); vận đơn chèn, đọc, sửa đúng các cột
-- trạng thái; sự kiện chèn, đọc. Không DELETE.
REVOKE ALL ON fulfillment.shipment_events FROM PUBLIC;
GRANT SELECT ON fulfillment.carrier_accounts TO crm_app;
GRANT SELECT, INSERT ON fulfillment.shipments, fulfillment.shipment_events TO crm_app;
GRANT UPDATE (tracking_code, fee, status, last_status_at, updated_at, version) ON fulfillment.shipments TO crm_app;
GRANT SELECT ON fulfillment.shipments_v TO crm_app;
