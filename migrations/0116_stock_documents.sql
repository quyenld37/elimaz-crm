-- Chứng từ kho và danh bạ nhà cung cấp (kế hoạch M1, Task M1-29; spec 01 mục 3 "Phiếu nhập, phiếu xuất: có nháp và
-- duyệt", "Danh bạ NCC trên phiếu nhập"; 05 mục 2 "Chứng từ kho"; Q33, Q-X6). Pha mở rộng: chỉ CREATE, GRANT.
-- Thứ tự khóa: docs/adr/0046-thu-tu-khoa.md nhóm 11 (warehouses → suppliers → stock_documents → stock_document_lines →
-- … → stock_balances). Ghi sổ phiếu (inventory/documents.go) khóa dòng phiếu rồi đổi số dư theo (variant_id,
-- warehouse_id) tăng dần qua câu ApplyDelta có sẵn; không có view công bố mới (docs/views.md không đổi).

-- +goose Up
-- Nhà cung cấp tối thiểu (Q33): tên, SĐT (dữ liệu cá nhân, Go: logging.Phone), ghi chú, trạng thái. Công nợ NCC: P3.
CREATE TABLE inventory.suppliers (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id   uuid NOT NULL REFERENCES platform.tenants (id),
    name        text NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    name_search text NOT NULL,
    phone       text NOT NULL DEFAULT '' CHECK (phone = '' OR phone ~ '^\+?[0-9]{8,15}$'),
    note        text NOT NULL DEFAULT '' CHECK (length(note) <= 1000),
    status      text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    version     bigint NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, id),
    CONSTRAINT suppliers_name_key UNIQUE (tenant_id, name)
);
SELECT platform.enable_tenant_rls('inventory.suppliers');

-- Mã phiếu lấy từ sequence (không khóa dòng), chung mọi tenant; Go ghép tiền tố theo loại (NK, XK, HL, XL, DC).
CREATE SEQUENCE inventory.stock_document_code_seq;

-- Phiếu kho: nháp → ghi sổ (posted) hoặc hủy (cancelled). Nhà cung cấp chỉ cho phiếu nhập (C5).
CREATE TABLE inventory.stock_documents (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id    uuid NOT NULL REFERENCES platform.tenants (id),
    warehouse_id uuid NOT NULL,
    code         text NOT NULL CHECK (code ~ '^[A-Z]{2}-[0-9]{6,}$'),
    kind         text NOT NULL CHECK (kind IN ('import', 'export', 'damage', 'damage_export', 'adjust')),
    status       text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'posted', 'cancelled')),
    supplier_id  uuid NULL,
    note         text NOT NULL DEFAULT '' CHECK (length(note) <= 1000),
    created_by   uuid NULL,
    posted_by    uuid NULL,
    posted_at    timestamptz NULL,
    cancelled_by uuid NULL,
    cancelled_at timestamptz NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    version      bigint NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, id),
    CONSTRAINT stock_documents_code_key UNIQUE (tenant_id, code),
    CONSTRAINT stock_documents_supplier_kind CHECK (supplier_id IS NULL OR kind = 'import'),
    CHECK ((status = 'posted') = (posted_at IS NOT NULL)),
    CHECK ((status = 'cancelled') = (cancelled_at IS NOT NULL)),
    FOREIGN KEY (tenant_id, warehouse_id) REFERENCES inventory.warehouses (tenant_id, id),
    FOREIGN KEY (tenant_id, supplier_id) REFERENCES inventory.suppliers (tenant_id, id)
);
SELECT platform.enable_tenant_rls('inventory.stock_documents');
CREATE INDEX stock_documents_list ON inventory.stock_documents (tenant_id, warehouse_id, id);
CREATE INDEX stock_documents_supplier ON inventory.stock_documents (tenant_id, supplier_id) WHERE supplier_id IS NOT NULL;

-- Dòng phiếu: một dòng mỗi biến thể. Số lượng ≠ 0; > 0 cho mọi loại trừ adjust (kiểm trong Go: CHECK không thấy cột
-- của bảng cha). seller_id suy ra từ biến thể (khóa ngoại ghép như stock_balances).
CREATE TABLE inventory.stock_document_lines (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id   uuid NOT NULL REFERENCES platform.tenants (id),
    document_id uuid NOT NULL,
    variant_id  uuid NOT NULL,
    seller_id   uuid NOT NULL,
    quantity    int NOT NULL CHECK (quantity <> 0 AND quantity BETWEEN -1000000 AND 1000000),
    unit_cost   bigint NULL CHECK (unit_cost BETWEEN 0 AND 9007199254740991),
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    CONSTRAINT stock_document_lines_variant_key UNIQUE (tenant_id, seller_id, document_id, variant_id),
    FOREIGN KEY (tenant_id, document_id) REFERENCES inventory.stock_documents (tenant_id, id),
    FOREIGN KEY (tenant_id, seller_id, variant_id) REFERENCES catalog.variants (tenant_id, seller_id, id)
);
SELECT platform.enable_tenant_rls('inventory.stock_document_lines');

-- Lớp thứ hai sau kiểm trong Go: dòng của phiếu không còn nháp không chèn, sửa, xóa được (mọi role). Hàm chạy bằng
-- quyền người gọi (RLS áp dụng); FOR SHARE giữ phiếu không đổi trạng thái tới hết transaction.
-- +goose StatementBegin
CREATE FUNCTION inventory.stock_document_lines_draft_only()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $fn$
DECLARE
    doc_status text;
BEGIN
    SELECT d.status INTO doc_status
    FROM inventory.stock_documents AS d
    WHERE d.tenant_id = (CASE WHEN TG_OP = 'DELETE' THEN OLD.tenant_id ELSE NEW.tenant_id END)
      AND d.id = (CASE WHEN TG_OP = 'DELETE' THEN OLD.document_id ELSE NEW.document_id END)
    FOR SHARE;
    IF doc_status IS DISTINCT FROM 'draft' THEN
        RAISE EXCEPTION 'stock_document_lines: phiếu không còn nháp (% bị chặn)', TG_OP
            USING ERRCODE = 'check_violation';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END
$fn$;
-- +goose StatementEnd

CREATE TRIGGER stock_document_lines_draft_only
    BEFORE INSERT OR UPDATE OR DELETE ON inventory.stock_document_lines
    FOR EACH ROW EXECUTE FUNCTION inventory.stock_document_lines_draft_only();

-- crm_app: NCC đọc, chèn, sửa cột nghiệp vụ; phiếu đọc, chèn, sửa trạng thái; dòng phiếu đọc, chèn, xóa (thay cả bộ
-- dòng khi còn nháp). Không DELETE phiếu, NCC.
GRANT SELECT, INSERT ON inventory.suppliers, inventory.stock_documents, inventory.stock_document_lines TO crm_app;
GRANT UPDATE (name, name_search, phone, note, status, updated_at, version) ON inventory.suppliers TO crm_app;
GRANT UPDATE (status, posted_by, posted_at, cancelled_by, cancelled_at, updated_at, version)
    ON inventory.stock_documents TO crm_app;
GRANT DELETE ON inventory.stock_document_lines TO crm_app;
GRANT USAGE ON SEQUENCE inventory.stock_document_code_seq TO crm_app;
