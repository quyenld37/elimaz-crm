// @vitest-environment jsdom
// Tệp xuất và chạy nền (Task 241, M6-18; mockup tep-xuat.html trạng thái A–C; BC04): ba nhóm theo quyền, trạng thái bằng
// chữ, Tải về chỉ ở dòng Xong dạng Excel (gọi …/file rồi mở url), Xem cho dạng bảng, tự tải lại khi có báo hiệu
// report.run_finished, chấm số tệp mới ở menu.
import { act, cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { onRealtimeChanged } from "../../app/queryClient";
import {
  EXPORT_CUSTOMERS,
  STOCK_ALL,
  fakeInventory,
  installFakeNow,
  makeRun,
  renderReports,
  renderUi,
  type InventoryFake,
} from "./inventoryTestkit";
import { ExportsNavCount, SEEN_KEY } from "./ExportsNavCount";
import { opener } from "./openFile";

installFakeNow();
beforeEach(() => {
  localStorage.clear();
});
afterEach(() => {
  vi.restoreAllMocks();
});

const PATH = "/bao-cao/tep-xuat";
const LONG = "Trần Nguyễn Hoàng Bảo Bình — trưởng ca live tối thứ Bảy và Chủ nhật, phụ trách Page ELIMAZ Công Sở";

function threeRuns(): InventoryFake {
  return fakeInventory({
    runs: {
      sales: [
        makeRun("r-ready", {
          report: "orders",
          format: "xlsx",
          status: "ready",
          rowCount: 96,
          params: { from: "2026-11-21", to: "2026-11-27" },
          requestedByName: LONG,
          finishedAt: "2026-11-27T07:10:00Z",
          expiresAt: "2026-12-04T07:10:00Z",
        }),
        makeRun("r-heavy", {
          report: "best_sellers",
          format: "xlsx",
          status: "failed",
          reason: "too_heavy",
          params: { from: "2025-01-01", to: "2025-12-31" },
        }),
        makeRun("r-old", {
          report: "orders",
          format: "xlsx",
          status: "expired",
          params: { from: "2026-10-01", to: "2026-10-31" },
          expiresAt: "2026-11-08T03:00:00Z",
        }),
      ],
      inventory: [],
      customers: [],
    },
  });
}

const rowOf = (text: RegExp) => {
  const r = screen.getAllByRole("row").find((x) => text.test(x.textContent));
  if (r === undefined) throw new Error(`thiếu dòng ${String(text)}`);
  return r;
};

describe("RunsPage", () => {
  it("ba lần chạy (Xong, Lỗi quá nặng, Hết hạn): Tải về chỉ ở dòng Xong; chữ trạng thái đúng", async () => {
    renderReports(threeRuns(), PATH);
    await screen.findByText("Xong");
    expect(screen.getAllByRole("button", { name: "Tải về" }).length).toBe(1);
    expect(within(rowOf(/Xong/)).getByRole("button", { name: "Tải về" })).toBeTruthy();
    expect(within(rowOf(/Lỗi: quá nặng/)).queryByRole("button")).toBeNull();
    expect(within(rowOf(/Hết hạn/)).queryByRole("button")).toBeNull();
    expect(within(rowOf(/Xong/)).getByText("96")).toBeTruthy();
    expect(within(rowOf(/Xong/)).getByText(/04\/12\/2026 14:10/)).toBeTruthy();
    expect(within(rowOf(/Xong/)).getByText("21/11/2026 – 27/11/2026")).toBeTruthy();
    expect(within(rowOf(/Xong/)).getByTitle(LONG)).toBeTruthy();
  });

  it("đủ các chữ trạng thái: Đang chờ, Đang chạy, bận, không đủ quyền, lỗi chung", async () => {
    const f = fakeInventory({
      runs: {
        sales: [
          makeRun("a", { report: "orders", format: "xlsx", status: "queued" }),
          makeRun("b", { report: "carriers", format: "xlsx", status: "running" }),
          makeRun("c", { report: "dashboard", format: "xlsx", status: "failed", reason: "busy" }),
          makeRun("d", { report: "orders", format: "xlsx", status: "failed", reason: "forbidden" }),
          makeRun("e", { report: "dashboard", format: "xlsx", status: "failed", reason: "error" }),
        ],
        inventory: [],
        customers: [],
      },
    });
    renderReports(f, PATH);
    await screen.findByText("Đang chờ");
    for (const t of ["Đang chạy", "Lỗi: hệ thống bận, thử lại sau", "Lỗi: không đủ quyền"]) {
      expect(screen.getByText(t)).toBeTruthy();
    }
    expect(screen.getByText("Lỗi")).toBeTruthy();
  });

  it("bấm Tải về: gọi …/file của đúng họ rồi mở url", async () => {
    const open = vi.spyOn(opener, "open").mockImplementation(() => undefined);
    const f = threeRuns();
    renderReports(f, PATH);
    fireEvent.click(await screen.findByRole("button", { name: "Tải về" }));
    await waitFor(() => {
      expect(open).toHaveBeenCalledWith("https://files.example.test/reports/run-1.xlsx");
    });
    expect(f.gets).toContain("/api/v1/reports/sales/runs/r-ready/file?");
  });

  it("đường tải không phải http(s) thì không mở và nói lỗi", async () => {
    const open = vi.spyOn(opener, "open").mockImplementation(() => undefined);
    const f = threeRuns();
    f.fileUrl = "javascript:alert(1)";
    renderReports(f, PATH);
    fireEvent.click(await screen.findByRole("button", { name: "Tải về" }));
    expect(await screen.findByText(/Đường dẫn tải không hợp lệ/)).toBeTruthy();
    expect(open).not.toHaveBeenCalled();
  });

  it("lần chạy dạng bảng đã xong có nút Xem tới màn báo cáo với đúng kỳ; không có Tải về", async () => {
    const f = fakeInventory({
      runs: {
        sales: [
          makeRun("j", {
            report: "dashboard",
            format: "json",
            status: "ready",
            params: { from: "2026-01-01", to: "2026-11-27" },
          }),
        ],
        inventory: [],
        customers: [],
      },
    });
    renderReports(f, PATH);
    const link = await screen.findByRole("link", { name: "Xem" });
    expect(link.getAttribute("href")).toBe("/bao-cao/tong-quan?ky=custom&tu=2026-01-01&den=2026-11-27");
    expect(screen.queryByRole("button", { name: "Tải về" })).toBeNull();
  });

  it("nhóm theo quyền: chỉ bán hàng → không có nhóm tồn và khách, chỉ gọi danh sách bán hàng", async () => {
    const f = threeRuns();
    f.grants = [{ permission: "report.sales.read", scope: { type: "all", id: "" } }];
    renderReports(f, PATH);
    await screen.findByText("Xong");
    expect(screen.getByRole("heading", { name: "Báo cáo bán hàng" })).toBeTruthy();
    expect(screen.queryByRole("heading", { name: "Báo cáo tồn kho" })).toBeNull();
    expect(screen.queryByRole("heading", { name: "Danh sách khách" })).toBeNull();
    expect(f.gets.some((g) => g.includes("/inventory/runs"))).toBe(false);
    expect(f.gets.some((g) => g.includes("/customers/exports"))).toBe(false);
  });

  it("ba nhóm khi đủ quyền; Tải về của tệp tồn và khách gọi đúng đường của họ", async () => {
    const open = vi.spyOn(opener, "open").mockImplementation(() => undefined);
    const f = fakeInventory({
      grants: [...STOCK_ALL, ...EXPORT_CUSTOMERS],
      runs: {
        sales: [],
        inventory: [
          makeRun("i1", { family: "inventory", report: "products", format: "xlsx", status: "ready", rowCount: 1200 }),
        ],
        customers: [
          makeRun("c1", {
            family: "customers",
            report: "customers",
            format: "xlsx",
            status: "ready",
            rowCount: 842,
            params: { tagIds: ["0195f3b2-0000-7000-8000-00000000a0a1"], minOrders: 2 },
          }),
        ],
      },
    });
    renderReports(f, PATH);
    expect(await screen.findByRole("heading", { name: "Báo cáo tồn kho" })).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Danh sách khách" })).toBeTruthy();
    expect(await screen.findByText(/Nhãn: Khách VIP/)).toBeTruthy();
    expect(screen.getByText(/Từ 2 đơn/)).toBeTruthy();
    for (const b of screen.getAllByRole("button", { name: "Tải về" })) fireEvent.click(b);
    await waitFor(() => {
      expect(open).toHaveBeenCalledTimes(2);
    });
    expect(f.gets).toContain("/api/v1/reports/inventory/runs/i1/file?");
    expect(f.gets).toContain("/api/v1/reports/customers/exports/c1/file?");
  });

  it("chưa có lần chạy nào: câu trống; lỗi tải: câu lỗi và Thử lại", async () => {
    const f = fakeInventory();
    renderReports(f, PATH);
    expect(await screen.findAllByText("Chưa có tệp xuất nào")).toBeTruthy();
    cleanup();
    const g = fakeInventory({ listError: { sales: { status: 500, body: { error: "internal", request_id: "t" } } } });
    renderReports(g, PATH);
    expect(await screen.findByText("Không tải được danh sách tệp xuất.")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Thử lại" })).toBeTruthy();
  });

  it("báo hiệu report.run_finished → danh sách tải lại và lần chạy hiện Xong", async () => {
    const f = fakeInventory({
      runs: {
        sales: [makeRun("r1", { report: "orders", format: "xlsx", status: "running" })],
        inventory: [],
        customers: [],
      },
    });
    const { qc } = renderReports(f, PATH);
    await screen.findByText("Đang chạy");
    f.runs.sales = [makeRun("r1", { report: "orders", format: "xlsx", status: "ready", rowCount: 5 })];
    act(() => {
      onRealtimeChanged(qc, { topic: "report.run_finished", entity: { type: "report_run", id: "r1" } });
    });
    expect(await screen.findByText("Xong")).toBeTruthy();
  });
});

describe("ExportsNavCount — chấm số tệp mới ở menu", () => {
  const seen = (iso: string) => {
    localStorage.setItem(SEEN_KEY, iso);
  };

  it("đếm lần chạy xong sau lần cuối mở Tệp xuất; chưa mở lần nào thì không đếm tệp cũ", async () => {
    const f = threeRuns();
    seen("2026-11-27T06:00:00Z");
    renderUi(f, <ExportsNavCount />);
    expect(await screen.findByText("1")).toBeTruthy();
    cleanup();
    localStorage.clear();
    renderUi(f, <ExportsNavCount />);
    await new Promise((r) => setTimeout(r, 30));
    expect(screen.queryByText("1")).toBeNull();
  });

  it("mở trang Tệp xuất → mốc đã xem về bây giờ", async () => {
    seen("2026-11-27T06:00:00Z");
    renderReports(threeRuns(), PATH);
    await screen.findByText("Xong");
    expect(localStorage.getItem(SEEN_KEY)).toBe("2026-11-27T07:32:05.000Z");
  });

  it("không có quyền xem nhóm nào → không gọi API", async () => {
    const f = threeRuns();
    f.grants = [];
    renderUi(f, <ExportsNavCount />);
    await new Promise((r) => setTimeout(r, 30));
    expect(f.gets.length).toBe(0);
  });
});
