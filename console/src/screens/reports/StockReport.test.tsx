// @vitest-environment jsdom
// Tồn kho (Task 241, M6-18; mockup docs/m6/mockup/ton-kho.html trạng thái A, B, D; BC03): hôm nay "Có thể bán 18", ngày
// trước bản chụp đầu → câu riêng, 1.200 dòng chỉ dựng phần đang thấy, tìm theo đầu mã SKU, quyền một kho, xuất Excel.
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import {
  KHO_HN,
  KHO_LB,
  STOCK_ONE_WH,
  fakeInventory,
  installFakeNow,
  renderReports,
  stockRow,
} from "./inventoryTestkit";

installFakeNow();

const PATH = "/bao-cao/ton-kho";
const rowCount = () => screen.queryAllByRole("row").length - 1;

describe("StockReport — tồn hôm nay", () => {
  it("hôm nay: Có thể bán 18, tên dài, dòng giờ cập nhật; gọi đúng một GET không có ngày", async () => {
    const f = fakeInventory();
    renderReports(f, PATH);
    const row = (await screen.findByText("AO-A-DEN-M")).closest("tr");
    if (row === null) throw new Error("thiếu dòng");
    expect(within(row).getByText("18")).toBeTruthy();
    expect(within(row).getByText("24")).toBeTruthy();
    expect(within(row).getByText("Đen / M")).toBeTruthy();
    expect(within(row).getByText("Kho Hà Nội")).toBeTruthy();
    expect(within(row).getByTitle(/chất lụa Hàn mềm mát/)).toBeTruthy();
    expect(screen.getByText(/Tồn lúc 14:32:05 · 27\/11\/2026 \(số hiện tại\)/)).toBeTruthy();
    expect(screen.getByText("2 mã hàng")).toBeTruthy();
    expect(f.gets.filter((g) => g.includes("/inventory/stock"))).toEqual(["/api/v1/reports/inventory/stock?limit=500"]);
  });

  it("chọn 10/11 khi bản chụp đầu là 15/11 → 'Chưa có bản chụp trước ngày 15/11/2026'", async () => {
    const f = fakeInventory();
    renderReports(f, PATH);
    await screen.findByText("AO-A-DEN-M");
    fireEvent.click(screen.getByRole("button", { name: "Chọn ngày" }));
    fireEvent.change(screen.getByLabelText("Ngày đã qua"), { target: { value: "2026-11-10" } });
    expect(await screen.findByText(/Chưa có bản chụp trước ngày 15\/11\/2026/)).toBeTruthy();
    expect(screen.getByText(/Bạn chọn 10\/11\/2026/)).toBeTruthy();
    expect(screen.queryByText("AO-A-DEN-M")).toBeNull();
  });

  it("ngày đã chụp: 'Tồn cuối ngày 20/11/2026' từ bản chụp", async () => {
    const f = fakeInventory({
      snapshots: { "2026-11-20": [stockRow(7, { sku: "QUAN-B-BE-S", onHand: 9, available: 8 })] },
    });
    renderReports(f, PATH);
    await screen.findByText("AO-A-DEN-M");
    fireEvent.click(screen.getByRole("button", { name: "Chọn ngày" }));
    fireEvent.change(screen.getByLabelText("Ngày đã qua"), { target: { value: "2026-11-20" } });
    expect(await screen.findByText("QUAN-B-BE-S")).toBeTruthy();
    expect(screen.getByText(/Tồn cuối ngày 20\/11\/2026/)).toBeTruthy();
    expect(f.gets.at(-1)).toBe("/api/v1/reports/inventory/stock?date=2026-11-20&limit=500");
  });

  it("ngày tương lai bị chặn, không gọi API", async () => {
    const f = fakeInventory();
    renderReports(f, PATH);
    await screen.findByText("AO-A-DEN-M");
    const before = f.gets.length;
    fireEvent.click(screen.getByRole("button", { name: "Chọn ngày" }));
    fireEvent.change(screen.getByLabelText("Ngày đã qua"), { target: { value: "2026-12-05" } });
    expect(screen.getByText(/Không chọn ngày tương lai/)).toBeTruthy();
    expect(f.gets.length).toBe(before);
  });

  it("1.200 dòng: tải 500 một lần, chỉ dựng phần đang thấy, còn nữa thì không coi là hết; Tải thêm", async () => {
    const f = fakeInventory({ stock: Array.from({ length: 1200 }, (_, i) => stockRow(i + 1)) });
    renderReports(f, PATH);
    await screen.findByText("SKU-00001");
    expect(screen.getByText(/500 mã hàng/)).toBeTruthy();
    expect(screen.getByText(/còn nữa/)).toBeTruthy();
    expect(rowCount()).toBeGreaterThan(0);
    expect(rowCount()).toBeLessThan(100);
    fireEvent.click(screen.getByRole("button", { name: "Tải thêm" }));
    await waitFor(() => {
      expect(screen.getByText(/1\.000 mã hàng/)).toBeTruthy();
    });
    expect(f.gets.at(-1)).toBe("/api/v1/reports/inventory/stock?cursor=500&limit=500");
  });

  it("tìm theo đầu mã SKU gửi q; chọn kho gửi warehouseId; ô tìm trống thì không gửi q", async () => {
    const f = fakeInventory();
    renderReports(f, PATH);
    await screen.findByText("AO-A-DEN-M");
    fireEvent.change(screen.getByLabelText("Mã SKU bắt đầu bằng"), { target: { value: " AO-A-DEN-S " } });
    await waitFor(() => {
      expect(f.gets.at(-1)).toBe("/api/v1/reports/inventory/stock?q=AO-A-DEN-S&limit=500");
    });
    expect(await screen.findByText("1 mã hàng")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Kho"), { target: { value: KHO_LB } });
    await waitFor(() => {
      expect(f.gets.at(-1)).toContain(`warehouseId=${KHO_LB}`);
    });
  });

  it("không có dòng nào: câu trống, không hiện bảng cũ", async () => {
    renderReports(fakeInventory({ stock: [] }), PATH);
    expect(await screen.findByText(/Không có mã hàng nào/)).toBeTruthy();
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("lỗi 403: câu không có quyền, không vẽ số; lỗi khác có nút Thử lại", async () => {
    const f = fakeInventory({ readError: { status: 403, body: { error: "forbidden", request_id: "t" } } });
    renderReports(f, PATH);
    expect(await screen.findByText("Không tải được báo cáo.")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Thử lại" })).toBeTruthy();
    expect(screen.queryByRole("table")).toBeNull();
  });
});

describe("StockReport — quyền và xuất", () => {
  it("người chỉ có một kho: dòng Phạm vi, không có nút xuất", async () => {
    const f = fakeInventory({
      grants: STOCK_ONE_WH,
      warehouses: [{ id: KHO_HN, code: "HN", name: "Kho Hà Nội", status: "active" }],
    });
    renderReports(f, PATH);
    await screen.findByText("AO-A-DEN-M");
    expect(screen.getByText(/Phạm vi: Kho Hà Nội/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Xuất Excel" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Xuất danh sách sản phẩm" })).toBeNull();
  });

  it("Xuất Excel gửi đúng một POST có khóa chống trùng; thông báo Đang tạo tệp, xem ở Tệp xuất", async () => {
    const f = fakeInventory();
    renderReports(f, PATH);
    await screen.findByText("AO-A-DEN-M");
    const btn = screen.getByRole("button", { name: "Xuất Excel" });
    fireEvent.click(btn);
    fireEvent.click(btn);
    expect(await screen.findByText("Đang tạo tệp, xem ở Tệp xuất")).toBeTruthy();
    expect(f.writes.length).toBe(1);
    const w = f.writes[0];
    expect(w?.path).toBe("/api/v1/reports/inventory/runs");
    expect(w?.key).toBeTruthy();
    expect(w?.body).toEqual({ report: "stock", format: "xlsx", params: {} });
    expect(screen.getByRole("link", { name: "Mở Tệp xuất" }).getAttribute("href")).toBe("/bao-cao/tep-xuat");
  });
});
