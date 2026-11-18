# Task 54 — Màn Nhân viên và quyền

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 54, M1-54). Mockup đã duyệt: `docs/mockups/m1/nhan-vien.html` (+ `png/nhan-vien-{1512,390,360}.png`). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Task chỉ làm màn hình; API của Task 26/315
  (`listStaff`, `createStaff`, `replaceStaffGrants`, `lockStaff`, `unlockStaff`, `listRoles`, `getMe`) đã đủ.
- Gói phụ thuộc ngược (go list): không đổi mã Go.
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng. `/api/v1/me` không cần thêm tên, vai trò: tên và vai trò
  người đang sửa lấy từ dòng của chính họ trong `listStaff` (màn này chỉ mở với người có `identity.users.manage`, nên
  luôn đọc được `listStaff`). Không chạy `make gen`.
- Chỗ dùng trên màn hình: mới `console/src/features/staff/{StaffList.tsx,StaffGrants.tsx,staffRules.ts}` (+ test
  vitest); `console/src/app/screens.tsx` thêm màn "Nhân viên" (nhóm Quản trị, quyền `identity.users.manage`, đường
  `/nhan-vien`, trang con `/nhan-vien/:id/quyen`); `console/src/app/errors.ts` đổi ba câu `grant_exceeds_actor`,
  `cannot_modify_self`, `email_taken` cho khớp kế hoạch và mockup (trạng thái D); `console/src/styles.css` thêm lớp của mockup
  (bảng, nút, ô nhập, hộp thoại, chip, box, role-opt, grant-row) chép từ `docs/mockups/m1/m1.css`;
  `console/e2e/staff.spec.ts` mới.
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: `cd console && npm test` (mọi test console, gồm errors.test.ts đọc x-error-codes), `npx tsc --noEmit`,
  `npx eslint .`, `npx prettier --check .`, `npx playwright test e2e/staff.spec.ts` (Chromium, 1512 và 390) cùng
  `e2e/shell.spec.ts` (khung bị chạm vì thêm mục menu đầu tiên); `make console-check`, `make catalog-check` nếu có.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: mục `console/src/shared` của
  `docs/danh-muc-dung-chung.md` (useWriteAction, unwrap, errorMessage, ERROR_MESSAGES, queryKey, hasPermission, Screen,
  Layout); `grep -rn "dialog\|tbl\|role-opt" console/src` → chưa có hộp thoại, bảng hay lớp CSS nào của màn nghiệp vụ
  (đây là màn nghiệp vụ đầu tiên).
- Dùng lại / viết mới vì: dùng lại client sinh (`listStaff`…), `unwrap`, `useWriteAction` cho mọi yêu cầu ghi (khóa
  chống trùng, gửi lại cùng khóa), `errorMessage`/`ERROR_MESSAGES` cho câu lỗi (chỉ đổi hai câu cho khớp mockup),
  `queryKey`, `Screen`. Viết mới `staffRules.ts` (luật hiển thị của riêng màn này: vai trò nào người sửa gán được ở
  phạm vi nào, dòng của chính mình) — logic của một màn, không đưa vào `shared` vì không màn khác dùng. Hộp thoại dùng
  `<dialog>` của trình duyệt (showModal: bẫy focus, Esc) thay vì viết bẫy focus; không thêm thư viện. Lớp CSS chép
  nguyên từ `m1.css` (mockup đã duyệt), không đặt màu, cỡ chữ, bo góc mới.

## Soát thiết kế

Mục 5 (khớp kiến trúc):
- 5.1–5.7, 5.10: không áp dụng: không thêm bảng, transaction, sự kiện, lệnh ra ngoài hay truy vấn; màn chỉ gọi API.
- 5.8 Nghiệp vụ không ở màn hình: luật quyền (không tự sửa mình, không vượt quyền người gán, Chủ cuối cùng, quyền
  quản trị chỉ hiệu lực toàn shop) do API kiểm (Task 26, 315). Màn chỉ làm mờ lựa chọn chắc chắn bị từ chối cho dễ
  hiểu; mọi câu chặn của máy chủ vẫn hiện bằng `errorMessage`. Không ẩn lỗi máy chủ vì màn đã kiểm trước.
- 5.9 Đường ghi: tạo, thay grant, khóa, mở khóa đều qua `useWriteAction` (một khóa khi người dùng bấm, gửi lại cùng
  khóa); mã nhân viên và `version` nằm trong `body` của run (như hướng dẫn của hook) để khóa không dùng lại sai thao tác.
- 5.11 Thư viện: không thêm.
- 5.12 Theo mẫu: tệp đúng mục Files của kế hoạch (`console/src/features/staff/*`, `console/e2e/staff.spec.ts`), màn
  khai một dòng `SCREENS`.

Mục 6 (đúng khi chạy đồng thời):
- 6.1 Hai người sửa cùng nhân viên: `replaceStaffGrants` mang `version` đọc lúc mở; máy chủ trả `version_conflict` →
  màn hiện câu và nút "Tải lại" (mockup D). Khóa/mở khóa không mang version (hợp đồng), máy chủ tự khóa dòng.
- 6.2 Gửi lại: `useWriteAction` (bấm hai lần → một yêu cầu; lỗi mạng/bận → cùng khóa).
- 6.3–6.6, 6.8, 6.10, 6.11: không áp dụng: không database, hàng đợi, tiền.
- 6.7 Bên kia quá tải: theo `useWriteAction` (503 `retry_later`), đọc theo `shouldRetry` của query client.
- 6.9: không áp dụng: không có câu lệnh database ở màn.
- 6.12 Dữ liệu cá nhân: màn chỉ hiện email, tên nhân viên cho người có `identity.users.manage`; không log, không đưa
  email vào URL (trang con dùng mã nhân viên).
- 6.13 Quyền: mặc định từ chối giữ ở API; màn chỉ thấy được khi có `identity.users.manage` (menu, route), nút ẩn trên
  dòng của chính mình chỉ để dễ hiểu, API vẫn trả `cannot_modify_self`. Cảnh báo `admin_needs_shop_scope` của Task 315
  (gán quyền quản trị theo kho → không hiệu lực) hiện dưới dòng nhân viên và trong trang sửa quyền.
- Dữ liệu sau khi ghi: thành công → làm mới `listStaff` (invalidate khóa `identity.staff`), không tự vá bộ nhớ đệm.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Thêm nhân viên (email trùng → câu dưới ô), gán "Kho" phạm vi kho chính → danh sách cập nhật; PUT mang version và Idempotency-Key | `staff.spec.ts` "thêm nhân viên rồi gán Kho phạm vi kho chính → danh sách cập nhật" (1512, 390) | `cd console && CRM_E2E_PORT=5154 npx playwright test e2e/staff.spec.ts e2e/shell.spec.ts e2e/shell.phone.spec.ts` | `23 passed (29.0s)` |
| Quản lý gán "Chủ" → vai trò mờ kèm "Bạn không thể cấp quyền mà chính bạn không có"; máy chủ trả grant_exceeds_actor → hộp báo cùng câu | `staff.spec.ts` "Quản lý sửa quyền: vai trò Chủ mờ kèm lý do…" | như trên | qua |
| Khóa Chủ cuối cùng → "Phải còn ít nhất một Chủ đang hoạt động", trạng thái không đổi | `staff.spec.ts` "khóa Chủ cuối cùng bị chặn…" | như trên | qua |
| Dòng của chính mình không có nút sửa quyền, khóa; mở thẳng trang sửa quyền của mình → câu chặn, không có nút lưu | `staff.spec.ts` "danh sách: chính mình không có nút…", "mở trang sửa quyền của chính mình…" | như trên | qua |
| Cảnh báo Task 315 (quyền quản trị gán theo kho) hiện ở danh sách và trang sửa quyền | `staff.spec.ts` "gán Quản lý theo kho → cảnh báo…" | như trên | qua |
| version_conflict → câu "vừa được người khác sửa, tải lại" + nút Tải lại lấy bản mới | `staff.spec.ts` "người khác vừa sửa…" | như trên | qua |
| Khóa/mở khóa gửi đúng đường; menu chỉ hiện với người có identity.users.manage | `staff.spec.ts` "khóa rồi mở khóa…", "menu: mục Nhân viên…" | như trên | qua |
| Không tràn ngang ở 1512 và 390 | `noHorizontalScroll` trong mọi ca của `staff.spec.ts` | như trên | qua; ảnh 1512/390/360 đã so tay với `docs/mockups/m1/png/nhan-vien-*.png` |
| Luật hiển thị: nhãn, phạm vi, canGrant, cảnh báo | `src/features/staff/staffRules.test.ts` (11 ca) | `cd console && npm test` | `Tests  101 passed (101)` |
| Câu lỗi phủ mọi mã của hợp đồng | `src/app/errors.test.ts` | `npm test` | qua |
| Kiểu, lint, định dạng, bản dựng, gitleaks | — | `npx tsc --noEmit`, `npx eslint .`, `npx prettier --check .`, `make console-check` | `TSC_OK`, `ESLINT_OK`, "All matched files use Prettier code style!", `built in 882ms`, `no leaks found` |
| Danh mục dùng chung khớp mã | — | `make catalog-check` | không chênh |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Plan ghi "gán Kho phạm vi Kho chính": chưa có API danh sách kho nên phạm vi kho hiện bằng mã kho ("Kho <mã>"), không có
  tên "Kho chính" (xem quyết định 1). Test dùng mã kho thật dạng uuid.
- Thêm `staffRules.ts`, `staffData.tsx`, `staff.css` ngoài hai tệp của mục Files (tách luật hiển thị để test bằng vitest,
  phần dùng chung của hai trang). `e2e/fixtures.ts`: `api.on` nhận thêm RegExp (đường có mã nhân viên ở giữa).
- `console/src/styles.css` thêm lớp chung của mockup (nút, ô nhập, bảng, chip, hộp, hộp thoại `<dialog>`, thanh lưu)
  chép từ `m1.css` — màn nghiệp vụ đầu tiên; màn sau dùng lại.

## Quyết định thay chủ shop

1. **Phạm vi kho khi chưa có API danh sách kho.** Module inventory (Task 28) chưa gộp và kế hoạch không có operation
   liệt kê kho. Chọn: ô "Phạm vi" gồm "Toàn shop" và mọi kho/Page đã có trong grant của bất kỳ nhân viên nào, nhãn
   "Kho <mã>". Vì sao: không đổi hợp đồng, không đụng module của task khác, dễ thay (một hàm `scopeLabel`,
   `scopeOptions`). Phương án khác: thêm `listWarehouses` (đụng Task 28 đang chạy song song), hoặc ô nhập mã kho tay
   (hành vi mới chưa có trong mockup). Hệ quả: shop mới chưa ai có grant theo kho thì chưa gán được phạm vi kho qua màn
   (vẫn gán được Toàn shop) — ghi ở "Việc phát hiện thêm".
2. **Không đổi `/api/v1/me`.** Tên và vai trò người sửa ("Người sửa: … · …") lấy từ dòng của chính họ trong
   `listStaff` (màn chỉ mở với người có `identity.users.manage`). Phương án khác: thêm `displayName`, `roles` vào `Me`
   (đổi hợp đồng, Go, mã sinh) — chưa cần cho màn này.
3. **Cảnh báo Task 315 hiện thế nào** (mockup chưa vẽ): một dòng chữ đỏ cỡ nhỏ (lớp có sẵn `small err-text`) dưới vai
   trò bị ảnh hưởng, ở danh sách và trang sửa quyền; câu cố định của màn theo mã `admin_needs_shop_scope` kèm "vai trò ·
   phạm vi", không hiện nguyên chữ máy chủ gửi (giữ luật errors.ts: không chèn chữ từ máy chủ).
4. **Câu lỗi:** đổi `grant_exceeds_actor`, `cannot_modify_self`, `email_taken` trong `errors.ts` cho đúng câu của kế
   hoạch và mockup (trạng thái B, D). Mã `version_conflict` trên màn này dùng câu riêng của mockup ("Quyền của người
   này vừa được người khác sửa, tải lại"), các màn khác giữ câu chung.
5. **Trang sửa quyền là trang con** `/nhan-vien/:id/quyen` (mockup vẽ thẻ, không nói mở ở đâu) có dòng đường dẫn
   "Nhân viên / Sửa quyền"; lưu xong về danh sách. Mở khóa không hỏi lại (mockup chỉ vẽ xác nhận cho Khóa).
6. Làm mờ vai trò dùng luật chặt hơn máy chủ một chút (màn không biết loại quyền shop/kho): có thể mờ một lựa chọn mà
   máy chủ cho phép trong ca hiếm (người sửa có quyền loại "không phạm vi" chỉ ở một kho). Máy chủ vẫn quyết.

## Việc phát hiện thêm

- Cần operation liệt kê kho (tên, mã) đọc được với `identity.users.manage` (hoặc `inventory.read`) sau khi Task 28 gộp,
  để ô "Phạm vi" có tên kho và có cả kho chưa ai được gán; khi có thì thay `scopeOptions`/`scopeLabel` trong
  `console/src/features/staff/staffRules.ts`. Đề xuất gắn vào Task 51 (Màn Kho cũng cần danh sách kho).
- (Reviewer) Test e2e menu mới kiểm phía không có quyền; nên thêm một ca tài khoản Chủ thấy link "Nhân viên" ở task màn
  kế tiếp chạm `screens.tsx`.
- (Reviewer, cho Task 55) `roleHint` hiện câu mô tả cố định của vai trò mẫu kể cả khi vai trò đó đã bị sửa
  (`customized`); nhân viên giữ vai trò đã lưu trữ thì lưu quyền bị `invalid_input` (máy chủ từ chối mọi grant của vai
  trò không `active`) — Task 55 cần câu rõ hơn hoặc màn tự gỡ vai trò đó.
- Task 55 (Vai trò tự định nghĩa) dùng lại `roleName` (vai trò ngoài bộ mẫu hiện "(tự tạo)") và mục "Thêm vai trò".

## Reviewer

Kết luận: ĐẠT (agent `reviewer`, trên `git diff --cached` sau khi đã `git merge main` — "Already up to date").

- Phát hiện phải sửa: không có. Đã kiểm: bốn đường ghi qua `useWriteAction` có `Idempotency-Key`, mã nhân viên và
  `version` trong body; body khớp hợp đồng; không `as`/`any` trên dữ liệu API; câu lỗi khớp kế hoạch và mockup; dòng
  của chính mình không có nút, trang sửa quyền của mình bị chặn; cảnh báo `admin_needs_shop_scope` ở danh sách và trang
  sửa quyền; `version_conflict` + Tải lại; mọi lệch mockup có trong "Quyết định thay chủ shop".
- Ghi chú không bắt buộc: (1) test menu chỉ kiểm phía không có quyền; (2), (3) cho Task 55 — chép vào "Việc phát hiện
  thêm"; (4) mục Ảnh hưởng ghi "hai câu" thay vì ba — đã sửa trong tệp này.
- Reviewer không chạy được lệnh test (hook chỉ cho lệnh đọc); kết quả test lấy từ bảng Bằng chứng do phiên làm chạy.
