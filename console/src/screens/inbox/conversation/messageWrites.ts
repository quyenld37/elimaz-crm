// Đường ghi và khóa truy vấn của màn hội thoại (Task 175), dùng chung cho ô gõ, trạng thái gửi, thao tác comment và
// danh sách "Khách chưa trả lời" của Inbox (CH16). Mọi yêu cầu ghi qua useWriteAction: khóa chống trùng sinh khi người
// dùng bấm, giữ nguyên khi tự gửi lại (503 retry_later, 409 request_in_progress, mạng). Mã hội thoại, mã tin nằm trong
// thân của run để khóa không bị dùng lại cho đối tượng khác.
import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import { listMessages, renderReplyTemplate, sendMessage, type SendMessageRequest } from "../../../api";
import { ApiError, errorMessage, unwrap } from "../../../app/errors";
import { useWriteAction } from "../../../app/idempotency";
import { queryKey } from "../../../app/queryClient";
import { shortTime } from "../../../app/time";
import { CONVERSATION_TOPIC } from "../useConversationList";
import { DENY_TEXT, isDenyReason } from "./conversationRules";

/** Chủ đề realtime "ai đang xem" (máy chủ: conversation.TopicPresence). */
export const PRESENCE_TOPIC = "conversation.presence";

export const detailKey = (id: string) => queryKey(CONVERSATION_TOPIC, "detail", id);
export const messagesKey = (id: string) => queryKey(CONVERSATION_TOPIC, "messages", id);
export const presenceKey = (id: string) => queryKey(PRESENCE_TOPIC, id);

/** Cỡ trang tin của hội thoại. */
export const MESSAGE_PAGE = 50;

/**
 * Truy vấn trang tin mới nhất của hội thoại id: dòng tin (MessageList) và gợi ý địa chỉ của khung bên phải (Task 501)
 * dùng chung một bản đệm; báo hiệu conversation.changed đúng hội thoại này tải lại nó.
 */
export const latestMessagesQuery = (id: string) => ({
  queryKey: messagesKey(id),
  queryFn: () => unwrap(listMessages({ path: { id }, query: { limit: MESSAGE_PAGE } })),
  meta: { realtimeEntity: id },
});

/** Tải lại chi tiết và tin của hội thoại id (sau một lần ghi; báo hiệu realtime cũng tới nhưng có thể chậm). */
export function refreshConversation(qc: QueryClient, id: string): void {
  void qc.invalidateQueries({ queryKey: detailKey(id) });
  void qc.invalidateQueries({ queryKey: messagesKey(id) });
}

export const idem = (key: string) => ({ "Idempotency-Key": key });

/** Câu cho lỗi gửi: 409 send_not_allowed nói rõ lý do chính sách (denyReason) và hạn; lỗi khác câu chung. */
export function sendErrorText(err: unknown): string {
  if (err instanceof ApiError && err.code === "send_not_allowed") {
    const r = err.details.denyReason;
    const text = isDenyReason(r) ? DENY_TEXT[r] : null;
    const until = err.details.until;
    if (text !== null)
      return typeof until === "string" && r === "window_closed"
        ? `${text}. Thẻ nhân viên (Human Agent) còn trả lời được tới ${shortTime(until)}: bấm "Trả lời (nhân viên)".`
        : text;
  }
  return errorMessage(err);
}

export interface SendBody {
  conversationId: string;
  req: SendMessageRequest;
}

/** Gửi một tin (chữ hoặc một ảnh) cho khách của hội thoại. */
export function useSendMessage() {
  return useWriteAction((key, b: SendBody) =>
    unwrap(sendMessage({ path: { id: b.conversationId }, body: b.req, headers: idem(key) })),
  );
}

export interface RenderBody {
  templateId: string;
  conversationId: string;
}

/** Điền biến của mẫu trả lời cho hội thoại (POST có khóa chống trùng, không đổi dữ liệu). */
export function useRenderTemplate() {
  return useWriteAction((key, b: RenderBody) =>
    unwrap(
      renderReplyTemplate({
        path: { id: b.templateId },
        body: { conversationId: b.conversationId },
        headers: idem(key),
      }),
    ),
  );
}

/** Hook tiện: QueryClient và hàm tải lại hội thoại id. */
export function useRefresh(id: string): () => void {
  const qc = useQueryClient();
  return () => {
    refreshConversation(qc, id);
  };
}
