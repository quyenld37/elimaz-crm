// @vitest-environment jsdom
// Xuất nhập tồn (Task 241, M6-18; mockup ton-kho.html trạng thái C; BC03): kỳ, kho, bảng đầu kỳ / nhập / nhập trả / xuất
// bán / xuất khác / kiểm kho / cuối kỳ; số âm có dấu trừ; kỳ không có phát sinh; kỳ quá dài bị chặn.
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { installFakeNow, fakeInventory, movementRow, renderReports } from "./inventoryTestkit";

installFakeNow();

const PATH = "/bao-cao/ton-kho?tab=xuat-nhap-ton";

describe("MovementsReport", () => {
  it("7 ngày: gọi đúng kỳ, hiện đủ cột, kiểm kho −1, cuối kỳ 24", async () => {
    const f = fakeInventory({ movements: [movementRow(1, { sku: "AO-A-DEN-M", productName: "Áo A" })] });
    renderReports(f, `${PATH}&ky=7d`);
    const row = (await screen.findByText("AO-A-DEN-M")).closest("tr");
    if (row === null) throw new Error("thiếu dòng");
    for (const h of ["Đầu kỳ", "Nhập", "Nhập trả", "Xuất bán", "Xuất khác", "Kiểm kho / điều chỉnh", "Cuối kỳ"]) {
      expect(screen.getByRole("columnheader", { name: h })).toBeTruthy();
    }
    expect(within(row).getByText("−1")).toBeTruthy();
    expect(within(row).getByText("24")).toBeTruthy();
    expect(within(row).getByText("30")).toBeTruthy();
    expect(f.gets.find((g) => g.includes("movements"))).toBe(
      "/api/v1/reports/inventory/movements?from=2026-11-21&to=2026-11-27&limit=500",
    );
    expect(screen.getByText(/đầu kỳ = tồn sổ lúc 00:00 ngày 21\/11/)).toBeTruthy();
  });

  it("đổi sang Hôm qua gọi lại với kỳ mới", async () => {
    const f = fakeInventory({ movements: [movementRow(1)] });
    renderReports(f, PATH);
    await screen.findByText("XNT-00001");
    fireEvent.click(screen.getByRole("button", { name: "Hôm qua" }));
    await waitFor(() => {
      expect(f.gets.at(-1)).toContain("from=2026-11-26&to=2026-11-26");
    });
  });

  it("kỳ không có dòng nào: câu 'Không có nhập, xuất trong kỳ', không bảng", async () => {
    renderReports(fakeInventory({ movements: [] }), PATH);
    expect(await screen.findByText(/Không có nhập, xuất trong kỳ/)).toBeTruthy();
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("còn nextCursor thì nói còn nữa và có Tải thêm", async () => {
    const f = fakeInventory({ movements: Array.from({ length: 600 }, (_, i) => movementRow(i + 1)) });
    renderReports(f, PATH);
    await screen.findByText("XNT-00001");
    expect(screen.getByText(/còn nữa/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Tải thêm" }));
    await waitFor(() => {
      expect(screen.getByText(/600 mã hàng/)).toBeTruthy();
    });
  });

  it("Xuất Excel gửi report stock_movements với from, to", async () => {
    const f = fakeInventory({ movements: [movementRow(1)] });
    renderReports(f, `${PATH}&ky=7d`);
    await screen.findByText("XNT-00001");
    fireEvent.click(screen.getByRole("button", { name: "Xuất Excel" }));
    await screen.findByText("Đang tạo tệp, xem ở Tệp xuất");
    expect(f.writes[0]?.body).toEqual({
      report: "stock_movements",
      format: "xlsx",
      params: { from: "2026-11-21", to: "2026-11-27" },
    });
  });
});
