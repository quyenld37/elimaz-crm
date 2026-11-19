// Dữ liệu dùng chung của màn Khách: khóa truy vấn theo chủ đề customer.changed (cùng chủ đề với ô tìm khách của Lên
// đơn), hồ sơ, dòng thời gian, nhãn, và cách đọc lại sau một lệnh ghi.
import { useInfiniteQuery, useQuery, type QueryClient } from "@tanstack/react-query";
import { getCustomer, getCustomerTimeline, listCustomerTags, type TimelinePage } from "../../api";
import { unwrap } from "../../app/errors";
import { queryKey } from "../../app/queryClient";
import { CUSTOMER_TOPIC } from "../len-don/CustomerPicker";

export { CUSTOMER_TOPIC };
export { usePerms } from "../don/orderData";

/** Số mục mỗi trang dòng thời gian (Limit tối đa 100). */
export const TIMELINE_PAGE = 50;

export const customerKey = (id: string) => queryKey(CUSTOMER_TOPIC, "detail", id);
export const timelineKey = (id: string) => queryKey(CUSTOMER_TOPIC, "timeline", id);
export const tagsKey = () => queryKey(CUSTOMER_TOPIC, "tags");

/** Hồ sơ theo gốc của khách id (id là gốc hay hồ sơ con đều được). */
export function useCustomer(id: string) {
  return useQuery({ queryKey: customerKey(id), queryFn: () => unwrap(getCustomer({ path: { id } })) });
}

/** Dòng thời gian của cả nhóm, mới nhất trước, tải thêm theo con trỏ. */
export function useTimeline(id: string) {
  return useInfiniteQuery({
    queryKey: timelineKey(id),
    initialPageParam: "",
    getNextPageParam: (last: TimelinePage) => last.nextCursor,
    queryFn: ({ pageParam }) =>
      unwrap(
        getCustomerTimeline({
          path: { id },
          query: { limit: TIMELINE_PAGE, ...(pageParam === "" ? {} : { cursor: pageParam }) },
        }),
      ),
  });
}

export function useTags() {
  return useQuery({ queryKey: tagsKey(), queryFn: () => unwrap(listCustomerTags()) });
}

/** Sau một lệnh ghi: đọc lại mọi dữ liệu khách (hồ sơ, dòng thời gian, danh sách, nhãn). */
export function reloadCustomers(qc: QueryClient) {
  void qc.invalidateQueries({ queryKey: queryKey(CUSTOMER_TOPIC) });
}

export const headers = (key: string) => ({ "Idempotency-Key": key });
