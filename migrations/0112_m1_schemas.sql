-- Schema của các module M1 (kế hoạch M1 bản 2, Task M1-25; dải migration M1 0100–0199, Q-X7). Chạy bằng crm_owner.
-- Chỉ tạo schema và quyền USAGE như identity ở 0001; bảng, view và quyền bảng do task của từng module tạo, cấp từng
-- bảng như 0003_identity.sql (không ALTER DEFAULT PRIVILEGES). Module order dùng schema ord (05 quy ước chung).
-- Thứ tự khóa của mọi bảng M1: docs/adr/0046-thu-tu-khoa.md. View công bố: docs/views.md.

-- +goose Up
CREATE SCHEMA catalog;
CREATE SCHEMA inventory;
CREATE SCHEMA customer;
CREATE SCHEMA ord;
CREATE SCHEMA fulfillment;
CREATE SCHEMA cash;
GRANT USAGE ON SCHEMA catalog, inventory, customer, ord, fulfillment, cash TO crm_app, crm_search;
