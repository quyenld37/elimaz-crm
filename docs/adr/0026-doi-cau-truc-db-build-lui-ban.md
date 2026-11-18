# ADR-26 — Đổi cấu trúc database, build, lùi bản, giới hạn thời gian

Trạng thái: đã duyệt (spec 07-adr, 12/11/2026); bổ sung bởi ADR-46 (giới hạn đặt theo từng transaction)
Ngày: 12/11/2026
Kiểm bằng: `TestMigrateRejectsContractWithoutMarker` (internal/platform/db/migrate_test.go); `TestOpenSetsConnectionLimits` (internal/platform/db/db_test.go); `TestProdCompose_ImagesPinnedNotBuilt` (tests/deploy/compose_test.go); `make image-check`, `make vuln`, `make gitleaks`

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-26. Tệp này không chép lại quyết định.

Mở rộng rồi mới thu hẹp; build một lần ở CI; lùi là deploy lại image trước, không lùi database; giới hạn
`statement_timeout` 30 s, `idle_in_transaction_session_timeout` 60 s, `lock_timeout` 5 s ở mọi kết nối; chuỗi cung
ứng: quét secret, quét lỗ hổng, container không chạy root, secret không nằm trong image.

## Kiểm ở đâu

- Điểm 1: `TestMigrateRejectsContractWithoutMarker`, `TestMigrateOutOfOrderRejectsContractWithoutMarker` (internal/platform/db/migrate_test.go).
- Điểm 2: `TestProdCompose_ImagesPinnedNotBuilt` (tests/deploy/compose_test.go); CI dựng image một lần, nhãn mã commit: `.github/workflows/ci.yml`, luật kiểm bằng `TestCI_ImageBuiltOnceTaggedByCommit` (tests/deploy/ci_test.go); chạy trên GitHub cần OK chủ shop.
- Điểm 3: thủ tục lùi trong `docs/RUNBOOK.md`; thử trên staging: chưa có, Task 23 bước 7 (cần OK chủ shop).
- Điểm 4: `TestOpenSetsConnectionLimits` (internal/platform/db/db_test.go); cảnh báo `TestPGRuleThresholds` (`long-tx`, `jobs-dead-tuples`, `db-size`;
  internal/platform/observability/rules_eval_test.go).
- Điểm 5: `make gitleaks`, `make vuln` (govulncheck), `make image-check` (`scripts/ops/image-check.sh`: uid khác 0, không `.env*`).
- Bài thử deploy dưới tải trên staging: chưa có, Task 23 bước 7.
