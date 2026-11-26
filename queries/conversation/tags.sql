-- Nhãn hội thoại dùng chung cả shop (Task 160; Q7). Xóa mềm; tên không trùng (không phân biệt hoa thường) giữa các
-- nhãn còn sống (index tags_name_live).

-- name: ListTags :many
SELECT id, name, color, sort, version
FROM conversation.tags
WHERE deleted_at IS NULL
ORDER BY sort, lower(name), id;

-- name: InsertTag :one
INSERT INTO conversation.tags (tenant_id, name, color, sort)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @name, @color, @sort)
RETURNING id, name, color, sort, version;

-- name: LockTag :one
-- Khóa nhãn còn sống trước khi sửa, xóa.
SELECT id, name, color, sort, version
FROM conversation.tags
WHERE id = @id AND deleted_at IS NULL
FOR UPDATE;

-- name: ShareLockTag :one
-- Giữ nhãn còn sống trong lúc gắn (xóa song song chờ tới khi gắn xong; nhóm 6: tags trước conversations).
SELECT id
FROM conversation.tags
WHERE id = @id AND deleted_at IS NULL
FOR SHARE;

-- name: UpdateTag :one
UPDATE conversation.tags
SET name = @name, color = @color, sort = @sort, version = version + 1
WHERE id = @id
RETURNING id, name, color, sort, version;

-- name: SoftDeleteTag :exec
UPDATE conversation.tags
SET deleted_at = now(), version = version + 1
WHERE id = @id AND deleted_at IS NULL;

-- name: AddTagLink :execrows
-- Gắn nhãn; gắn lại không lỗi (0 dòng).
INSERT INTO conversation.conversation_tag_links (tenant_id, conversation_id, tag_id, created_by)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @conversation_id, @tag_id, sqlc.narg(created_by)::uuid)
ON CONFLICT DO NOTHING;

-- name: RemoveTagLink :execrows
DELETE FROM conversation.conversation_tag_links
WHERE conversation_id = @conversation_id AND tag_id = @tag_id;

-- name: GetConversationPage :one
-- Page của một hội thoại (kiểm quyền theo Page trước khi gắn, gỡ nhãn).
SELECT channel_account_id
FROM conversation.conversations
WHERE id = @id;

-- name: ListTaggedConversationIDs :many
-- Hội thoại mang nhãn còn sống tag_id (index conversation_tag_links_tag); nhãn đã xóa → không dòng nào.
SELECT l.conversation_id
FROM conversation.conversation_tag_links AS l
JOIN conversation.tags AS t ON t.id = l.tag_id AND t.deleted_at IS NULL
WHERE l.tag_id = @tag_id
ORDER BY l.conversation_id;
