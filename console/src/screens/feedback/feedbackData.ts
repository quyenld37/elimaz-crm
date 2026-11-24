// Dữ liệu của màn Góp ý và khối trong hồ sơ khách: khóa truy vấn (máy chủ không báo hiệu realtime cho góp ý, Task 455
// Quyết định: màn tự tải), cách thay một góp ý trong các trang đã tải sau lệnh ghi. Nội dung, liên lạc chỉ nằm trong bộ
// nhớ của React Query: không ghi log, không ghi localStorage.
import type { InfiniteData, QueryClient } from "@tanstack/react-query";
import type { Feedback, FeedbackPage } from "../../api";
import { queryKey } from "../../app/queryClient";

/** Đầu khóa truy vấn của mọi dữ liệu góp ý (không phải chủ đề realtime). */
export const FEEDBACK_KEY = "customer.feedback";
/** Số góp ý mỗi lần tải ở màn Góp ý. */
export const PAGE = 50;
/** Số góp ý mới nhất trong khối hồ sơ khách. */
export const PROFILE_PAGE = 5;

export const listKey = (q: object) => queryKey(FEEDBACK_KEY, "list", q);
/** Số góp ý theo trạng thái (thanh nhãn của màn Góp ý, Task 486). */
export const countsKey = queryKey(FEEDBACK_KEY, "counts");
/** Khối Góp ý của hồ sơ khách (một trang thường, không phải InfiniteData như listKey). */
export const customerKey = (customerId: string) => queryKey(FEEDBACK_KEY, "customer", customerId);
export const headers = (key: string) => ({ "Idempotency-Key": key });

/** Thay góp ý vừa sửa trong mọi danh sách đã tải (không tải lại cả danh sách); khối hồ sơ, số theo trạng thái đọc lại. */
export function putFeedback(qc: QueryClient, f: Feedback) {
  void qc.invalidateQueries({ queryKey: queryKey(FEEDBACK_KEY, "customer") });
  void qc.invalidateQueries({ queryKey: countsKey });
  qc.setQueriesData<InfiniteData<FeedbackPage>>({ queryKey: queryKey(FEEDBACK_KEY, "list") }, (d) =>
    d === undefined
      ? d
      : { ...d, pages: d.pages.map((p) => ({ ...p, items: p.items.map((x) => (x.id === f.id ? f : x)) })) },
  );
}

/** Đọc lại mọi danh sách góp ý (sau 409). */
export function reloadFeedback(qc: QueryClient) {
  return qc.invalidateQueries({ queryKey: queryKey(FEEDBACK_KEY) });
}
