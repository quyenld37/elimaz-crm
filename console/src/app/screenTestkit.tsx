// Dùng trong test vitest của các màn (src/screens/<tên>/*Testkit.tsx): trả lời JSON của API giả, đọc thân yêu cầu ghi,
// dựng các màn của một route.tsx trong router bộ nhớ. Chỉ test nhập tệp này.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import type { ReactNode } from "react";
import { createMemoryRouter, RouterProvider } from "react-router";
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
