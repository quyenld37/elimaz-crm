-- Khách (kế hoạch M1, Task M1-30; 05 mục 1). Câu chèn của M1 thay bằng InsertCustomerProfile (write.sql, Task M2-09):
-- customer.Create của M1 là lớp bọc của Writer.CreateCustomer (Q30). Chạy trong tenancy.WithTenant.

-- name: GetRootOf :one
SELECT root_id
FROM customer.customers
WHERE id = @id;

-- name: ListGroupMembers :many
SELECT id
FROM customer.customers
WHERE root_id = @root_id
ORDER BY id;
