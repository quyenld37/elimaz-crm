// Truy vấn và quyền của khung bên phải Hộp chat (Task 176). Khóa truy vấn bắt đầu bằng chủ đề realtime (queryKey):
// chi tiết hội thoại, nhắc việc, cuộc gọi theo conversation.changed; thẻ khách theo order.changed (đơn mới, đơn đổi
// trạng thái làm đổi số đơn, đơn nháp). Ghi xong thì tải lại đúng truy vấn đã đổi.
import { useQuery, type QueryClient } from "@tanstack/react-query";
import { getConversation, getConversationCustomerCard, getMe, type Grant } from "../../../api";
import { unwrap } from "../../../app/errors";
import { hasPermission } from "../../../app/nav";
import { queryKey } from "../../../app/queryClient";
import { ORDER_TOPIC } from "../../len-don/OrderForm";
import { canOnPage } from "../conversation/conversationRules";
import { CONVERSATION_TOPIC } from "../useConversationList";

export const detailKey = (id: string) => queryKey(CONVERSATION_TOPIC, "detail", id);
export const cardKey = (id: string) => queryKey(ORDER_TOPIC, "conversation-card", id);
/** Cùng khóa với danh sách nhãn của màn Inbox (dùng chung bộ đệm). */
export const tagsKey = () => queryKey("conversation.tags", "list");
export const remindersKey = () => queryKey(CONVERSATION_TOPIC, "my-reminders");
export const callsKey = (customerId: string) => queryKey(CONVERSATION_TOPIC, "calls", customerId);

export function useMe() {
  return useQuery({ queryKey: queryKey("identity.me"), queryFn: () => unwrap(getMe()) });
}

/** Chi tiết hội thoại; chỉ tải lại khi báo hiệu conversation.changed đúng hội thoại này. */
export function useDetail(id: string) {
  return useQuery({
    queryKey: detailKey(id),
    meta: { realtimeEntity: id },
    queryFn: () => unwrap(getConversation({ path: { id } })),
  });
}

export function useCard(id: string) {
  return useQuery({ queryKey: cardKey(id), queryFn: () => unwrap(getConversationCustomerCard({ path: { id } })) });
}

/** Tải lại chi tiết và thẻ của hội thoại id (sau khi ghi). */
export function reloadConversation(qc: QueryClient, id: string): void {
  void qc.invalidateQueries({ queryKey: detailKey(id) });
  void qc.invalidateQueries({ queryKey: cardKey(id) });
}

/**
 * Quyền perm trên Page của hội thoại (máy chủ kiểm quyền hộp chat theo Page, PQ04): dùng chung bản của màn hội thoại
 * (Task 175). Quyền không theo Page (khách, đơn) dùng hasPermission.
 */
export { canOnPage };

/**
 * Lên đơn được ngay ở khung chat (HV28, Task 411): máy chủ đòi order.create ở kho giữ hàng (toàn shop hoặc theo kho);
 * gán theo Page không đủ. Không có grant nào như vậy → khung lên đơn khóa sẵn.
 */
export function canCreateOrder(grants: readonly Grant[]): boolean {
  return grants.some(
    (g) => g.permission === "order.create" && (g.scope.type === "all" || g.scope.type === "warehouse"),
  );
}

export { hasPermission };
