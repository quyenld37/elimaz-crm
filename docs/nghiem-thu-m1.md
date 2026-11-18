# Nghiệm thu M1 — Sản phẩm và kho; kho tệp và xử lý ảnh

Viết ở Task M1-24 (18/11/2026), trước khi có mã M1. Nguồn: kế hoạch `docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (mục "Xong M1 khi", "Tiêu chí nghiệm thu → ca kiểm thử", "Vùng rủi ro cao của M1", "Ngoài phạm vi M1"), spec `08-roadmap.md` hàng M1 và `11-kich-ban-kiem-thu.md` (các ca Mốc = M1). Kế hoạch là nguồn gốc; tệp này viết lại bằng lời nghiệp vụ để chủ shop đọc và duyệt. Hai nơi khác nhau thì sửa cả hai trong cùng commit.

Cột "Kết quả" để trống tới khi task tương ứng xong; Task M1-55 điền lần cuối, kèm kết quả nhân viên chạy trên staging.

## Xong M1 khi

| # | Tiêu chí (lời nghiệp vụ) | Task M1 | Kết quả |
|---|---|---|---|
| 1 | **Một đơn đi trọn.** Nhân viên tạo một mẫu áo, nhập 5 chiếc vào kho, lên một đơn nhập tay 1 chiếc: hàng được giữ ngay. Gửi đơn cho hãng vận chuyển thử, hãng báo đã lấy rồi đã giao: đơn thành "đã giao", kho còn 4, sổ tiền thu hộ ghi đúng số tiền phải thu từ hãng. Chạy được trên máy thử (staging); nếu staging chưa có thì ghi "chưa chạy trên staging" và chạy khi có. | 33 (Điểm dừng M1-A) | Cục bộ đạt 18/11 (`TestMotDonDiTron`: nhập 5, đơn 2 cái, giao xong còn 3, sổ COD `carrier_holding` 530000, tổng sổ 0; kịch bản `scripts/demo/mot-don-di-tron.sh` chạy đủ 6 bước). **Chưa chạy trên staging** (cần chủ shop OK, service token). |
| 2 | **Nhập 200 mẫu bằng Excel.** Nhân viên tải tệp mẫu, điền 200 mẫu (khoảng 1.000 màu/size), tải lên: xem trước đúng số mẫu sẽ tạo. Tệp có dòng sai thì màn hình chỉ đúng dòng, đúng cột, câu lỗi tiếng Việt, và không tạo gì cho tới khi sửa hết. Tải lại đúng tệp đó lần hai: hệ thống báo "đã nhập rồi", không sinh mẫu trùng. | 38, 47, 52 | |
| 3 | **Kiểm kho cho ra đúng tồn.** Sổ ghi 10 chiếc, 3 chiếc đang trên đường giao, nhân viên đếm trên kệ được 7: hệ thống hiểu là đủ, không chỉnh. Đếm thiếu khi có đơn đang giữ hàng: ghi phần chênh, báo quản lý, không tự hủy hay đổi đơn nào. | 35, 51 | |
| 4 | **Hai đơn tranh chiếc cuối.** Hai nhân viên cùng lúc lên đơn chiếc áo cuối cùng: chỉ một đơn giữ được hàng, người kia thấy báo hết hàng. Tồn không bao giờ âm. | 28, 30, 33 | Đạt qua API thật 18/11 (`TestMotDonDiTron_HaiDonSongSongTon1`: tồn 1, hai đơn song song → một 201, một 409 `out_of_stock`, 1 đơn, tồn không âm). |
| 5 | **Kho tệp tắt đột ngột không mất ảnh đã báo xong.** Rút điện kho tệp giữa lúc đang tải ảnh: ảnh nào hệ thống đã báo "xong" thì vẫn còn nguyên khi bật lại. | 46 | |
| 6 | **Dựng lại kho tệp từ bản sao.** Kho tệp hỏng hẳn: dựng lại từ bản sao ở kho ngoài, có bấm giờ. Ảnh mới hơn lần chép cuối (tối đa khoảng 1 giờ) bị mất thì hiện ô trống có ghi chú "ảnh bị mất, tải lại", và có danh sách các tệp mất. | 46, 53 | |
| 7 | **Một ảnh độc không làm treo hệ thống.** Một ảnh cố tình làm treo bộ xử lý ảnh bị ngắt đúng hạn, bị từ chối; các việc khác (ảnh khác, đơn, tin) vẫn chạy bình thường, không phải khởi động lại gì. | 42, 43 | |
| 8 | **Đủ ca kiểm thử và màn hình đã duyệt.** 22 trong 23 ca M1 của `11-kich-ban-kiem-thu.md` đạt (TP11 là câu pháp lý còn mở, chờ chủ shop). Mọi màn hình M1 làm đúng mockup đã duyệt. Nhân viên chạy kịch bản của mốc trên staging. | 55 (Điểm dừng M1-D) | |

## Tiêu chí → ca kiểm thử

Mỗi ca có Mốc = M1 trong `11-kich-ban-kiem-thu.md` và nơi kiểm. Số task là số nội bộ M1; tên test chép từ kế hoạch, task tương ứng ghi lại đúng tên trong mã (Task M1-55 kiểm từng tên có trong mã).

| Ca | Tình huống (lời nghiệp vụ) | Task M1 | Test | Kết quả |
|---|---|---|---|---|
| KH01 | Nhập khoảng 200 mẫu bằng Excel; dòng sai báo từng dòng; nhập lại không trùng | 38, 47, 52 | `TestValidate_SevenSeededErrors`, `TestApply_200Models`, `TestApply_SameFileTwice`; e2e màn nhập | |
| KH02 | Tạo mẫu có màu, size, giá, ảnh | 27, 44, 50 | `TestCreateProduct_SixVariants`, `TestAttachImage_OrderAndColor`; e2e màn sản phẩm | |
| KH03 | Nhập hàng về kho: tồn tăng, có dòng sổ | 29, 51 | `TestPostImport_IncreasesOnHandAndLedger` | |
| KH04 | "Có thể bán" = tồn − hàng lỗi − đang giao − tạm giữ | 28 | `TestAvailability_Formula`, thuộc tính `TestInventoryInvariants` | |
| KH05 | Kiểm kho khi có hàng đang giao: sổ 10, giao 3, đếm 7 → không chỉnh | 35, 51 | `TestStocktake_InTransitNotAdjusted` | |
| KH06 | Hai mã hàng (SKU) trùng → không cho | 27 | `TestCreateProduct_DuplicateSKU` | |
| KH07 | Đếm thiếu khi có đơn đang giữ: ghi chênh, cảnh báo, không đụng đơn | 35 | `TestStocktake_ShortageKeepsHolds` | |
| KH08 | Hàng lỗi ghi bằng chứng từ, không bán được | 29 | `TestPostDamage_ReducesAvailable` | |
| KH10 | Xuất tay vượt phần đã giữ cho đơn → chặn | 29 | `TestPostExport_CannotEatHeld` | |
| KH12 | Ngừng bán: không chọn được khi lên đơn; đơn cũ vẫn tra được | 27, 30 | `TestVariantForSale_Discontinued`, `TestCreateOrder_DiscontinuedVariant` | |
| KH13 | Sổ và số tồn lệch → đối chiếu đêm báo đúng mã hàng | 36 | `TestReconcile_ReportsExactVariant` | |
| KH14 | Hai người bán: một đơn không trộn hàng của hai bên; mọi ca đơn, kho, vận chuyển vẫn đạt | 30, 37 | `TestOrderItems_SellerFK`, bộ `forEachSeller` | |
| PQ07 | Nhân viên tự nâng quyền mình; khóa Chủ cuối cùng → từ chối | 26, 54 | `TestReplaceGrants_*`, `TestLock_LastOwner*` | |
| TP01 | Tải ảnh: kiểm, mã hóa lại, tạo sẵn kích thước | 41–44, 50 | `TestFileProcess_ReadyWithTwelveVariants` | |
| TP03 | Tệp giả ảnh, ảnh quá lớn → từ chối trước khi ai mở được | 41, 42 | `TestImager_RejectsHTMLAsJPEG`, `TestImager_PixelBombRejectedBeforeDecode`, `TestCreateUpload_TooLarge` | |
| TP05 | Đường dẫn ảnh riêng tư bị chia sẻ → hết hạn; không có quyền thì không lấy được đường dẫn mới | 44 | `TestOriginalURL_ExpiresAndNeedsPermission` | |
| TP06 | Xóa nhầm ảnh → chỉ đánh dấu xóa, khôi phục được trong hạn | 44, 50 | `TestImageSoftDeleteRestore` | |
| TP07 | Thêm kích thước ảnh mới → tạo lại từ ảnh gốc | 44 | `TestRegeneratePresetV2` | |
| TP08 | Kho tệp tắt đột ngột giữa lúc ghi → tệp đã báo xong còn nguyên | 46 | `drill: TestFileStoreCrash` | |
| TP09 | Kho tệp hỏng hẳn → dựng lại từ bản sao, mất khoảng 1 giờ gần nhất | 46 | `scripts/files/restore-drill.sh` (có bấm giờ) | |
| TP10 | Chép sang kho ngoài ngừng → quá 90 phút cảnh báo gấp; đối chiếu số tệp mỗi ngày | 45 | `TestRule_FileSyncAge`, `filesync_test` | |
| TP12 | Sau khi dựng lại, ảnh mới nhất bị mất → ô trống có ghi chú, có danh sách tệp mất | 46, 53 | `TestVerify_MarksLost`, e2e màn tệp mất | |
| TP11 | Dữ liệu khách có phải đặt trong nước không | — | **Còn mở** (câu O1 của kế hoạch), chờ chủ shop và tư vấn pháp lý | |

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
