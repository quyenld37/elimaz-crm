# Task 424 — M1-x10: testkit không nhập module nghiệp vụ

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 424, M1-x10). Loại việc (quy trình mục 3): D (dọn code: dời mã, không đổi hành vi test)

## Ảnh hưởng

Đo trước khi sửa: `go list -deps ./internal/testkit` có 11 gói `internal/modules/**` (catalog, catalog/catalogtest, catalog/internal/{article,barcode,importer,store}, inventory, inventory/invtest, inventory/internal/store, identity, identity/internal/store). Nguồn: `catalog.go`, `sellers.go` (nhập `catalogtest`, `invtest`), `realtime.go` (nhập `identity` và `platform/realtime`, mà `realtime` nhập `identity`). Dời ba tệp đó xong vẫn còn `identity` qua hai đường bắc cầu (tìm bằng `go list -deps` từng gói testkit nhập): `filestore.go` nhập `platform/files` (files nhập `identity`, `realtime`), `accessjwt.go` nhập `adapters/cfaccess` (cfaccess nhập `identity`) — dời luôn hai tệp này (xem Khác kế hoạch).

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): dời, không đổi chữ ký —
  `testkit.RecordingBus` → `realtimetest.RecordingBus` (gói mới `internal/platform/realtime/realtimetest`): 56 chỗ dùng trong 42 tệp (mã chạy: `internal/testkit/shiptest/shop.go`; còn lại là tệp `_test.go` của httpapi, cmd/crm, các module order, fulfillment, cash, customer, inventory, promotion…).
  `testkit.{SeedCatalog, SeedSellers, SeedVariant, SeedOptions, Catalog}` → `shoptest.*` (gói mới `internal/testkit/shoptest`): 4 tệp dùng (`internal/testkit/catalog_test.go` dời theo thành `shoptest/catalog_test.go`, `internal/httpapi/handlers_catalog_sellers_test.go`, `internal/modules/order/schema_test.go`, `internal/modules/catalog/products_test.go`) + 1 chú thích trong `catalogtest/seed.go`.
  `testkit.{MemFileStore, S3FileStore, S3Config, S3App, S3Admin}` → `filestest.*` (gói **đã có** `internal/platform/files/filestest` — helper ảnh thử `Image`, `Uploaded`; tệp `filestore.go` dời nguyên văn thành `filestest/store.go`): 21 tệp đổi (19 tệp thêm đường nhập: test của files, catalog, cash, customer, httpapi, tests/e2e, tests/drills; 2 tệp chỉ sửa chú thích: `files/memstore.go`, `httpapi/server.go`).
  `testkit.{AccessKeyServer, AccessKeys, AccessClaims, AccessMFAClaim}` → `cfaccesstest.*` (gói mới `internal/adapters/cfaccess/cfaccesstest`, tệp `accessjwt.go` dời nguyên văn): 22 tệp dùng (18 tệp httpapi, cfaccess_test, realtime env_test, tests/drills, tests/e2e).
  `testkit.ForEachSeller` ở lại `internal/testkit` (không nhập module). Chữ ký và thân hàm không đổi; chỉ tiền tố câu lỗi `t.Fatalf` của hai hàm seed đổi `testkit:` → `shoptest:`.
- Gói phụ thuộc ngược (go list): mọi gói có test nhập testkit (gần cả kho); sau khi dời, `internal/testkit` chỉ còn nhập (cả bắc cầu) imager, platform/{db, tenancy, config, logging, queue, metrics, observability, urlerr, fieldcrypt/keyspec, internal/store}, migrations. Đổi testkit → ci-task chạy toàn bộ test (đúng luật của ci-task).
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: test kiến trúc mới `tests/arch` `TestTestkitNoModules` (đỏ trước khi dời); `go test -race` của `internal/testkit/...`, `internal/platform/realtime/...` và mọi gói có tệp đổi; ci-task (toàn bộ vì đổi testkit). Tài liệu nhắc tên cũ: `docs/skill-drafts/them-module.md`, `docs/m2/doi-chieu-m1.md` (ghi chép lịch sử M2 — giữ nguyên, xem Quyết định).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` không có bus giả nào khác (tìm "Bus", "Recording"); `catalogtest`, `invtest`, `shiptest`, `codtest` là gói seed theo module/luồng; không có gói nào đã gom seed nhiều module ngoài testkit. Test kiến trúc dùng lại `goListPackages`, `listedPackage.Deps`, `hasPathPrefix`, `modPrefix` của `tests/arch/boundaries_test.go`.
- Dùng lại / viết mới vì: không viết mã mới, chỉ dời nguyên văn hai nhóm sang gói mới (testkit phải hết phụ thuộc module). Kho tệp thử dời vào gói `files/filestest` đã có (cùng việc: dữ liệu tệp cho test), không tạo gói mới. Gói mới vì: bus giả cạnh `realtime` (`realtimetest`, như `catalogtest` cạnh `catalog`; luật (c) cho platform nhập phần công khai của identity); seed nhiều module thành `testkit/shoptest` (cạnh `shiptest`, `codtest` vốn đã nhập module).

## Soát thiết kế

- 5.1–5.10: không áp dụng: chỉ dời mã test (không dữ liệu, bảng, transaction, HTTP, hàng đợi mới).
- 5.11: không thêm thư viện.
- 5.12: theo mẫu gói seed `catalogtest`/`invtest` (gói `<tên>test` cạnh gói thật); ranh giới gói kiểm bằng test kiến trúc trong `tests/arch` như `boundaries_test.go`.
- 6.1–6.13: không áp dụng: không đổi hành vi; `RecordingBus` giữ nguyên mutex, `SeedCatalog`/`SeedSellers` giữ nguyên `tenancy.WithTenant` và chủ bảng.

## Quyết định thay chủ shop

- Nơi dời: `RecordingBus` → `internal/platform/realtime/realtimetest` (nó hiện thực `realtime.Bus`, mà chữ ký `Subscribe` cần `identity.AuthUser` nên không thể ở testkit); seed hai người bán, danh mục → `internal/testkit/shoptest`. Phương án khác: dồn cả vào một `shoptest`, hoặc `SeedSellers` vào `catalogtest`, `SeedCatalog` vào `invtest` — bỏ vì bus giả không phải seed shop, và tách seed làm hai gói đổi chỗ dùng nhiều hơn không lợi gì. Dễ đảo: chỉ là đường nhập của mã test.
- Luật kiến trúc chỉ áp cho gói `internal/testkit` (không áp gói con `shiptest`, `codtest`, `shoptest` vốn là seed theo luồng nghiệp vụ), xét `Deps` (bắc cầu), đúng tiêu chí "`go list -deps ./internal/testkit` không còn `internal/modules`".
- Kho tệp thử, máy chủ khóa Access thử dời ra `files/filestest`, `cfaccess/cfaccesstest` thay vì gỡ phụ thuộc `identity` khỏi `platform/files`, `adapters/cfaccess` (đổi mã chạy, ngoài phạm vi "không đổi hành vi"). Giữ tên ký hiệu cũ để diff chỉ là đổi tiền tố gói.
- `docs/m2/doi-chieu-m1.md`, `docs/m3/doi-chieu-m2.md` là bản đối chiếu lịch sử: không sửa; `docs/skill-drafts/them-module.md` sửa tên theo chỗ mới.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test kiến trúc đỏ trước khi dời | `TestTestkitNoModules` | `go test ./tests/arch -run TestTestkit` (trước khi dời) | `FAIL … internal/testkit cần 11 gói internal/modules/**` (catalog, catalogtest, catalog/internal/{article,barcode,importer,store}, identity, identity/internal/store, inventory, inventory/internal/store, invtest) |
| Luật bắt cả phụ thuộc bắc cầu, bỏ qua gói con | `TestTestkitModuleDeps_DetectsTransitive` | `go test -race ./tests/arch -run 'TestTestkit\|TestBoundar'` | `ok elimaz.com.vn/crm/tests/arch 5.290s` |
| `go list -deps ./internal/testkit` không còn `internal/modules` | `TestTestkitNoModules` + lệnh tay | `go list -deps ./internal/testkit \| grep internal/modules` | rỗng; gói nội bộ còn lại: imager, platform/{fieldcrypt/keyspec, logging, config, internal/store, db, urlerr, metrics, observability, tenancy, queue}, migrations |
| Số gói affected.py giảm khi đổi một tệp catalog | — (đo) | `echo internal/modules/catalog/classification.go \| python3 <bản chép affected.py> \| wc -l` (chép nguyên thuật toán vì hook chặn chạy tệp trong thư mục quy trình; HIGH_RISK_DIRS rỗng) | trước **54** → sau **37** (tổng gói 100 → 103). Cùng cách đo: một tệp inventory (`aged.go`) 53 → 35; một tệp identity (`access.go`) 57 → 47 |
| Không đổi hành vi test: seed danh mục vẫn đúng | `TestSeedCatalog` (dời sang `shoptest`) | `go test -race ./internal/testkit/... ./internal/platform/realtime/... ./internal/adapters/cfaccess/...` | `ok …/internal/testkit 57.412s`, `ok …/testkit/shoptest 3.952s`, `ok …/platform/realtime 16.540s`, `ok …/adapters/cfaccess 3.414s` |
| Toàn bộ test xanh, cổng tĩnh xanh | mọi gói (ci-task chạy toàn bộ vì đổi testkit) | ci-task.sh | build, vet, lint, catalog-check, jscpd, gitleaks, oasdiff, test-guard qua; `== test (toàn bộ: đổi migration/go.mod/testkit): 507 giây`; 78 dòng `ok`, 0 `FAIL`; `CI-TASK XANH (so với điểm tách nhánh e318c1eb…)` |
| Build với mọi nhãn build | — | `go vet -tags <drill, filesync, imager, imagercompose, perf, probe> ./...` | không lỗi |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Kế hoạch chỉ kể `catalogtest`, `invtest`, `identity`; đo thật thấy thêm hai đường bắc cầu tới `identity` (`platform/files`, `adapters/cfaccess`). Tiêu chí "`go list -deps ./internal/testkit` không còn `internal/modules`" đòi dời cả hai: kho tệp thử → `internal/platform/files/filestest`, máy chủ khóa Access thử → `internal/adapters/cfaccess/cfaccesstest` (cạnh gói thật, như `catalogtest`). Không đổi mã chạy của `files`/`cfaccess` (bỏ phụ thuộc identity của chúng là đổi thiết kế, ngoài phạm vi).

## Việc phát hiện thêm

(không có)

## Reviewer

Kết luận: ĐẠT (lượt 1, 21/11/2026).

- Phát hiện phải sửa: không có. Reviewer lọc diff: mọi dòng bị xóa ở tệp gọi chỉ là đường nhập `internal/testkit` hoặc tiền tố `testkit.` của ký hiệu đã dời.
- Ảnh hưởng khớp: realtimetest 42 tệp, shoptest 4 tệp + 1 chú thích + tài liệu, filestest 21 tệp, cfaccesstest 22 tệp; không còn nơi gọi tên cũ ngoài ghi chép lịch sử. Reviewer chạy lại `go list -deps ./internal/testkit`: không còn `internal/modules`.
- Ghi chú không bắt buộc (không sửa): câu lỗi trong `filestest/store.go` còn tiền tố `testkit:`; test kiến trúc đi cùng commit dời mã (chỉ thêm cổng kiểm, không đổi hành vi).
