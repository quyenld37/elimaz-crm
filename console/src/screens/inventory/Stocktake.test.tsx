// @vitest-environment jsdom
// Màn Kiểm kho (kế hoạch M1 Task 329): phần không có trong e2e (e2e/stocktake.phone.spec.ts): ô chọn kho chỉ có kho
// đang hoạt động mà người dùng được đếm (theo phạm vi quyền), không kho nào → câu báo; phiên không có → câu lỗi.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Grant, Me, Warehouse } from "../../api";
import { StocktakeSession, StocktakeStart, TEXT_NO_COUNT_WAREHOUSE } from "./Stocktake";

const A = "0195f3b2-0000-7000-8000-00000000a001";
const B = "0195f3b2-0000-7000-8000-00000000a002";
const C = "0195f3b2-0000-7000-8000-00000000a003";
const WAREHOUSES: Warehouse[] = [
  { id: A, code: "MAIN", name: "Kho chính", status: "active" },
  { id: B, code: "PHU", name: "Kho phụ", status: "active" },
  { id: C, code: "CU", name: "Kho cũ", status: "closed" },
];

let grants: Grant[];

const ok = (b: unknown, status = 200) =>
  new Response(JSON.stringify(b), { status, headers: { "Content-Type": "application/json" } });

beforeEach(() => {
  vi.stubGlobal(
    "fetch",
    vi.fn((input: Request) => {
      const path = new URL(input.url).pathname;
      if (path === "/api/v1/me") {
        const me: Me = { userId: "u", email: "kho@elimaz.vn", grants };
        return Promise.resolve(ok(me));
      }
      if (path === "/api/v1/inventory/warehouses") return Promise.resolve(ok(WAREHOUSES));
      if (path.startsWith("/api/v1/inventory/stocktakes/")) {
        return Promise.resolve(ok({ error: "not_found", request_id: "t" }, 404));
      }
      throw new Error(`không khai: ${input.url}`);
    }),
  );
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function renderAt(path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/kho/kiem-kho" element={<StocktakeStart />} />
          <Route path="/kho/kiem-kho/:id" element={<StocktakeSession />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

/** Mở trang bắt đầu, trả tên các kho trong ô chọn kho. */
async function warehouseOptions() {
  renderAt("/kho/kiem-kho");
  const select = await screen.findByRole("combobox", { name: "Kho" });
  return within(select)
    .getAllByRole("option")
    .map((o) => o.textContent);
}

describe("StocktakeStart", () => {
  it("chỉ kho đang hoạt động mà người dùng được đếm: phạm vi kho phụ → chỉ Kho phụ", async () => {
    grants = [
      { permission: "inventory.read", scope: { type: "all", id: "" } },
      { permission: "inventory.stocktake", scope: { type: "warehouse", id: B } },
    ];
    expect(await warehouseOptions()).toEqual(["Kho phụ"]);
    expect(screen.getByRole("button", { name: "Bắt đầu kiểm Kho phụ" })).toBeTruthy();
  });

  it("toàn shop → mọi kho đang hoạt động, kho đã đóng không có", async () => {
    grants = [{ permission: "inventory.stocktake", scope: { type: "all", id: "" } }];
    expect(await warehouseOptions()).toEqual(["Kho chính", "Kho phụ"]);
  });

  it("không được đếm kho nào → câu báo, không có nút bắt đầu", async () => {
    grants = [{ permission: "inventory.stocktake", scope: { type: "warehouse", id: C } }];
    renderAt("/kho/kiem-kho");
    expect(await screen.findByText(TEXT_NO_COUNT_WAREHOUSE)).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Bắt đầu kiểm/ })).toBeNull();
  });
});

describe("StocktakeSession", () => {
  it("phiên không có (404) → câu lỗi và đường về trang bắt đầu", async () => {
    grants = [{ permission: "inventory.stocktake", scope: { type: "all", id: "" } }];
    renderAt("/kho/kiem-kho/0195f3b2-0000-7000-8000-0000000c0009");
    expect(await screen.findByText("Không tìm thấy, có thể đã bị xóa")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Về Kiểm kho" }).getAttribute("href")).toBe("/kho/kiem-kho");
  });
});
