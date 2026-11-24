// Dữ liệu của màn Quà sinh nhật. Khóa truy vấn dưới chủ đề realtime promotion.coupon (máy chủ báo hiệu khi lượt phát
// tạo đợt mã, khi mã đổi), nên báo hiệu và reloadBirthday tải lại cả màn.
import { useQuery, type QueryClient } from "@tanstack/react-query";
import { getBirthdaySettings, listBirthdayCustomers, listBirthdayTodo } from "../../api";
import { ApiError, unwrap } from "../../app/errors";
import { queryKey } from "../../app/queryClient";
import { COUPON_TOPIC } from "../ma-giam-gia/couponData";

export { headers, saveTextFile, usePerms } from "../ma-giam-gia/couponData";

/** Quyền ghi của màn (Task 468). */
export const MANAGE = "promotion.birthday.manage";
/** Trần của listBirthdayTodo. */
export const TODO_LIMIT = 500;
export const TEXT_FORBIDDEN = "Bạn không có quyền xem khuyến mãi.";

export const birthdayKey = (...parts: unknown[]) => queryKey(COUPON_TOPIC, "birthday", ...parts);
export const settingsKey = birthdayKey("settings");

export const isForbidden = (e: unknown): boolean => e instanceof ApiError && e.status === 403;

export function useGiftSettings() {
  return useQuery({ queryKey: settingsKey, queryFn: () => unwrap(getBirthdaySettings()) });
}

export function useTodo() {
  return useQuery({
    queryKey: birthdayKey("todo"),
    queryFn: () => unwrap(listBirthdayTodo({ query: { limit: TODO_LIMIT } })),
  });
}

/** Số đếm theo trạng thái của mọi khách (không lọc), cho các số tổng ở đầu trang. */
export function useTotals() {
  return useQuery({
    queryKey: birthdayKey("totals"),
    queryFn: () => unwrap(listBirthdayCustomers({ query: { limit: 1 } })),
  });
}

/** Sau một lệnh ghi: đọc lại mọi dữ liệu quà sinh nhật, đợt mã. */
export function reloadBirthday(qc: QueryClient) {
  void qc.invalidateQueries({ queryKey: queryKey(COUPON_TOPIC) });
}
