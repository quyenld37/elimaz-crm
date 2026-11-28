# ADR-48 — Đọc Nhanh một chiều, chỉ đọc, chỉ trước và trong đêm mở

Trạng thái: đã duyệt (chủ shop chốt 28/11/2026); sửa ADR-11
Ngày: 28/11/2026
Kiểm bằng: `TestCall_RejectsNonReadPathsBeforeSending`, `TestReadPaths_NoWriteVerb`, `TestCall_RateLimitWaitsUntilUnlockedAt`, `TestCollect_HardLimitFifty` (internal/adapters/nhanh); `TestNhanhSnapshot_WritesPrivateFileAndPrintsCountsOnly` (cmd/crm); `TestNoNhanh_RealCode`, `TestNhanhAdapter_OnlySnapshotCommandImports` (tests/arch)

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-48. Tệp này không chép lại quyết định.

CRM được đọc API Nhanh POS v3 một chiều, chỉ trước và trong đêm mở, để chuyển khách, tối đa 50 mẫu sản phẩm mới và
tồn của chúng (số dự kiến). Không ghi lên Nhanh, không chuyển đơn, mã giảm giá, lịch sử mua. Ngoại lệ duy nhất của luật
"CRM không gọi Nhanh": gói `internal/adapters/nhanh` và lệnh `crm nhanh-snapshot`; `tests/arch/no_nhanh_test.go` cấm mọi
nơi khác.
