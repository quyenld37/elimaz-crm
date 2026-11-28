-- Số dư "hãng đang giữ" theo vận đơn (kế hoạch M3, Task M3-48; chủ dữ liệu: module cash). Bài EXPLAIN của đường trọn M3
-- (200.000 vận đơn, hơn 1.000.000 dòng sổ COD) thấy "Lệch đối soát" và "Quá hạn chưa có tiền" phải cộng ngăn
-- carrier_holding của mọi vận đơn (cash.shipment_cod_v) rồi mới lọc khác 0: quét cả fulfillment.shipments,
-- cash.cod_transfers, cash.cod_statement_lines. Bảng này chỉ giữ vận đơn có carrier_holding khác 0, do trigger của
-- cash.cod_ledger_entries ghi trong cùng transaction với dòng sổ (sổ vẫn là nguồn sự thật; ADR-24): mỗi dòng sổ ngăn
-- carrier_holding cộng vào số dư của vận đơn, số dư về 0 thì xóa dòng. Không ai khác ghi bảng: crm_app chỉ đọc; hàm
-- trigger SECURITY DEFINER của chủ bảng (RLS vẫn áp theo app.tenant_id của transaction).
-- Thứ tự khóa (ADR-46 nhóm 9): dòng của bảng này bị khóa (chèn hay cộng) ngay sau khi chèn dòng sổ của cùng vận đơn,
-- trong lúc bên ghi đã giữ khóa tư vấn sổ COD của vận đơn đó (Consumer, chốt bảng kê, điều chỉnh): không thêm thứ tự
-- khóa mới.
-- Pha mở rộng: thêm bảng, hàm, trigger, index, quyền; điền số dư hiện có từ sổ.

-- +goose Up
CREATE TABLE cash.cod_open_holdings (
    tenant_id       uuid NOT NULL REFERENCES platform.tenants (id),
    shipment_id     uuid NOT NULL,
    carrier_holding bigint NOT NULL, -- khác 0 sau mỗi lần trigger chạy xong (về 0 thì dòng bị xóa)
    updated_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, shipment_id),
    FOREIGN KEY (tenant_id, shipment_id) REFERENCES fulfillment.shipments (tenant_id, id)
);
SELECT platform.enable_tenant_rls('cash.cod_open_holdings');
GRANT SELECT ON cash.cod_open_holdings TO crm_app;

-- Cộng amount của dòng sổ ngăn carrier_holding vào số dư của vận đơn (chèn hay cộng một câu: hai transaction cùng chèn
-- dòng đầu của một vận đơn thì câu sau chờ câu trước rồi cộng, không lỗi unique); tổng về 0 → xóa dòng. Chỉ chạy từ
-- trigger.
-- +goose StatementBegin
CREATE FUNCTION cash.cod_open_holdings_apply()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = ''
AS $fn$
BEGIN
    INSERT INTO cash.cod_open_holdings AS h (tenant_id, shipment_id, carrier_holding)
    VALUES (NEW.tenant_id, NEW.shipment_id, NEW.amount)
    ON CONFLICT (tenant_id, shipment_id)
    DO UPDATE SET carrier_holding = h.carrier_holding + EXCLUDED.carrier_holding, updated_at = pg_catalog.now();
    DELETE FROM cash.cod_open_holdings h
    WHERE h.tenant_id = NEW.tenant_id AND h.shipment_id = NEW.shipment_id AND h.carrier_holding = 0;
    RETURN NULL;
END
$fn$;
-- +goose StatementEnd
REVOKE EXECUTE ON FUNCTION cash.cod_open_holdings_apply() FROM PUBLIC;

CREATE TRIGGER cod_ledger_entries_open_holdings
AFTER INSERT ON cash.cod_ledger_entries
FOR EACH ROW WHEN (NEW.account = 'carrier_holding')
EXECUTE FUNCTION cash.cod_open_holdings_apply();

-- Điền số dư hiện có, từng tenant trong ngữ cảnh của tenant đó (RLS ép cả chủ bảng).
-- +goose StatementBegin
DO $backfill$
DECLARE
    t uuid;
BEGIN
    FOR t IN SELECT id FROM platform.tenants LOOP
        PERFORM set_config('app.tenant_id', t::text, true);
        INSERT INTO cash.cod_open_holdings (tenant_id, shipment_id, carrier_holding)
        SELECT e.tenant_id, e.shipment_id, sum(e.amount)
        FROM cash.cod_ledger_entries e
        WHERE e.account = 'carrier_holding'
        GROUP BY e.tenant_id, e.shipment_id
        HAVING sum(e.amount) <> 0;
    END LOOP;
    PERFORM set_config('app.tenant_id', '', true);
END
$backfill$;
-- +goose StatementEnd

-- "Lệch đối soát": dòng duplicate, external, amount_mismatch của bảng kê (index nhỏ, chỉ các dòng cần xem).
CREATE INDEX cod_statement_lines_discrepancy ON cash.cod_statement_lines (tenant_id, id)
    WHERE match_status IN ('duplicate', 'external', 'amount_mismatch');
