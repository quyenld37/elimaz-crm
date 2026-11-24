// Chữ của màn Góp ý (mockup docs/mockups/m5/feedback.html; HV22: mức hài lòng bằng chữ). Hàm thuần, test ở
// feedbackRules.test.ts.
import type { FeedbackState, FeedbackTopic } from "../../api";

export const STATES: readonly FeedbackState[] = ["new", "seen", "answered"];
export const TOPICS: readonly FeedbackTopic[] = ["product", "delivery", "website", "service", "other"];

export const STATE_TEXT: Readonly<Record<FeedbackState, string>> = {
  new: "Mới",
  seen: "Đã xem",
  answered: "Đã phản hồi",
};

export const STATE_CHIP: Readonly<Record<FeedbackState, string>> = {
  new: "chip chip-err",
  seen: "chip",
  answered: "chip chip-ok",
};

/** Chữ của nút chuyển sang trạng thái (dòng mở). */
export const STATE_ACTION: Readonly<Record<FeedbackState, string>> = {
  new: "Đánh dấu chưa xem",
  seen: "Đánh dấu đã xem",
  answered: "Đã phản hồi khách",
};

export const TOPIC_TEXT: Readonly<Record<FeedbackTopic, string>> = {
  product: "Sản phẩm",
  delivery: "Giao hàng",
  website: "Trang web",
  service: "Tư vấn, chăm sóc",
  other: "Khác",
};

const MOOD_TEXT = ["Rất không hài lòng", "Không hài lòng", "Bình thường", "Hài lòng", "Rất hài lòng"] as const;

/** "2/5 · Không hài lòng"; không có hay ngoài 1–5 → "—". */
export function moodText(mood: number | undefined): string {
  const t = mood === undefined ? undefined : MOOD_TEXT[mood - 1];
  return mood === undefined || t === undefined ? "—" : `${String(mood)}/5 · ${t}`;
}

/** Trạng thái mà dòng mở có nút chuyển sang: mọi trạng thái khác trạng thái hiện tại (bấm nhầm sửa lại được). */
export function nextStates(s: FeedbackState): FeedbackState[] {
  return STATES.filter((x) => x !== s);
}

export const asTopic = (v: string): FeedbackTopic | "" => TOPICS.find((t) => t === v) ?? "";
