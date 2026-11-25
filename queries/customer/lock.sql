-- Khóa hồ sơ đại diện (kế hoạch M4, Task 152). Chạy trong tenancy.WithTenant. ADR-46 nhóm 5: dòng customer.customers
-- là đầu nhóm; đường M4 khóa khách trước hội thoại (C11).

-- name: LockCustomerRow :one
-- Khóa dòng id; bên gọi kiểm root_id = id (vẫn là gốc) và đọc lại gốc của hồ sơ con dưới khóa.
SELECT tenant_id, root_id, do_not_contact
FROM customer.customers
WHERE id = @id
FOR UPDATE;
