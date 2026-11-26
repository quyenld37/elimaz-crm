// Chữ và phép đổi số của khung bên phải Hộp chat (Task 176). Bảng kết quả cuộc gọi dùng chung với dòng thời gian khách
// (C7, src/screens/khach/NotesPanel.tsx): một nguồn cho một tên.
import type { CallOutcome } from "../../../api";

/** Sáu kết quả cuộc gọi theo thứ tự của hộp thoại (mockup khung-ben-phai.html trạng thái C, HV30). */
export const CALL_OUTCOMES: readonly CallOutcome[] = [
  "closed",
  "no_answer",
  "callback",
  "wrong_number",
  "declined",
  "other",
];

export const CALL_OUTCOME_TEXT: Readonly<Record<CallOutcome, string>> = {
  closed: "Đã chốt",
  no_answer: "Không nghe máy",
  callback: "Hẹn gọi lại",
  wrong_number: "Sai số",
  declined: "Khách từ chối",
  other: "Khác",
};

/** Chữ của một kết quả từ máy chủ (chuỗi lạ giữ nguyên, không vỡ màn khi máy chủ thêm kết quả). */
const OUTCOME_BY_KEY: ReadonlyMap<string, string> = new Map(Object.entries(CALL_OUTCOME_TEXT));

export function outcomeText(outcome: string): string {
  return OUTCOME_BY_KEY.get(outcome) ?? outcome;
}

/** Loại hội thoại trong dòng thời gian khách. */
export function conversationKindText(kind: string | undefined): string {
  return kind === "comment" ? "Hội thoại comment" : "Hội thoại tin nhắn";
}

/**
 * Phần trăm nguyên làm tròn xuống của tỷ lệ phần vạn (Stats.ReturnRateBP của M2: hoàn / (giao + hoàn); không tính lại).
 * 2000 → 20; 1250 → 12.
 */
export function floorPercent(bp: number): number {
  return Math.floor(bp / 100);
}

/** Giờ địa phương của ô datetime-local ("2026-11-27T15:00") → ISO UTC; ô rỗng hay sai → null. */
export function localInputToIso(v: string): string | null {
  if (v === "") return null;
  const t = new Date(v).getTime();
  return Number.isNaN(t) ? null : new Date(t).toISOString();
}
