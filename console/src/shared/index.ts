// Mã dùng chung của màn hình (luật console 6). tools/catalog đọc các export ở đây vào docs/danh-muc-dung-chung.md;
// ghi chú "khi nào không dùng" ở docs/danh-muc-ghi-chu.yaml (khóa console/src/shared).

/** Lớp realtime: báo hiệu "đối tượng vừa đổi" qua SSE, tự canh im lặng, nối lại, nhận ra phiên hết hạn. */
export { createRealtimeClient } from "../realtime/client";
export type { ChangedEvent, RealtimeClient, RealtimeOptions, RealtimeState } from "../realtime/client";
/** Canh API bằng /health/live; báo khi đổi giữa sống và tạm ngừng. */
export { watchApiLive } from "../shell/apiHealth";
export type { ApiLiveOptions } from "../shell/apiHealth";
/** Băng báo của khung theo câu chữ spec 04 mục 6. */
export { Banner, TEXT_API_DOWN, TEXT_RECONNECTING, TEXT_SESSION_EXPIRED } from "../shell/Banner";
export type { BannerProps } from "../shell/Banner";
/** Gửi yêu cầu ghi với khóa chống trùng; tự gửi lại cùng khóa khi máy chủ bận (503 retry_later, 409 request_in_progress, mạng). */
export { TEXT_BUSY, useWriteAction } from "../app/idempotency";
export type { WriteAction, WriteActionOptions, WriteResult } from "../app/idempotency";
/** Lỗi API: đổi kết quả client sinh thành dữ liệu hoặc ApiError/NetworkError; câu tiếng Việt cho mọi mã lỗi của hợp đồng. */
export {
  ApiError,
  ERROR_MESSAGES,
  NetworkError,
  TEXT_NETWORK,
  TEXT_UNKNOWN_ERROR,
  errorMessage,
  unwrap,
} from "../app/errors";
export type { ClientResult } from "../app/errors";
/** Lớp lấy dữ liệu: khóa truy vấn theo chủ đề realtime, nối báo hiệu changed/resync vào TanStack Query. */
export {
  TOPICS,
  createQueryClient,
  onRealtimeChanged,
  onRealtimeResync,
  queryKey,
  shouldRetry,
  subscribeChanged,
} from "../app/queryClient";
export type { RealtimeMeta, Topic } from "../app/queryClient";
/** Giờ hiện trên màn hình theo múi Việt Nam: "HH:mm dd/MM". */
export { VN_TIME_ZONE, shortTime } from "../app/time";
/** Menu và định tuyến: mỗi màn tự đăng ký bằng src/screens/<tên>/route.tsx (src/app/loadScreens.ts); menu chỉ hiện màn có quyền. */
export { NAV_GROUPS, allowedScreens, hasPermission, navSections } from "../app/nav";
export type { NavGroup, NavSection, Screen } from "../app/nav";
/** Khung điều hướng theo mockup M1: menu trái, thanh trên 56 px, menu trượt trên điện thoại. */
export { Layout } from "../shell/Layout";
export type { LayoutProps } from "../shell/Layout";
/** Số đếm cạnh mục menu (HV02 M3): màn khai `badge: <NavCount n={…} late={…} />` trong route.tsx. */
export { NavCount } from "../shell/NavCount";
export type { NavCountProps } from "../shell/NavCount";
/** Form lên đơn nhanh (màn /don/moi, Task 84): dùng lại nguyên khối ở M4 cạnh hội thoại; gửi đơn qua useWriteAction. */
export { OrderForm } from "../screens/len-don/OrderForm";
export type { OrderFormProps } from "../screens/len-don/OrderForm";
