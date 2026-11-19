# Task 411 — Lên đơn: kiểm phạm vi kho của kho giữ hàng (M1-r6)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (task bù review độc lập M1, phát hiện D-02 của `docs/review-doc-lap-m1.md`, làm theo mục "Quyết định cho Task 406, 409, 410, 411"). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): **thêm** `identity.GrantedAt(u AuthUser, p Permission, target Scope) bool` (mới, 0 chỗ tham chiếu trước task; sau task: 1 chỗ trong `internal/httpapi/handlers_order.go` + test). Không đổi chữ ký nào đã có: `identity.Can`, `order.Writer.Create`, `order.Writer.CreateManual`, `order.CreateInput` giữ nguyên. Hàm không xuất mới trong httpapi: `orderWarehouseAt`, hằng `permOrderCreate` (handlers_order.go); `isOrderRefusal` thêm `errForbiddenTarget` (403 không lưu theo khóa; chỗ dùng thứ hai `isReturnRefusal` đã coi lỗi này là từ chối qua `inventoryError`, không đổi hành vi).
- Gói phụ thuộc ngược (go list): `identity` được nhập bởi `cmd/crm`, `internal/adapters/cfaccess`, `internal/httpapi`, `internal/modules/catalog`, `internal/modules/customer`, `internal/platform/files`, `internal/platform/realtime`, `internal/testkit`, `tools/loadkit` — chỉ thêm hàm, không gói nào phải sửa. `httpapi` được nhập bởi `cmd/crm`, `tests/**`.
- Operation OpenAPI bị chạm, oasdiff: `createOrder` (POST /api/v1/orders) — chỉ đổi `description` của operation và của trường `warehouseId` (ghi 403 khi kho ngoài phạm vi). Mã 403 đã nằm trong `default: Error`; không đổi schema → oasdiff không có thay đổi phá vỡ. `make gen` sinh lại: chỉ chú thích đổi trong `internal/httpapi/gen/oas_{handlers,schemas,server,unimplemented}_gen.go`, `console/src/api/{sdk,types}.gen.ts`. `createReturn` (đơn thay thế) không nhận kho từ yêu cầu (lấy kho của đơn gốc, `order/returns.go:267`) nên không chạm.
- Chỗ dùng trên màn hình: màn Lên đơn `console/src/screens/len-don/OrderForm.tsx` (khối báo lỗi sau khi lưu): thêm câu riêng cho `403 forbidden` của createOrder, chỉ ở chế độ tạo đơn (`edit === undefined`; sau khi gộp main, form dùng chung cho sửa đơn của Task 85, 403 của sửa đơn vẫn câu chung); không thêm ô chọn kho. Test `OrderForm.test.tsx`.
- Câu sqlc, bảng, pha migration: không có (dùng `inventory.DefaultWarehouse` đã có để biết kho chính khi thân yêu cầu bỏ trống `warehouseId`).
- Test sẽ chạy: `go test -race ./internal/modules/identity/...` (vùng rủi ro cao: cả bộ), `go test -race ./internal/httpapi/...` (gói đổi; có test tạo đơn M1 và M2), `cd console && npm test -- len-don`, `npx tsc --noEmit`, `npx eslint .`; cuối cùng cổng kiểm của task (ci-task.sh) một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (chỉ có `realtime.WarehouseScope` cho báo hiệu); grep trong `internal/modules/identity` (`Can`, `Effective`, `KindOf`), `internal/httpapi/handlers_inventory.go` (`warehouseScope`, `canAt`, `errForbiddenTarget`).
- Dùng lại / viết mới vì: dùng lại `warehouseScope` (đích kho), `errForbiddenTarget` (403 `forbidden`, đã ánh xạ ở `errors.go`), `inventory.DefaultWarehouse` (kho chính). Không dùng được `identity.Can`/`canAt`: `order.create` là quyền loại `none` nên `Can` bỏ qua đích (Task 315), và luật `docs/quyen.md` cấm đổi loại phạm vi của quyền đã có. Viết mới `identity.GrantedAt` ở gói identity (gói sở hữu luật grant) thay vì duyệt `u.Grants` trong httpapi: một chỗ duy nhất biết grant nào có hiệu lực (`Effective`) và `all` là gì.

## Soát thiết kế

- 5.1 Chủ dữ liệu: luật quyền thuộc identity (`GrantedAt`); kiểm ở lớp HTTP như các đường kho (handler kiểm đích). Không dữ liệu mới.
- 5.2, 5.3: không ghi, không đọc bảng module khác; kho chính đọc qua public API `inventory.DefaultWarehouse`.
- 5.4: kiểm quyền và tạo đơn cùng một transaction `WriteTx` (đọc kho chính trong tx của tenant); thiếu quyền thì không ghi gì.
- 5.5, 5.6: không áp dụng: không sự kiện, không gọi ra ngoài mới.
- 5.7: `inventory.DefaultWarehouse` chạy trong tx tenant của `WriteTx`.
- 5.8: identity không nhập HTTP; handler mỏng.
- 5.9: đường ghi đã có `Idempotency-Key` và `x-permission: order.create`; 403 là lỗi, không lưu như kết quả theo khóa (test: gửi lại cùng khóa sau khi được cấp quyền thì tạo được đơn).
- 5.10, 5.11: không áp dụng (không Valkey, không thư viện mới).
- 5.12: theo mẫu đường kho (`canAt` → `errForbiddenTarget`) và module mẫu identity.
- 6.1–6.3: không áp dụng: không thêm khóa hay câu ghi; kiểm quyền chỉ đọc grant đã nạp vào `AuthUser`.
- 6.4: fn của `WriteTx` có thể chạy lại: kho đích tính lại mỗi lần chạy, không cộng dồn.
- 6.5–6.12: không áp dụng (không mức cách ly mới, việc nền, gọi ngoài, tiền, dữ liệu cá nhân mới).
- 6.13 Quyền: mặc định từ chối — chỉ grant `order.create` phạm vi `all` hoặc `warehouse:<kho giữ hàng>` mới cho giữ hàng ở kho đó; gán theo Page hay theo kho khác → 403, kể cả khi bỏ trống `warehouseId` (kho chính cũng được kiểm). Kiểm ở API; màn chỉ hiện lỗi.

## Quyết định thay chủ shop

- **Phạm vi kho của nhân viên khi lên đơn = các grant `order.create` phạm vi `all` hoặc `warehouse:<kho>`.** Vì sao: đúng câu chữ quyết định 19/11 ("quyền tạo đơn theo phạm vi kho, giống phiếu kho" — phiếu kho kiểm đúng quyền của việc ở đúng kho). Phương án khác: lấy kho từ grant của bất kỳ quyền nào (rộng hơn, không theo mẫu phiếu kho); hay kiểm `inventory.read` ở kho (đề xuất của review, nhưng trộn hai quyền khác việc).
- **Không đổi loại phạm vi của `order.create` (vẫn `none`)**, thêm `identity.GrantedAt` để kiểm riêng ở bước giữ hàng. Vì sao: `docs/quyen.md` cấm đổi loại phạm vi của quyền đã có; đổi sẽ ảnh hưởng mọi chỗ dùng `Can(order.create)` và luật gán vai trò. Các việc khác của đơn (đọc, sửa, xác nhận) không đổi.
- **Gán `order.create` theo một Page (không có kho) không giữ hàng được ở kho nào → 403**, kể cả kho chính. Vì sao: mặc định từ chối (6.13); chủ shop/Quản lý gán toàn shop không bị ảnh hưởng. Ghi rõ trong `docs/quyen.md`. Đảo ngược: cho grant theo Page dùng kho chính, hoặc bỏ kiểm ở handler (một hàm).
- **Bỏ trống `warehouseId` cũng kiểm (kho chính)**: không thì nhân viên gán theo kho phụ vẫn khóa hàng kho chính.
- **Màn Lên đơn**: chỉ thêm câu báo cho lỗi 403 mà API trả, không thêm ô chọn kho, không ẩn nút trước (việc đó cần mockup, Task 412/M2-27).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Đỏ trước khi sửa: nhân viên theo kho chính giữ được hàng ở kho phụ, theo Page tạo được đơn | `TestOrderAPI_WarehouseScope`, `TestOrderAPI_WarehouseScopeNotStored` | `go test -race -count=1 ./internal/httpapi/ -run 'TestOrderAPI_WarehouseScope'` (trước khi sửa handler) | `theo kho chính, kho phụ: mã 201, mong 403 … bị chặn mà vẫn ghi: đơn 4→5, giữ chính 3→3, giữ phụ 1→2`; `theo kho chính, kho không có: mã 500`; FAIL |
| Chỉ grant `order.create` all hoặc đúng kho cho giữ hàng ở kho đó; kho phụ, kho không có, theo Page, bỏ trống kho (kho chính) ngoài phạm vi → 403 `forbidden`, không đơn, không giữ hàng; đơn nháp M2 cũng chặn; toàn shop không đổi | `TestOrderAPI_WarehouseScope` (11 ca bảng) | `go test -race -count=1 ./internal/httpapi/ -run 'TestOrderAPI_WarehouseScope'` | `ok elimaz.com.vn/crm/internal/httpapi 2.292s` |
| 403 không lưu theo `Idempotency-Key`: được cấp kho rồi gửi lại cùng khóa → 201 | `TestOrderAPI_WarehouseScopeNotStored` | như trên | `ok` (lần đầu viết trả 409 `request_in_progress` → sửa thành từ chối không lưu qua `isOrderRefusal`) |
| `GrantedAt`: all hoặc đúng đích, bỏ qua loại phạm vi; grant quản trị ngoài all, quyền lạ, quyền khác → sai; không rộng hơn `Can` | `TestGrantedAtTable` (11 ca), `TestGrantedAtProperty` (rapid) | `go test -race -count=1 ./internal/modules/identity/` | `ok elimaz.com.vn/crm/internal/modules/identity 45.467s` |
| Không gãy đường tạo đơn M1/M2, trả/đổi, mọi handler | cả gói httpapi | `go test -race -count=1 ./internal/httpapi/...` | `ok elimaz.com.vn/crm/internal/httpapi 55.004s` |
| Cổng kiểm của task trên bản đã gộp main (build, vet, lint, gen-check, jscpd, oasdiff, test-guard, console, test gói đổi + phụ thuộc + vùng cao) | toàn bộ theo cổng | ci-task.sh (chạy nền, lần 2 sau khi sửa nolint thừa và 2 đoạn trùng jscpd ở bộ sinh rapid — tách `permissionKeys`, `scopeGen`, `grantsGen` trong `permissions_test.go`) | `CI-TASK XANH (so với điểm tách nhánh 331c072…)`; lần 1 đỏ: lint (nolint thừa), jscpd (2 đoạn mới), gen-check (mã sinh chưa stage), e2e `TestBootEachRoleThenGracefulStop/api` cổng 50101 bị phiên khác chiếm |
| Màn Lên đơn hiện câu riêng khi API trả 403, đơn chưa tạo, không nút Gửi lại | `OrderForm.test.tsx` › "403 forbidden (kho giữ hàng ngoài phạm vi, Task 411)…" | `npx vitest run src/screens/len-don/` (sau khi gộp main) | `Tests 278 passed (278)`; `npx tsc --noEmit`, `npx eslint src/screens/len-don`, `prettier --check` sạch |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

- Người có `order.create` toàn shop gửi `warehouseId` không tồn tại → 500 `internal` (có từ trước, thấy khi viết `TestOrderAPI_WarehouseScope`: ca nhân viên theo kho trước khi sửa trả 500). Nên là 422/404; không sửa ở task này (ngoài phạm vi D-02). Sau task, nhân viên có phạm vi hẹp nhận 403 ở ca này.
- Màn Lên đơn chưa ẩn/khóa nút lưu khi người dùng không có phạm vi kho chính; chỉ hiện lỗi sau khi bấm. Việc chọn kho trong phạm vi cần mockup (quyết định 19/11: M2-27/Task 412).

## Reviewer

Kết luận: ĐẠT (lượt 1, trên bản đã gộp main 331c072).

- Phát hiện phải sửa: không có. Reviewer kiểm thêm: fingerprint đơn dùng kho đã điền kho chính nên gửi lại cùng khóa vẫn khớp đơn cũ (`order/create_m2.go:190, 293-296, 335`); fn của `WriteTx` chạy lại an toàn; chỉ `createOrder` nhận kho từ người gọi (đơn thay thế lấy kho đơn gốc, sửa đơn giữ ở kho của đơn); `isReturnRefusal` không đổi hành vi.
- Ảnh hưởng khớp; tách hàm sinh rapid trong `permissions_test.go` có ở bảng bằng chứng (chỉ dời mã test).
- Ghi chú không bắt buộc (không sửa): nhân viên chỉ gán `order.create` theo Page sẽ không lên đơn được (đã ghi ở Quyết định và `docs/quyen.md`; chủ shop cần gán thêm theo kho/toàn shop); chú thích của `TestCanProperty` giờ nằm trên `permissionKeys`; câu báo 403 trên màn cũng hiện cho 403 hiếm khác của createOrder.
