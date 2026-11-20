-- Địa chỉ lấy hàng của kho có version (kế hoạch M3, Task M3-09): inventory.SetWarehousePickup ghi hai cột của 0301 và
-- tăng version (sửa khi version còn khớp, sai → 409). 0301 chỉ cấp quyền sửa hai cột địa chỉ nên crm_app chưa tăng được
-- version. Pha mở rộng: chỉ thêm quyền. Số 0309 vì 0307 (Task M3-22), 0308 (Task M3-48) đã giữ chỗ; goose cho chạy lệch
-- số. Thứ tự khóa không đổi (một câu UPDATE có điều kiện trên một dòng kho).

-- +goose Up
GRANT UPDATE (version, updated_at) ON inventory.warehouses TO crm_app;
