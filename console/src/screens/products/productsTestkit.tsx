// Dữ liệu và cách dựng dùng chung cho test vitest của feature Sản phẩm (ProductList, ProductEdit). Chỉ test nhập tệp
// này; mã chạy thật không dùng.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import type { Category, Me } from "../../api";
import { queryKey } from "../../app/queryClient";

/** Tên mẫu 80 ký tự của mockup. */
export const NAME_80 = "Đầm suông công sở cổ vuông tay lỡ phối nút ngọc trai dáng dài qua gối màu be sữa";
export const VAY = "0195f3b2-0000-7000-8000-00000000c001";
export const CATS: Category[] = [
  { id: VAY, name: "Váy", slug: "vay", depth: 0, position: 1, status: "active", version: 1 },
];

/** Dựng ui dưới QueryClient (không thử lại, /api/v1/me có sẵn như khung) và MemoryRouter ở path. */
export function renderAt(me: Me, path: string, ui: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  qc.setQueryData(queryKey("identity.me"), me);
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>{ui}</MemoryRouter>
    </QueryClientProvider>,
  );
}

/** Kết quả thành công của một hàm client sinh (dạng {data, request, response}) cho vi.mocked(...).mockImplementation. */
export const ok = <T,>(data: T, status = 200) =>
  Promise.resolve({ data, request: new Request("http://x"), response: new Response(null, { status }) });

/** Kết quả lỗi API (thân {"error", "request_id"}) của một hàm client sinh. */
export const fail = (status: number, error: string) =>
  Promise.resolve({
    data: undefined,
    error: { error, request_id: "r1" },
    request: new Request("http://x"),
    response: new Response(null, { status }),
  });
