// @vitest-environment jsdom
// Màn Tồn kho (kế hoạch M1 Task 51): gửi đúng tham số listBalances (kho đầu tiên, onlyShort, q sau khi ngừng gõ),
// dòng có thể bán âm ghi rõ bằng chữ, trạng thái trống. Fetch giả trả đúng kiểu của client sinh.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Balance, BalancePage, Warehouse } from "../../api";
import { Balances, discontinued, signed, TEXT_NO_SHORT, TEXT_NO_WAREHOUSE } from "./Balances";

const W1 = "0195f3b2-0000-7000-8000-00000000a001";
const W2 = "0195f3b2-0000-7000-8000-00000000a002";

function bal(sku: string, s: Partial<Balance>): Balance {
  const b: Balance = {
    variantId: `0195f3b2-0000-7000-8000-${sku
      .padStart(12, "0")
      .slice(-12)
      .replace(/[^0-9a-f]/gi, "0")}`,
    sku,
    barcode: "2000000000015",
    productCode: sku,
    productName: "Quần âu",
    color: "Đen",
    size: "M",
    productStatus: "active",
    variantStatus: "active",
    onHand: 0,
    held: 0,
    inTransit: 0,
    damaged: 0,
    available: 0,
    ...s,
  };
  b.available = b.onHand - b.damaged - b.held - b.inTransit;
  return b;
}

let warehouses: Warehouse[];
let pages: Record<string, Balance[]>;
let calls: URLSearchParams[];

beforeEach(() => {
  warehouses = [
    { id: W1, code: "MAIN", name: "Kho chính", status: "active" },
    { id: W2, code: "PHU", name: "Kho phụ", status: "active" },
  ];
  pages = { [W1]: [bal("QU1", { onHand: 2, held: 4 }), bal("AO2", { onHand: 5 })], [W2]: [] };
  calls = [];
  vi.stubGlobal(
    "fetch",
    vi.fn((input: Request) => {
      const url = new URL(input.url);
      const body = (b: unknown) =>
        Promise.resolve(new Response(JSON.stringify(b), { headers: { "Content-Type": "application/json" } }));
      if (url.pathname === "/api/v1/inventory/warehouses") return body(warehouses);
      calls.push(url.searchParams);
      let items = pages[url.searchParams.get("warehouseId") ?? ""] ?? [];
      if (url.searchParams.get("onlyShort") === "true") items = items.filter((b) => b.available < 0);
      const page: BalancePage = { items };
      return body(page);
    }),
  );
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function renderScreen() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <Balances />
    </QueryClientProvider>,
  );
}

describe("Balances", () => {
  it("gọi tồn của kho đầu tiên; dòng âm có chữ thiếu so với phần giữ", async () => {
    renderScreen();
    const row = (await screen.findByText(/QU1 ·/)).closest("tr");
    expect(row).not.toBeNull();
    const r = within(row as HTMLElement);
    expect(r.getByText("−2")).toBeTruthy();
    expect(r.getByText("Thiếu 2 so với phần giữ")).toBeTruthy();
    expect(row?.className).toContain("is-err");
    expect(calls[0]?.get("warehouseId")).toBe(W1);
    expect(calls[0]?.get("onlyShort")).toBeNull();
  });

  it('bật lọc "Thiếu so với phần giữ" gửi onlyShort=true; kho không có dòng thiếu → câu trống và Tắt lọc', async () => {
    renderScreen();
    await screen.findByText(/AO2 ·/);
    fireEvent.click(screen.getByRole("button", { name: "Thiếu so với phần giữ" }));
    await waitFor(() => {
      expect(calls.at(-1)?.get("onlyShort")).toBe("true");
    });
    await waitFor(() => {
      expect(screen.queryByText(/AO2 ·/)).toBeNull();
    });
    fireEvent.change(screen.getByRole("combobox", { name: "Kho" }), { target: { value: W2 } });
    expect(await screen.findByText(TEXT_NO_SHORT)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Tắt lọc" }));
    expect(await screen.findByText("Kho phụ chưa có hàng")).toBeTruthy();
    expect(calls.at(-1)?.get("onlyShort")).toBeNull();
  });

  it("chỉ có kho API trả (phạm vi w1): không có lựa chọn w2, không gọi w2", async () => {
    warehouses = [{ id: W1, code: "MAIN", name: "Kho chính", status: "active" }];
    renderScreen();
    await screen.findByText(/QU1 ·/);
    const opts = within(screen.getByRole("combobox", { name: "Kho" })).getAllByRole("option");
    expect(opts.map((o) => o.textContent)).toEqual(["Kho chính"]);
    expect(calls.every((p) => p.get("warehouseId") === W1)).toBe(true);
  });

  it("không được xem kho nào: không gọi tồn", async () => {
    warehouses = [];
    renderScreen();
    expect(await screen.findByText(TEXT_NO_WAREHOUSE)).toBeTruthy();
    expect(calls).toHaveLength(0);
  });

  it("dấu trừ thật và ngừng bán", () => {
    expect(signed(-3)).toBe("−3");
    expect(signed(4)).toBe("4");
    expect(discontinued(bal("X", { variantStatus: "discontinued" }))).toBe(true);
    expect(discontinued(bal("X", { productStatus: "draft" }))).toBe(false);
  });
});
