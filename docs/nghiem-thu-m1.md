# Nghiệm thu M1 — Sản phẩm và kho; kho tệp và xử lý ảnh

Viết ở Task M1-24 (18/11/2026), trước khi có mã M1. Nguồn: kế hoạch `docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (mục "Xong M1 khi", "Tiêu chí nghiệm thu → ca kiểm thử", "Vùng rủi ro cao của M1", "Ngoài phạm vi M1"), spec `08-roadmap.md` hàng M1 và `11-kich-ban-kiem-thu.md` (các ca Mốc = M1). Kế hoạch là nguồn gốc; tệp này viết lại bằng lời nghiệp vụ để chủ shop đọc và duyệt. Hai nơi khác nhau thì sửa cả hai trong cùng commit.

Cột "Kết quả" điền lần cuối ở Task 405 (M1-55d, 21/11/2026) trên HEAD `e53d001` (main sau Task 403, 404, 406–414, 420–429); sau khi gộp main (`4535a0d`, thêm Task 95 của M3) chạy lại 38 test Go của các ca và TP01 imager thật: đều PASS. Lệnh chạy và bảng bằng chứng: `docs/tasks/task-405.md`. Phần staging và nhân viên chạy thử ghi "chờ chủ shop"; điền khi có (Điểm dừng M1-D).

TP01 (đường imager thật), TP03 (bom điểm ảnh), TP08, TP09, TP10 và phần hết bộ nhớ của tiêu chí 7 không chạy trong `make ci` (nhãn build riêng hoặc cần libvips): lệnh, lịch chạy và lần chạy gần nhất ở `docs/ca-chay-tay.md` (Task 414); Task M1-55 chạy lại theo bảng đó trước khi điền cột Kết quả của các dòng này.

## Xong M1 khi

| # | Tiêu chí (lời nghiệp vụ) | Task M1 | Kết quả |
|---|---|---|---|
| 1 | **Một đơn đi trọn.** Nhân viên tạo một mẫu áo, nhập 5 chiếc vào kho, lên một đơn nhập tay 2 chiếc: hàng được giữ ngay. Gửi đơn cho hãng vận chuyển thử, hãng báo đã lấy rồi đã giao: đơn thành "đã giao", kho còn 3, sổ tiền thu hộ ghi đúng số tiền phải thu từ hãng. Chạy được trên máy thử (staging); nếu staging chưa có thì ghi "chưa chạy trên staging" và chạy khi có. | 33 (Điểm dừng M1-A) | 21/11 (Task 405, HEAD `e53d001`): `TestMotDonDiTron` PASS lại (8.56s). Cục bộ đạt 18/11 (`TestMotDonDiTron`: nhập 5, đơn 2 cái, giao xong còn 3, sổ COD `carrier_holding` 530000, tổng sổ 0; kịch bản `scripts/demo/mot-don-di-tron.sh` chạy đủ 6 bước). Chạy được trên compose dev 18/11 (`make demo-mot-don`, Task 395: đủ 6 bước, sổ COD `carrier_holding` 530000, tổng 0, còn 3). **Đạt trên máy; staging: chờ chủ shop** (OK deploy, service token). |
| 2 | **Nhập 200 mẫu bằng Excel.** Nhân viên tải tệp mẫu, điền 200 mẫu (khoảng 1.000 màu/size), tải lên: xem trước đúng số mẫu sẽ tạo. Tệp có dòng sai thì màn hình chỉ đúng dòng, đúng cột, câu lỗi tiếng Việt, và không tạo gì cho tới khi sửa hết. Tải lại đúng tệp đó lần hai: hệ thống báo "đã nhập rồi", không sinh mẫu trùng. | 38, 47, 52 | **Đạt trên máy.** 21/11 (Task 405, HEAD `e53d001`): `TestValidate_SevenSeededErrors` (7 lỗi đúng dòng, cột), `TestApply_200Models` (8.03s), `TestApply_SameFileTwice` PASS; e2e màn Nhập Excel (`e2e/imports.spec.ts`, chromium) PASS. Tệp quá lớn để áp trong hạn giờ báo `apply_too_long` rõ ràng thay vì treo (Task 407). Nhân viên nhập 200 mẫu thật trên staging: **chờ chủ shop**. |
| 3 | **Kiểm kho cho ra đúng tồn.** Sổ ghi 10 chiếc, 3 chiếc đang trên đường giao, nhân viên đếm trên kệ được 7: hệ thống hiểu là đủ, không chỉnh. Đếm thiếu khi có đơn đang giữ hàng: ghi phần chênh, báo quản lý, không tự hủy hay đổi đơn nào. | 35, 51 | **Đạt trên máy.** 21/11 (Task 405, HEAD `e53d001`): `TestStocktake_InTransitNotAdjusted` (sổ 10, giao 3, đếm 7 → không chỉnh), `TestStocktake_ShortageKeepsHolds` PASS; e2e màn Kiểm kho KH05, KH07 trên WebKit iPhone 390 PASS. Từ Task 406 (A-01): đếm thiếu dưới phần giữ vẫn ghi chênh và cảnh báo, không đụng đơn; lấy hàng/giao các đơn đang giữ bị chặn bằng `stock_short` (câu rõ) thay vì lỗi database. |
| 4 | **Hai đơn tranh chiếc cuối.** Hai nhân viên cùng lúc lên đơn chiếc áo cuối cùng: chỉ một đơn giữ được hàng, người kia thấy báo hết hàng. Tồn không bao giờ âm. | 28, 30, 33 | Đạt qua API thật 18/11 (`TestMotDonDiTron_HaiDonSongSongTon1`: tồn 1, hai đơn song song → một 201, một 409 `out_of_stock`, 1 đơn, tồn không âm). 21/11 (Task 405, HEAD `e53d001`): PASS lại (3.48s); thuộc tính `TestInventoryInvariants` (không ngăn nào âm) PASS. **Đạt.** |
| 5 | **Kho tệp tắt đột ngột không mất ảnh đã báo xong.** Rút điện kho tệp giữa lúc đang tải ảnh: ảnh nào hệ thống đã báo "xong" thì vẫn còn nguyên khi bật lại. | 46 | **Đạt.** 21/11 (Task 405, HEAD `e53d001`), compose riêng `crm-t405`: `scripts/files/crash-drill.sh` → `FILES CRASH OK uploads=200 completed=119 failed_midway=81 retried_ok=81` (`TestFileStoreCrash` 36.6s): mọi tệp đã báo xong còn nguyên sau khi giết SeaweedFS; tệp đang dở tải lại được. |
| 6 | **Dựng lại kho tệp từ bản sao.** Kho tệp hỏng hẳn: dựng lại từ bản sao ở kho ngoài, có bấm giờ. Ảnh mới hơn lần chép cuối (tối đa khoảng 1 giờ) bị mất thì hiện ô trống có ghi chú "ảnh bị mất, tải lại", và có danh sách các tệp mất. | 46, 53 | **Đạt trên máy (kho ngoài giả).** 21/11 (Task 405, HEAD `e53d001`), `crm-t405`: `scripts/files/restore-drill.sh` → `FILES RESTORE OK restore_seconds=14.2 lost=2 verify_seconds=0.2 files=22` (`TestFileStoreRestore`): dựng lại 14,2 giây, đúng 2 ảnh tải sau lần chép cuối thành "mất". Sau dựng lại, 273 đối tượng `crm-public` và 12 biến thể ở `crm-private` giữ đúng `content-type` (`image/avif`, `image/webp`, `image/jpeg`; kiểm bằng HEAD từng đối tượng) nên kiểm loại biến thể của Task 414 không từ chối nhầm; ảnh gốc trong `crm-originals` về `application/octet-stream` (xem "Việc phát hiện thêm" của `docs/tasks/task-405.md`). Ô trống "ảnh bị mất" và danh sách tệp mất: `TestVerify_MarksLost`, e2e `lost-files.spec.ts` PASS. Kho ngoài thật (G5) và `restore-drill.sh` với kho thật: **chờ chủ shop** tạo tài khoản. |
| 7 | **Một ảnh độc không làm treo hệ thống.** Một ảnh cố tình làm treo bộ xử lý ảnh bị ngắt đúng hạn, bị từ chối; các việc khác (ảnh khác, đơn, tin) vẫn chạy bình thường, không phải khởi động lại gì. | 42, 43 | **Đạt.** 21/11 (Task 405, HEAD `e53d001`): `make test-imager` (libvips thật trong docker) → `TestImager_HangKilledWithinDeadline` (1.03s), `TestImager_OutOfMemoryKilled`, `TestServer_StillAnswersAfterOOM`, `TestExecRunner_TimeoutKillsProcessGroup`, `TestExecRunner_MemoryLimit` PASS (49 PASS, `ok internal/imager 13.188s`); `TestFileProcess_HangDoesNotBlockWorker` (gói files) PASS: ảnh treo bị giết đúng hạn, việc khác vẫn chạy. Lỗi tạm của imager được thử lại, không thành từ chối vĩnh viễn (Task 409, 426). |
| 8 | **Đủ ca kiểm thử và màn hình đã duyệt.** 22 trong 23 ca M1 của `11-kich-ban-kiem-thu.md` đạt (TP11 là câu pháp lý còn mở, chờ chủ shop). Mọi màn hình M1 làm đúng mockup đã duyệt. Nhân viên chạy kịch bản của mốc trên staging. | 55 (Điểm dừng M1-D) | **Phần máy đạt; phần người chờ chủ shop.** 22/23 ca M1 có test đạt lần chạy 21/11 (Task 405, HEAD `e53d001`) (bảng dưới); TP11 còn mở. Màn hình M1: điều phối viên duyệt hình mockup theo Q-X8 ngày 18/11 (merge Task 49, `8676e11`); 35 thay đổi hành vi H01–H35 chờ chủ shop OK riêng. Test màn 21/11: vitest 891/891 PASS (71 tệp); Playwright 16 tệp màn M1: 157 PASS chromium, WebKit 8/10 (2 ca đỏ do giả lập iPhone chặn `click`/`dblclick` của chuột, xem "Việc phát hiện thêm" Task 405; các ca dùng chạm `tap` cùng nút PASS). Nhân viên chạy kịch bản trên staging (KH01, KH02, KH03, KH05, KH08, KH12, TP01, TP06, PQ07): **chờ chủ shop**. Thử tải: **chờ Task 402 (dời theo chủ shop 19/11)**. |

## Tiêu chí → ca kiểm thử

Mỗi ca có Mốc = M1 trong `11-kich-ban-kiem-thu.md` và nơi kiểm. Số task là số nội bộ M1; tên test chép từ kế hoạch, task tương ứng ghi lại đúng tên trong mã (Task M1-55 kiểm từng tên có trong mã).

| Ca | Tình huống (lời nghiệp vụ) | Task M1 | Test | Kết quả |
|---|---|---|---|---|
| KH01 | Nhập khoảng 200 mẫu bằng Excel; dòng sai báo từng dòng; nhập lại không trùng | 38, 47, 52 | `TestValidate_SevenSeededErrors`, `TestApply_200Models`, `TestApply_SameFileTwice`; e2e màn nhập | PASS 21/11 (3 test Go + e2e `imports.spec.ts`) |
| KH02 | Tạo mẫu có màu, size, giá, ảnh | 27, 44, 50 | `TestCreateProduct_SixVariants`, `TestAttachImage_OrderAndColor`; e2e màn sản phẩm | PASS 21/11 (2 test Go + e2e `products`, `product-edit`, `product-images`, `product-article`) |
| KH03 | Nhập hàng về kho: tồn tăng, có dòng sổ | 29, 51 | `TestPostImport_IncreasesOnHandAndLedger` | PASS 21/11 |
| KH04 | "Có thể bán" = tồn − hàng lỗi − đang giao − tạm giữ | 28 | `TestAvailability_Formula`, thuộc tính `TestInventoryInvariants` | PASS 21/11 (cả thuộc tính rapid) |
| KH05 | Kiểm kho khi có hàng đang giao: sổ 10, giao 3, đếm 7 → không chỉnh | 35, 51 | `TestStocktake_InTransitNotAdjusted` | PASS 21/11 (+ e2e Kiểm kho WebKit 390) |
| KH06 | Hai mã hàng (SKU) trùng → không cho | 27 | `TestCreateProduct_DuplicateSKU` | PASS 21/11 |
| KH07 | Đếm thiếu khi có đơn đang giữ: ghi chênh, cảnh báo, không đụng đơn | 35 | `TestStocktake_ShortageKeepsHolds` | PASS 21/11 (+ e2e Kiểm kho WebKit 390); hành vi lấy hàng theo Task 406 (`stock_short`) |
| KH08 | Hàng lỗi ghi bằng chứng từ, không bán được | 29 | `TestPostDamage_ReducesAvailable` | PASS 21/11 |
| KH10 | Xuất tay vượt phần đã giữ cho đơn → chặn | 29 | `TestPostExport_CannotEatHeld` | PASS 21/11 |
| KH12 | Ngừng bán: không chọn được khi lên đơn; đơn cũ vẫn tra được | 27, 30 | `TestVariantForSale_Discontinued`, `TestCreateOrder_DiscontinuedVariant` | PASS 21/11 |
| KH13 | Sổ và số tồn lệch → đối chiếu đêm báo đúng mã hàng | 36 | `TestReconcile_ReportsExactVariant` | PASS 21/11 |
| KH14 | Hai người bán: một đơn không trộn hàng của hai bên; mọi ca đơn, kho, vận chuyển vẫn đạt | 30, 37 | `TestOrderItems_SellerFK`, bộ `forEachSeller` | PASS 21/11 (`TestOrderItems_SellerFK`; bộ chạy theo người bán là `testkit.ForEachSeller`: `TestAttachShipment`, `TestApplyShipmentProgress_Flow` PASS) |
| PQ07 | Nhân viên tự nâng quyền mình; khóa Chủ cuối cùng → từ chối | 26, 54 | `TestReplaceGrants_*`, `TestLock_LastOwner*` | PASS 21/11 (`TestReplaceGrants_ExceedsActor`, `_SelfModify`, `_VersionAndInput`, `_ActorLostManageMeanwhile`, `TestLock_LastOwner`, `TestLock_LastOwnerConcurrent`; e2e `staff`, `roles`) |
| TP01 | Tải ảnh: kiểm, mã hóa lại, tạo sẵn kích thước | 41–44, 50 | `TestFileProcess_ReadyWithTwelveVariants` | PASS 21/11: `TestFileProcess_ReadyWithTwelveVariants` (imager giả) và đường imager thật `TestFilesPipeline_PublicPhotoGPS`, `TestFilesPipeline_PrivateImages` trên `crm-t405` |
| TP03 | Tệp giả ảnh, ảnh quá lớn → từ chối trước khi ai mở được | 41, 42 | `TestImager_RejectsHTMLAsJPEG`, `TestImager_PixelBombRejectedBeforeDecode`, `TestCreateUpload_TooLarge` | PASS 21/11 (libvips thật qua `make test-imager`) |
| TP05 | Đường dẫn ảnh riêng tư bị chia sẻ → hết hạn; không có quyền thì không lấy được đường dẫn mới | 44 | `TestOriginalURL_ExpiresAndNeedsPermission` | PASS 21/11 |
| TP06 | Xóa nhầm ảnh → chỉ đánh dấu xóa, khôi phục được trong hạn | 44, 50 | `TestImageSoftDeleteRestore` | PASS 21/11 |
| TP07 | Thêm kích thước ảnh mới → tạo lại từ ảnh gốc | 44 | `TestRegeneratePresetV2` | PASS 21/11 |
| TP08 | Kho tệp tắt đột ngột giữa lúc ghi → tệp đã báo xong còn nguyên | 46 | `drill: TestFileStoreCrash` | PASS 21/11 (`crash-drill.sh`, `crm-t405`) |
| TP09 | Kho tệp hỏng hẳn → dựng lại từ bản sao, mất khoảng 1 giờ gần nhất | 46 | `scripts/files/restore-drill.sh` (có bấm giờ) | PASS 21/11 (`restore-drill.sh`: `restore_seconds=14.2 lost=2`); kho ngoài thật chờ chủ shop |
| TP10 | Chép sang kho ngoài ngừng → quá 90 phút cảnh báo gấp; đối chiếu số tệp mỗi ngày | 45 | `TestRule_FileSyncAge`, `filesync_test` | PASS 21/11 (`TestRule_FileSyncAge`; container `TestFileSync` 26.18s) |
| TP12 | Sau khi dựng lại, ảnh mới nhất bị mất → ô trống có ghi chú, có danh sách tệp mất | 46, 53 | `TestVerify_MarksLost`, e2e màn tệp mất | PASS 21/11 (+ e2e `lost-files.spec.ts`) |
| TP11 | Dữ liệu khách có phải đặt trong nước không | — | **Còn mở** (câu O1 của kế hoạch), chờ chủ shop và tư vấn pháp lý | Còn mở (O1), không tính vào 22/23 |

Tiêu chí không có mã ca nhưng thuộc M1: "một đơn đi trọn" (Task M1-33); "ảnh treo bị ngắt đúng hạn, worker không ảnh hưởng" (Task M1-42, M1-43); màn "Sức khỏe hệ thống", "Sự kiện lỗi" (Task M1-53); danh bạ nhà cung cấp trên phiếu nhập (Task M1-29, `TestSupplier_OnImportOnly`); bài viết chi tiết dạng khối (Task M1-27, M1-50, `TestArticle_RejectsUnknownBlock`); vai trò tự định nghĩa (Task M1-56, `TestCustomRole_CannotExceedActor`).

## Vùng rủi ro cao của M1

Theo ADR-25: có test sinh tình huống ngẫu nhiên (rapid) cho từng quy luật, qua mutation testing ở Task M1-55, review độc lập. Thêm vào danh sách vùng cao của M0.

| Vùng | Vì sao cao | Quy luật phải luôn đúng |
|---|---|---|
| `internal/modules/inventory` | Tồn sai là bán vượt hoặc mất hàng | Không ngăn nào âm; "có thể bán" đúng công thức; sổ cộng ra đúng số dư; khóa theo thứ tự ADR-46 |
| `internal/modules/order` | Giữ hàng cùng transaction với đơn | Đơn có thì hàng đã giữ; một đơn không trộn hai người bán |
| `internal/modules/cash` | Tiền thu hộ | Sổ hai vế, tổng mỗi bút toán bằng 0; chỉ ghi thêm |
| `internal/modules/fulfillment/status.go` | Trạng thái vận đơn đổi đơn và tồn | Chỉ vận đơn hiện hành đổi được đơn; trạng thái chỉ tiến |
| `internal/modules/identity/staff.go`, `roles.go` (phần luật quyền) | Nâng quyền, khóa mất Chủ | Không tự sửa quyền mình; không cấp vượt quyền người cấp; luôn còn ít nhất một Chủ |
| `internal/platform/files` | Ký đường dẫn, kiểm quyền tệp | Không đọc được tệp shop khác; đường dẫn ký có hạn và đúng kích thước; ứng dụng không xóa vĩnh viễn được |
| `internal/imager` | Giải mã dữ liệu lạ bằng C | Từ chối trước khi giải mã; quá hạn là giết; không có khóa bí mật |
| `internal/modules/catalog/internal/importer/apply.go` | Ghi hàng loạt từ tệp ngoài | Ghi tất cả hoặc không; nhập lại không trùng |
| `internal/modules/catalog/article_html.go` | HTML hiện trên website | Chỉ thẻ trong danh sách cho phép; chữ của nhân viên không chèn được mã |

## Ngoài phạm vi M1

- Phần còn lại của đơn hàng (nháp, hủy, sửa, trả, đổi, đơn trùng, mã giảm giá, form lên đơn) và khách hàng đầy đủ (SĐT, địa chỉ, gộp trùng): M2.
- Ba hãng vận chuyển thật, giao thất bại, hoàn hàng, hủy vận đơn, bảng kê COD, "giữ hàng quá hạn" (KH09): M3.
- Flash sale, Valkey, website đọc sản phẩm từ CRM: M5.
- Dọn vĩnh viễn tệp đã xóa mềm; tệp hội thoại, nhãn vận đơn, báo cáo xuất: M3–M4, M6.
- Chuyển kho, công nợ nhà cung cấp, hạn mức tồn, giá vốn bình quân: P2/P3.
- Xuất Excel danh sách sản phẩm, chụp tồn theo ngày: M6.
- Bất kỳ việc gì đọc hay gọi Nhanh: không bao giờ (ADR-11).

## Phụ thuộc M0 còn mở (18/11)

Theo đối chiếu ở `docs/tasks/task-24.md`: tiêu chí 1 cần M0 Task 14, 15, 18, 23; tiêu chí 3 cần Task 19 (cảnh báo); tiêu chí 5, 6 cần Task 23 (sổ tay vận hành) và Task 19 (cảnh báo chép tệp); tiêu chí 8 cần Task 17 (màn hình), 20, 22, 23.
