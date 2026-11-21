# Task 450 — Sửa kế hoạch M5: chuyển toàn bộ admin sang CRM, website chỉ gọi CRM

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (task tài liệu của điều phối viên, 21/11; số toàn cục 450, nội bộ M5-x0). Loại việc (quy trình mục 3): A — **chỉ tài liệu**. Kế hoạch M5, M7 và bảng số task nằm ở `../../docs/superpowers/plans/` (ngoài git của CRM); phần trong git của task là tệp này và `docs/adr/0046-thu-tu-khoa.md`.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có — task không sửa mã Go. Chỉ **đọc** mã để ghi đúng tên vào kế hoạch: `files.Purpose` (`purpose.go:35`, không có trường bộ kích thước), `files.Preset`/bộ `v1` (`preset.go`, rộng tối đa 1600), `files.RegisterPurpose` (`purpose.go:180`), `console/src/app/nav.ts` `NAV_GROUPS` (nhóm "Hệ thống", "Quản trị" đang có), quyền `audit.read` (`permissions.go:47`), `promotion.read`/`promotion.manage` (`permissions.go:83-84`), `ord.order_settings` (`0204_order.sql:334`), `customers.birthday` (`0201_customer.sql:24`); `openapi.yaml` chưa có operation đọc audit.
- Gói phụ thuộc ngược (go list): không áp dụng (không đổi gói nào).
- Operation OpenAPI bị chạm, oasdiff: không.
- Chỗ dùng trên màn hình: không (chỉ đọc `console/src/app/nav.ts`, `ls console/src/screens`).
- Câu sqlc, bảng, pha migration: không. Kế hoạch **cấp sẵn** số migration mới 0520–0527 cho Task 452–468 (dải riêng, không đụng 0501–0508 đã cấp và 0509+ giữ cho sửa chữa).
- Tệp bị sửa:
  - `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md`: ghi chú đầu tệp; QĐ16, QĐ17, QĐ20 (ghi "thay bằng"); QĐ32–QĐ42 mới; mục mới "Chuyển toàn bộ admin sang CRM" (đợt 1/đợt 2, bảng kiểm kê 14 mục admin, xếp module, menu, bảng mới + migration + nhóm khóa); "Việc cần chủ shop" (bỏ worktree admin, thay dòng "ẩn hai mục", thêm 3 dòng, sửa dòng đêm mở); Global Constraints (website, migration, vùng rủi ro cao); Cấu trúc file; Phụ thuộc/Làn (L1, L4, L5, 17 dòng task mới, M5-35, M5-38); ghi chú trong Task 192, 193, 195, 198, 205, 209, 213, 214 (bỏ phần admin), 215, 221; Phần H mới (Task 452–468); bảng ca (KM04); Ước lượng; Ngoài phạm vi.
  - `../../docs/superpowers/plans/2026-11-18-crm-m7-dang-nhap-khoi-dong.md`: ghi chú đầu tệp; G15; bước 3 của bảng bảy bước (M7-25) và đoạn "tắt admin sau bước 7"; sổ tay đêm mở (M7-31); một dòng "Việc cần chủ shop".
  - `../../docs/superpowers/plans/danh-so-task-toan-cuc.md`: dòng dải M5; dòng 450 (đúng chữ điều phối viên giao) và 17 dòng 452–468.
  - `docs/adr/0046-thu-tu-khoa.md` (trong git): chèn bảng mới vào nhóm có sẵn — nhóm 5 `feedback`, `birthday_profiles`; nhóm 7 `web_intake`; nhóm 10 `web_campaigns`, `web_campaign_targets`, `birthday_settings`, `birthday_runs` (trước `flash_sales`). **Không** chèn nhóm mới 12d (xem Quyết định Q3).
  - `docs/tasks/task-450.md` (tệp này).
- Không sửa: `apps/admin`, `apps/storefront*` (chỉ đọc `origin/main` bằng `git show`), spec, kế hoạch M0–M4, M6.
- Test sẽ chạy: không có test mã mới. `go test ./tests/arch/ -run TestLockOrder` (đọc `docs/adr/0046-thu-tu-khoa.md`) và cổng `ci-task.sh` một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: không viết hàm hay kiểu nào. Kế hoạch mới tái dùng những cái CRM đã có, ghi rõ trong Phần H: `promotion.EnsureWebBatch`/`webMarkCouponGiven` của M5-14 (sinh nhật), `files.RegisterPurpose` + `files.Status` + `files.PublicVariants` (ảnh banner), `customer.NormalizePhone` và danh tính `phone` (gắn góp ý), quyền `audit.read`, `promotion.read`/`promotion.manage` (không thêm quyền trùng việc), yêu cầu về đơn hàng của M5-12 và hãng trên web của M5-15 (bổ sung luật admin vào task có sẵn thay vì task mới), bảng chuyển mã của M5-32 (dùng lúc xuất).
- Dùng lại / viết mới vì: chỗ CRM chưa có (chiến dịch web, banner, bộ phối, góp ý, tạm dừng nhận đơn, đọc audit, sinh nhật đầy đủ, lệnh nhập dữ liệu) mới thành task mới; mỗi task ghi cái nó dùng lại.

## Soát thiết kế

- 5.1 Đúng chủ dữ liệu: chiến dịch, sinh nhật (cài đặt, lượt phát) → `promotion`; banner, bộ phối → module mới `content` (ADR-28 "một khối nội dung website trong CRM"); góp ý, hồ sơ sinh nhật → `customer`; tạm dừng nhận đơn web → `order`; hãng trên web → `fulfillment`; nhật ký → `identity` (audit). Khớp cách xếp điều phối viên chốt.
- 5.2, 5.3: `content` chỉ đọc view (`catalog.*_v`, `promotion.web_campaigns_v` mới); `promotion` đọc khách qua `customer.birthday_profiles_v`; không task nào ghi bảng module khác. Lệnh nhập (Task 467) gọi public API của từng module.
- 5.4, 5.5: tạo đơn web vẫn một transaction (M5-10); chiến dịch đọc trong cùng transaction báo giá/tạo đơn; làm mới bộ đệm theo sự kiện sau commit (`promotion.web_campaign.changed`, `content.*.changed`).
- 5.6: không có gọi ra ngoài mới (gửi Zalo/email sinh nhật vẫn ở website, QĐ40).
- 5.7: mọi bảng mới ghi RLS, khóa ghép `tenant_id`.
- 5.8: module không nhập `net/http`; handler mỏng ở `internal/httpapi`.
- 5.9: mọi đường ghi mới khai `Idempotency-Key` + quyền; xem trước dùng `x-idempotency: exempt` (QĐ12).
- 5.10: không quyết định nghiệp vụ nào dựa trên Valkey (bộ đệm chỉ cho đường đọc của website).
- 5.11: không thêm thư viện.
- 5.12: theo module mẫu (Writer, sentinel, `errors_<vùng>.go`).
- 6.1: danh sách banner/bộ phối lưu cả danh sách theo `revision` (khóa dòng `*_sets`); "tối đa 6 trang chủ" kiểm sau khi khóa `look_sets`, có test hai lần lưu song song; chiến dịch theo `revision`; góp ý, tạm dừng theo `version`.
- 6.2: khóa chống trùng cho mọi đường ghi; lệnh nhập chạy lại lần hai không đổi gì (khóa tự nhiên).
- 6.3: bảng mới xếp vào ADR-46 (đã chèn nhóm 5, 7, 10; nhóm 12d để Task 453 chèn cùng test); đường "tắt hãng cuối cùng" khóa `carrier_accounts` theo `id` tăng dần rồi `channel_carrier_settings`.
- 6.4, 6.5, 6.9: theo `db.RetryTx` của M0, không đổi.
- 6.6–6.8: lượt phát mã sinh nhật là việc nền theo lịch, một lượt mỗi lúc (khóa tư vấn); hàm chạy lại được.
- 6.10 Tiền: chiến dịch `int64` đồng, làm tròn chép đúng engine website, có bảng mẫu dùng chung và test rapid; mệnh giá quà có CHECK.
- 6.11: rapid cho giá chiến dịch và luật bộ phối.
- 6.12: góp ý, SĐT sinh nhật là dữ liệu cá nhân: không log, audit chỉ tên trường; thư mục xuất `0700`, không có token Zalo.
- 6.13: quyền mới ghi ở từng task (`content.*`, `customer.feedback.*`, `order.web_intake.manage`, `promotion.birthday.manage`, `web.feedback.write`, `web.birthdays.write`), mặc định từ chối.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| (1) Liệt kê đủ chức năng admin + dữ liệu website | bảng "Kiểm kê admin" (14 mục: 11 mục admin + đăng nhập + mục không có trong admin; mỗi mục có luật, nơi lưu, chỗ đi trong CRM, task) | `git -C apps/admin ls-tree -r --name-only origin/main` (tất cả `src/components/*.tsx`, `src/lib/*.mjs`, `gateway.mjs` đã đọc); `git -C apps/storefront show origin/main:…` (`server/admin/http.ts`, `campaigns`, `banners`, `looks`, `feedback`, `birthday`, `support`, `commerce/service.ts`) | admin `origin/main` `7199466`, website `90627d0`; dữ liệu: `commerce.sqlite` (`discount_campaigns`, `home_banners`, `home_looks`, `support_requests`), `banner-images/`, `feedback.sqlite`, `birthday.sqlite`, `admin.sqlite` (`checkoutPause`, `admin_audit`), `carrier-settings.json` |
| (2) Task mới từ 452, đăng ký bảng số | 17 tiêu đề `### Task 452 (M5-40)` … `### Task 468 (M5-56)`; 18 dòng mới trong `danh-so-task-toan-cuc.md` | `grep -cE '^### Task 4(5[2-9]\|6[0-8]) \(M5-…' m5.md`; `grep -c '\| 45[0-9] \|\|\| 46[0-8] \|' danh-so…` | `17`; `19` (gồm 451 có sẵn); dòng 450 đúng chữ giao |
| (2) Đủ các phần: schema+API CRM, màn CRM, API web, website đổi nguồn, script chuyển có chạy thử | Task 452–457, 468 (schema + API nhân viên + API web); 458–462 (màn); M5-22 mở rộng 7 mockup mới; 463–465 (website đọc CRM); 466 (xuất) + 467 (nhập, `--dry-run`, chạy thử trên bản sao dữ liệu dev) | đọc Phần H | có đủ |
| (2) Bỏ worktree `apps/admin-crm` và phần "ẩn hai mục" của M5-31 | Task 214, Cấu trúc file, Global Constraints, Việc cần chủ shop, Phần F | `grep -n admin-crm m5.md` | 4 chỗ còn lại đều là câu "không có `apps/admin-crm`" |
| (2) Phụ thuộc/Làn, migration, ADR-46, Việc cần chủ shop | bảng Làn L1/L4/L5 và 17 dòng "Cần xong trước"; migration 0520–0527; bảng "Bảng mới, migration, thứ tự khóa"; 3 dòng Việc cần chủ shop mới | `grep -cE '^\| M5-(4[0-9]\|5[0-6]) \(' m5.md` | `17` |
| ADR-46 chèn đúng luật "chỉ chèn" | `TestLockOrderGroupCodes`, `TestLockOrderFrame` (`tests/arch/lockorder_frame_test.go`) | `go test ./tests/arch/ -run 'TestLockOrder' -count=1` | `ok elimaz.com.vn/crm/tests/arch 1.060s` |
| (3) M7 đêm mở: chuyển dữ liệu + tắt admin | G15, bước 3 bảng bảy bước, đoạn "Sau bước 7", sổ tay M7-31, dòng Việc cần chủ shop | `grep -c 'Task 450' m7.md` | `6` |
| (3) Khung đêm mở vẫn đạt MO04 (sửa sau review vòng 1) | bảng bảy bước M7-25: phút 10 + 120 + 30 + 30 + 15 + 30 + 5 | cộng tay | `240` ≤ khung 240 (bản cũ 230); `drillPassed` không hỏng từ kế hoạch |
| Phụ thuộc tới task mới (sửa sau review vòng 1) | dòng M5-09 thêm `40 (452)`, M5-26 và M5-30 thêm `56 (468)` | đọc bảng "Cần xong trước" | có |
| Đợt 1 / đợt 2 | mục "Hai đợt của M5", Làn, M5-38, Ước lượng, Ngoài phạm vi | đọc | đợt 2 = M5-04, 16–21, 23, 33, 34, 36, 37 |
| Bảng Markdown không lệch cột | kiểm số `\|` mỗi dòng trong từng bảng (bỏ qua trong `` ` ``) | script Python trong scratchpad, chạy trên 4 tệp | không dòng nào lệch |
| Chỉ sửa đúng chỗ | `diff` bản sao lúc bắt đầu (scratchpad) với tệp hiện tại | `diff orig/… plans/…` | M5: 32 dòng cũ đổi (đều liệt kê ở Ảnh hưởng), 375 dòng mới; M7: 11 dòng; bảng số: 20 dòng |
| Cổng kiểm | — | `ci-task.sh` (một lần, sau khi gộp `main` `6051279`) | `CI-TASK XANH (so với điểm tách nhánh 60512794…)`; test kiến trúc `ok … 17.196s`; jscpd 0 clone; gitleaks "no leaks found" |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Lệnh giao việc bảo cập nhật "nhóm khóa ADR-46 cho bảng mới" trong kế hoạch; task còn chèn luôn bảng vào nhóm có sẵn của `docs/adr/0046-thu-tu-khoa.md` (như M5-00 đã làm cho bảng M5 cũ), riêng nhóm mới 12d để Task 453 (Q3).
- Thêm task 468 (sinh nhật đầy đủ) và 457, 462 (nhật ký) ngoài danh sách gợi ý, vì kiểm kê thấy admin có tab Cần nhắn/Cài đặt quà và màn Nhật ký mà CRM chưa có; không làm thì tắt admin sẽ mất chức năng (Q4, Q5).

## Quyết định thay chủ shop

- **Q1 — Không trái spec.** ADR-28 đã định đích "bỏ admin, mọi việc trên CRM", chỉ để ngỏ thời điểm ("chỉ thời điểm là còn điều chỉnh được") và đã cân nhắc phương án "dời hết trước ngày mở". Nên không dừng hỏi; câu chữ spec cần sửa (bảng ADR-28, 04 mục 3, 04 mục 12, ca KM04) ghi vào `de-xuat-sua-spec.md` ở M5-38 (QĐ32). Phương án khác: dừng chờ sửa spec — bỏ vì chủ shop đã nói rõ ý.
- **Q2 — Đánh số nội bộ M5-40 → M5-56** cho 452–468 (nối tiếp M5-39), số migration dải riêng 0520–0529. Vì: không xen số vào giữa task cũ, không đụng 0509+ đã giữ cho sửa chữa. Đảo: đổi số trong bảng.
- **Q3 — Không chèn nhóm 12d `content` vào ADR-46 ở task này.** `TestLockOrderGroupCodes` ghim đúng danh sách mã nhóm; thêm 12d là phải sửa test (mã), trái "chỉ tài liệu". Task 453 chèn dòng và sửa test cùng lúc (ghi trong Files của Task 453). Phương án khác: sửa test ở đây — bỏ vì task tài liệu.
- **Q4 — Sinh nhật chuyển hẳn vào CRM (QĐ40), trừ gửi tin tự động.** Tắt admin thì nhân viên mất tab Cần nhắn, Cài đặt quà; nên chọn khách, phát mã, danh sách, xuất CSV vào CRM; gửi Zalo/email tự động ở lại website vì gửi tin cho khách là P2 (`notification`). Phương án khác: giữ QĐ17 (website chọn khách) — trái "website chỉ gọi CRM lấy dữ liệu".
- **Q5 — Nhật ký = audit CRM + API đọc + màn (QĐ38); không nhập nhật ký cũ** (người làm là email, khác khóa người dùng). Xuất CSV lưu kèm bản sao lưu.
- **Q6 — Không chuyển yêu cầu về đơn hàng cũ và "Đơn cần kiểm tra" (QĐ36)**: chúng gắn với đơn Nhanh, CRM không có đơn đó (khóa ngoại của M5-12). Nhân viên đóng hết trước giờ chốt (dòng Việc cần chủ shop mới).
- **Q7 — Chiến dịch và bộ phối giữ `id` cũ khi nhập (QĐ35)** để link đã chia sẻ còn sống; chiến dịch đang chạy nhập ở trạng thái tạm dừng, nhân viên xem trước rồi bật (cần OK, đã ghi bảng Việc cần chủ shop). Phương án khác: nhập ở trạng thái bật — bỏ vì luật "bật cần bản xem trước khớp" của admin.
- **Q8 — Website chặn ghi `/api/admin/*` bằng 410 khi nguồn CRM (QĐ41)** thay cho "ẩn hai mục trong admin"; admin không sửa, tắt ở M7 **sau bước 7** (trước đó admin còn là đường lùi). Phương án khác: tắt admin ngay bước 3 — bỏ vì lùi trước bước 7 cần admin.
- **Q9 — Chuyển dữ liệu bằng tệp xuất + lệnh CLI của CRM (QĐ42)**, không mở API nhập mới. Q10 — **Bảng riêng `ord.web_intake`** thay cột trong `ord.order_settings` để hai màn không đụng `version`. Q11 — **Giữ nhóm menu "Hệ thống", "Quản trị"** làm "Cài đặt" (QĐ39), chỉ thêm "Website". Q12 — **Không màn Tổng quan** (QĐ37). Q13 — **Ảnh banner cần bộ kích thước 2400/1200**: Task 453 Step 0 chọn mở rộng `files.Purpose` (vùng rủi ro cao), ghi ở Task 453.
- **Q14 — Giữ khung đêm mở 240 phút** (review vòng 1): bước 3 chỉ tăng 20 → 30 phút; xuất dữ liệu admin và chạy khô do người thứ hai làm song song trong bước 2 (120 phút kiểm đếm), bước 3 chỉ `--apply` và bật chiến dịch. Tổng 240 ≤ khung. Phương án khác: nâng khung lên 270 — bỏ vì khung 240 là con số đã ghi trong M7 (Q của M7) và sổ tay; nâng khung là quyết định của chủ shop sau T4.

## Việc phát hiện thêm

- Số 452–468 đăng ký ngày 21/11 trong lúc Task 451 và Task 134 chạy song song: điều phối viên kiểm không có phiên nào khác lấy cùng số khi gộp bảng (bảng nằm ngoài git).
- `console/src/app/nav.ts` là tệp chung: Task 462 phải chạy sau lượt gộp console của Task 134.
- Ca KM04 trong `11-kich-ban-kiem-thu.md` còn ghi "P2, sau ngày mở": đề xuất sửa spec ở M5-38.
- Website `origin/main` có bộ sưu tập tên `flash-sale` thực chất lấy chiến dịch `clearance` (không phải flash sale của CRM): Task 463 giữ đúng tên đường dẫn, M5-33/34 (đợt 2) cần tránh trùng tên.

## Reviewer

Kết luận: ĐẠT (vòng 2, 21/11).

- Vòng 1: CẦN SỬA — (1) bước 3 của sổ đêm mở M7 lên 50 phút làm tổng bảy bước 260 > khung 240, MO04 hỏng từ kế hoạch; (2) bảng "Cần xong trước" thiếu `56 (468)` cho M5-26, M5-30 (và `40 (452)` cho M5-09). Ghi chú không bắt buộc: sót "nhánh admin" ở Việc cần chủ shop; G15 thiếu dấu ngăn; Task 467 nên ghi mỗi loại một transaction; Task 468 ghi khóa tư vấn vào ADR-46; ghi chú M5-30 phải nói rõ là phần thắng. Đã sửa hết (Q14).
- Vòng 2: ĐẠT, không phát hiện phải sửa. Ghi chú: khung đêm mở nay không còn phút dự phòng (240/240) — rủi ro đã ghi ở Q14, nâng khung là quyết định của chủ shop sau T4.
