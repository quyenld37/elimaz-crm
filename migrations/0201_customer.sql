-- Hồ sơ khách M2 (kế hoạch M2, Task M2-03; spec 05 mục 1). Mở rộng bảng customer.customers của M1 (Task M1-30) bằng
-- ALTER theo luật mở rộng trước: cột mới có DEFAULT hoặc cho phép rỗng → điền dữ liệu cho dòng có sẵn → SET NOT NULL;
-- ràng buộc mới NOT VALID rồi VALIDATE. Không tạo lại bảng của M1, customer.Create của M1 không phải đổi (code lấy
-- DEFAULT từ sequence, root_id do trigger đặt khi để trống).
-- Bảng mới: danh tính, địa chỉ, gộp, nhãn, liên kết nhãn, quy tắc nhãn, ghi chú (chỉ ghi thêm), lần nhập.
-- Thứ tự khóa: docs/adr/0046-thu-tu-khoa.md nhóm 5 (customers → customer_identities → customer_addresses →
-- customer_tag_links → customer_tag_rules → customer_merges → customer_imports; customer_notes, customer_tags chỉ chèn).
-- View công bố: đúng cột của docs/views.md (customers_v chỉ nối cột vào cuối, R3).

-- +goose Up

-- GIN ghép (tenant_id, …) cần operator class btree cho uuid. Tạo trong schema platform (crm_owner làm chủ; không có
-- quyền CREATE trên public ở máy chủ). PostgreSQL tìm operator class mặc định không theo search_path.
CREATE EXTENSION IF NOT EXISTS btree_gin SCHEMA platform;

-- Mã khách KH000001…: một sequence cho mọi tenant (như ord.order_code_seq), không khóa dòng.
CREATE SEQUENCE customer.customer_code_seq AS bigint START 1;

-- (1) Cột mới. code cho phép rỗng lúc đầu, có DEFAULT để dòng mới (kể cả customer.Create của M1) tự có mã.
ALTER TABLE customer.customers
    ADD COLUMN IF NOT EXISTS code text DEFAULT ('KH' || lpad(nextval('customer.customer_code_seq')::text, 6, '0')),
    ADD COLUMN IF NOT EXISTS name_search text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS lifecycle text NOT NULL DEFAULT 'lead',
    ADD COLUMN IF NOT EXISTS birthday date,
    ADD COLUMN IF NOT EXISTS level_id uuid,
    ADD COLUMN IF NOT EXISTS order_count int NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS delivered_count int NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS returned_count int NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS total_spent bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS last_order_at timestamptz,
    ADD COLUMN IF NOT EXISTS stats_updated_at timestamptz,
    ADD COLUMN IF NOT EXISTS created_by uuid;

-- (2) Điền dòng có sẵn. RLS bị ép cả với chủ bảng (FORCE): tạm bỏ ép trong transaction của migration để chủ bảng thấy
-- mọi tenant, rồi ép lại ngay; CheckSchemaRules kiểm lại sau migrate. name_search: cùng phép của textnorm.Fold —
-- dạng dựng sẵn (NFC), bỏ dấu rời còn lại, bảng dấu hoa và thường → chữ gốc, chữ thường, gộp khoảng trắng.
ALTER TABLE customer.customers NO FORCE ROW LEVEL SECURITY;

-- ADD COLUMN với DEFAULT gọi nextval đã điền code cho từng dòng có sẵn (PostgreSQL ghi lại bảng); câu dưới giữ cho
-- bước (2) đúng kể cả khi cột code đã có từ trước mà còn rỗng (ADD COLUMN IF NOT EXISTS không làm gì).
UPDATE customer.customers
SET code = 'KH' || lpad(nextval('customer.customer_code_seq')::text, 6, '0')
WHERE code IS NULL;

UPDATE customer.customers
SET name_search = btrim(regexp_replace(
        lower(translate(
            regexp_replace(normalize(full_name, NFC), '[̀-ͯ]', '', 'g'),
            'àáảãạăằắẳẵặâầấẩẫậèéẻẽẹêềếểễệìíỉĩịòóỏõọôồốổỗộơờớởỡợùúủũụưừứửữựỳýỷỹỵđÀÁẢÃẠĂẰẮẲẴẶÂẦẤẨẪẬÈÉẺẼẸÊỀẾỂỄỆÌÍỈĨỊÒÓỎÕỌÔỒỐỔỖỘƠỜỚỞỠỢÙÚỦŨỤƯỪỨỬỮỰỲÝỶỸỴĐ',
            'aaaaaaaaaaaaaaaaaeeeeeeeeeeeiiiiiooooooooooooooooouuuuuuuuuuuyyyyydaaaaaaaaaaaaaaaaaeeeeeeeeeeeiiiiiooooooooooooooooouuuuuuuuuuuyyyyyd')),
        '[\s\u0085   -     　]+', ' ', 'g'))
WHERE name_search = '';

ALTER TABLE customer.customers FORCE ROW LEVEL SECURITY;

-- (3) Thu chặt sau khi đã điền: mọi dòng đã có code, cột có DEFAULT nên không mã nào ghi NULL.
-- contract: không có bản nào ghi code NULL (cột mới thêm ngay trên, có DEFAULT từ sequence)
ALTER TABLE customer.customers ALTER COLUMN code SET NOT NULL;

ALTER TABLE customer.customers
    ADD CONSTRAINT customers_code_key UNIQUE (tenant_id, code);

ALTER TABLE customer.customers
    ADD CONSTRAINT customers_lifecycle_check CHECK (lifecycle IN ('lead', 'customer')) NOT VALID,
    ADD CONSTRAINT customers_code_check CHECK (code ~ '^KH[0-9]{6,}$') NOT VALID,
    ADD CONSTRAINT customers_stats_check
        CHECK (order_count >= 0 AND delivered_count >= 0 AND returned_count >= 0 AND total_spent >= 0) NOT VALID;
ALTER TABLE customer.customers VALIDATE CONSTRAINT customers_lifecycle_check;
ALTER TABLE customer.customers VALIDATE CONSTRAINT customers_code_check;
ALTER TABLE customer.customers VALIDATE CONSTRAINT customers_stats_check;

-- Khóa ngoại root_id của M1 (tự trỏ, ghép tenant_id): kiểm lúc commit, để gộp và hoàn tác đổi root_id nhiều dòng trong
-- một transaction mà không phụ thuộc thứ tự câu lệnh.
ALTER TABLE customer.customers
    ALTER CONSTRAINT customers_tenant_id_root_id_fkey DEFERRABLE INITIALLY DEFERRED;

-- Đường ghi M2 không phải tự đặt root_id: để trống thì là chính nó (hồ sơ chưa gộp).
-- +goose StatementBegin
CREATE FUNCTION customer.customers_default_root()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $fn$
BEGIN
    NEW.root_id := COALESCE(NEW.root_id, NEW.id);
    RETURN NEW;
END
$fn$;
-- +goose StatementEnd

CREATE TRIGGER customers_default_root
    BEFORE INSERT ON customer.customers
    FOR EACH ROW EXECUTE FUNCTION customer.customers_default_root();

CREATE INDEX customers_root ON customer.customers (tenant_id, root_id);
CREATE INDEX customers_name_search_trgm ON customer.customers USING gin (tenant_id, name_search gin_trgm_ops);

-- Danh tính: một SĐT (một PSID trong một Page) chỉ thuộc một khách (05 mục 1, B5). scope NOT NULL DEFAULT '' vì
-- unique coi hai NULL là khác nhau. value_reversed: chữ số đảo ngược để tìm theo đuôi số bằng câu khoảng.
CREATE TABLE customer.customer_identities (
    id             uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id      uuid NOT NULL REFERENCES platform.tenants (id),
    customer_id    uuid NOT NULL,
    kind           text NOT NULL CHECK (kind IN ('phone', 'fb_psid', 'zalo_uid', 'email', 'web_account')),
    scope          text NOT NULL DEFAULT '',
    value          text NOT NULL,
    value_reversed text NOT NULL DEFAULT '',
    verified_at    timestamptz,
    source         text NOT NULL CHECK (source IN ('order', 'chat', 'staff', 'web', 'import')),
    created_by     uuid,
    created_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    CONSTRAINT customer_identities_value_key UNIQUE (tenant_id, kind, scope, value),
    FOREIGN KEY (tenant_id, customer_id) REFERENCES customer.customers (tenant_id, id)
);
SELECT platform.enable_tenant_rls('customer.customer_identities');
CREATE INDEX customer_identities_reversed
    ON customer.customer_identities (tenant_id, kind, value_reversed text_pattern_ops);
CREATE INDEX customer_identities_customer ON customer.customer_identities (tenant_id, customer_id);

-- Địa chỉ của khách (API dùng tên của địa chỉ giao M1: name ↔ recipient_name, line ↔ street). Xóa mềm.
CREATE TABLE customer.customer_addresses (
    id             uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id      uuid NOT NULL REFERENCES platform.tenants (id),
    customer_id    uuid NOT NULL,
    recipient_name text NOT NULL,
    phone          text NOT NULL,
    province_code  text NOT NULL,
    ward_code      text NOT NULL,
    street         text NOT NULL,
    is_default     boolean NOT NULL DEFAULT false,
    deleted_at     timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    version        bigint NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, customer_id) REFERENCES customer.customers (tenant_id, id)
);
SELECT platform.enable_tenant_rls('customer.customer_addresses');
CREATE UNIQUE INDEX customer_addresses_one_default
    ON customer.customer_addresses (tenant_id, customer_id) WHERE is_default AND deleted_at IS NULL;
CREATE INDEX customer_addresses_customer ON customer.customer_addresses (tenant_id, customer_id);

-- Lần gộp: moved_ids là các hồ sơ đổi root_id trong lần này (hoàn tác trả đúng các hồ sơ đó về).
CREATE TABLE customer.customer_merges (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id   uuid NOT NULL REFERENCES platform.tenants (id),
    survivor_id uuid NOT NULL,
    merged_id   uuid NOT NULL,
    moved_ids   uuid[] NOT NULL,
    matched_by  text NOT NULL CHECK (matched_by IN ('phone', 'staff')),
    actor_id    uuid NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    undone_at   timestamptz,
    undone_by   uuid,
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, survivor_id) REFERENCES customer.customers (tenant_id, id),
    FOREIGN KEY (tenant_id, merged_id) REFERENCES customer.customers (tenant_id, id)
);
SELECT platform.enable_tenant_rls('customer.customer_merges');
CREATE INDEX customer_merges_survivor ON customer.customer_merges (tenant_id, survivor_id, created_at DESC);
CREATE INDEX customer_merges_moved ON customer.customer_merges USING gin (tenant_id, moved_ids);

-- Nhãn khách. color là khóa màu; console ánh xạ khóa → màu thật (quyết định QĐ1 của Task 60).
CREATE TABLE customer.customer_tags (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id  uuid NOT NULL REFERENCES platform.tenants (id),
    name       text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 50),
    color      text NOT NULL CHECK (color IN
        ('gray', 'red', 'orange', 'yellow', 'green', 'teal', 'blue', 'purple', 'pink', 'brown')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version    bigint NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, id),
    CONSTRAINT customer_tags_name_key UNIQUE (tenant_id, name)
);
SELECT platform.enable_tenant_rls('customer.customer_tags');

CREATE TABLE customer.customer_tag_links (
    tenant_id   uuid NOT NULL REFERENCES platform.tenants (id),
    customer_id uuid NOT NULL,
    tag_id      uuid NOT NULL,
    source      text NOT NULL CHECK (source IN ('staff', 'rule')),
    created_by  uuid,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, customer_id, tag_id),
    FOREIGN KEY (tenant_id, customer_id) REFERENCES customer.customers (tenant_id, id),
    FOREIGN KEY (tenant_id, tag_id) REFERENCES customer.customer_tags (tenant_id, id)
);
SELECT platform.enable_tenant_rls('customer.customer_tag_links');
CREATE INDEX customer_tag_links_tag ON customer.customer_tag_links (tenant_id, tag_id);

CREATE TABLE customer.customer_tag_rules (
    tenant_id  uuid NOT NULL REFERENCES platform.tenants (id),
    tag_id     uuid NOT NULL,
    conditions jsonb NOT NULL,
    enabled    boolean NOT NULL DEFAULT true,
    version    bigint NOT NULL DEFAULT 1,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, tag_id),
    FOREIGN KEY (tenant_id, tag_id) REFERENCES customer.customer_tags (tenant_id, id)
);
SELECT platform.enable_tenant_rls('customer.customer_tag_rules');

-- Ghi chú khách: chỉ ghi thêm (trigger theo câu lệnh như ord.order_status_history, chặn mọi role).
CREATE TABLE customer.customer_notes (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id   uuid NOT NULL REFERENCES platform.tenants (id),
    customer_id uuid NOT NULL,
    body        text NOT NULL CHECK (length(body) BETWEEN 1 AND 4000),
    author_id   uuid NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, customer_id) REFERENCES customer.customers (tenant_id, id)
);
SELECT platform.enable_tenant_rls('customer.customer_notes');
CREATE INDEX customer_notes_customer ON customer.customer_notes (tenant_id, customer_id, created_at);

-- +goose StatementBegin
CREATE FUNCTION customer.customer_notes_append_only()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $fn$
BEGIN
    RAISE EXCEPTION 'customer_notes is append-only (% bị chặn)', TG_OP
        USING ERRCODE = 'insufficient_privilege';
END
$fn$;
-- +goose StatementEnd

CREATE TRIGGER customer_notes_append_only
    BEFORE UPDATE OR DELETE OR TRUNCATE ON customer.customer_notes
    FOR EACH STATEMENT EXECUTE FUNCTION customer.customer_notes_append_only();

-- Lần nhập khách từ tệp (Task M2-13). errors: danh sách lỗi theo dòng, không chứa giá trị cá nhân.
CREATE TABLE customer.customer_imports (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id    uuid NOT NULL REFERENCES platform.tenants (id),
    file_id      uuid NOT NULL,
    status       text NOT NULL CHECK (status IN ('pending', 'running', 'done', 'failed')),
    total_rows   int CHECK (total_rows >= 0),
    ok_rows      int CHECK (ok_rows >= 0),
    skipped_rows int CHECK (skipped_rows >= 0),
    error_rows   int CHECK (error_rows >= 0),
    errors       jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(errors) = 'array'),
    created_by   uuid NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    finished_at  timestamptz,
    UNIQUE (tenant_id, id)
);
SELECT platform.enable_tenant_rls('customer.customer_imports');
CREATE INDEX customer_imports_created ON customer.customer_imports (tenant_id, created_at DESC);

-- View công bố (Q-X2; cột đúng docs/views.md). customers_v: giữ 4 cột của M1 ở vị trí 1–4, nối cột mới vào cuối;
-- chỉ số chỉ đúng ở dòng có id = root_id.
CREATE OR REPLACE VIEW customer.customers_v WITH (security_invoker = true) AS
SELECT id, root_id, full_name, created_at, tenant_id, code, lifecycle, birthday, order_count, delivered_count,
       returned_count, total_spent, last_order_at, updated_at, version
FROM customer.customers;

CREATE VIEW customer.customer_identities_v WITH (security_invoker = true) AS
SELECT tenant_id, id, customer_id, kind, scope, value, verified_at, source, created_at
FROM customer.customer_identities;

CREATE VIEW customer.customer_addresses_v WITH (security_invoker = true) AS
SELECT tenant_id, id, customer_id, recipient_name, phone, province_code, ward_code, street, is_default
FROM customer.customer_addresses
WHERE deleted_at IS NULL;

CREATE VIEW customer.customer_tags_v WITH (security_invoker = true) AS
SELECT tenant_id, id, name, color
FROM customer.customer_tags;

CREATE VIEW customer.customer_tag_links_v WITH (security_invoker = true) AS
SELECT tenant_id, customer_id, tag_id, source
FROM customer.customer_tag_links;

-- Quyền của crm_app (quyết định QĐ4 của Task 60): đọc, chèn mọi bảng; sửa đúng các cột đường ghi M2 đổi; xóa chỉ
-- danh tính và liên kết nhãn. Ghi chú không sửa, không xóa.
GRANT USAGE ON SEQUENCE customer.customer_code_seq TO crm_app;
GRANT SELECT, INSERT ON customer.customer_identities, customer.customer_addresses, customer.customer_merges,
    customer.customer_tags, customer.customer_tag_links, customer.customer_tag_rules, customer.customer_notes,
    customer.customer_imports TO crm_app;
GRANT UPDATE (root_id, full_name, name_search, lifecycle, birthday, level_id, order_count, delivered_count,
    returned_count, total_spent, last_order_at, stats_updated_at, updated_at, version) ON customer.customers TO crm_app;
GRANT UPDATE (verified_at) ON customer.customer_identities TO crm_app;
GRANT DELETE ON customer.customer_identities, customer.customer_tag_links TO crm_app;
GRANT UPDATE (recipient_name, phone, province_code, ward_code, street, is_default, deleted_at, updated_at, version)
    ON customer.customer_addresses TO crm_app;
GRANT UPDATE (undone_at, undone_by) ON customer.customer_merges TO crm_app;
GRANT UPDATE (name, color, updated_at, version) ON customer.customer_tags TO crm_app;
GRANT UPDATE (conditions, enabled, version, updated_at) ON customer.customer_tag_rules TO crm_app;
GRANT UPDATE (status, total_rows, ok_rows, skipped_rows, error_rows, errors, finished_at)
    ON customer.customer_imports TO crm_app;
REVOKE ALL ON customer.customer_notes FROM PUBLIC;
REVOKE UPDATE, DELETE, TRUNCATE ON customer.customer_notes FROM crm_app;
GRANT SELECT ON customer.customers_v, customer.customer_identities_v, customer.customer_addresses_v,
    customer.customer_tags_v, customer.customer_tag_links_v TO crm_app;
