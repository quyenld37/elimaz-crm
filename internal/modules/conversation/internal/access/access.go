// Package access là phần của module conversation kiểm quyền theo Page (conversation.read, conversation.reply…, phạm vi
// page:<channel_accounts.id>, PQ04): danh sách Page người dùng được xem, kiểm quyền trên Page của đối tượng; không có
// quyền thì trả "không có" (404) để không lộ sự tồn tại. Khung của Task 143; hiện thực ở Task 151.
package access
