// Số yêu cầu đang mở cạnh mục menu "Yêu cầu về đơn hàng" (HV02 của mockup M5 order-requests.html, Task 486): đọc
// countOrderRequests, tải lại theo báo hiệu order_request.changed (cùng khóa với thanh nhãn của danh sách). Chưa đọc
// được số (đang tải, lỗi) → không vẽ gì.
import { NavCount } from "../../shared";
import { useRequestCounts } from "./requestData";

export function RequestNavCount() {
  const counts = useRequestCounts();
  return <NavCount n={counts.data?.open ?? 0} />;
}
