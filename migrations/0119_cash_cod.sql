-- Sổ COD tối thiểu — lát mỏng (kế hoạch M1, Task M1-32; spec 05 mục 2 "Sổ COD"; ADR-24; Q-X1). Tên bảng, cột đúng
-- ERD; M3 mở rộng bằng ALTER (thêm cột, mở rộng CHECK kind/account, khóa ngoại statement_line_id khi có
-- cash.cod_statement_lines) và bảng mới, không CREATE lại hai bảng này. Pha mở rộng: CREATE, GRANT.
-- Thứ tự khóa: docs/adr/0046-thu-tu-khoa.md nhóm 9 (cod_transfers → cod_ledger_entries). Cả hai bảng chỉ ghi thêm.

-- +goose Up
-- Một bút toán (một lần tiền COD của một vận đơn chuyển ngăn). idempotency_key chống ghi hai lần.
CREATE TABLE cash.cod_transfers (
    id                   uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id            uuid NOT NULL REFERENCES platform.tenants (id),
    shipment_id          uuid NOT NULL,
    kind                 text NOT NULL CHECK (kind IN ('expected', 'collected', 'remitted', 'returned', 'adjustment')),
    statement_line_id    uuid NULL,
    reverses_transfer_id uuid NULL,
    idempotency_key      text NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 200),
    reason               text NOT NULL DEFAULT '' CHECK (length(reason) <= 500),
    actor_type           text NOT NULL CHECK (actor_type IN ('staff', 'system', 'carrier')),
    actor_id             uuid NULL,
    created_at           timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    CONSTRAINT cod_transfers_idempotency_key UNIQUE (tenant_id, idempotency_key),
    -- Khóa ngoại xuyên module (ADR-27 mục 4, như fulfillment.shipments → ord.orders): sổ luôn thuộc một vận đơn thật.
    FOREIGN KEY (tenant_id, shipment_id) REFERENCES fulfillment.shipments (tenant_id, id),
    FOREIGN KEY (tenant_id, reverses_transfer_id) REFERENCES cash.cod_transfers (tenant_id, id)
);
SELECT platform.enable_tenant_rls('cash.cod_transfers');
CREATE INDEX cod_transfers_shipment ON cash.cod_transfers (tenant_id, shipment_id);

-- Các vế của một bút toán: số có dấu, đồng; mỗi ngăn một dòng trong một bút toán.
CREATE TABLE cash.cod_ledger_entries (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id   uuid NOT NULL REFERENCES platform.tenants (id),
    transfer_id uuid NOT NULL,
    account     text NOT NULL
        CHECK (account IN ('to_collect', 'carrier_holding', 'shop_received', 'carrier_fee', 'sales_clearing')),
    amount      bigint NOT NULL CHECK (amount <> 0 AND abs(amount) <= 9007199254740991),
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    CONSTRAINT cod_ledger_entries_account_key UNIQUE (tenant_id, transfer_id, account),
    FOREIGN KEY (tenant_id, transfer_id) REFERENCES cash.cod_transfers (tenant_id, id)
);
SELECT platform.enable_tenant_rls('cash.cod_ledger_entries');

-- Cân hai vế (ADR-24): lúc commit, mỗi bút toán vừa chèn (hay vừa thêm dòng) phải có ≥ 2 dòng và tổng amount = 0.
-- Constraint trigger hoãn tới commit nên bên ghi chèn bút toán rồi từng dòng trong cùng transaction. Chạy bằng quyền
-- người gọi: RLS chỉ cho thấy dòng của tenant đang đặt, nên đếm sai tenant ra 0 dòng → từ chối (an toàn).
-- +goose StatementBegin
CREATE FUNCTION cash.cod_transfer_balanced()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $fn$
DECLARE
    tid uuid;
    n   bigint;
    s   numeric;
BEGIN
    IF TG_TABLE_NAME = 'cod_transfers' THEN
        tid := NEW.id;
    ELSE
        tid := NEW.transfer_id;
    END IF;
    SELECT count(*), coalesce(sum(amount), 0) INTO n, s
    FROM cash.cod_ledger_entries
    WHERE tenant_id = NEW.tenant_id AND transfer_id = tid;
    IF n < 2 OR s <> 0 THEN
        RAISE EXCEPTION 'cod transfer unbalanced'
            USING ERRCODE = 'check_violation', DETAIL = format('transfer %s: %s dòng, tổng %s', tid, n, s);
    END IF;
    RETURN NULL;
END
$fn$;
-- +goose StatementEnd

CREATE CONSTRAINT TRIGGER cod_transfers_balanced
    AFTER INSERT ON cash.cod_transfers
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION cash.cod_transfer_balanced();

CREATE CONSTRAINT TRIGGER cod_ledger_entries_balanced
    AFTER INSERT ON cash.cod_ledger_entries
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION cash.cod_transfer_balanced();

-- Chỉ ghi thêm: chặn UPDATE, DELETE, TRUNCATE của mọi role (như inventory.stock_ledger, 0115).
-- +goose StatementBegin
CREATE FUNCTION cash.cod_append_only()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $fn$
BEGIN
    RAISE EXCEPTION '% is append-only (% bị chặn)', TG_TABLE_NAME, TG_OP
        USING ERRCODE = 'insufficient_privilege';
END
$fn$;
-- +goose StatementEnd

CREATE TRIGGER cod_transfers_append_only
    BEFORE UPDATE OR DELETE OR TRUNCATE ON cash.cod_transfers
    FOR EACH STATEMENT EXECUTE FUNCTION cash.cod_append_only();
CREATE TRIGGER cod_ledger_entries_append_only
    BEFORE UPDATE OR DELETE OR TRUNCATE ON cash.cod_ledger_entries
    FOR EACH STATEMENT EXECUTE FUNCTION cash.cod_append_only();

-- Số dư từng ngăn của từng vận đơn (docs/views.md; M3 mở rộng). Ngăn đã về 0 vẫn có dòng.
CREATE VIEW cash.cod_balances_v WITH (security_invoker = true) AS
SELECT t.shipment_id, e.account, sum(e.amount)::bigint AS balance
FROM cash.cod_ledger_entries e
JOIN cash.cod_transfers t ON t.tenant_id = e.tenant_id AND t.id = e.transfer_id
GROUP BY t.shipment_id, e.account;

-- crm_app: chỉ đọc và chèn. Không UPDATE, DELETE.
REVOKE ALL ON cash.cod_transfers, cash.cod_ledger_entries FROM PUBLIC;
GRANT SELECT, INSERT ON cash.cod_transfers, cash.cod_ledger_entries TO crm_app;
GRANT SELECT ON cash.cod_balances_v TO crm_app;
