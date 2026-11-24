// Dùng trong test vitest của các màn (src/screens/<tên>/*Testkit.tsx): trả lời JSON của API giả, đọc thân yêu cầu ghi,
// dựng các màn của một route.tsx trong router bộ nhớ. Chỉ test nhập tệp này.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import type { ReactNode } from "react";
import { createMemoryRouter, RouterProvider } from "react-router";
import { vi } from "vitest";
import type { Grant, Me } from "../api";
import type { ScreenRoute } from "./loadScreens";

/** Một yêu cầu ghi API giả đã nhận. */
export interface Write {
  method: string;
  path: string;
  key: string | null;
  body: Record<string, unknown>;
}

export const NOT_FOUND = { error: "not_found", request_id: "t" };
export const reply = (body: unknown, status = 200) => Response.json(body, { status });

/** Trả lời GET /api/v1/me của người đang đăng nhập với các quyền grants. */
export const meReply = (grants: Grant[]) => reply({ userId: "me", email: "a@elimaz.vn", grants } satisfies Me);

/** Trang giả cho liên kết sang chi tiết đơn ("/don/:id"), dùng làm `extra` của renderScreens. */
export const ORDER_PAGE = [{ path: "/don/:id", element: <p>Trang đơn</p> }] as const;

/** Đọc thân JSON của yêu cầu ghi thành đối tượng (thân rỗng hay không phải đối tượng → {}). */
export async function bodyOf(req: Request): Promise<Record<string, unknown>> {
  const raw = await req.text();
  const v: unknown = raw === "" ? {} : JSON.parse(raw);
  return typeof v === "object" && v !== null && !Array.isArray(v) ? Object.fromEntries(Object.entries(v)) : {};
}

/** Dựng các màn `screens` ở đường `path`; `extra` là trang giả cho liên kết sang màn khác (ví dụ "/don/:id"). */
export function renderScreens(
  screens: readonly ScreenRoute[],
  path: string,
  extra: readonly { path: string; element: ReactNode }[] = [],
) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const router = createMemoryRouter([...screens.map((s) => ({ path: s.path, element: s.element })), ...extra], {
    initialEntries: [path],
  });
  const view = render(
    <QueryClientProvider client={qc}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return { qc, router, ...view };
}

/** Hàng đợi ghi của API giả: yêu cầu ghi đã nhận, câu trả lời xếp sẵn (lấy lần lượt), lời hứa giữ yêu cầu ghi lại. */
export interface WriteQueue {
  writes: Write[];
  replies: { status: number; body: unknown }[];
  /** Có thì yêu cầu ghi chờ lời hứa này rồi mới trả (xem trạng thái "đang gửi"). */
  hold: Promise<void> | null;
}

/** Hàng đợi ghi rỗng, để trải vào API giả của một màn. */
export const emptyWrites = (): WriteQueue => ({ writes: [], replies: [], hold: null });

/** Một trang của all theo tham số limit (mặc định 50) và cursor (vị trí bắt đầu) như các API danh sách. */
export function page<T>(all: readonly T[], q: URLSearchParams): { items: T[]; nextCursor?: string } {
  const limit = Number(q.get("limit") ?? "50");
  const from = Number(q.get("cursor") ?? "0");
  const items = all.slice(from, from + limit);
  return from + limit < all.length ? { items, nextCursor: String(from + limit) } : { items };
}

/**
 * Thay fetch bằng API giả fake và trả lại fake: GET → read(fake, đường, tham số); yêu cầu ghi được ghi vào fake.writes
 * (kèm Idempotency-Key), chờ fake.hold, rồi trả câu xếp sẵn đầu tiên của fake.replies hoặc write(fake, đường, thân).
 */
export function stubApi<F extends WriteQueue>(
  fake: F,
  read: (fake: F, path: string, query: URLSearchParams) => Response,
  write: (fake: F, path: string, body: Record<string, unknown>) => Response,
): F {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (req: Request) => {
      const url = new URL(req.url);
      if (req.method === "GET") return read(fake, url.pathname, url.searchParams);
      const body = await bodyOf(req);
      fake.writes.push({ method: req.method, path: url.pathname, key: req.headers.get("Idempotency-Key"), body });
      if (fake.hold !== null) await fake.hold;
      const queued = fake.replies.shift();
      return queued === undefined ? write(fake, url.pathname, body) : reply(queued.body, queued.status);
    }),
  );
  return fake;
}
