-- Đơn hàng M2 (kế hoạch M2, Task M2-05; spec 05 mục 2). Mở rộng ord.orders, ord.order_items, ord.order_status_history
-- của M1 (Task M1-30) bằng ALTER theo luật mở rộng trước: cột mới có DEFAULT hoặc cho phép rỗng → điền dòng có sẵn →
-- SET NOT NULL; ràng buộc mới NOT VALID rồi VALIDATE. Không tạo lại bảng của M1, không đổi chiều khóa ghép của M1.
-- Bảng mới: ghi chú đơn (chỉ ghi thêm), nguồn đơn, cài đặt đơn, cặp đơn nghi trùng, phiếu trả, dòng phiếu trả.
-- Mã đơn vẫn chỉ từ ord.order_code_seq (order.nextCode, M1); mã phiếu trả từ ord.return_code_seq (order.nextReturnCode).
-- Thứ tự khóa: docs/adr/0046-thu-tu-khoa.md nhóm 7 (orders → order_items → order_duplicate_links →
-- order_status_history → returns → return_items → traffic_sources → order_settings).

-- +goose Up

-- (1) Nguồn đơn trước (orders trỏ tới). key: khóa cố định của nguồn hệ thống (B12), rỗng với nguồn nhân viên tạo.
CREATE TABLE ord.traffic_sources (
    id                 uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id          uuid NOT NULL REFERENCES platform.tenants (id),
    key                text CHECK (key ~ '^[a-z][a-z0-9_]{0,63}$'),
    name               text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 100),
    channel_account_id uuid,
    active             boolean NOT NULL DEFAULT true,
    position           int NOT NULL CHECK (position >= 0),
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    version            bigint NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, id),
    CONSTRAINT traffic_sources_name_key UNIQUE (tenant_id, name)
);
SELECT platform.enable_tenant_rls('ord.traffic_sources');
CREATE UNIQUE INDEX traffic_sources_key_key ON ord.traffic_sources (tenant_id, key) WHERE key IS NOT NULL;

-- (2) Cột mới của đơn (M1 có: id, tenant_id, seller_id, code, customer_id, channel, idempotency_key, status,
-- current_shipment_id, ship_address, subtotal, discount_total, shipping_fee_charged, cod_amount, grand_total,
-- created_by, cancel_reason, created_at, updated_at, version).
ALTER TABLE ord.orders
    ADD COLUMN IF NOT EXISTS checkout_group_id uuid,
    ADD COLUMN IF NOT EXISTS channel_account_id uuid,
    ADD COLUMN IF NOT EXISTS traffic_source_id uuid,
    ADD COLUMN IF NOT EXISTS conversation_id uuid,
    ADD COLUMN IF NOT EXISTS external_ref text,
    ADD COLUMN IF NOT EXISTS pending_reason text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS packing_status text NOT NULL DEFAULT 'none',
    ADD COLUMN IF NOT EXISTS payment_status text NOT NULL DEFAULT 'unpaid',
    ADD COLUMN IF NOT EXISTS order_type text NOT NULL DEFAULT 'ship',
    ADD COLUMN IF NOT EXISTS warehouse_id uuid,
    ADD COLUMN IF NOT EXISTS ship_phone text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS ship_phone_reversed text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS address_complete boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS referral_ref jsonb,
    ADD COLUMN IF NOT EXISTS line_discount_total bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS order_discount bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS coupon_discount bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS prepaid_total bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS draft_expires_at timestamptz,
    ADD COLUMN IF NOT EXISTS carrier_note text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS care_by uuid,
    ADD COLUMN IF NOT EXISTS confirmed_at timestamptz,
    ADD COLUMN IF NOT EXISTS confirmed_by uuid,
    ADD COLUMN IF NOT EXISTS discount_approved_by uuid,
    ADD COLUMN IF NOT EXISTS discount_approved_at timestamptz,
    ADD COLUMN IF NOT EXISTS cancel_note text,
    ADD COLUMN IF NOT EXISTS cancelled_at timestamptz,
    ADD COLUMN IF NOT EXISTS duplicate_suspect boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS replaced_return_id uuid;

ALTER TABLE ord.order_items
    ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();

ALTER TABLE ord.order_status_history
    ADD COLUMN IF NOT EXISTS from_pending text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS to_pending text NOT NULL DEFAULT '';

-- (3) Điền đơn có sẵn (lát mỏng M1), từng tenant trong ngữ cảnh của tenant đó (RLS ép cả chủ bảng; không tắt RLS):
-- warehouse_id = kho của dòng giữ hàng của đơn, không có thì kho MAIN; ship_phone theo luật customer.NormalizePhone
-- (bỏ khoảng trắng, tab, chấm, gạch; "+84"/"84"/"0" + 9–10 chữ số không bắt đầu bằng 0 → "+84…", còn lại ''),
-- ship_phone_reversed = chữ số đảo ngược; confirmed_at, confirmed_by cho đơn M1 tạo thẳng confirmed;
-- line_discount_total, order_discount từ dòng đơn. Sau đó kiểm tổng tiền như trigger ở (7).
-- +goose StatementBegin
DO $backfill$
DECLARE
    t uuid;
    bad text;
BEGIN
    FOR t IN SELECT id FROM platform.tenants LOOP
        PERFORM set_config('app.tenant_id', t::text, true);

        UPDATE ord.orders o
        SET warehouse_id = COALESCE(
            (SELECT r.warehouse_id
             FROM ord.order_items i
             JOIN inventory.stock_reservations r ON r.tenant_id = i.tenant_id AND r.order_item_id = i.id
             WHERE i.tenant_id = o.tenant_id AND i.order_id = o.id
             ORDER BY i.position, r.id
             LIMIT 1),
            (SELECT w.id FROM inventory.warehouses w WHERE w.tenant_id = o.tenant_id AND w.code = 'MAIN'))
        WHERE o.warehouse_id IS NULL;

        UPDATE ord.orders o
        SET ship_phone = p.phone,
            ship_phone_reversed = reverse(regexp_replace(p.phone, '[^0-9]', '', 'g'))
        FROM (
            SELECT id, CASE WHEN rest ~ '^[1-9][0-9]{8,9}$' THEN '+84' || rest ELSE '' END AS phone
            FROM (
                SELECT id, CASE
                    WHEN s LIKE '+84%' THEN substr(s, 4)
                    WHEN s LIKE '84%' THEN substr(s, 3)
                    WHEN s LIKE '0%' THEN substr(s, 2)
                    ELSE '' END AS rest
                FROM (
                    SELECT id, translate(COALESCE(ship_address ->> 'phone', ''), ' ' || chr(9) || '.-', '') AS s
                    FROM ord.orders
                ) a
            ) b
        ) p
        WHERE o.id = p.id AND o.ship_phone = '';

        UPDATE ord.orders
        SET confirmed_at = created_at, confirmed_by = created_by
        WHERE status IN ('confirmed', 'delivered') AND confirmed_at IS NULL;

        UPDATE ord.orders o
        SET line_discount_total = s.ld, order_discount = s.alloc
        FROM (
            SELECT order_id, sum(line_discount)::bigint AS ld, sum(allocated_order_discount)::bigint AS alloc
            FROM ord.order_items
            GROUP BY order_id
        ) s
        WHERE o.id = s.order_id AND (o.line_discount_total, o.order_discount) <> (s.ld, s.alloc);

        SELECT o.code INTO bad
        FROM ord.orders o
        LEFT JOIN LATERAL (
            SELECT COALESCE(sum(i.quantity::bigint * i.unit_price), 0) AS sub
            FROM ord.order_items i
            WHERE i.tenant_id = o.tenant_id AND i.order_id = o.id
        ) s ON true
        WHERE s.sub <> o.subtotal
        LIMIT 1;
        IF bad IS NOT NULL THEN
            RAISE EXCEPTION 'đơn % có subtotal khác tổng dòng đơn', bad;
        END IF;
    END LOOP;
    PERFORM set_config('app.tenant_id', '', true);
END
$backfill$;
-- +goose StatementEnd

-- (4) Thu chặt sau khi đã điền: mọi đơn có kho; mã mới (CreateManual của M1, Create của M2) luôn ghi warehouse_id.
-- contract: không bản nào ghi warehouse_id NULL (cột mới thêm ngay trên; CreateManual ghi kho giữ hàng từ bản này)
ALTER TABLE ord.orders ALTER COLUMN warehouse_id SET NOT NULL;

ALTER TABLE ord.orders
    ADD CONSTRAINT orders_pending_reason_check
        CHECK (pending_reason IN ('', 'awaiting_customer', 'out_of_stock', 'discount_approval')) NOT VALID,
    ADD CONSTRAINT orders_packing_status_check CHECK (packing_status IN ('none', 'picking', 'packed')) NOT VALID,
    ADD CONSTRAINT orders_payment_status_check
        CHECK (payment_status IN ('unpaid', 'partial', 'paid', 'refunded')) NOT VALID,
    ADD CONSTRAINT orders_order_type_check CHECK (order_type IN ('ship', 'exchange', 'customer_return')) NOT VALID,
    ADD CONSTRAINT orders_new_money_check CHECK (
        line_discount_total BETWEEN 0 AND 9007199254740991 AND order_discount BETWEEN 0 AND 9007199254740991
        AND coupon_discount BETWEEN 0 AND 9007199254740991 AND prepaid_total BETWEEN 0 AND 9007199254740991) NOT VALID,
    ADD CONSTRAINT orders_discount_sum_check
        CHECK (discount_total = line_discount_total + order_discount + coupon_discount) NOT VALID,
    ADD CONSTRAINT orders_cod_prepaid_check CHECK (cod_amount = GREATEST(grand_total - prepaid_total, 0)) NOT VALID,
    ADD CONSTRAINT orders_draft_expiry_check CHECK ((status = 'draft') = (draft_expires_at IS NOT NULL)) NOT VALID,
    ADD CONSTRAINT orders_pending_status_check CHECK (status IN ('draft', 'new') OR pending_reason = '') NOT VALID,
    ADD CONSTRAINT orders_discount_approval_check
        CHECK (pending_reason <> 'discount_approval' OR status IN ('draft', 'new')) NOT VALID,
    ADD CONSTRAINT orders_cancel_reason_q18_check CHECK (cancel_reason IN
        ('customer_changed_mind', 'out_of_stock', 'unreachable', 'wrong_info', 'other', 'draft_expired', 'duplicate'))
        NOT VALID,
    ADD CONSTRAINT orders_warehouse_fkey FOREIGN KEY (tenant_id, warehouse_id)
        REFERENCES inventory.warehouses (tenant_id, id) NOT VALID,
    ADD CONSTRAINT orders_traffic_source_fkey FOREIGN KEY (tenant_id, traffic_source_id)
        REFERENCES ord.traffic_sources (tenant_id, id) NOT VALID;
ALTER TABLE ord.orders VALIDATE CONSTRAINT orders_pending_reason_check;
ALTER TABLE ord.orders VALIDATE CONSTRAINT orders_packing_status_check;
ALTER TABLE ord.orders VALIDATE CONSTRAINT orders_payment_status_check;
ALTER TABLE ord.orders VALIDATE CONSTRAINT orders_order_type_check;
ALTER TABLE ord.orders VALIDATE CONSTRAINT orders_new_money_check;
ALTER TABLE ord.orders VALIDATE CONSTRAINT orders_discount_sum_check;
ALTER TABLE ord.orders VALIDATE CONSTRAINT orders_cod_prepaid_check;
ALTER TABLE ord.orders VALIDATE CONSTRAINT orders_draft_expiry_check;
ALTER TABLE ord.orders VALIDATE CONSTRAINT orders_pending_status_check;
ALTER TABLE ord.orders VALIDATE CONSTRAINT orders_discount_approval_check;
ALTER TABLE ord.orders VALIDATE CONSTRAINT orders_cancel_reason_q18_check;
ALTER TABLE ord.orders VALIDATE CONSTRAINT orders_warehouse_fkey;
ALTER TABLE ord.orders VALIDATE CONSTRAINT orders_traffic_source_fkey;

-- CHECK cod_amount ≤ grand_total của M1 được thay bằng cod_amount = max(grand_total − prepaid_total, 0) (đã VALIDATE ở
-- trên; mọi đơn M1 có cod_amount = grand_total, prepaid_total = 0). Bỏ ràng buộc là nới ra.
ALTER TABLE ord.orders DROP CONSTRAINT orders_check1;

-- Một đơn một mã ngoài theo kênh của người bán (đơn web, đơn sàn; NULL không đụng nhau).
ALTER TABLE ord.orders
    ADD CONSTRAINT orders_external_ref_key UNIQUE (tenant_id, seller_id, channel, external_ref);

-- Index danh sách đơn (04 mục 7).
CREATE INDEX orders_status_created ON ord.orders (tenant_id, status, created_at DESC, id DESC);
CREATE INDEX orders_open_created ON ord.orders (tenant_id, created_at DESC, id DESC)
    WHERE status IN ('draft', 'new', 'confirmed', 'cancel_requested');
CREATE INDEX orders_customer_created ON ord.orders (tenant_id, customer_id, created_at DESC);
CREATE INDEX orders_ship_phone_reversed ON ord.orders (tenant_id, ship_phone_reversed text_pattern_ops);
CREATE INDEX orders_ship_phone ON ord.orders (tenant_id, ship_phone, created_at DESC);
CREATE INDEX orders_draft_expires ON ord.orders (tenant_id, draft_expires_at) WHERE status = 'draft';
CREATE INDEX orders_duplicate_suspect ON ord.orders (tenant_id, created_at DESC) WHERE duplicate_suspect;

-- (5) Dòng đơn: giảm theo dòng không vượt tiền dòng; một biến thể một dòng trong đơn. Unique có seller_id ngay sau
-- tenant_id (luật (g), ADR-29; đơn đã cố định người bán nên tương đương (tenant_id, order_id, variant_id)).
ALTER TABLE ord.order_items
    ADD CONSTRAINT order_items_line_discount_within_check
        CHECK (line_discount >= 0 AND line_discount <= quantity::bigint * unit_price) NOT VALID;
ALTER TABLE ord.order_items VALIDATE CONSTRAINT order_items_line_discount_within_check;
ALTER TABLE ord.order_items
    ADD CONSTRAINT order_items_variant_key UNIQUE (tenant_id, seller_id, order_id, variant_id);

-- (6) Lịch sử: thêm tác nhân service (dịch vụ nội bộ, M5); ràng buộc mới VALIDATE rồi bỏ ràng buộc 4 giá trị (nới ra).
ALTER TABLE ord.order_status_history
    ADD CONSTRAINT order_status_history_actor_type_m2_check
        CHECK (actor_type IN ('staff', 'carrier', 'system', 'customer', 'service')) NOT VALID,
    ADD CONSTRAINT order_status_history_pending_check CHECK (
        from_pending IN ('', 'awaiting_customer', 'out_of_stock', 'discount_approval')
        AND to_pending IN ('', 'awaiting_customer', 'out_of_stock', 'discount_approval')) NOT VALID;
ALTER TABLE ord.order_status_history VALIDATE CONSTRAINT order_status_history_actor_type_m2_check;
ALTER TABLE ord.order_status_history VALIDATE CONSTRAINT order_status_history_pending_check;
ALTER TABLE ord.order_status_history DROP CONSTRAINT order_status_history_actor_type_check;

-- Khóa ngoại xuyên module (đánh đổi có chủ ý, docs/views.md): sửa đơn bỏ được dòng đã nhả hàng (Task M2-20); dòng giữ
-- hàng released và sổ tồn vẫn còn, chỉ mất liên kết tới dòng đơn. Bỏ NOT NULL là nới ra.
ALTER TABLE inventory.stock_reservations ALTER COLUMN order_item_id DROP NOT NULL;
ALTER TABLE inventory.stock_reservations
    DROP CONSTRAINT stock_reservations_order_item_fkey,
    ADD CONSTRAINT stock_reservations_order_item_fkey FOREIGN KEY (tenant_id, order_item_id)
        REFERENCES ord.order_items (tenant_id, id) ON DELETE SET NULL (order_item_id) NOT VALID;
ALTER TABLE inventory.stock_reservations VALIDATE CONSTRAINT stock_reservations_order_item_fkey;

-- (7) Tổng tiền đơn = tổng dòng (B4), kiểm lúc commit để đường ghi chèn, sửa đơn và dòng theo thứ tự khóa ADR-46 mà
-- không phải giữ tổng đúng sau từng câu. Chạy bằng quyền của người ghi (RLS áp như câu ghi).
-- +goose StatementBegin
CREATE FUNCTION ord.check_order_totals()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $fn$
DECLARE
    v_tenant uuid;
    v_order  uuid;
    o        record;
    s        record;
BEGIN
    IF TG_TABLE_NAME = 'orders' THEN
        IF TG_OP = 'DELETE' THEN
            v_tenant := OLD.tenant_id; v_order := OLD.id;
        ELSE
            v_tenant := NEW.tenant_id; v_order := NEW.id;
        END IF;
    ELSIF TG_OP = 'DELETE' THEN
        v_tenant := OLD.tenant_id; v_order := OLD.order_id;
    ELSE
        v_tenant := NEW.tenant_id; v_order := NEW.order_id;
    END IF;

    SELECT code, subtotal, line_discount_total, order_discount, coupon_discount INTO o
    FROM ord.orders
    WHERE tenant_id = v_tenant AND id = v_order;
    IF NOT FOUND THEN
        RETURN NULL;
    END IF;

    SELECT COALESCE(sum(quantity::bigint * unit_price), 0) AS sub,
           COALESCE(sum(line_discount), 0) AS ld,
           COALESCE(sum(allocated_order_discount), 0) AS alloc
    INTO s
    FROM ord.order_items
    WHERE tenant_id = v_tenant AND order_id = v_order;

    IF s.sub <> o.subtotal THEN
        RAISE EXCEPTION 'đơn %: subtotal % khác tổng quantity × unit_price %', o.code, o.subtotal, s.sub
            USING ERRCODE = 'check_violation', CONSTRAINT = 'check_order_totals', COLUMN = 'subtotal';
    END IF;
    IF s.ld <> o.line_discount_total THEN
        RAISE EXCEPTION 'đơn %: line_discount_total % khác tổng line_discount %', o.code, o.line_discount_total, s.ld
            USING ERRCODE = 'check_violation', CONSTRAINT = 'check_order_totals', COLUMN = 'line_discount_total';
    END IF;
    IF s.alloc <> o.order_discount + o.coupon_discount THEN
        RAISE EXCEPTION 'đơn %: tổng allocated_order_discount % khác order_discount + coupon_discount %',
            o.code, s.alloc, o.order_discount + o.coupon_discount
            USING ERRCODE = 'check_violation', CONSTRAINT = 'check_order_totals', COLUMN = 'allocated_order_discount';
    END IF;
    RETURN NULL;
END
$fn$;
-- +goose StatementEnd

CREATE CONSTRAINT TRIGGER orders_check_totals
    AFTER INSERT OR UPDATE OR DELETE ON ord.orders
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION ord.check_order_totals();

CREATE CONSTRAINT TRIGGER order_items_check_totals
    AFTER INSERT OR UPDATE OR DELETE ON ord.order_items
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION ord.check_order_totals();

-- (8) Bảng mới. Ghi chú đơn: chỉ ghi thêm (trigger theo câu lệnh như order_status_history, chặn mọi role).
CREATE TABLE ord.order_notes (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id  uuid NOT NULL REFERENCES platform.tenants (id),
    order_id   uuid NOT NULL,
    body       text NOT NULL CHECK (length(body) BETWEEN 1 AND 2000),
    author_id  uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, order_id) REFERENCES ord.orders (tenant_id, id)
);
SELECT platform.enable_tenant_rls('ord.order_notes');
CREATE INDEX order_notes_order ON ord.order_notes (tenant_id, order_id, created_at);

-- +goose StatementBegin
CREATE FUNCTION ord.order_notes_append_only()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $fn$
BEGIN
    RAISE EXCEPTION 'order_notes is append-only (% bị chặn)', TG_OP
        USING ERRCODE = 'insufficient_privilege';
END
$fn$;
-- +goose StatementEnd

CREATE TRIGGER order_notes_append_only
    BEFORE UPDATE OR DELETE OR TRUNCATE ON ord.order_notes
    FOR EACH STATEMENT EXECUTE FUNCTION ord.order_notes_append_only();

-- Cài đặt đơn của shop, một dòng mỗi tenant (order.EnsureDefaults).
CREATE TABLE ord.order_settings (
    tenant_id                     uuid PRIMARY KEY REFERENCES platform.tenants (id),
    draft_ttl_minutes             int NOT NULL DEFAULT 1440 CHECK (draft_ttl_minutes BETWEEN 10 AND 10080),
    duplicate_window_minutes      int NOT NULL DEFAULT 1440 CHECK (duplicate_window_minutes BETWEEN 0 AND 10080),
    discount_approval_threshold_bp int NOT NULL DEFAULT 1000
        CHECK (discount_approval_threshold_bp BETWEEN 0 AND 10000),
    return_warning_days           int NOT NULL DEFAULT 7 CHECK (return_warning_days BETWEEN 0 AND 365),
    version                       bigint NOT NULL DEFAULT 1,
    updated_at                    timestamptz NOT NULL DEFAULT now(),
    updated_by                    uuid
);
SELECT platform.enable_tenant_rls('ord.order_settings');

-- Cặp đơn nghi trùng (order_id < other_order_id: mỗi cặp một dòng).
CREATE TABLE ord.order_duplicate_links (
    tenant_id      uuid NOT NULL REFERENCES platform.tenants (id),
    order_id       uuid NOT NULL,
    other_order_id uuid NOT NULL,
    matched_by     text NOT NULL CHECK (matched_by IN ('customer', 'phone')),
    created_at     timestamptz NOT NULL DEFAULT now(),
    resolved_at    timestamptz,
    resolved_by    uuid,
    resolution     text CHECK (resolution IN ('not_duplicate', 'cancelled')),
    PRIMARY KEY (tenant_id, order_id, other_order_id),
    CHECK (order_id < other_order_id),
    CHECK ((resolved_at IS NULL) = (resolution IS NULL)),
    FOREIGN KEY (tenant_id, order_id) REFERENCES ord.orders (tenant_id, id),
    FOREIGN KEY (tenant_id, other_order_id) REFERENCES ord.orders (tenant_id, id)
);
SELECT platform.enable_tenant_rls('ord.order_duplicate_links');
CREATE INDEX order_duplicate_links_other ON ord.order_duplicate_links (tenant_id, other_order_id);

-- Phiếu trả (M2 là chủ duy nhất, Q-X2, B7; M3 chỉ ALTER thêm). shipment_id chưa có khóa ngoại (bảng vận đơn của M3).
CREATE SEQUENCE ord.return_code_seq AS bigint START 1;

CREATE TABLE ord.returns (
    id                   uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id            uuid NOT NULL REFERENCES platform.tenants (id),
    code                 text NOT NULL CHECK (code ~ '^TH[0-9]{7,}$'),
    order_id             uuid NOT NULL,
    shipment_id          uuid,
    kind                 text NOT NULL
        CHECK (kind IN ('carrier_return', 'customer_return', 'exchange', 'partial_delivery')),
    status               text NOT NULL DEFAULT 'requested'
        CHECK (status IN ('requested', 'received', 'restocked', 'closed', 'cancelled')),
    reason               text NOT NULL DEFAULT '',
    refund_amount        bigint NOT NULL DEFAULT 0 CHECK (refund_amount BETWEEN 0 AND 9007199254740991),
    replacement_order_id uuid,
    created_by           uuid,
    received_by          uuid,
    received_at          timestamptz,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    version              bigint NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, id),
    CONSTRAINT returns_code_key UNIQUE (tenant_id, code),
    FOREIGN KEY (tenant_id, order_id) REFERENCES ord.orders (tenant_id, id),
    FOREIGN KEY (tenant_id, replacement_order_id) REFERENCES ord.orders (tenant_id, id)
);
SELECT platform.enable_tenant_rls('ord.returns');
CREATE UNIQUE INDEX returns_carrier_shipment_key ON ord.returns (tenant_id, order_id, shipment_id)
    WHERE kind = 'carrier_return' AND status <> 'cancelled';
CREATE INDEX returns_order ON ord.returns (tenant_id, order_id);

CREATE TABLE ord.return_items (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id     uuid NOT NULL REFERENCES platform.tenants (id),
    return_id     uuid NOT NULL,
    order_item_id uuid NOT NULL,
    quantity      int NOT NULL CHECK (quantity > 0),
    condition     text NOT NULL CHECK (condition IN ('sellable', 'damaged', 'missing')),
    refund_amount bigint NOT NULL DEFAULT 0 CHECK (refund_amount BETWEEN 0 AND 9007199254740991),
    UNIQUE (tenant_id, id),
    CONSTRAINT return_items_item_key UNIQUE (tenant_id, return_id, order_item_id),
    FOREIGN KEY (tenant_id, return_id) REFERENCES ord.returns (tenant_id, id),
    FOREIGN KEY (tenant_id, order_item_id) REFERENCES ord.order_items (tenant_id, id)
);
SELECT platform.enable_tenant_rls('ord.return_items');
CREATE INDEX return_items_order_item ON ord.return_items (tenant_id, order_item_id);

-- (9) Quyền của crm_app: đọc, chèn bảng mới; sửa đúng các cột đường ghi M2 đổi (trạng thái, tiền, địa chỉ, kho, chăm
-- sóc, duyệt, hủy, nghi trùng). Không DELETE (Task sửa đơn tự thêm khi cần). Ghi chú đơn không sửa, không xóa.
GRANT SELECT, INSERT ON ord.order_notes, ord.traffic_sources, ord.order_settings, ord.order_duplicate_links,
    ord.returns, ord.return_items TO crm_app;
GRANT USAGE ON SEQUENCE ord.return_code_seq TO crm_app;
GRANT UPDATE (pending_reason, packing_status, payment_status, traffic_source_id, warehouse_id, ship_address, ship_phone,
    ship_phone_reversed, address_complete, subtotal, line_discount_total, order_discount, coupon_discount,
    discount_total, shipping_fee_charged, grand_total, prepaid_total, cod_amount, draft_expires_at, carrier_note,
    care_by, confirmed_at, confirmed_by, discount_approved_by, discount_approved_at, cancel_note, cancelled_at,
    duplicate_suspect, replaced_return_id) ON ord.orders TO crm_app;
GRANT UPDATE (quantity, unit_price, line_discount, allocated_order_discount, net_amount, updated_at)
    ON ord.order_items TO crm_app;
GRANT UPDATE (name, channel_account_id, active, position, updated_at, version) ON ord.traffic_sources TO crm_app;
GRANT UPDATE (draft_ttl_minutes, duplicate_window_minutes, discount_approval_threshold_bp, return_warning_days,
    version, updated_at, updated_by) ON ord.order_settings TO crm_app;
GRANT UPDATE (resolved_at, resolved_by, resolution) ON ord.order_duplicate_links TO crm_app;
GRANT UPDATE (status, reason, refund_amount, replacement_order_id, received_by, received_at, updated_at, version)
    ON ord.returns TO crm_app;
GRANT UPDATE (quantity, condition, refund_amount) ON ord.return_items TO crm_app;
REVOKE ALL ON ord.order_notes FROM PUBLIC;
REVOKE UPDATE, DELETE, TRUNCATE ON ord.order_notes FROM crm_app;
