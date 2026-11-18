# Task 398 — Sửa main không biên dịch được sau khi gộp Task 28, 34 (M1-x1)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (mục Task 398 M1-x1). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Chỉ sửa một tệp test (`catalog_test`) dùng `testkit.RecordingBus.Signals()` đã có; mã sinh sqlc sinh lại không đổi ký hiệu nào đang được dùng (chỉ thêm kiểu model danh mục/thuộc tính/nhãn vào store của inventory, đổi `ActorID pgtype.UUID → *uuid.UUID` ở model `InventoryStockLedger(V)` trong store của catalog — không chỗ nào trong catalog dùng hai kiểu đó, `go vet` xác nhận).
- Gói phụ thuộc ngược (go list): `internal/modules/{catalog,inventory}/internal/store` là gói nội bộ, chỉ module chủ nhập.
- Operation OpenAPI bị chạm, oasdiff: không.
- Chỗ dùng trên màn hình: không.
- Câu sqlc, bảng, pha migration: không đổi câu SQL hay migration; chỉ chạy `make gen` vì `models.go` trên main chưa khớp schema sau khi gộp Task 28 (bảng inventory) và Task 34 (bảng danh mục, thuộc tính, nhãn) song song.
- Test sẽ chạy: `go vet ./...`; `go test -race ./internal/modules/catalog/... ./internal/modules/inventory/...`; `make ci` một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `testkit.RecordingBus` (`internal/testkit/realtime.go`), có `Signals()` trả bản sao.
- Dùng lại / viết mới vì: dùng lại `Signals()`; không thêm `Reset()` — test lấy số báo hiệu trước khi gọi rồi xét phần mới (`Signals()[before:]`), cùng ý nghĩa với `signals = nil` cũ, không mở rộng API testkit.

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

- 5.1–5.12: không áp dụng: không đổi kiến trúc, gói, ranh giới module; chỉ sửa test và sinh lại mã sqlc từ nguồn có sẵn.
- 6.1–6.13: không áp dụng: không đổi mã chạy, transaction, khóa, hàng đợi hay quyền. `RecordingBus` vẫn khóa mutex trong `Signals()`.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Trước khi sửa: main đỏ go vet | — | `go vet ./...` | `classification_test.go:358:8: e.bus.signals undefined (... but does have method Signals)` |
| Sau khi sửa: vet sạch | — | `go vet ./...` | không lỗi |
| Test catalog, inventory qua | `TestClassification…` và cả gói | `go test -race ./internal/modules/catalog/... ./internal/modules/inventory/...` | `ok …/catalog 26.251s`, `ok …/inventory 64.315s` |
| Mã sinh khớp nguồn | gen-check | `make ci` (đã stage) | qua build, vet, lint, gen-check, sqlc-vet, test (mọi gói `ok`), vuln, oasdiff, catalog-check |
| jscpd | — | `make ci` | đỏ: 3 clone có sẵn (`platform/db/migrate_test.go`, `platform/queue/{errors,worker}_test.go` — Task 399; `console/e2e/{shell,staff}.spec.ts`), không dính tệp của task này |
| Bước sau jscpd | — | `make test-guard gitleaks console-check` | EXIT=0, `no leaks found` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Kế hoạch chỉ nói lỗi `e.bus.signals`. `make ci` (gen-check) còn đỏ vì `models.go` sinh của catalog và inventory chưa khớp schema sau khi gộp: chạy `make gen`, commit phần sinh lại. Cùng nguyên nhân (gộp 28+34), cần để main xanh.

## Quyết định thay chủ shop

- Sinh lại mã sqlc trong task này thay vì tách task mới: cùng nguyên nhân, không đổi nguồn, đảo ngược dễ (revert commit). Phương án khác: ghi vào "Việc phát hiện thêm" và để main đỏ gen-check — chặn mọi phiên khác.
- Thay `e.bus.signals = nil` bằng `before := len(Signals())` thay vì thêm `RecordingBus.Reset()`: không đổi API dùng chung.

## Việc phát hiện thêm

- jscpd: clone `console/e2e/shell.spec.ts` ↔ `staff.spec.ts` có sẵn trên main, ngoài phạm vi Task 399 (db/queue); cần ai đó xử lý hoặc đưa vào baseline.
- Lần chạy `make ci` đầu, `tests/arch` đỏ vì "parallel golangci-lint is running" (phiên khác chạy lint cùng lúc); chạy lại riêng thì qua.

## Reviewer

Kết luận: ĐẠT (agent reviewer, diff đã stage sau khi gộp main).

- Phát hiện phải sửa: không có.
- Test vẫn kiểm đúng ý cũ: lấy số báo hiệu ngay trước `setClassification`, chỉ xét phần thêm sau → tương đương `signals = nil` cũ.
- Mã sinh khớp override trong `sqlc.yaml` (catalog: uuid rỗng → `*uuid.UUID`; inventory: `pgtype.UUID`).
- Ghi chú: commit message nhắc cả phần sinh lại `models.go`.
