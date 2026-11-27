// @vitest-environment jsdom
// Nút "Xuất danh sách sản phẩm" (Task 241, M6-18; hành vi (d); mockup nut-xuat.html trạng thái C): chỉ hiện khi có
// report.inventory.read và report.export phạm vi toàn shop; hộp chọn trạng thái mẫu, đầu mã SKU; một POST có khóa.
import { cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { stubDialog } from "../don/donTestkit";
import { ProductsExportButton } from "./ProductsExportButton";
import { fakeInventory, renderUi, STOCK_ALL } from "./inventoryTestkit";

beforeEach(() => {
  stubDialog();
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const open = () => fireEvent.click(screen.getByRole("button", { name: "Xuất danh sách sản phẩm" }));

describe("ProductsExportButton", () => {
  const NONE = [
    ["thiếu report.export", [{ permission: "report.inventory.read", scope: { type: "all", id: "" } }]],
    ["thiếu report.inventory.read", [{ permission: "report.export", scope: { type: "all", id: "" } }]],
    [
      "report.inventory.read chỉ ở một kho (cần phạm vi toàn shop)",
      [
        { permission: "report.inventory.read", scope: { type: "warehouse", id: "w1" } },
        { permission: "report.export", scope: { type: "all", id: "" } },
      ],
    ],
  ] as const;
  it.each(NONE)("%s → không có nút", async (_name, grants) => {
    const f = fakeInventory({ grants: [...grants] });
    renderUi(f, <ProductsExportButton />);
    await new Promise((r) => setTimeout(r, 20));
    expect(screen.queryByRole("button", { name: "Xuất danh sách sản phẩm" })).toBeNull();
  });

  it("Xuất: đúng một POST có Idempotency-Key, report products, params status và q", async () => {
    const f = fakeInventory({ grants: STOCK_ALL });
    renderUi(f, <ProductsExportButton />);
    await screen.findByRole("button", { name: "Xuất danh sách sản phẩm" });
    open();
    fireEvent.change(screen.getByLabelText("Trạng thái mẫu"), { target: { value: "active" } });
    fireEvent.change(screen.getByLabelText("Mã SKU bắt đầu bằng"), { target: { value: "AO-" } });
    const go = screen.getByRole("button", { name: "Xuất" });
    fireEvent.click(go);
    fireEvent.click(go);
    expect(await screen.findByText("Đang tạo tệp, xem ở Tệp xuất")).toBeTruthy();
    expect(f.writes.length).toBe(1);
    expect(f.writes[0]?.path).toBe("/api/v1/reports/inventory/runs");
    expect(f.writes[0]?.key).toBeTruthy();
    expect(f.writes[0]?.body).toEqual({ report: "products", format: "xlsx", params: { status: "active", q: "AO-" } });
  });

  it("mọi trạng thái và SKU trống → params rỗng; lỗi 429 hiện câu riêng, hộp vẫn mở", async () => {
    const f = fakeInventory({ replies: [{ status: 429, body: { error: "too_many_runs", request_id: "t" } }] });
    renderUi(f, <ProductsExportButton />);
    await screen.findByRole("button", { name: "Xuất danh sách sản phẩm" });
    open();
    fireEvent.click(screen.getByRole("button", { name: "Xuất" }));
    expect(await screen.findByText(/Bạn đang có nhiều báo cáo chờ chạy/)).toBeTruthy();
    expect(f.writes[0]?.body).toEqual({ report: "products", format: "xlsx", params: {} });
    expect(screen.getByRole("button", { name: "Hủy" })).toBeTruthy();
  });
});
