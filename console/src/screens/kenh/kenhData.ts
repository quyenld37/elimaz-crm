// Dữ liệu của màn Kênh kết nối và Cài đặt hộp chat (kế hoạch M4 Task 178). Khóa truy vấn bắt đầu bằng chủ đề realtime
// của máy chủ: tài khoản kênh "channel.status" (Task 145, 150), cài đặt Page, nhãn, mẫu "conversation.changed" (Task 160,
// 161): báo hiệu tới thì truy vấn đang mở tự tải lại (src/app/queryClient.ts). Khối đang sửa giữ giá trị nhập và
// version gốc (không bị báo hiệu đè).
import { useQuery } from "@tanstack/react-query";
import {
  getMe,
  getPageSettings,
  listAssignableUsers,
  listAutomationRules,
  listChannelAccounts,
  listConversationTags,
  listReplyTemplates,
  listTrafficSources,
} from "../../api";
import { unwrap } from "../../app/errors";
import { queryKey } from "../../app/queryClient";
import { ORDER_TOPIC } from "../don/orderData";

export const CHANNEL_TOPIC = "channel.status";
export const CONVERSATION_TOPIC = "conversation.changed";

export const CHANNELS_KEY = queryKey(CHANNEL_TOPIC, "accounts");
export const TAGS_KEY = queryKey(CONVERSATION_TOPIC, "tags");
export const TEMPLATES_KEY = queryKey(CONVERSATION_TOPIC, "templates");
export const pageSettingsKey = (pageId: string) => queryKey(CONVERSATION_TOPIC, "page-settings", pageId);
export const rulesKey = (pageId: string) => queryKey(CONVERSATION_TOPIC, "automation-rules", pageId);

/** Người đang đăng nhập (cùng khóa với khung src/shell/App.tsx nên không gọi lại /api/v1/me). */
export function useMe() {
  return useQuery({ queryKey: queryKey("identity.me"), queryFn: () => unwrap(getMe()) });
}

export function useChannels() {
  return useQuery({ queryKey: CHANNELS_KEY, queryFn: () => unwrap(listChannelAccounts()) });
}

export function usePageSettings(pageId: string) {
  return useQuery({
    queryKey: pageSettingsKey(pageId),
    queryFn: () => unwrap(getPageSettings({ path: { id: pageId } })),
  });
}

export function useAutomationRules(pageId: string) {
  return useQuery({
    queryKey: rulesKey(pageId),
    queryFn: () => unwrap(listAutomationRules({ path: { id: pageId } })),
  });
}

export function useAssignableUsers(pageId: string) {
  return useQuery({
    queryKey: queryKey(CONVERSATION_TOPIC, "assignable-users", pageId),
    queryFn: () => unwrap(listAssignableUsers({ path: { id: pageId } })),
  });
}

export function useTags() {
  return useQuery({ queryKey: TAGS_KEY, queryFn: () => unwrap(listConversationTags()) });
}

/** Mẫu trả lời: không có pageId → mẫu chung và mẫu của mọi Page đọc được (Task 161 quyết định 9). */
export function useTemplates() {
  return useQuery({ queryKey: TEMPLATES_KEY, queryFn: () => unwrap(listReplyTemplates()) });
}

/** Nguồn đơn của M2 (listTrafficSources cần order.read; cùng khóa với useSources của don): không có quyền thì không gọi. */
export function useTrafficSources(canRead: boolean) {
  return useQuery({
    queryKey: queryKey(ORDER_TOPIC, "traffic-sources"),
    queryFn: () => unwrap(listTrafficSources()),
    enabled: canRead,
  });
}
