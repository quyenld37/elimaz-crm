-- Sổ COD của M3 (kế hoạch M3, Task M3-02; spec 05 mục 2 "Sổ COD"; ADR-24; Q-32, Q-X1, Q-X2). Mở rộng hai bảng của M1
-- (0119) bằng ALTER, không CREATE lại; thêm bảng kê, phiếu thu, nghĩa vụ hoàn, đối chiếu; công bố view. Chủ dữ liệu:
-- module cash. Khóa ngoại xuyên module mới: cod_statements → fulfillment.carrier_accounts; payments,
-- refund_obligations → ord.orders; refund_obligations → ord.returns (docs/views.md).
-- Pha mở rộng: thêm giá trị CHECK, cột, ràng buộc, bảng, view. SET NOT NULL ở dưới là thu chặt cột vừa thêm/điền
-- (dòng contract ngay phía trên).
-- Thứ tự khóa: docs/adr/0046-thu-tu-khoa.md nhóm 9 (cod_statements → cod_statement_lines → cod_transfers →
-- cod_ledger_entries → payments → refund_obligations → reconcile_runs); migration chạy một mình.

-- +goose Up

-- (1) Bảng kê COD của hãng: một tệp (sha256) chỉ nhập một lần cho mỗi hãng.
CREATE TABLE cash.cod_statements (
    id                 uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id          uuid NOT NULL REFERENCES platform.tenants (id),
    carrier_key        text NOT NULL,
    carrier_account_id uuid NOT NULL,
    file_id            uuid NOT NULL,
    file_sha256        text NOT NULL CHECK (file_sha256 ~ '^[0-9a-f]{64}$'),
    format             text NOT NULL,
    period_from        date,
    period_to          date,
    declared_total     bigint,
    status             text NOT NULL
        CHECK (status IN ('imported', 'parsing', 'parse_failed', 'parsed', 'matched', 'confirmed')),
    parse_error        text,
    line_count         int NOT NULL DEFAULT 0,
    imported_by        uuid NOT NULL,
    confirmed_by       uuid,
    confirmed_at       timestamptz,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    version            bigint NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, id),
    CONSTRAINT cod_statements_file_key UNIQUE (tenant_id, carrier_key, file_sha256),
    FOREIGN KEY (tenant_id, carrier_account_id) REFERENCES fulfillment.carrier_accounts (tenant_id, id)
);
SELECT platform.enable_tenant_rls('cash.cod_statements');

-- (2) Dòng của bảng kê. posted_transfer_id: bút toán đã ghi từ dòng này.
CREATE TABLE cash.cod_statement_lines (
    id                 uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id          uuid NOT NULL REFERENCES platform.tenants (id),
    statement_id       uuid NOT NULL,
    line_no            int NOT NULL CHECK (line_no >= 1),
    tracking_code      text NOT NULL,
    cod_amount         bigint NOT NULL CHECK (cod_amount >= 0),
    fee_amount         bigint NOT NULL DEFAULT 0,
    other_fee          bigint NOT NULL DEFAULT 0,
    net_amount         bigint NOT NULL,
    paid_on            date,
    match_status       text NOT NULL DEFAULT 'pending'
        CHECK (match_status IN ('pending', 'matched', 'amount_mismatch', 'external', 'duplicate', 'fee_only')),
    shipment_id        uuid,
    expected_amount    bigint,
    diff_amount        bigint,
    posted_transfer_id uuid,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    CONSTRAINT cod_statement_lines_line_key UNIQUE (tenant_id, statement_id, line_no),
    FOREIGN KEY (tenant_id, statement_id) REFERENCES cash.cod_statements (tenant_id, id),
    FOREIGN KEY (tenant_id, posted_transfer_id) REFERENCES cash.cod_transfers (tenant_id, id)
);
SELECT platform.enable_tenant_rls('cash.cod_statement_lines');
CREATE INDEX cod_statement_lines_tracking ON cash.cod_statement_lines (tenant_id, tracking_code);
CREATE INDEX cod_statement_lines_shipment ON cash.cod_statement_lines (tenant_id, shipment_id);

-- (3) Bút toán: thêm loại fee, reversal (giữ đủ năm giá trị của M1; Q-32); thời điểm việc xảy ra; lý do bắt buộc đúng
-- với adjustment, reversal; một bút toán chỉ bị đảo một lần; dòng bảng kê có thật. actor_type đã có CHECK ba giá trị
-- và khóa ngoại reverses_transfer_id đã có từ 0119.
ALTER TABLE cash.cod_transfers
    DROP CONSTRAINT cod_transfers_kind_check,
    ADD CONSTRAINT cod_transfers_kind_check
        CHECK (kind IN ('expected', 'collected', 'remitted', 'returned', 'adjustment', 'fee', 'reversal')),
    ADD COLUMN occurred_at timestamptz,
    ADD CONSTRAINT cod_transfers_reason_kind CHECK ((kind IN ('adjustment', 'reversal')) = (reason <> '')) NOT VALID,
    ADD CONSTRAINT cod_transfers_reversal_target CHECK ((kind = 'reversal') = (reverses_transfer_id IS NOT NULL))
        NOT VALID,
    ADD CONSTRAINT cod_transfers_statement_line_fk FOREIGN KEY (tenant_id, statement_line_id)
        REFERENCES cash.cod_statement_lines (tenant_id, id);
ALTER TABLE cash.cod_transfers VALIDATE CONSTRAINT cod_transfers_reason_kind;
ALTER TABLE cash.cod_transfers VALIDATE CONSTRAINT cod_transfers_reversal_target;
CREATE UNIQUE INDEX cod_transfers_reverses_key ON cash.cod_transfers (tenant_id, reverses_transfer_id)
    WHERE reverses_transfer_id IS NOT NULL;
CREATE INDEX cod_transfers_shipment_occurred ON cash.cod_transfers (tenant_id, shipment_id, occurred_at);

-- (4) Dòng sổ mang vận đơn của bút toán (số dư theo vận đơn đọc thẳng index; database kiểm cùng vận đơn ở (6)).
ALTER TABLE cash.cod_ledger_entries ADD COLUMN shipment_id uuid;

-- (5) Điền dòng có sẵn của M1, từng tenant trong ngữ cảnh của tenant đó (RLS ép cả chủ bảng; không tắt RLS). Hai
-- bảng chỉ ghi thêm: tắt trigger chặn sửa của M1 chỉ trong lúc điền (chủ bảng, migration chạy một mình), bật lại ngay.
ALTER TABLE cash.cod_transfers DISABLE TRIGGER cod_transfers_append_only;
ALTER TABLE cash.cod_ledger_entries DISABLE TRIGGER cod_ledger_entries_append_only;
-- +goose StatementBegin
DO $backfill$
DECLARE
    t uuid;
BEGIN
    FOR t IN SELECT id FROM platform.tenants LOOP
        PERFORM set_config('app.tenant_id', t::text, true);

        UPDATE cash.cod_transfers SET occurred_at = created_at WHERE occurred_at IS NULL;

        UPDATE cash.cod_ledger_entries e
        SET shipment_id = tr.shipment_id
        FROM cash.cod_transfers tr
        WHERE tr.tenant_id = e.tenant_id AND tr.id = e.transfer_id AND e.shipment_id IS NULL;
    END LOOP;
    PERFORM set_config('app.tenant_id', '', true);
END
$backfill$;
-- +goose StatementEnd
ALTER TABLE cash.cod_transfers ENABLE TRIGGER cod_transfers_append_only;
ALTER TABLE cash.cod_ledger_entries ENABLE TRIGGER cod_ledger_entries_append_only;

ALTER TABLE cash.cod_transfers ALTER COLUMN occurred_at SET DEFAULT now();
-- contract: không bản nào chèn bút toán occurred_at NULL (cột mới thêm ở (3), điền ở (5), DEFAULT now())
ALTER TABLE cash.cod_transfers ALTER COLUMN occurred_at SET NOT NULL;
-- contract: không bản nào chèn dòng sổ thiếu shipment_id (cột mới thêm ở (4), điền ở (5); InsertEntry ghi từ bản này)
ALTER TABLE cash.cod_ledger_entries ALTER COLUMN shipment_id SET NOT NULL;
CREATE INDEX cod_ledger_entries_shipment_account ON cash.cod_ledger_entries (tenant_id, shipment_id, account);

-- (6) Cân hai vế (ADR-24), thay thân hàm của M1 (giữ tên và hai constraint trigger hoãn tới commit): mỗi bút toán chạm
-- tới phải có ≥ 2 dòng, tổng amount = 0 và mọi dòng cùng vận đơn với bút toán.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION cash.cod_transfer_balanced()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $fn$
DECLARE
    tid   uuid;
    ship  uuid;
    n     bigint;
    s     numeric;
    other bigint;
BEGIN
    IF TG_TABLE_NAME = 'cod_transfers' THEN
        tid := NEW.id;
    ELSE
        tid := NEW.transfer_id;
    END IF;
    SELECT shipment_id INTO ship
    FROM cash.cod_transfers
    WHERE tenant_id = NEW.tenant_id AND id = tid;
    SELECT count(*), coalesce(sum(amount), 0), count(*) FILTER (WHERE shipment_id IS DISTINCT FROM ship)
    INTO n, s, other
    FROM cash.cod_ledger_entries
    WHERE tenant_id = NEW.tenant_id AND transfer_id = tid;
    IF n < 2 OR s <> 0 OR other > 0 THEN
        RAISE EXCEPTION 'cod transfer unbalanced: %', tid
            USING ERRCODE = 'check_violation',
                  DETAIL = format('%s dòng, tổng %s, %s dòng khác vận đơn của bút toán', n, s, other);
    END IF;
    RETURN NULL;
END
$fn$;
-- +goose StatementEnd

-- (7) Nghĩa vụ hoàn tiền cho khách: một nghĩa vụ mỗi (đơn, nguồn, phiếu trả) — return_id rỗng cũng chỉ một.
CREATE TABLE cash.refund_obligations (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id   uuid NOT NULL REFERENCES platform.tenants (id),
    order_id    uuid NOT NULL,
    return_id   uuid,
    source      text NOT NULL CHECK (source IN ('order_cancel', 'customer_return', 'exchange', 'partial_delivery')),
    amount      bigint NOT NULL CHECK (amount > 0),
    paid_amount bigint NOT NULL DEFAULT 0 CHECK (paid_amount >= 0 AND paid_amount <= amount),
    status      text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'paid', 'waived')),
    waive_reason text,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    version     bigint NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, id),
    CONSTRAINT refund_obligations_source_key UNIQUE NULLS NOT DISTINCT (tenant_id, order_id, source, return_id),
    FOREIGN KEY (tenant_id, order_id) REFERENCES ord.orders (tenant_id, id),
    FOREIGN KEY (tenant_id, return_id) REFERENCES ord.returns (tenant_id, id)
);
SELECT platform.enable_tenant_rls('cash.refund_obligations');

-- Phiếu thu tiền ngoài COD (chuyển khoản, cọc, tiền mặt, bù đổi hàng) và phiếu chi hoàn (số âm). Chỉ ghi thêm.
CREATE TABLE cash.payments (
    id                   uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id            uuid NOT NULL REFERENCES platform.tenants (id),
    order_id             uuid NOT NULL,
    method               text NOT NULL
        CHECK (method IN ('bank_transfer', 'deposit', 'cash', 'exchange_credit', 'refund')),
    amount               bigint NOT NULL CHECK (amount <> 0),
    reference            text NOT NULL DEFAULT '',
    received_at          timestamptz NOT NULL,
    refund_obligation_id uuid,
    actor_id             uuid NOT NULL,
    created_at           timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    CONSTRAINT payments_refund_sign CHECK ((method = 'refund') = (amount < 0)),
    FOREIGN KEY (tenant_id, order_id) REFERENCES ord.orders (tenant_id, id),
    FOREIGN KEY (tenant_id, refund_obligation_id) REFERENCES cash.refund_obligations (tenant_id, id)
);
SELECT platform.enable_tenant_rls('cash.payments');
CREATE INDEX payments_order ON cash.payments (tenant_id, order_id);

-- (8) Đối chiếu sổ COD (M7 đọc, A22). Phát hiện chỉ ghi thêm; detail chỉ mang mã, số tiền, không dữ liệu cá nhân.
CREATE TABLE cash.reconcile_runs (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id  uuid NOT NULL REFERENCES platform.tenants (id),
    ran_at     timestamptz NOT NULL,
    ok         boolean NOT NULL,
    findings   int NOT NULL CHECK (findings >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id)
);
SELECT platform.enable_tenant_rls('cash.reconcile_runs');

CREATE TABLE cash.reconcile_findings (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id   uuid NOT NULL REFERENCES platform.tenants (id),
    run_id      uuid NOT NULL,
    kind        text NOT NULL,
    shipment_id uuid,
    detail      jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(detail) = 'object'),
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, run_id) REFERENCES cash.reconcile_runs (tenant_id, id)
);
SELECT platform.enable_tenant_rls('cash.reconcile_findings');

-- Chỉ ghi thêm: dùng lại hàm chặn của M1 (0119; báo theo TG_TABLE_NAME).
CREATE TRIGGER payments_append_only
    BEFORE UPDATE OR DELETE OR TRUNCATE ON cash.payments
    FOR EACH STATEMENT EXECUTE FUNCTION cash.cod_append_only();
CREATE TRIGGER reconcile_findings_append_only
    BEFORE UPDATE OR DELETE OR TRUNCATE ON cash.reconcile_findings
    FOR EACH STATEMENT EXECUTE FUNCTION cash.cod_append_only();

-- (9) View công bố (Q-X2; docs/views.md). cash.cod_balances_v của M1 giữ nguyên.
CREATE VIEW cash.shipment_cod_v WITH (security_invoker = true) AS
SELECT tenant_id, shipment_id,
       coalesce(sum(amount) FILTER (WHERE account = 'to_collect'), 0)::bigint      AS to_collect,
       coalesce(sum(amount) FILTER (WHERE account = 'carrier_holding'), 0)::bigint AS carrier_holding,
       coalesce(sum(amount) FILTER (WHERE account = 'shop_received'), 0)::bigint   AS shop_received,
       coalesce(sum(amount) FILTER (WHERE account = 'carrier_fee'), 0)::bigint     AS carrier_fee,
       coalesce(sum(amount) FILTER (WHERE account = 'sales_clearing'), 0)::bigint  AS sales_clearing
FROM cash.cod_ledger_entries
GROUP BY tenant_id, shipment_id;

CREATE VIEW cash.cod_transfers_v WITH (security_invoker = true) AS
SELECT tenant_id, id, shipment_id, kind, statement_line_id, reverses_transfer_id, reason, actor_type, occurred_at
FROM cash.cod_transfers;

CREATE VIEW cash.payments_v WITH (security_invoker = true) AS
SELECT tenant_id, id, order_id, method, amount, received_at
FROM cash.payments;

CREATE VIEW cash.refund_obligations_v WITH (security_invoker = true) AS
SELECT tenant_id, id, order_id, return_id, source, amount, paid_amount, status
FROM cash.refund_obligations;

CREATE VIEW cash.reconcile_runs_v WITH (security_invoker = true) AS
SELECT tenant_id, id, ran_at, ok, findings
FROM cash.reconcile_runs;

-- (10) Quyền của crm_app: bảng chỉ ghi thêm chỉ SELECT, INSERT; bảng có trạng thái thêm UPDATE theo cột.
REVOKE ALL ON cash.cod_statements, cash.cod_statement_lines, cash.payments, cash.refund_obligations,
    cash.reconcile_runs, cash.reconcile_findings FROM PUBLIC;
GRANT SELECT, INSERT ON cash.cod_statements, cash.cod_statement_lines, cash.payments, cash.refund_obligations,
    cash.reconcile_runs, cash.reconcile_findings TO crm_app;
GRANT UPDATE (period_from, period_to, declared_total, status, parse_error, line_count, confirmed_by, confirmed_at,
    updated_at, version) ON cash.cod_statements TO crm_app;
GRANT UPDATE (match_status, shipment_id, expected_amount, diff_amount, posted_transfer_id, updated_at)
    ON cash.cod_statement_lines TO crm_app;
GRANT UPDATE (paid_amount, status, waive_reason, updated_at, version) ON cash.refund_obligations TO crm_app;
GRANT SELECT ON cash.shipment_cod_v, cash.cod_transfers_v, cash.payments_v, cash.refund_obligations_v,
    cash.reconcile_runs_v TO crm_app;
