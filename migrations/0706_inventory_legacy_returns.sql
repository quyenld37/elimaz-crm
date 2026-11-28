-- Hàng hoàn của đơn cũ (trước CRM) về kho (kế hoạch M7, Task 264 = M7-19; MO08, Q11, G05). Pha mở rộng: chỉ CREATE,
-- GRANT. Mỗi kiện một dòng: phiếu nhập (import) mọi dòng, phiếu hỏng (damage) cho dòng hỏng; "lý do legacy_return"
-- nằm ở bảng nối này (chứng từ M1 không có cột lý do). reference_norm (bỏ mọi khoảng trắng, viết hoa) duy nhất theo
-- tenant: một kiện không nhập hai lần. Thứ tự khóa: docs/adr/0046-thu-tu-khoa.md nhóm 11 (stock_documents →
-- stock_document_lines → legacy_returns → … → stock_balances). Không view công bố mới.

-- +goose Up
CREATE TABLE inventory.legacy_returns (
    id                 uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id          uuid NOT NULL REFERENCES platform.tenants (id),
    warehouse_id       uuid NOT NULL,
    reference          text NOT NULL CHECK (char_length(reference) BETWEEN 3 AND 64),
    reference_norm     text NOT NULL CHECK (char_length(reference_norm) BETWEEN 3 AND 64),
    stock_document_id  uuid NOT NULL,
    damage_document_id uuid NULL,
    created_by         uuid NOT NULL,
    created_at         timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    CONSTRAINT legacy_returns_reference_key UNIQUE (tenant_id, reference_norm),
    FOREIGN KEY (tenant_id, warehouse_id) REFERENCES inventory.warehouses (tenant_id, id),
    FOREIGN KEY (tenant_id, stock_document_id) REFERENCES inventory.stock_documents (tenant_id, id),
    FOREIGN KEY (tenant_id, damage_document_id) REFERENCES inventory.stock_documents (tenant_id, id)
);

SELECT platform.enable_tenant_rls('inventory.legacy_returns');

-- crm_app: đọc, chèn; không sửa, không xóa (sổ chỉ ghi thêm).
GRANT SELECT, INSERT ON inventory.legacy_returns TO crm_app;
