---
paths:
  - "migrations/**"
  - "queries/**"
  - "sqlc.yaml"
---

# Luật migration và SQL

1. Mở rộng rồi mới thu hẹp: một lần phát hành chỉ **thêm** (bảng, cột cho phép rỗng, index). Bỏ hay đổi tên ở lần phát hành sau, khi code đã ngừng dùng cái cũ.
2. Lệnh `DROP TABLE`, `DROP COLUMN`, `RENAME`, `ALTER COLUMN … TYPE`, `SET NOT NULL` phải có dòng `-- contract: <bản phát hành đã ngừng dùng>` ngay phía trên.
3. Không sửa migration đã commit; thêm tệp mới. `CREATE INDEX CONCURRENTLY` đặt trong tệp riêng có `-- +goose NO TRANSACTION`.
4. Mọi câu ghi tên schema (`platform.jobs`, `identity.users`). Không tạo bảng nào trong `public`. Module chỉ nhắc schema của chính nó, và view đã công bố của module khác.
5. Bảng nghiệp vụ: `tenant_id uuid NOT NULL`, `UNIQUE (tenant_id, id)`, mọi unique khác bắt đầu bằng `tenant_id`, khóa ngoại ghép `(tenant_id, …)`, bật và ép RLS bằng `platform.enable_tenant_rls`.
6. Câu trong `queries/` phải qua `sqlc vet` (chuẩn bị trên PostgreSQL 18 thật). Câu không viết được cho sqlc thì đặt trong `*_dynsql.go` kèm test trên database thật.
