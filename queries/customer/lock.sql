-- Khóa hồ sơ đại diện (kế hoạch M4, Task 152). Chạy trong tenancy.WithTenant. ADR-46 nhóm 5: dòng customer.customers
-- là đầu nhóm; đường M4 khóa khách trước hội thoại (C11).

-- name: LockCustomerRow :one
-- Khóa dòng id; bên gọi kiểm root_id = id (vẫn là gốc) và đọc lại gốc của hồ sơ con dưới khóa. FOR NO KEY UPDATE (Task
-- 172): vẫn chặn khóa này song song, gộp khách, lockOwner (FOR UPDATE) và UPDATE cột thường, nhưng không chặn FOR KEY
-- SHARE của câu kiểm khóa ngoại — đường tin vào khóa hội thoại rồi sửa lại chính dòng đó thì PostgreSQL kiểm lại
-- customer_id (FOR KEY SHARE dòng khách); FOR UPDATE ở đây làm hai đường chờ nhau (deadlock C11).
SELECT tenant_id, root_id, do_not_contact
FROM customer.customers
WHERE id = @id
FOR NO KEY UPDATE;
