// @vitest-environment jsdom
// Ô chọn nhà cung cấp có thêm nhanh (kế hoạch M1 Task 327; mockup docs/mockups/m1/kho-phieu.html trạng thái B). Gõ tên
// chưa có → dòng "+ Thêm nhà cung cấp "…"" → đúng một createSupplier, ô chọn nhận nhà cung cấp mới.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Supplier } from "../../api";
import { SEARCH_DELAY_MS } from "./Balances";
import { SupplierPicker } from "./SupplierPicker";
import { TEXT_NAME_TAKEN } from "./supplierRules";
import { sup, stubSupplierApi, type FakeSuppliers } from "./supplierTestkit";

let fake: FakeSuppliers;
let picked: (Supplier | null)[];

beforeEach(() => {
  fake = stubSupplierApi([sup("Xưởng may Hoa Sen"), sup("Vải Ninh Hiệp cô Thủy")]);
  picked = [];
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function Host({ canCreate }: { canCreate: boolean }) {
  const [v, setV] = useState<Supplier | null>(null);
  return (
    <>
      <SupplierPicker
        value={v}
        canCreate={canCreate}
        onChange={(s) => {
          picked.push(s);
          setV(s);
        }}
      />
      <output data-testid="value">{v?.name ?? "(trống)"}</output>
    </>
  );
}

function renderPicker(canCreate = true) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <Host canCreate={canCreate} />
    </QueryClientProvider>,
  );
}

const input = () => screen.getByRole("combobox", { name: "Nhà cung cấp" });

describe("SupplierPicker", () => {
  it('thêm nhanh "Xưởng Hoa": một createSupplier có khóa chống trùng, ô chọn nhận nhà cung cấp mới', async () => {
    renderPicker();
    fireEvent.change(input(), { target: { value: "Xưởng Hoa" } });
    expect(await screen.findByRole("option", { name: "Xưởng may Hoa Sen" })).toBeTruthy();
    expect(input().getAttribute("aria-expanded")).toBe("true");
    const add = await screen.findByRole("option", { name: '+ Thêm nhà cung cấp "Xưởng Hoa"' });
    fireEvent.click(add);
    fireEvent.click(add); // bấm hai lần khi đang gửi: vẫn một yêu cầu
    await waitFor(() => {
      expect(screen.getByTestId("value").textContent).toBe("Xưởng Hoa");
    });
    expect(fake.writes).toHaveLength(1);
    expect(fake.writes[0]).toMatchObject({
      method: "POST",
      path: "/api/v1/inventory/suppliers",
      body: { name: "Xưởng Hoa" },
    });
    expect(fake.writes[0]?.key).toMatch(/^[0-9a-f-]{36}$/);
    expect(picked.at(-1)?.name).toBe("Xưởng Hoa");
    expect((input() as HTMLInputElement).value).toBe("Xưởng Hoa");
    expect(input().getAttribute("aria-expanded")).toBe("false");
  });

  // Task 421: trong lúc chờ gõ (SEARCH_DELAY_MS), danh sách còn là kết quả của từ khóa cũ; tên gõ có trong danh sách hay
  // không thì chưa biết, nên chưa có dòng thêm nhanh. Trước đây dòng này hiện theo kết quả cũ rồi mất rồi hiện lại (nút
  // mới): bấm trúng nút vừa bị gỡ thì không có gì xảy ra. Đồng hồ giả chỉ cho setTimeout (hẹn chờ gõ).
  it("đang chờ gõ, danh sách còn của từ khóa cũ: chưa có dòng thêm nhanh", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    try {
      renderPicker();
      fireEvent.change(input(), { target: { value: "Xưởng Hoa" } });
      await act(async () => {
        await vi.advanceTimersByTimeAsync(SEARCH_DELAY_MS - 1);
      });
      expect(fake.reads.map((r) => r.get("q"))).toEqual([null]);
      expect(screen.getByRole("option", { name: "Xưởng may Hoa Sen" })).toBeTruthy();
      expect(screen.queryByRole("option", { name: /Thêm nhà cung cấp/ })).toBeNull();
      await act(async () => {
        await vi.advanceTimersByTimeAsync(1); // hết chờ gõ: hỏi lại với từ khóa mới
      });
      // Kết quả mới về qua vài lượt hẹn của React Query (setTimeout 0, đồng hồ giả): tua tới khi danh sách đổi.
      const addRow = () => screen.queryByRole("option", { name: '+ Thêm nhà cung cấp "Xưởng Hoa"' });
      for (let i = 0; i < 100 && addRow() === null; i++) {
        await act(async () => {
          await vi.advanceTimersByTimeAsync(1);
        });
      }
      expect(addRow()).toBeTruthy();
      expect(fake.reads.map((r) => r.get("q"))).toEqual([null, "Xưởng Hoa"]);
      expect(screen.queryByRole("option", { name: "Xưởng may Hoa Sen" })).toBeNull();
    } finally {
      vi.useRealTimers();
    }
  });

  it("chọn nhà cung cấp có sẵn: không ghi gì; gõ lại thì bỏ lựa chọn cũ", async () => {
    renderPicker();
    fireEvent.change(input(), { target: { value: "hoa" } });
    fireEvent.click(await screen.findByRole("option", { name: "Xưởng may Hoa Sen" }));
    expect(screen.getByTestId("value").textContent).toBe("Xưởng may Hoa Sen");
    expect(fake.writes).toHaveLength(0);
    fireEvent.change(input(), { target: { value: "Xưởng may Hoa" } });
    expect(screen.getByTestId("value").textContent).toBe("(trống)");
  });

  it("gõ đúng tên đã có: không có dòng thêm nhanh", async () => {
    renderPicker();
    fireEvent.change(input(), { target: { value: "Xưởng may Hoa Sen " } });
    await screen.findByRole("option", { name: "Xưởng may Hoa Sen" });
    expect(screen.queryByRole("option", { name: /Thêm nhà cung cấp/ })).toBeNull();
  });

  it("không có quyền quản lý nhà cung cấp: không có dòng thêm nhanh", async () => {
    renderPicker(false);
    fireEvent.change(input(), { target: { value: "Xưởng Hoa" } });
    await screen.findByRole("option", { name: "Xưởng may Hoa Sen" });
    expect(screen.queryByRole("option", { name: /Thêm nhà cung cấp/ })).toBeNull();
  });

  it("máy chủ báo trùng tên (nhà cung cấp đã lưu trữ cùng tên): câu dưới ô, không chọn gì", async () => {
    renderPicker();
    fake.failNext = { status: 409, code: "supplier_name_taken" };
    fireEvent.change(input(), { target: { value: "Xưởng Hoa" } });
    fireEvent.click(await screen.findByRole("option", { name: '+ Thêm nhà cung cấp "Xưởng Hoa"' }));
    expect(await screen.findByText(TEXT_NAME_TAKEN)).toBeTruthy();
    expect(screen.getByTestId("value").textContent).toBe("(trống)");
    expect(input().getAttribute("aria-invalid")).toBe("true");
  });

  it("bên gọi truyền nhà cung cấp sau khi ô đã hiện (phiếu nháp tải sau): ô hiện tên đó", () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const s = sup("Xưởng may Hoa Sen");
    const ui = (v: Supplier | null) => (
      <QueryClientProvider client={qc}>
        <SupplierPicker value={v} canCreate onChange={() => undefined} />
      </QueryClientProvider>
    );
    const { rerender } = render(ui(null));
    expect((input() as HTMLInputElement).value).toBe("");
    rerender(ui(s));
    expect((input() as HTMLInputElement).value).toBe("Xưởng may Hoa Sen");
  });

  it("Esc đóng danh sách", async () => {
    renderPicker();
    fireEvent.change(input(), { target: { value: "hoa" } });
    await screen.findByRole("option", { name: "Xưởng may Hoa Sen" });
    act(() => {
      fireEvent.keyDown(input(), { key: "Escape" });
    });
    expect(screen.queryByRole("listbox")).toBeNull();
  });
});
