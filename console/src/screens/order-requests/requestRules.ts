// Luật hiển thị của màn Yêu cầu về đơn hàng (kế hoạch M5 Task 207; mockup docs/mockups/m5/order-requests.html): chữ của
// loại, trạng thái, lý do; trạng thái kế tiếp theo sơ đồ của máy chủ (openapi setOrderRequestStatus, Task 195) chỉ để
// chọn nút — máy chủ vẫn kiểm (422 invalid_transition); kiểm ô nhập theo đúng giới hạn của máy chủ.
import type { OrderRequestFile, OrderRequestKind, OrderRequestStatus } from "../../api";
import { VN_TIME_ZONE } from "../../app/time";

export const KIND_TEXT: Readonly<Record<OrderRequestKind, string>> = {
  exchange: "Đổi hàng",
  return: "Trả hàng",
  cancel: "Hủy đơn",
  question: "Hỏi về đơn",
};
export const KINDS: readonly OrderRequestKind[] = ["exchange", "return", "cancel", "question"];

/** Tiêu đề trang chi tiết: "Yêu cầu đổi hàng YC0000148". */
export const KIND_TITLE: Readonly<Record<OrderRequestKind, string>> = {
  exchange: "Yêu cầu đổi hàng",
  return: "Yêu cầu trả hàng",
  cancel: "Yêu cầu hủy đơn",
  question: "Câu hỏi về đơn",
};

export const STATUS_TEXT: Readonly<Record<OrderRequestStatus, string>> = {
  received: "Đã tiếp nhận",
  in_review: "Đang kiểm tra",
  needs_info: "Cần bổ sung",
  confirmed: "Đã xác nhận đổi",
  items_received: "Đã nhận hàng gửi về",
  resolved: "Hoàn tất",
  rejected: "Từ chối",
  cancelled: "Khách đã hủy",
};
/** Thứ tự trên thanh lọc (mockup A). */
export const STATUSES: readonly OrderRequestStatus[] = [
  "received",
  "in_review",
  "needs_info",
  "confirmed",
  "items_received",
  "resolved",
  "rejected",
  "cancelled",
];

/** Trạng thái chưa đóng: nhãn "Đang mở" và số cạnh mục menu (order.OpenRequestStatuses). */
export const OPEN_STATUSES: readonly OrderRequestStatus[] = [
  "received",
  "in_review",
  "needs_info",
  "confirmed",
  "items_received",
];

/** Lớp chip theo trạng thái (mockup A): mới tiếp nhận đỏ, đang làm viền, hoàn tất xanh, còn lại xám. */
export function statusChip(s: OrderRequestStatus): string {
  if (s === "received") return "chip chip-err";
  if (s === "resolved") return "chip chip-ok";
  if (s === "needs_info" || s === "confirmed" || s === "items_received") return "chip chip-line";
  return "chip";
}

/** Đã đóng: không đổi trạng thái, không gửi tin cho khách được nữa. */
export const isClosed = (s: OrderRequestStatus): boolean => s === "resolved" || s === "rejected" || s === "cancelled";

const REASON_TEXT: Readonly<Record<string, string>> = {
  CHANGED_MIND: "Đổi ý",
  DUPLICATE: "Đặt trùng đơn",
  ADDRESS_CHANGE: "Đổi địa chỉ",
  SIZE: "Không vừa size",
  QUALITY: "Lỗi chất lượng",
  WRONG_ITEM: "Giao nhầm hàng",
  OTHER: "Lý do khác",
};
/** Lý do cấp yêu cầu bằng chữ; mã lạ hiện nguyên. */
export const reasonText = (code: string): string => REASON_TEXT[code] ?? code;

const LINE_REASON_TEXT: Readonly<Record<string, string>> = {
  TOO_SMALL: "Chật, nhỏ",
  TOO_LARGE: "Rộng, to",
  FIT: "Không vừa dáng",
  COLOR: "Màu không như ý",
  NOT_AS_PICTURED: "Không giống ảnh",
  DEFECT: "Lỗi sản phẩm",
  WRONG_ITEM: "Giao nhầm",
  OTHER: "Khác",
};
export const lineReasonText = (code: string): string => LINE_REASON_TEXT[code] ?? code;

/** Trạng thái kế tiếp máy chủ cho (sơ đồ của setOrderRequestStatus); confirmed chỉ cho yêu cầu đổi hàng. */
export function nextStatuses(kind: OrderRequestKind, s: OrderRequestStatus): OrderRequestStatus[] {
  const confirm: OrderRequestStatus[] = kind === "exchange" ? ["confirmed"] : [];
  switch (s) {
    case "received":
      return ["in_review", "needs_info", ...confirm, "resolved", "rejected"];
    case "in_review":
      return ["needs_info", ...confirm, "resolved", "rejected"];
    case "needs_info":
      return ["in_review", "rejected"];
    case "confirmed":
      return ["items_received", "rejected"];
    case "items_received":
      return ["resolved", "rejected"];
    default:
      return [];
  }
}

/** Trạng thái đổi qua hộp thoại riêng (HV08): Cần bổ sung bắt buộc lời nhắn; Từ chối, Hoàn tất hỏi xác nhận. */
export type DialogStatus = "needs_info" | "rejected" | "resolved";
export const isDialogStatus = (s: OrderRequestStatus): s is DialogStatus =>
  s === "needs_info" || s === "rejected" || s === "resolved";

/** Giới hạn của máy chủ (Task 195): tin 5–1000 sau khi cắt khoảng trắng, lý do từ chối 1–500, mã vận đơn ≤ 60. */
export const MSG_MIN = 5;
export const MSG_MAX = 1000;
export const REASON_MAX = 500;
export const TRACKING_MAX = 60;

/** Số ký tự như máy chủ đếm (utf8.RuneCountInString): theo điểm mã, không theo UTF-16 (emoji đơn là 1). */
export const runeCount = (s: string): number => Array.from(s).length;

/** Lời nhắn: rỗng được khi không bắt buộc; có chữ thì 5–1000. null là hợp lệ. */
export function messageError(text: string, required: boolean): string | null {
  const n = runeCount(text.trim());
  if (n === 0) return required ? "Cần nhập lời nhắn cho khách." : null;
  if (n < MSG_MIN) return "Lời nhắn cần ít nhất 5 ký tự.";
  if (n > MSG_MAX) return "Lời nhắn tối đa 1000 ký tự.";
  return null;
}

export interface ShipBackInput {
  address: string;
  packageCode: string;
  /** Ngày "YYYY-MM-DD" của ô ngày. */
  deadline: string;
}

export interface ShipBackErrors {
  address: string | null;
  packageCode: string | null;
  deadline: string | null;
}

const DAY = new Intl.DateTimeFormat("en-CA", {
  timeZone: VN_TIME_ZONE,
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
});
/** Ngày hôm nay theo giờ Việt Nam, "YYYY-MM-DD". */
export const vnToday = (now: number): string => DAY.format(now);

/** Kiểm hướng dẫn gửi trả như máy chủ: địa chỉ 10–300, mã gói 3–40, hạn không trước hôm nay (giờ Việt Nam). */
export function shipBackErrors(v: ShipBackInput, today: string): ShipBackErrors {
  const a = runeCount(v.address.trim());
  const c = runeCount(v.packageCode.trim());
  return {
    address: a < 10 || a > 300 ? "Địa chỉ nhận hàng cần 10–300 ký tự." : null,
    packageCode: c < 3 || c > 40 ? "Mã ghi trên kiện cần 3–40 ký tự." : null,
    deadline: !/^\d{4}-\d{2}-\d{2}$/.test(v.deadline)
      ? "Chọn ngày hạn gửi hàng."
      : v.deadline < today
        ? "Hạn gửi hàng không được là ngày đã qua."
        : null,
  };
}

/** Link ảnh chỉ mở khi là http(s) (link ký của kho tệp riêng); khác thì không vẽ thành liên kết. */
export function safeImageUrl(f: OrderRequestFile): string | null {
  if (f.status !== "ready" || f.url === undefined) return null;
  try {
    const u = new URL(f.url);
    // Trả nguyên chuỗi máy chủ ký (URL chuẩn hóa có thể đổi cách mã hóa, làm hỏng chữ ký).
    return u.protocol === "https:" || u.protocol === "http:" ? f.url : null;
  } catch {
    return null;
  }
}
