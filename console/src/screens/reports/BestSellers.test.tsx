// @vitest-environment jsdom
// Bán chạy /bao-cao/ban-chay (Task 240, M6-17; mockup docs/m6/mockup/ban-chay.html trạng thái A–C; BC02): 50 dòng đầu,
// "Xem thêm" tới 500, mức mẫu Áo A 7 đứng đầu và Quần B trước Váy C, cơ sở đơn thành công, tên sản phẩm 120 ký tự.
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { LONG_LEN_PRODUCT, exportExcel, fakeSales, installFakeNow, renderSales, skuRow } from "./salesTablesTestkit";

installFakeNow();

const PATH = "/bao-cao/ban-chay?ky=month";
const bestGets = (f: { gets: string[] }) => f.gets.filter((g) => g.startsWith("/api/v1/reports/sales/best-sellers?"));
const table = () => within(screen.getByRole("table", { name: "Bán chạy" }));
const dataRows = () => table().getAllByRole("row").length - 1;

/** Bấm "Xem thêm" và chờ đủ 500 dòng. */
async function expandTo500() {
  fireEvent.click(screen.getByRole("button", { name: "Xem thêm (tới 500 dòng)" }));
  await waitFor(() => {
    expect(dataRows()).toBe(500);
  });
}

describe("BestSellers — mức SKU", () => {
  it("tháng này, SKU, đơn lên chưa hủy: gọi level=sku&basis=placed&limit=50; hiện 50 dòng của 1.200 SKU", async () => {
    const f = fakeSales();
    renderSales(f, PATH);
    await screen.findByRole("table", { name: "Bán chạy" });
    expect(bestGets(f)).toEqual([
      "/api/v1/reports/sales/best-sellers?from=2026-11-01&to=2026-11-27&level=sku&basis=placed&limit=50",
    ]);
    expect(dataRows()).toBe(50);
    expect(screen.getByText("Đang hiện 50 dòng đầu / 1.200 SKU có bán trong kỳ")).toBeTruthy();
    const first = table().getByText("SKU-00001").closest("tr");
    if (first === null) throw new Error("thiếu dòng 1");
    expect(within(first).getByText("1")).toBeTruthy();
    expect(within(first).getByText("Đen / M")).toBeTruthy();
    expect(within(first).getByText("999")).toBeTruthy();
    expect(within(first).getByText("298.701.000 ₫")).toBeTruthy();
    expect(screen.getByText(/Doanh số = tiền hàng sau giảm giá, chưa gồm ship/)).toBeTruthy();
  });

  it("'Xem thêm' gửi limit=500 một lần, hiện 500 dòng, hết nút và hiện câu tối đa", async () => {
    const f = fakeSales();
    renderSales(f, PATH);
    await screen.findByRole("table", { name: "Bán chạy" });
    await expandTo500();
    expect(bestGets(f).map((g) => new URLSearchParams(g.split("?")[1]).get("limit"))).toEqual(["50", "500"]);
    expect(screen.queryByRole("button", { name: /Xem thêm/ })).toBeNull();
    expect(screen.getByText(/Đang hiện 500 dòng \(tối đa trên màn hình\)\. Tệp Excel có đủ mọi dòng\./)).toBeTruthy();
  });

  it("ít dòng hơn 50: không có nút Xem thêm", async () => {
    const f = fakeSales({ skuRows: [skuRow(1), skuRow(2)] });
    renderSales(f, PATH);
    await screen.findByRole("table", { name: "Bán chạy" });
    expect(dataRows()).toBe(2);
    expect(screen.queryByRole("button", { name: /Xem thêm/ })).toBeNull();
  });

  it("tên sản phẩm 120 ký tự: cắt bằng dấu ba chấm, có title đủ tên, bảng trong khung cuộn ngang", async () => {
    const f = fakeSales({ skuRows: [skuRow(1, { name: LONG_LEN_PRODUCT })] });
    renderSales(f, PATH);
    const name = await screen.findByTitle(LONG_LEN_PRODUCT);
    expect(LONG_LEN_PRODUCT.length).toBeGreaterThanOrEqual(120);
    expect(name.className).toContain("is-wide");
    expect(name.closest(".scroll-x")).not.toBeNull();
  });

  it("đổi 'Tính trên' sang Đơn thành công gửi basis=delivered và về 50 dòng", async () => {
    const f = fakeSales();
    renderSales(f, PATH);
    await screen.findByRole("table", { name: "Bán chạy" });
    await expandTo500();
    fireEvent.change(screen.getByLabelText("Tính trên"), { target: { value: "delivered" } });
    await waitFor(() => {
      expect(bestGets(f).at(-1)).toContain("basis=delivered&limit=50");
    });
    await waitFor(() => {
      expect(dataRows()).toBe(50);
    });
    expect(screen.getByText("ngày hoàn tất")).toBeTruthy();
  });
});

describe("BestSellers — mức mẫu", () => {
  it("Mẫu: Áo A 7 đứng đầu, Quần B trước Váy C; không có cột SKU, màu, size", async () => {
    const f = fakeSales();
    renderSales(f, PATH);
    await screen.findByRole("table", { name: "Bán chạy" });
    fireEvent.click(screen.getByRole("button", { name: "Mẫu" }));
    await screen.findByText("Áo A");
    expect(bestGets(f).at(-1)).toContain("level=product&basis=placed&limit=50");
    const names = table()
      .getAllByRole("row")
      .slice(1)
      .map((r) => within(r).getAllByRole("cell")[1]?.textContent);
    expect(names).toEqual(["Áo A", "Quần B", "Váy C"]);
    const first = table().getByText("Áo A").closest("tr");
    if (first === null) throw new Error("thiếu dòng Áo A");
    expect(within(first).getByText("7")).toBeTruthy();
    expect(within(first).getByText("1.050.000 ₫")).toBeTruthy();
    expect(screen.queryByRole("columnheader", { name: "SKU" })).toBeNull();
    expect(screen.queryByRole("columnheader", { name: "Màu / size" })).toBeNull();
    expect(screen.getByRole("columnheader", { name: "Mẫu" })).toBeTruthy();
  });
});

describe("BestSellers — trạng thái, xuất Excel, quyền", () => {
  it("không có dòng: 'Chưa bán sản phẩm nào trong kỳ'", async () => {
    renderSales(fakeSales({ skuRows: [] }), PATH);
    expect(await screen.findByText("Chưa bán sản phẩm nào trong kỳ")).toBeTruthy();
    expect(screen.queryByRole("table", { name: "Bán chạy" })).toBeNull();
  });

  it("lỗi tải: câu lỗi và nút Thử lại", async () => {
    renderSales(fakeSales({ readError: { status: 500, body: { error: "internal", request_id: "r" } } }), PATH);
    expect(await screen.findByText("Không tải được báo cáo.")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Thử lại" })).toBeTruthy();
  });

  it("xuất Excel: một POST report=best_sellers, params kỳ, mức, cơ sở (không có limit)", async () => {
    const f = fakeSales();
    renderSales(f, PATH);
    await screen.findByRole("table", { name: "Bán chạy" });
    expect(await exportExcel(f)).toEqual({
      report: "best_sellers",
      format: "xlsx",
      params: { from: "2026-11-01", to: "2026-11-27", level: "sku", basis: "placed" },
    });
  });
});
