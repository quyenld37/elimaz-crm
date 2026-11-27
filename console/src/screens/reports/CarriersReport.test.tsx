// @vitest-environment jsdom
// Hãng vận chuyển /bao-cao/van-chuyen (Task 240, M6-17; mockup docs/m6/mockup/van-chuyen.html trạng thái A, B; BC02):
// bảng theo hãng và tài khoản, tỷ lệ "—" khi chưa vận đơn nào kết thúc, bấm tên hãng mở Báo cáo đơn nhóm theo Hãng.
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import {
  CARRIERS,
  LONG_ACCOUNT,
  ONE_PAGE,
  exportExcel,
  fakeSales,
  installFakeNow,
  renderSales,
} from "./salesTablesTestkit";

installFakeNow();

const PATH = "/bao-cao/van-chuyen?ky=7d";
const carrierGets = (f: { gets: string[] }) => f.gets.filter((g) => g.startsWith("/api/v1/reports/sales/carriers?"));
const table = () => within(screen.getByRole("table", { name: "Hãng vận chuyển" }));
const rowOf = (text: string) => {
  const row = table().getByText(text).closest("tr");
  if (row === null) throw new Error(`thiếu dòng ${text}`);
  return row;
};

describe("CarriersReport — bảng theo hãng và tài khoản", () => {
  it("gọi đúng kỳ 7 ngày; VTP 66,66%; Best '—'; Tự giao 100,00%; dòng Tổng của máy chủ", async () => {
    const f = fakeSales();
    renderSales(f, PATH);
    await screen.findByRole("table", { name: "Hãng vận chuyển" });
    expect(carrierGets(f)).toEqual(["/api/v1/reports/sales/carriers?from=2026-11-21&to=2026-11-27"]);
    const vtp = rowOf("Viettel Post — Kho chính");
    expect(within(vtp).getByText("66,66%")).toBeTruthy();
    expect(within(vtp).getByText("806.000 ₫")).toBeTruthy();
    expect(within(vtp).getByText("9.020.000 ₫")).toBeTruthy();
    const best = rowOf("Best Express — Kho 1");
    expect(within(best).getByText("—")).toBeTruthy();
    expect(within(rowOf("Shop tự giao")).getByText("100,00%")).toBeTruthy();
    const total = rowOf("Tổng");
    expect(within(total).getByText("87")).toBeTruthy();
    expect(within(total).getByText("83,58%")).toBeTruthy();
    expect(within(total).getByText("2.142.000 ₫")).toBeTruthy();
    expect(within(total).getByText("24.620 ₫")).toBeTruthy();
    expect(within(total).getByText("26.660.000 ₫")).toBeTruthy();
    expect(within(total).queryAllByRole("link")).toHaveLength(0);
  });

  it("hai tài khoản GHN hiện thành hai dòng; tên tài khoản dài cắt bằng dấu ba chấm, có title đủ tên", async () => {
    renderSales(fakeSales(), PATH);
    await screen.findByRole("table", { name: "Hãng vận chuyển" });
    expect(table().getAllByRole("link", { name: "GHN" })).toHaveLength(2);
    const name = table().getByTitle(LONG_ACCOUNT);
    expect(name.className).toContain("nm");
    expect(name.closest(".scroll-x")).not.toBeNull();
  });

  it("bấm tên hãng mở Báo cáo đơn nhóm theo Hãng, lọc đúng hãng và đúng kỳ", async () => {
    const f = fakeSales();
    const { router } = renderSales(f, PATH);
    await screen.findByRole("table", { name: "Hãng vận chuyển" });
    fireEvent.click(table().getByRole("link", { name: "Viettel Post" }));
    await screen.findByRole("table", { name: "Báo cáo đơn" });
    expect(router.state.location.pathname).toBe("/bao-cao/don");
    await waitFor(() => {
      expect(f.gets.some((g) => g.startsWith("/api/v1/reports/sales/orders?"))).toBe(true);
    });
    const q = new URLSearchParams(f.gets.find((g) => g.startsWith("/api/v1/reports/sales/orders?"))?.split("?")[1]);
    expect(q.get("groupBy")).toBe("carrier");
    expect(q.getAll("carrierKey")).toEqual(["vtp"]);
    expect(q.get("from")).toBe("2026-11-21");
    expect(q.get("to")).toBe("2026-11-27");
    expect(q.get("basis")).toBe("placed");
  });

  it("hãng tự giao lọc carrierKey=self", async () => {
    const f = fakeSales();
    const { router } = renderSales(f, PATH);
    await screen.findByRole("table", { name: "Hãng vận chuyển" });
    fireEvent.click(table().getByRole("link", { name: "Tự giao" }));
    await screen.findByRole("table", { name: "Báo cáo đơn" });
    expect(new URLSearchParams(router.state.location.search).getAll("hang")).toEqual(["self"]);
  });
});

describe("CarriersReport — trạng thái, xuất Excel, quyền", () => {
  it("không có vận đơn: 'Chưa có vận đơn nào trong kỳ', không bảng", async () => {
    const f = fakeSales({
      carriers: {
        ...CARRIERS,
        rows: [],
        totals: { ...CARRIERS.totals, created: 0, delivered: 0, returned: 0, successRateBp: null },
      },
    });
    renderSales(f, PATH);
    expect(await screen.findByText("Chưa có vận đơn nào trong kỳ")).toBeTruthy();
    expect(screen.queryByRole("table", { name: "Hãng vận chuyển" })).toBeNull();
  });

  it("lỗi tải: câu lỗi và nút Thử lại; đang tải không hiện số cũ", async () => {
    const f = fakeSales({ readError: { status: 500, body: { error: "internal", request_id: "r" } } });
    renderSales(f, PATH);
    expect(screen.getByText("Đang tải số liệu…")).toBeTruthy();
    expect(await screen.findByText("Không tải được báo cáo.")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Thử lại" })).toBeTruthy();
  });

  it("xuất Excel: một POST report=carriers, format=xlsx, params là kỳ; ẩn khi thiếu report.export", async () => {
    const f = fakeSales();
    renderSales(f, PATH);
    await screen.findByRole("table", { name: "Hãng vận chuyển" });
    expect(await exportExcel(f)).toEqual({
      report: "carriers",
      format: "xlsx",
      params: { from: "2026-11-21", to: "2026-11-27" },
    });
  });

  it("người chỉ có quyền một Page: không có nút Xuất Excel, có dòng Phạm vi", async () => {
    renderSales(fakeSales({ grants: ONE_PAGE }), PATH);
    await screen.findByRole("table", { name: "Hãng vận chuyển" });
    expect(screen.queryByRole("button", { name: "Xuất Excel" })).toBeNull();
    expect(screen.getByText(/Phạm vi: chỉ đơn của Page bạn được giao/)).toBeTruthy();
  });
});
