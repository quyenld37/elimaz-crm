// Thông báo của báo cáo chạy nền và tệp xuất (Task 239, M6-16; mockup tong-quan.html trạng thái G: .toast, .box-err).
import type { ReactNode } from "react";

export interface RunNoticeProps {
  /** info: thanh thông báo tối (đang chạy, xong); err: hộp lỗi. */
  tone?: "info" | "err";
  text: string;
  /** Nút hay liên kết đi kèm (ví dụ "Mở Tệp xuất", "Tải về"). */
  action?: ReactNode;
}

export function RunNotice({ tone = "info", text, action }: RunNoticeProps) {
  if (tone === "err") {
    return (
      <div className="box box-err stack-sm" role="alert">
        <b>{text}</b>
        {action}
      </div>
    );
  }
  return (
    <div className="toast" role="status">
      <span className="grow">{text}</span>
      {action}
    </div>
  );
}
