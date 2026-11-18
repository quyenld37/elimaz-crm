// Lớp lấy dữ liệu (TanStack Query; lựa chọn: docs/chon-cong-cu-console.md) và chỗ nối với báo hiệu realtime.
//
// Quy ước khóa truy vấn: phần tử đầu là chủ đề realtime của dữ liệu (`queryKey(TOPICS.product, "list", params)`).
// Báo hiệu changed của chủ đề X → đánh dấu cũ mọi truy vấn bắt đầu bằng X, chỉ tải lại truy vấn đang mở (có màn đang
// xem). resync (nối lại, lưới an toàn 20 s) → như vậy cho mọi truy vấn. Báo hiệu không mang dữ liệu: luôn tải lại
// bằng API (spec 04 mục 6).
import { QueryClient, type QueryKey } from "@tanstack/react-query";
import type { ChangedEvent } from "../realtime/client";
import { ApiError } from "./errors";

/** Chủ đề realtime của M1 (máy chủ: realtime.Signal.Topic). */
export const TOPICS = {
  product: "catalog.product",
  balance: "inventory.balance",
  file: "files.file",
} as const;

export type Topic = (typeof TOPICS)[keyof typeof TOPICS];

/** Khóa truy vấn theo quy ước: chủ đề đứng đầu. Dữ liệu không có chủ đề realtime dùng chuỗi riêng (ví dụ "identity.me"). */
export function queryKey(topic: string, ...parts: unknown[]): QueryKey {
  return [topic, ...parts];
}

/** Thử lại truy vấn đọc: lỗi mạng và 5xx tối đa 2 lần; 4xx không (quyền, không có, sai dữ liệu không tự hết). */
export function shouldRetry(failureCount: number, error: unknown): boolean {
  if (failureCount >= 2) return false;
  return !(error instanceof ApiError && error.status < 500);
}

export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        retry: shouldRetry,
        // Dữ liệu tươi nhờ báo hiệu realtime; không tải lại chỉ vì quay lại tab hay nối mạng lại (resync lo việc đó).
        staleTime: 30_000,
        refetchOnWindowFocus: false,
        refetchOnReconnect: false,
      },
      // Đường ghi đi qua useWriteAction (tự gửi lại cùng khóa); mutation của TanStack không tự thử lại.
      mutations: { retry: false },
    },
  });
}

/** Nối onChanged của createRealtimeClient: tải lại đúng các truy vấn đang mở của chủ đề. */
export function onRealtimeChanged(qc: QueryClient, e: ChangedEvent): void {
  void qc.invalidateQueries({ queryKey: [e.topic], refetchType: "active" });
}

/** Nối onResync của createRealtimeClient: tải lại mọi truy vấn đang mở. */
export function onRealtimeResync(qc: QueryClient): void {
  void qc.invalidateQueries({ refetchType: "active" });
}
