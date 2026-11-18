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
