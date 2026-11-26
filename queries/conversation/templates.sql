-- Mẫu trả lời có biến (Task 161; 01 mục 6). Chạy trong tenancy.WithTenant. Đọc module khác chỉ qua view công bố.

-- name: InsertReplyTemplate :one
INSERT INTO conversation.reply_templates (tenant_id, channel_account_id, shortcut, title, body, attachment_file_id,
                                          quick_replies, created_by)
VALUES (current_setting('app.tenant_id')::uuid, sqlc.narg(channel_account_id), @shortcut, @title, @body,
        sqlc.narg(attachment_file_id), @quick_replies, @created_by)
RETURNING id, channel_account_id, shortcut, title, body, attachment_file_id, quick_replies, version, created_at;

-- name: GetReplyTemplate :one
-- Mẫu còn sống id (không khóa).
SELECT id, channel_account_id, shortcut, title, body, attachment_file_id, quick_replies, version, created_at
FROM conversation.reply_templates
WHERE id = @id
  AND deleted_at IS NULL;

-- name: LockReplyTemplate :one
-- Khóa mẫu còn sống id trước khi sửa, xóa (so version ở Go).
SELECT id, channel_account_id, shortcut, title, body, attachment_file_id, quick_replies, version, created_at
FROM conversation.reply_templates
WHERE id = @id
  AND deleted_at IS NULL
FOR UPDATE;

-- name: UpdateReplyTemplate :one
UPDATE conversation.reply_templates
SET channel_account_id = sqlc.narg(channel_account_id), shortcut = @shortcut, title = @title, body = @body,
    attachment_file_id = sqlc.narg(attachment_file_id), quick_replies = @quick_replies, version = version + 1,
    updated_at = now()
WHERE id = @id
RETURNING id, channel_account_id, shortcut, title, body, attachment_file_id, quick_replies, version, created_at;

-- name: SoftDeleteReplyTemplate :execrows
UPDATE conversation.reply_templates
SET deleted_at = now(), version = version + 1, updated_at = now()
WHERE id = @id
  AND deleted_at IS NULL;

-- name: ListReplyTemplates :many
-- Mẫu còn sống dùng được ở các Page @pages (mẫu chung luôn có; @all_pages: mọi mẫu của Page), phím tắt hay tiêu đề
-- bắt đầu bằng @prefix (rỗng = mọi mẫu, không phân biệt hoa thường), theo phím tắt.
SELECT id, channel_account_id, shortcut, title, body, attachment_file_id, quick_replies, version, created_at
FROM conversation.reply_templates
WHERE deleted_at IS NULL
  AND (channel_account_id IS NULL OR @all_pages::boolean OR channel_account_id = ANY(@pages::uuid[]))
  AND (starts_with(lower(shortcut), lower(@prefix::text)) OR starts_with(lower(title), lower(@prefix::text)))
ORDER BY lower(shortcut), id
LIMIT @lim::int;

-- name: TemplateCustomerName :one
-- Tên và nguồn tên của gốc nhóm khách của hồ sơ id (cột name_source chỉ đúng ở gốc, docs/views.md).
SELECT g.full_name, g.name_source
FROM customer.customers_v AS c
JOIN customer.customers_v AS g ON g.id = c.root_id
WHERE c.id = @id;

-- name: TemplateOrder :one
-- Mã và khách của đơn id.
SELECT code, customer_id
FROM ord.orders_v
WHERE id = @id;

-- name: TemplateLatestOrder :one
-- Đơn chưa hủy mới nhất của nhóm khách (mọi hồ sơ @members).
SELECT code
FROM ord.orders_v
WHERE customer_id = ANY(@members::uuid[])
  AND status <> 'cancelled'
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- name: TemplateStaffName :one
SELECT display_name
FROM identity.users_v
WHERE id = @id;
