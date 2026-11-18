// Băng báo của khung, câu chữ theo spec 04 mục 6. Phiên hết hạn che các băng khác: tải lại trang là việc duy nhất
// nhân viên cần làm lúc đó.
import type { RealtimeState } from "../realtime/client";

export const TEXT_RECONNECTING = "Mất kết nối, đang nối lại";
export const TEXT_SESSION_EXPIRED = "Phiên đã hết hạn, tải lại trang";
export const TEXT_API_DOWN = "Hệ thống tạm ngừng, đang thử lại";

export interface BannerProps {
  /** Trạng thái lớp realtime; null khi chưa nối lần nào. */
  realtime: RealtimeState | null;
  /** /health/live không trả lời. */
  apiDown: boolean;
}

export function Banner({ realtime, apiDown }: BannerProps) {
  if (realtime === "session_expired") {
    return (
      <div className="banners">
        <p className="banner banner-err" role="alert">
          {TEXT_SESSION_EXPIRED}
        </p>
      </div>
    );
  }
  if (!apiDown && realtime !== "reconnecting") return null;
  return (
    <div className="banners">
      {apiDown && (
        <p className="banner banner-soft" role="status">
          {TEXT_API_DOWN}
        </p>
      )}
      {realtime === "reconnecting" && (
        <p className="banner banner-info" role="status">
          {TEXT_RECONNECTING}
        </p>
      )}
    </div>
  );
}
