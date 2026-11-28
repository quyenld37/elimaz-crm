-- Nhập số đếm kiểm kho bằng Excel (kế hoạch M7, Task 263 = M7-18). Pha mở rộng: chỉ CREATE TABLE, GRANT, một dòng danh
-- mục mục đích tệp (R5, G06; cùng mẫu với 0604, không đụng CHECK hay bảng platform.files). Go đăng ký cùng khóa bằng
-- inventory.RegisterCountImportPurpose() lúc khởi động.
-- - inventory.count_imports: một bản nhập số đếm vào một phiên kiểm. Handler đọc và phân tích tệp ngoài transaction
--   (ADR-46), lưu các dòng số đọc được vào pending_rows ([{row, code, qty}], mã là SKU/barcode, không dữ liệu khách) và
--   lỗi ô vào error_rows ([{row, code}], không nội dung ô); việc nền inventory.count_import tra mã, áp mọi dòng hợp lệ
--   trong một transaction, đặt done và xóa pending_rows. Cùng tệp (băm SHA-256), cùng phiên, cùng mode → một bản nhập.
-- Thứ tự khóa: docs/adr/0046-thu-tu-khoa.md nhóm 11 (… → stocktakes → stocktake_lines → count_imports → …).

-- +goose Up
CREATE TABLE inventory.count_imports (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id    uuid NOT NULL REFERENCES platform.tenants (id),
    stocktake_id uuid NOT NULL,
    file_id      uuid NOT NULL,
    file_sha256  text NOT NULL CHECK (file_sha256 ~ '^[0-9a-f]{64}$'),
    mode         text NOT NULL CHECK (mode IN ('replace', 'add')),
    status       text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'done', 'failed')),
    pending_rows jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(pending_rows) = 'array'),
    applied_rows int NOT NULL DEFAULT 0 CHECK (applied_rows >= 0),
    error_rows   jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(error_rows) = 'array'),
    created_by   uuid NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    finished_at  timestamptz,
    UNIQUE (tenant_id, id),
    CONSTRAINT count_imports_file_key UNIQUE (tenant_id, stocktake_id, file_sha256, mode),
    FOREIGN KEY (tenant_id, stocktake_id) REFERENCES inventory.stocktakes (tenant_id, id),
    FOREIGN KEY (tenant_id, file_id) REFERENCES platform.files (tenant_id, id)
);
SELECT platform.enable_tenant_rls('inventory.count_imports');

GRANT SELECT, INSERT ON inventory.count_imports TO crm_app;
GRANT UPDATE (status, pending_rows, applied_rows, error_rows, finished_at) ON inventory.count_imports TO crm_app;

INSERT INTO platform.file_purposes (key, bucket)
VALUES ('inventory_count_import', 'private')
ON CONFLICT DO NOTHING;
