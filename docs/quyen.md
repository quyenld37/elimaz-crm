# Bảng quyền của CRM

Bảng chuẩn duy nhất của mọi khóa quyền (kế hoạch M1, A9). Mã: `identity.Permissions` (loại phạm vi) và
`identity.DefaultRoles` (vai trò mẫu nhận) trong `internal/modules/identity`. Test `TestPermissionsDoc_MatchesCode`
đòi bảng này và mã khớp nhau từng dòng.

Luật cho mốc sau: chỉ **thêm** dòng (và thêm quyền vào `Permissions`, `DefaultRoles`); không khai lại quyền đã có,
không đổi loại phạm vi của quyền đã có.

- **Loại phạm vi** `none`: quyền không gắn với đích; có hiệu lực toàn shop khi nhân viên có quyền này trong **bất kỳ**
  gán vai trò nào (gán theo một kho, một Page cũng được; Task 315). `theo đích: <loại>`: grant `all` hoặc grant đúng
  đích (một Page, một kho) cho quyền trên đích đó. Phạm vi chỉ thu hẹp quyền theo đích.
- `GET /api/v1/me` không cần quyền nào: chỉ cần đăng nhập và tài khoản không bị khóa (`x-authenticated`, Task 315).
- **Vai trò mẫu**: `owner` Chủ, `manager` Quản lý, `sale` Bán hàng, `warehouse` Kho, `accountant` Kế toán. Vai trò
  chủ shop đã sửa (`customized`) không được thêm quyền tự động, trừ `owner` luôn đủ mọi quyền. Mỗi lần `migrate` xong,
  CRM đồng bộ vai trò mẫu cho mọi shop (`identity.SyncDefaultRoles`): shop chưa có vai trò mẫu nhận đủ; quyền mới thêm
  vào bảng này vào các vai trò mẫu chưa sửa; vai trò đã sửa và vai trò tự định nghĩa không đổi.
- Gán vai trò cho một nhân viên: người gán phải có mọi quyền của vai trò đó trên đúng phạm vi gán (quyền `none`: người
  gán có nó ở bất kỳ grant nào, vì nó có hiệu lực toàn shop với cả hai bên); không ai sửa quyền hay khóa chính mình;
  không khóa hay gỡ `owner` của Chủ còn hoạt động cuối cùng (PQ07).

| Khóa | Loại phạm vi | Mốc đặt ra | Vai trò mẫu nhận | Việc |
|---|---|---|---|---|
| `realtime.subscribe` | none | M0 | owner, manager, sale, warehouse, accountant | Mở luồng tin báo thời gian thực |
| `audit.read` | none | M0 | owner, manager | Xem nhật ký thao tác |
| `platform.jobs.manage` | none | M0 | owner | Xem, chạy lại, bỏ việc nền lỗi |
| `platform.health.read` | none | M0 | owner | Xem tình trạng hệ thống |
| `identity.users.manage` | none | M0 | owner | Quản lý nhân viên: tạo, gán vai trò, khóa, mở |
| `conversation.read` | theo đích: page | M0 | owner, manager | Đọc hội thoại của Page được giao |
| `catalog.read` | none | M1 | owner, manager, sale, warehouse, accountant | Xem sản phẩm, biến thể, giá |
| `catalog.manage` | none | M1 | owner, manager | Tạo, sửa, ngừng bán sản phẩm; danh mục, thuộc tính, nhãn |
| `catalog.import` | none | M1 | owner, manager | Nhập sản phẩm bằng tệp Excel |
| `order.create` | none | M1 | owner, manager, sale | Tạo đơn |
| `order.read` | none | M1 | owner, manager, sale, accountant | Xem đơn |
| `files.upload` | none | M1 | owner, manager | Tải ảnh, tệp lên kho tệp |
| `platform.files.manage` | none | M1 | owner | Quản trị kho tệp (khôi phục, tình trạng bản sao) |
| `inventory.suppliers.manage` | none | M1 | owner, manager, warehouse | Quản lý danh bạ nhà cung cấp |
| `identity.roles.manage` | none | M1 | owner | Tạo, sửa, lưu trữ vai trò tự định nghĩa (Task M1-56) |
| `inventory.read` | theo đích: warehouse | M1 | owner, manager, sale, warehouse, accountant | Xem tồn, sổ kho của kho được giao |
| `inventory.documents.create` | theo đích: warehouse | M1 | owner, manager, warehouse | Soạn nháp phiếu nhập, xuất, hàng lỗi |
| `inventory.documents.post` | theo đích: warehouse | M1 | owner, manager | Ghi sổ phiếu kho |
| `inventory.stocktake` | theo đích: warehouse | M1 | owner, manager, warehouse | Đếm kiểm kho |
| `inventory.stocktake.apply` | theo đích: warehouse | M1 | owner, manager | Áp chênh lệch kiểm kho vào sổ |
| `fulfillment.read` | theo đích: warehouse | M1 | owner, manager, sale, warehouse, accountant | Xem vận đơn của kho được giao |
| `fulfillment.ship` | theo đích: warehouse | M1 | owner, manager, sale, warehouse | Tạo, gửi vận đơn từ kho được giao |
