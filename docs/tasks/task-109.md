# Task 109 — M3-18: Đối chiếu địa giới theo hãng

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 109 = M3-18). Loại việc (quy trình mục 3): A

Ca kiểm: nền cho VC02, VC03 với hãng thật (Q-19); task này không phủ trọn một mã ca của `11-kich-ban-kiem-thu.md`.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ THÊM, không đổi ký hiệu có sẵn. Mới trong `fulfillment`: `LocationsKindName`, `LocationsKind()`, `(*Service).SyncLocations`, `(*Service).LocationMap`, `(*Service).UnmappedLocations`, kiểu `UnmappedLocation`, `UnmappedFilter`. Gói mới `fulfillment/internal/locations` (chỉ fulfillment dùng): `Normalize`, `Unit`, `Remote`, `Pair`, `Match`, `Row`, `Map`, `NewMap`. `fulfillment.LocationMap` (interface, 7 chỗ nhắc, không đổi), `fulfillment.Location`, `CarrierAdapter.Locations` không đổi. `area_locations.go`: `locationsHandlers` trả lệnh mới (wire.go đã gom sẵn, không sửa).
- Gói phụ thuộc ngược (go list): `fulfillment` được nhập bởi cmd/crm, internal/adapters/carrier/{,best,carriertest,fake,manual,spx,vtp}, internal/httpapi, internal/modules/cash (+formats, statement), fulfillment/internal/carriers/fake, testkit/shiptest, tools/fakecarrier/server. Chỉ thêm ký hiệu nên không gãy; ci-task chạy test các gói này.
- Operation OpenAPI bị chạm, oasdiff: `syncCarrierLocations`, `listUnmappedLocations` đã có trong `openapi.yaml` từ Task 95 (khung trả 501); task này chỉ hiện thực handler, không sửa hợp đồng → oasdiff không đổi.
- Chỗ dùng trên màn hình: chưa màn nào gọi hai operation này (grep console/src ngoài mã sinh: 0 chỗ).
- Câu sqlc, bảng, pha migration: tệp mới `queries/fulfillment/locations.sql` (đọc danh mục qua view, upsert `fulfillment.carrier_locations`, đọc bảng mã theo hãng, danh sách chưa khớp). Bảng `fulfillment.carrier_locations` (0300) đã có quyền INSERT, UPDATE(carrier_code, carrier_name, synced_at, updated_at). Migration mới `0315_customer_admin_units_view.sql` (0314 đã là của Task 120 trên main) (pha mở rộng: chỉ tạo view `customer.admin_units_v` + GRANT SELECT). `docs/views.md` thêm dòng view; `tests/arch/sqlusage_test.go` thêm `customer.admin_units_v` vào ngoại lệ "chỉ đọc view" của fulfillment (xem Quyết định thay chủ shop).
- Tệp khác thực sự đổi: `internal/*/internal/store/models.go` của mọi khối sqlc (mã sinh: thêm kiểu `CustomerAdminUnitsV` của view mới, `make gen`); `tests/arch/viewsdoc_test.go` (số view công bố 38 → 39); `tests/arch/testdata/m3_pending.txt` (bỏ hai operation đã làm); `docs/carriers/dia-gioi.md` (mới); `internal/httpapi/handlers_carrier_accounts.go`: commit 1 tách `accountJob` (dùng cho `CheckCarrierAccount`, không đổi hành vi), commit 2 thêm `SyncCarrierLocations`, `ListUnmappedLocations`.
- Test sẽ chạy: `go test -race ./internal/modules/fulfillment/...` (test mới `locations_sync_test.go`, `internal/locations/locations_test.go`), `./internal/httpapi/ -run 'Locations|M3Routes'`, `./tests/arch/...`, rồi cổng kiểm `ci-task.sh` một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `textnorm.Fold` (bỏ dấu, chữ thường, gộp khoảng trắng); `customer.ListProvinces`/`ListWards` (G-M2-8); `commands.CommandKind`, mẫu lệnh đọc `checkCommand`, `RefreshCommand` (Lookup luôn not_sent, `classifyTrackError`, `MarkAuthFailed`); `ResolveAccount`; `queue.NewQueue().Enqueue`; `idCursor` của httpapi; `ErrInvalidPage`, `ErrCarrierNotSupported`, `ErrAccountTransition` đã có mã HTTP ở `errors.go`.
- Dùng lại / viết mới vì: dùng lại `textnorm.Fold` (tên địa giới là chữ để so như cột tìm kiếm, không phải định danh) rồi thêm bước bỏ dấu câu và tiền tố hành chính (riêng việc khớp địa giới, nằm trong `fulfillment/internal/locations`). Không gọi được `customer.ListWards`: fulfillment không được nhập customer (ranh giới M3, `moduleCalls`) và `ListWards` chặn 50 dòng/tỉnh → đọc danh mục qua view công bố `customer.admin_units_v` (mẫu `inventory.warehouses_v` của Task 100). Dùng lại `classifyTrackError` (cùng lớp lỗi đọc hãng: 429, 401/403, quá giờ, không hỗ trợ, từ chối) và `MarkAuthFailed`. Cursor danh sách chưa khớp là `level:code` (khóa không phải uuid nên không dùng `idCursor`).

## Soát thiết kế

- 5.1 Chủ dữ liệu: `carrier_locations` thuộc fulfillment (0300). Danh mục địa giới thuộc customer; fulfillment chỉ đọc.
- 5.2 Không ghi bảng module khác: chỉ ghi `fulfillment.carrier_locations`, `platform.jobs` (qua queue).
- 5.3 Đọc qua view: danh mục đọc qua view mới `customer.admin_units_v` (security_invoker), công bố ở `docs/views.md`, ngoại lệ "chỉ đọc view" như `inventory.warehouses_v`.
- 5.4 không áp dụng: không có việc nguyên tử nhiều module.
- 5.5, 5.6 Gọi hãng chỉ trong `Execute` của lệnh `command.carrier.locations` (ngoài transaction); ghi kết quả ở `OnDone`. Lệnh đọc: `AutoRetryNotSent = true`, `Lookup` luôn not_sent (chạy lại không hại; upsert lặp không thêm dòng).
- 5.7 Tenant: mọi câu trong `tenancy.WithTenant` hay tx của Runner; bảng đã có RLS. View danh mục không có tenant_id (dữ liệu tham chiếu chung).
- 5.8 Module không nhập net/http; handler mỏng ở `handlers_carrier_accounts.go`.
- 5.9 `syncCarrierLocations` có `IdempotencyKey` và `x-permission …manage` (đã có); `listUnmappedLocations` là đọc (`…read`).
- 5.10 không áp dụng: không dùng Valkey. 5.11 không thêm thư viện. 5.12 theo mẫu `accounts_check.go`, `refresh_status.go`.
- 6.1 Hai lần đồng bộ cùng lúc: upsert `ON CONFLICT DO UPDATE` theo khóa chính, dòng sắp theo (level, our_code) trước khi ghi (cùng thứ tự khóa dòng, không deadlock). Không đặt `OrderKey`: với `OrderStrict` mặc định một việc dead (ví dụ khóa API bị từ chối) sẽ chặn mọi lần đồng bộ sau của hãng; hai lần chạy song song chỉ cùng ghi đè một bảng bằng cùng dữ liệu của hãng.
- 6.2 Bấm hai lần: Idempotency-Key ở HTTP; việc kế tiếp có `DedupeKey sync:<account>:<run>:<cursor>` nên OnDone chạy lại không ghi hai việc.
- 6.3 Khóa: chỉ dòng của `carrier_locations` (nhóm 8, cuối) rồi `platform.jobs` do Runner.
- 6.4 không áp dụng: đường yêu cầu chỉ ghi việc. 6.5 READ COMMITTED mặc định.
- 6.6, 6.7 Lỗi hãng: 429 → not_sent theo Retry-After; 401/403 → rejected + `MarkAuthFailed`; quá giờ, mạng, 5xx → not_sent; không hỗ trợ, từ chối → rejected (dead).
- 6.8 Tiến trình chết: lệnh in_doubt → Lookup not_sent → chạy lại từ cursor của payload.
- 6.9 Treo: mỗi lần gọi hãng hạn 8 s; ngừng gọi tiếp khi đã dùng 15 s (đồng hồ `Deps.Now`) hoặc ctx còn dưới 10 s; Kind timeout 25 s; mỗi việc luôn xử lý ít nhất một tỉnh nên luôn tiến.
- 6.10 không áp dụng: không có tiền. 6.11 không thuộc vùng rủi ro cao; vẫn có test quy luật (rapid) cho `Match`: không mã nào ghép hai lần, tên trùng không ghép.
- 6.12 Dữ liệu cá nhân: tên địa giới không phải dữ liệu cá nhân; kết quả việc không có khóa API, không địa chỉ khách.
- 6.13 Quyền: sync cần `fulfillment.carrier_accounts.manage`, danh sách cần `…read` (guard kiểm theo `x-permission`).

## Quyết định thay chủ shop

1. **Đọc danh mục địa giới qua view mới `customer.admin_units_v` (migration 0315)** thay cho gọi `customer.ListProvinces/ListWards` như G-M2-8 ghi. Vì sao: ranh giới M3 chỉ cho fulfillment gọi `order` (tests/arch `moduleCalls`), và `ListWards` chặn 50 xã mỗi lần (một tỉnh có hơn 100 xã). Có tiền lệ: `inventory.warehouses_v` (Task 100) là ngoại lệ "chỉ đọc view" cùng kiểu; thêm một dòng vào danh sách ngoại lệ của `tests/arch/sqlusage_test.go` kèm lý do, và dòng trong `docs/views.md`. Đây là thêm một ngoại lệ có tên, không nới luật chung. Dễ đảo ngược: bỏ view và dòng ngoại lệ. Phương án khác: thêm hàm đọc đủ danh mục ở customer rồi bọc qua public API của order (chạm hai module, order là vùng rủi ro cao); hay thêm cổng vào `Deps` (phải sửa module.go mà khung Task 95 cấm).
2. **Thêm `run` vào khóa chống trùng của việc kế tiếp** (`sync:<account>:<run>:<cursor>` thay cho `sync:<account>:<cursor>`): khóa chống trùng là duy nhất mãi theo (tenant, kind, khóa); không có `run` thì lần đồng bộ thứ hai bị gộp vào việc của lần đầu và bỏ sót.
3. **Hạn 25 s giả lập bằng đồng hồ `Deps.Now`** (đồng hồ giả tăng mỗi lần gọi hãng) thay cho `Timeout` nhỏ: "Bẫy đã gặp" cấm lấy giờ thật làm tiêu chí đúng/sai. Mã vẫn kiểm cả hạn thật của ctx.
4. **Test tích hợp đặt ở `internal/modules/fulfillment/locations_sync_test.go`** (dùng lại môi trường `accEnv` có hãng giả, khóa, worker); `internal/locations/locations_test.go` giữ test thuần của chuẩn hóa tên, ghép, bảng tra.
5. **Khớp thêm tiền tố "tp." / "tp"** ngoài danh sách của kế hoạch ("tỉnh", "thành phố", "phường", "xã", "đặc khu") vì hãng ghi "TP. Hồ Chí Minh" (chính ca test của kế hoạch).
6. **Tên trùng (hai đơn vị cùng tên sau chuẩn hóa trong cùng cha, ở bên nào cũng vậy) → không ghép** (vào danh sách chưa khớp), theo Q-19 "không gửi đoán".
7. **`LocationMap` "đệm theo transaction"** hiểu là: đọc một lần mọi mã của hãng trong tx của bên gọi, trả bảng trong bộ nhớ để dùng cho mọi lần tra trong tx đó (không giữ bộ đệm dùng chung giữa các tx, nên không bao giờ cũ hơn tx).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Commit 1 (cấu trúc, không đổi hành vi): `CheckCarrierAccount` dùng hàm chung `accountJob` (jscpd báo trùng với handler đồng bộ mới) | `TestCarrierAccountsAPI_VC01`, `TestCarrierAccountsAPI_Refusals` | `go test -race ./internal/httpapi/ -run 'TestCarrierAccountsAPI' -count=1` | `ok elimaz.com.vn/crm/internal/httpapi 2.446s` |
| Đồng bộ → 3 tỉnh, 11 phường khớp, 1 chưa khớp nằm ở danh sách chưa khớp; `LocationMap.Carrier("ward", mã chưa khớp)` → false | `TestLocations_SyncAndRerun` (+ `checkSynced`) | `go test -race ./internal/modules/fulfillment/ -run 'TestLocations' -count=1` | `ok elimaz.com.vn/crm/internal/modules/fulfillment 3.405s` |
| Chạy lại → không thêm dòng (14), `synced_at` mới ở cả 14 dòng | `TestLocations_SyncAndRerun` | như trên | như trên |
| Hạn 25 s (đồng hồ giả 10 s mỗi lần gọi phường) → việc kế tiếp có cursor `L79`, khóa `sync:<acc>:<run>:L79`; tổng cuối [3 0 11 1] như ca đầu; lần đồng bộ thứ hai không bị gộp | `TestLocations_CursorContinues` | như trên | như trên |
| Lỗi hãng: 401 → dead `auth` + tài khoản `auth_failed`; 429 → chờ Retry-After rồi xong; lỗi mạng → thử lại rồi xong; không hỗ trợ → dead; ca auth kiểm thêm không ghi dòng nào | `TestLocations_CarrierErrors` (4 ca con) | như trên | như trên |
| Ghi lệnh bị từ chối (tài khoản lạ, nhập tay); danh sách chưa khớp: tham số sai → `ErrInvalidPage`, phân trang không sót không lặp | `TestLocations_Refusals` | như trên | như trên |
| Chuẩn hóa tên ("TP. Hồ Chí Minh" = "Thành phố Hồ Chí Minh", có/không dấu, "P.", "Đặc khu"); tên trùng không ghép; quy luật Match (rapid); bảng tra | `TestNormalize`, `TestMatch`, `TestMatch_Properties`, `TestMap` | `go test -race ./internal/modules/fulfillment/internal/locations/ -count=1` | `ok …/fulfillment/internal/locations 2.384s` |
| HTTP: POST sync 202 + audit `fulfillment.carrier_account.locations_sync`, quyền read → 403, tài khoản lạ → 404; GET unmapped chỉ còn phường hãng không có, phân trang `nextCursor`, tham số sai → 400, không quyền → 403 | `TestCarrierLocationsAPI` | `go test -race ./internal/httpapi/ -run 'TestCarrierLocationsAPI\|TestCarrierAccountsAPI\|TestM3Routes' -count=1` | `ok elimaz.com.vn/crm/internal/httpapi 4.208s` |
| Luật kiến trúc: view công bố, ngoại lệ chỉ đọc view, hai operation không còn 501, lint | `tests/arch` (`TestSQLUsageProductCode`, `TestViewsDocParses`, `TestM3Routes_PendingList`, `TestLintCleanOnRealCode`) | cổng kiểm `ci-task.sh` (toàn bộ test vì đổi migration) | `CI-TASK XANH (so với điểm tách nhánh fcbcdcf…)`; `ok elimaz.com.vn/crm/tests/arch 28.870s`, `ok …/fulfillment 85.092s`, `ok …/httpapi 141.708s` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Đọc danh mục qua view `customer.admin_units_v` thay cho `ListProvinces`/`ListWards` (Quyết định 1); thêm tệp `migrations/0315_customer_admin_units_view.sql`, dòng `docs/views.md`, ngoại lệ trong `tests/arch/sqlusage_test.go`, số view trong `tests/arch/viewsdoc_test.go` (38 → 39: test đếm view công bố, mỗi view mới cập nhật số này).
- `LocationsKind` là hàm (không `var`: luật không biến toàn cục, giống `CheckKind()`).
- Khóa chống trùng có thêm `run` (Quyết định 2); hạn giả lập bằng đồng hồ module (Quyết định 3).
- Test tích hợp ở `internal/modules/fulfillment/locations_sync_test.go`, test HTTP `internal/httpapi/carrier_locations_test.go`; `internal/locations/locations_test.go` là test thuần (Quyết định 4).
- Ghi chú thêm mã tay: tệp chung `docs/carriers/dia-gioi.md` (chưa có tệp `docs/carriers/<hãng>.md` nào; task adapter của từng hãng nhắc tới tệp này).
- Bỏ dòng `syncCarrierLocations`, `listUnmappedLocations` khỏi `tests/arch/testdata/m3_pending.txt` (đã hiện thực).

## Việc phát hiện thêm

- `Account.Locations` (bảng đổi mã adapter dùng) chưa được điền ở `ResolveAccount`; Task M3-12, M3-13 gọi `Service.LocationMap` rồi gán khi cần (không làm ở đây).
- Mã hãng đã ghi không bao giờ bị xóa khi lần đồng bộ sau không còn khớp (crm_app không có quyền DELETE trên `carrier_locations`); nếu hãng đổi tên làm mất khớp thì dòng cũ vẫn giữ mã cũ. Cần quyết định ở mốc sau (đánh dấu cũ theo `synced_at` hay cấp quyền xóa).
- `TestHealthDeepFileSyncAge` (internal/httpapi, không do task này chạm) đỏ một lần ở lần chạy cổng kiểm đầu (`lastFileSyncAgeM = 29, muốn 30`: so phút theo giờ thật), chạy lại riêng thì qua, lần cổng kiểm sau xanh: test chập chờn theo giờ, cần sửa ở task riêng.
- Kết quả việc đồng bộ (`platform.jobs.result`) chứa mọi cặp mã của phần đã đọc: với bộ 2025 đầy đủ (~3.300 xã) khoảng 200 KB mỗi lần đồng bộ; danh mục GSO đầy đủ (Task 61 phần B) chưa nạp nên chưa đo trên dữ liệu thật.

## Reviewer

Commit 1 (cấu trúc, `accountJob`): **ĐẠT**. Không có phát hiện phải sửa; ghi chú không bắt buộc: chưa test nào kiểm chuỗi action audit `fulfillment.carrier_account.check` (đã đối chiếu bằng đọc diff).

Commit 2 (đồng bộ địa giới, lệnh `command.carrier.locations`, `LocationMap`, hai route): **ĐẠT** (lần review đầu). Không có phát hiện phải sửa. Reviewer đã kiểm cả 7 quyết định thay chủ shop (đúng spec, an toàn, dễ đảo ngược). Ghi chú không bắt buộc: (1) dòng bằng chứng `TestLocations_CarrierErrors` ghi rộng hơn test ("không ghi dòng" chỉ ca auth kiểm): đã sửa chữ trong bảng; (2) `ListUnmappedLocations` dùng helper `inventoryRead` (tên gây hiểu lầm, hành vi đúng); (3) kết quả việc khoảng 200 KB với bộ đầy đủ (đã ghi ở Việc phát hiện thêm).
