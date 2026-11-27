// @vitest-environment jsdom
// Báo cáo đơn /bao-cao/don (Task 240, M6-17; mockup docs/m6/mockup/don.html trạng thái A–F; BC02): số của kịch bản chín
// đơn, bấm ô số mở danh sách chi tiết, cơ sở ngày hoàn tất ẩn cột null, bộ lọc nhiều lựa chọn, chạy nền, xuất Excel.
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import {
  AN,
  BACKGROUND,
  LONG_NAME,
  ONE_PAGE,
  P1,
  P2,
  fakeSales,
  installFakeNow,
  renderSales,
  exportExcel,
  settleRun,
  CARE_BY,
} from "./salesTablesTestkit";

installFakeNow();

const PATH = "/bao-cao/don";
const ordersGets = (f: { gets: string[] }) => f.gets.filter((g) => g.startsWith("/api/v1/reports/sales/orders?"));
const rowOf = (text: string) => {
  const row = within(screen.getByRole("table", { name: "Báo cáo đơn" }))
    .getByText(text)
    .closest("tr");
  if (row === null) throw new Error(`thiếu dòng ${text}`);
  return row;
};
const asQuery = (get: string | undefined) => new URLSearchParams((get ?? "").split("?")[1] ?? "");

describe("OrdersReport — số liệu kịch bản chín đơn", () => {
  it("nhóm Nhân viên phụ trách: An 4 / Bình 3 / Chưa phân công 1, dòng Tổng 8, 1.910.000, TB 272.857, 66,66%", async () => {
    const f = fakeSales();
    renderSales(f, PATH);
    await screen.findByText("Nguyễn Thị An");
    expect(within(rowOf("Nguyễn Thị An")).getByRole("button", { name: "4" })).toBeTruthy();
    expect(within(rowOf("Nguyễn Thị An")).getByText("1.150.000 ₫")).toBeTruthy();
    expect(within(rowOf("Nguyễn Thị An")).getByText("287.500 ₫")).toBeTruthy();
    expect(within(rowOf("Nguyễn Thị An")).getByText("66,66%")).toBeTruthy();
    expect(within(rowOf(LONG_NAME)).getByRole("button", { name: "3" })).toBeTruthy();
    expect(within(rowOf(LONG_NAME)).getByText("—")).toBeTruthy();
    expect(within(rowOf("Chưa phân công")).getByRole("button", { name: "1" })).toBeTruthy();
    const total = rowOf("Tổng");
    expect(within(total).getByText("8")).toBeTruthy();
    expect(within(total).getByText("1.910.000 ₫")).toBeTruthy();
    expect(within(total).getByText("272.857 ₫")).toBeTruthy();
    expect(within(total).getByText("66,66%")).toBeTruthy();
    // Dòng Tổng không có nút bấm.
    expect(within(total).queryAllByRole("button")).toHaveLength(0);
    expect(asQuery(ordersGets(f)[0]).toString()).toBe("from=2026-11-27&to=2026-11-27&basis=placed&groupBy=care_by");
  });

  it("tên nhân viên 120 ký tự: cắt bằng dấu ba chấm, có title đủ tên; bảng nằm trong khung cuộn ngang", async () => {
    renderSales(fakeSales(), PATH);
    const name = await screen.findByTitle(LONG_NAME);
    expect(name.className).toContain("nm");
    expect(name.closest(".scroll-x")).not.toBeNull();
  });

  it("đổi Nhóm theo sang Kênh: gọi lại groupBy=channel; số cũ không hiện khi đang tải", async () => {
    const f = fakeSales();
    renderSales(f, PATH);
    await screen.findByText("Nguyễn Thị An");
    fireEvent.change(screen.getByLabelText("Nhóm theo"), { target: { value: "channel" } });
    expect(screen.queryByText("Nguyễn Thị An")).toBeNull();
    expect(screen.getByText("Đang tải số liệu…")).toBeTruthy();
    await waitFor(() => {
      expect(asQuery(ordersGets(f).at(-1)).get("groupBy")).toBe("channel");
    });
    await screen.findByRole("table", { name: "Báo cáo đơn" });
  });

  it("không có đơn khớp: câu trống và nút Bỏ lọc", async () => {
    const f = fakeSales({
      orders: () => ({
        ...CARE_BY,
        rows: [],
        totals: {
          ...CARE_BY.totals,
          orders: 0,
          placed: 0,
          active: 0,
          cancelled: 0,
          goodsAmount: 0,
          avgGoodsAmount: null,
        },
      }),
    });
    renderSales(f, PATH);
    expect(await screen.findByText("Không có đơn khớp bộ lọc")).toBeTruthy();
  });
});

describe("OrdersReport — bấm ô số mở danh sách chi tiết (hành vi (a))", () => {
  it("bấm ô 4 của An: ngăn chi tiết 'Nguyễn Thị An · Đơn lên · 4 đơn', gọi metric=placed, cellDimension=care_by, cellKey của An", async () => {
    const f = fakeSales();
    renderSales(f, PATH);
    await screen.findByText("Nguyễn Thị An");
    fireEvent.click(within(rowOf("Nguyễn Thị An")).getByRole("button", { name: "4" }));
    const panel = await screen.findByRole("complementary", { name: "Danh sách chi tiết" });
    expect(await within(panel).findByText("Nguyễn Thị An · Đơn lên · 4 đơn")).toBeTruthy();
    expect(within(panel).getByText("DH0004801")).toBeTruthy();
    const q = asQuery(f.gets.find((g) => g.includes("/orders/drilldown")));
    expect(q.get("metric")).toBe("placed");
    expect(q.get("cellDimension")).toBe("care_by");
    expect(q.get("cellKey")).toBe(AN);
    expect(q.get("basis")).toBe("placed");
    expect(q.get("from")).toBe("2026-11-27");
    // Dòng của ô đang xem được đánh dấu.
    expect(within(rowOf("Nguyễn Thị An")).getByRole("button", { name: "4", pressed: true })).toBeTruthy();
  });

  it("bấm ô Doanh số mở metric=placed_active; ô Thành công mở metric=delivered", async () => {
    const f = fakeSales();
    renderSales(f, PATH);
    await screen.findByText("Nguyễn Thị An");
    fireEvent.click(within(rowOf("Nguyễn Thị An")).getByRole("button", { name: "1.150.000 ₫" }));
    await screen.findByText("Nguyễn Thị An · Đơn chưa hủy · 4 đơn");
    expect(asQuery(f.gets.filter((g) => g.includes("/drilldown")).at(-1)).get("metric")).toBe("placed_active");
    fireEvent.click(within(rowOf("Nguyễn Thị An")).getByRole("button", { name: "2" }));
    await screen.findByText("Nguyễn Thị An · Thành công · 2 đơn");
    expect(asQuery(f.gets.filter((g) => g.includes("/drilldown")).at(-1)).get("metric")).toBe("delivered");
  });

  it("ô 0 không bấm được", async () => {
    renderSales(fakeSales(), PATH);
    await screen.findByText("Nguyễn Thị An");
    // Bình không có đơn thành công: "0" là chữ tĩnh.
    const cells = within(rowOf(LONG_NAME)).getAllByText("0");
    for (const c of cells) expect(c.tagName).not.toBe("BUTTON");
  });

  it("ô đang mở nằm trên địa chỉ: tải lại trang vẫn mở đúng ô", async () => {
    const f = fakeSales();
    renderSales(f, `${PATH}?ky=today&o=placed&b=placed&d=care_by&k=${AN}`);
    expect(await screen.findByText("Nguyễn Thị An · Đơn lên · 4 đơn")).toBeTruthy();
  });
});

describe("OrdersReport — cơ sở ngày hoàn tất", () => {
  it("đổi sang Ngày hoàn tất: cột Đơn lên, Hủy ẩn; Số đơn là chữ tĩnh; Doanh số (đơn giao xong) bấm được", async () => {
    const f = fakeSales();
    renderSales(f, PATH);
    await screen.findByText("Nguyễn Thị An");
    expect(screen.getByRole("columnheader", { name: "Đơn lên" })).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Tính theo"), { target: { value: "completed" } });
    await screen.findByText("Facebook");
    expect(screen.queryByRole("columnheader", { name: "Đơn lên" })).toBeNull();
    expect(screen.queryByRole("columnheader", { name: "Hủy" })).toBeNull();
    expect(screen.getByRole("columnheader", { name: "Số đơn" })).toBeTruthy();
    expect(screen.getByRole("columnheader", { name: "Doanh số (đơn giao xong)" })).toBeTruthy();
    expect(within(rowOf("Facebook")).queryByRole("button", { name: "3" })).toBeNull();
    expect(within(rowOf("Facebook")).getByRole("button", { name: "750.000 ₫" })).toBeTruthy();
    // Nhóm không có đơn: giá trị TB và tỷ lệ đều "—" (không chia cho 0).
    expect(within(rowOf("Không xác định")).getAllByText("—")).toHaveLength(2);
    expect(asQuery(ordersGets(f).at(-1)).get("basis")).toBe("completed");
  });

  it("bấm ô Thành công của cơ sở hoàn tất gửi basis=completed", async () => {
    const f = fakeSales({ cells: { "delivered|facebook": [] } });
    renderSales(f, `${PATH}?tinh=completed&nhom=channel`);
    await screen.findByText("Facebook");
    fireEvent.click(within(rowOf("Facebook")).getByRole("button", { name: "2" }));
    await waitFor(() => {
      expect(f.gets.some((g) => g.includes("/drilldown"))).toBe(true);
    });
    const q = asQuery(f.gets.find((g) => g.includes("/drilldown")));
    expect(q.get("basis")).toBe("completed");
    expect(q.get("metric")).toBe("delivered");
    expect(q.get("cellDimension")).toBe("channel");
    expect(q.get("cellKey")).toBe("facebook");
  });
});

describe("OrdersReport — bộ lọc nhiều lựa chọn", () => {
  it("lọc Page P1: gửi pageId=P1; chọn thêm P2 gửi hai pageId; danh sách chi tiết mang cùng bộ lọc", async () => {
    const f = fakeSales();
    renderSales(f, PATH);
    await screen.findByText("Nguyễn Thị An");
    fireEvent.click(screen.getByRole("button", { name: "Page: tất cả" }));
    const group = await screen.findByRole("group", { name: "Lọc theo Page" });
    fireEvent.click(within(group).getByRole("checkbox", { name: "Page ELIMAZ Công Sở" }));
    fireEvent.click(within(group).getByRole("button", { name: "Áp dụng" }));
    await waitFor(() => {
      expect(asQuery(ordersGets(f).at(-1)).getAll("pageId")).toEqual([P1]);
    });
    fireEvent.click(await screen.findByRole("button", { name: "Page: 1 đã chọn" }));
    const again = await screen.findByRole("group", { name: "Lọc theo Page" });
    fireEvent.click(within(again).getByRole("checkbox", { name: "Page ELIMAZ Dạo Phố" }));
    fireEvent.click(within(again).getByRole("button", { name: "Áp dụng" }));
    await waitFor(() => {
      expect(asQuery(ordersGets(f).at(-1)).getAll("pageId")).toEqual([P1, P2]);
    });
    await screen.findByText("Nguyễn Thị An");
    fireEvent.click(within(rowOf("Nguyễn Thị An")).getByRole("button", { name: "4" }));
    await screen.findByRole("complementary", { name: "Danh sách chi tiết" });
    expect(asQuery(f.gets.find((g) => g.includes("/drilldown"))).getAll("pageId")).toEqual([P1, P2]);
  });

  it("bộ lọc không có ô gõ chữ; Bỏ lọc xóa hết", async () => {
    const f = fakeSales();
    renderSales(f, `${PATH}?page=${P1}`);
    await screen.findByText("Nguyễn Thị An");
    expect(screen.queryByRole("textbox")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Bỏ lọc" }));
    await waitFor(() => {
      expect(asQuery(ordersGets(f).at(-1)).getAll("pageId")).toEqual([]);
    });
  });

  it("giá trị lạ trên địa chỉ (nhóm, cơ sở, mã lọc) bị bỏ, không gửi lên máy chủ", async () => {
    const f = fakeSales();
    renderSales(f, `${PATH}?nhom=sql&tinh=x&page=a%20b%3B&hang=vtp`);
    await screen.findByText("Nguyễn Thị An");
    const q = asQuery(ordersGets(f)[0]);
    expect(q.get("groupBy")).toBe("care_by");
    expect(q.get("basis")).toBe("placed");
    expect(q.getAll("pageId")).toEqual([]);
    expect(q.getAll("carrierKey")).toEqual(["vtp"]);
  });
});

describe("OrdersReport — trạng thái, chạy nền, xuất Excel, quyền", () => {
  it("lỗi tải: 'Không tải được báo cáo.' và nút Thử lại", async () => {
    const f = fakeSales({ readError: { status: 500, body: { error: "internal", request_id: "r" } } });
    renderSales(f, PATH);
    expect(await screen.findByText("Không tải được báo cáo.")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Thử lại" })).toBeTruthy();
  });

  it("chạy nền: đúng một POST …/runs có Idempotency-Key, báo 'đang chạy nền'; xong thì hiện số như xem tại chỗ", async () => {
    const f = fakeSales({ steps: [BACKGROUND] });
    const { qc } = renderSales(f, PATH);
    expect(await screen.findByText(/Báo cáo nặng, đang chạy nền/)).toBeTruthy();
    await waitFor(() => {
      expect(f.writes).toHaveLength(1);
    });
    const w = f.writes[0];
    expect(w?.path).toBe("/api/v1/reports/sales/runs");
    expect(w?.key).toBeTruthy();
    expect(w?.body).toMatchObject({
      report: "orders",
      format: "json",
      params: { groupBy: "care_by", basis: "placed" },
    });
    await settleRun(f, qc, { status: "ready", report: "orders", result: { ...CARE_BY } });
    expect(await screen.findByText("Nguyễn Thị An")).toBeTruthy();
    expect(f.writes).toHaveLength(1);
  });

  it("xuất Excel (có report.export): một POST report=orders, format=xlsx, params đủ kỳ, cơ sở, nhóm, lọc", async () => {
    const f = fakeSales();
    renderSales(f, `${PATH}?page=${P1}`);
    await screen.findByText("Nguyễn Thị An");
    expect(await exportExcel(f)).toEqual({
      report: "orders",
      format: "xlsx",
      params: { from: "2026-11-27", to: "2026-11-27", basis: "placed", groupBy: "care_by", pageId: [P1] },
    });
  });

  it("người chỉ có quyền một Page: không có nút Xuất Excel; có dòng Phạm vi", async () => {
    renderSales(fakeSales({ grants: ONE_PAGE }), PATH);
    await screen.findByText("Nguyễn Thị An");
    expect(screen.queryByRole("button", { name: "Xuất Excel" })).toBeNull();
    expect(screen.getByText(/Phạm vi: chỉ đơn của Page bạn được giao/)).toBeTruthy();
  });

  it("kỳ quá 366 ngày: không gọi báo cáo", async () => {
    const f = fakeSales();
    renderSales(f, `${PATH}?ky=custom&tu=2026-01-01&den=2027-01-03`);
    await screen.findByText(/Khoảng tối đa 366 ngày/);
    expect(ordersGets(f)).toHaveLength(0);
  });
});
