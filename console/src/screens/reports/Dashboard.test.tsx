// @vitest-environment jsdom
// Màn Tổng quan /bao-cao/tong-quan (Task 239, M6-16; mockup docs/m6/mockup/tong-quan.html trạng thái A–H; BC01, BC05):
// số của kịch bản chín đơn, khối Chờ xử lý, bấm ô mở danh sách chi tiết, chạy nền, xuất Excel theo quyền, trạng thái
// trống/lỗi/bận, phạm vi một Page.
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  BACKGROUND,
  DASH,
  EMPTY_DASH,
  LONG_NAME,
  ONE_PAGE,
  OWNER,
  amounts,
  busySteps,
  fakeReports,
  settleRun,
  orderRow,
  renderDashboard,
} from "./reportsTestkit";

beforeEach(() => {
  // Chỉ giả ngày: 27/11/2026 14:32 giờ Việt Nam. waitFor của Testing Library vẫn chạy bằng đồng hồ thật.
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2026-11-27T07:32:05Z"));
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

const kpi = (name: RegExp) => screen.getByRole("link", { name });

describe("Dashboard — số liệu kịch bản chín đơn", () => {
  it("Hôm nay: 8 đơn lên, 1.910.000 doanh số, 272.857 giá trị TB, hủy 1, thành công 2, hoàn 1, khách trả 1 cái", async () => {
    const f = fakeReports();
    renderDashboard(f);
    await screen.findByRole("link", { name: /^Đơn lên/ });
    expect(within(kpi(/^Đơn lên/)).getByText("8")).toBeTruthy();
    expect(within(kpi(/^Đơn lên/)).getByText("gồm 1 đơn đã hủy")).toBeTruthy();
    expect(within(kpi(/^Doanh số/)).getByText("1.910.000 ₫")).toBeTruthy();
    expect(within(kpi(/^Doanh số/)).getByText("7 đơn chưa hủy")).toBeTruthy();
    expect(within(kpi(/^Giá trị TB/)).getByText("272.857 ₫")).toBeTruthy();
    expect(within(kpi(/^Đơn hủy/)).getByText("1")).toBeTruthy();
    expect(within(kpi(/^Thành công/)).getByText("2")).toBeTruthy();
    expect(within(kpi(/^Thành công/)).getByText(/750\.000 ₫/)).toBeTruthy();
    expect(within(kpi(/^Hoàn/)).getByText("1")).toBeTruthy();
    expect(within(kpi(/^Hàng khách trả/)).getByText("1 cái")).toBeTruthy();
    expect(within(kpi(/^Hàng khách trả/)).getByText("150.000 ₫")).toBeTruthy();
    // Gọi đúng kỳ Hôm nay theo giờ Việt Nam.
    expect(f.gets[0]).toBe("/api/v1/reports/sales/dashboard?from=2026-11-27&to=2026-11-27");
    expect(screen.getByText(/Cập nhật lúc/)).toBeTruthy();
  });

  it("khối Chờ xử lý 1 / 1 / 1; bảng Theo kênh có dòng Tổng khớp (8, 7, 1.910.000, 150.000)", async () => {
    renderDashboard(fakeReports());
    const now = (await screen.findByText("Chờ xử lý")).closest(".now");
    if (!(now instanceof HTMLElement)) throw new Error("thiếu khối Chờ xử lý");
    for (const name of ["Chờ xác nhận", "Chờ gửi hãng", "Đang yêu cầu hủy"]) {
      expect(within(now).getByRole("link", { name: new RegExp(`^${name}`) }).textContent).toContain("1");
    }
    const table = screen.getByRole("table", { name: "Theo kênh" });
    const rows = within(table)
      .getAllByRole("row")
      .map((r) => r.textContent);
    expect(rows[1]).toContain("Facebook");
    expect(rows[1]).toContain("1.300.000 ₫");
    expect(rows[2]).toContain("Website");
    expect(rows[3]).toContain("Nhập tay");
    const total = within(table).getByText("Tổng").closest("tr");
    expect(total?.textContent).toBe("Tổng871.910.000 ₫150.000 ₫");
    expect(screen.getByText(/Doanh số = tiền hàng sau giảm giá/)).toBeTruthy();
  });

  it("kênh lạ (unknown) hiện 'Không xác định'; kênh không có đơn không có nút bấm", async () => {
    const f = fakeReports({
      dashboard: {
        ...DASH,
        byChannel: [...DASH.byChannel, { channel: "unknown", placed: amounts(0, 0), active: amounts(0, 0) }],
      },
    });
    renderDashboard(f);
    const row = (await screen.findByText("Không xác định")).closest("tr");
    expect(row).not.toBeNull();
    expect(within(row as HTMLElement).queryAllByRole("button").length).toBe(0);
  });
});

describe("Dashboard — bấm ô số mở danh sách chi tiết (hành vi (a))", () => {
  it("ô Thành công mở danh sách theo ngày hoàn tất (basis=completed), ô Đơn hủy theo ngày lên đơn", async () => {
    const f = fakeReports({ drill: { delivered: [orderRow(1), orderRow(9)] } });
    renderDashboard(f);
    fireEvent.click(await screen.findByRole("link", { name: /^Thành công/ }));
    expect(await screen.findByRole("heading", { name: "Thành công · 2 đơn" })).toBeTruthy();
    const q = new URLSearchParams(f.gets.find((g) => g.includes("/drilldown"))?.split("?")[1]);
    expect([q.get("metric"), q.get("basis")]).toEqual(["delivered", "completed"]);
    expect(screen.getByText(/theo ngày hoàn tất/, { selector: "p.xs" })).toBeTruthy();
  });

  it("bấm ô 'Chờ xác nhận' → DrilldownPanel gọi metric=pending_confirm, đầu bảng '1 đơn', dòng O4", async () => {
    const f = fakeReports();
    renderDashboard(f);
    fireEvent.click(await screen.findByRole("link", { name: /^Chờ xác nhận/ }));
    expect(await screen.findByRole("heading", { name: "Chờ xác nhận · 1 đơn" })).toBeTruthy();
    expect(screen.getByRole("link", { name: "DH0004804" })).toBeTruthy();
    const call = f.gets.find((g) => g.includes("/drilldown"));
    expect(new URLSearchParams(call?.split("?")[1]).get("metric")).toBe("pending_confirm");
    // Ô đang xem được tô đậm (aria-current) và nút ✕ đóng ngăn.
    expect(kpi(/^Chờ xác nhận/).getAttribute("aria-current")).toBe("true");
    fireEvent.click(screen.getByRole("button", { name: "Đóng" }));
    await waitFor(() => {
      expect(screen.queryByRole("heading", { name: "Chờ xác nhận · 1 đơn" })).toBeNull();
    });
  });

  it("bấm ô 'Đơn lên' của dòng Facebook → cellDimension=channel, cellKey=facebook, metric=placed", async () => {
    const f = fakeReports({ drill: { placed: [orderRow(1), orderRow(2)] } });
    renderDashboard(f);
    const row = (await screen.findByText("Facebook")).closest("tr");
    const [firstCell] = within(row ?? document.body).getAllByRole("button");
    if (firstCell === undefined) throw new Error("thiếu ô số");
    fireEvent.click(firstCell);
    expect(await screen.findByRole("heading", { name: "Đơn lên · Facebook · 2 đơn" })).toBeTruthy();
    const q = new URLSearchParams(f.gets.find((g) => g.includes("/drilldown"))?.split("?")[1]);
    expect([q.get("cellDimension"), q.get("cellKey"), q.get("metric")]).toEqual(["channel", "facebook", "placed"]);
  });

  it("ngăn mở ở địa chỉ (?o=…): tải lại trang vẫn đúng ô, nút '← Tổng quan' (điện thoại) đóng ngăn", async () => {
    const f = fakeReports();
    renderDashboard(f, "/bao-cao/tong-quan?o=pending_confirm");
    expect(await screen.findByRole("heading", { name: "Chờ xác nhận · 1 đơn" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "← Tổng quan" }));
    await waitFor(() => {
      expect(screen.queryByRole("heading", { name: /1 đơn$/ })).toBeNull();
    });
  });

  it("bấm mã đơn mở màn chi tiết đơn của M2 (/don/:id)", async () => {
    const f = fakeReports();
    const { router } = renderDashboard(f, "/bao-cao/tong-quan?o=pending_confirm");
    fireEvent.click(await screen.findByRole("link", { name: "DH0004804" }));
    await screen.findByText("Trang đơn");
    expect(router.state.location.pathname).toMatch(/^\/don\/0195f3b2-/);
  });
});

describe("Dashboard — chọn kỳ", () => {
  it("bấm 7 ngày gọi lại API với 21/11–27/11 và hiện bảng Theo ngày có thanh ngang; ngày không đơn không có nút", async () => {
    const f = fakeReports();
    renderDashboard(f);
    await screen.findByRole("link", { name: /^Đơn lên/ });
    f.dashboard = {
      ...DASH,
      period: { from: "2026-11-21", to: "2026-11-27" },
      byDay: [
        { day: "2026-11-27", active: amounts(7, 1910000), delivered: amounts(2, 750000) },
        { day: "2026-11-26", active: amounts(16, 5120000), delivered: amounts(14, 4400000) },
        { day: "2026-11-25", active: amounts(0, 0), delivered: amounts(0, 0) },
      ],
    };
    fireEvent.click(screen.getByRole("button", { name: "7 ngày" }));
    const table = await screen.findByRole("table", { name: "Theo ngày" });
    expect(f.gets.at(-1)).toBe("/api/v1/reports/sales/dashboard?from=2026-11-21&to=2026-11-27");
    const rows = within(table).getAllByRole("row");
    expect(rows[1]?.textContent).toContain("27/11/2026");
    const empty = within(table).getByText("25/11/2026").closest("tr");
    expect(within(empty as HTMLElement).queryAllByRole("button").length).toBe(0);
    expect(table.querySelectorAll(".bar i").length).toBe(3);
    // Thanh dài nhất = ngày doanh số lớn nhất (26/11); ngày không đơn 0%.
    const widths = [...table.querySelectorAll<HTMLElement>(".bar i")].map((i) => i.style.width);
    expect(widths).toEqual(["37%", "100%", "0%"]);
  });

  it("một ngày: không có bảng Theo ngày", async () => {
    renderDashboard(fakeReports());
    await screen.findByRole("link", { name: /^Đơn lên/ });
    expect(screen.queryByRole("table", { name: "Theo ngày" })).toBeNull();
  });
});

describe("Dashboard — trạng thái", () => {
  it("không có đơn: số 0, giá trị TB '—', thông báo trống của mockup", async () => {
    renderDashboard(fakeReports({ dashboard: EMPTY_DASH }));
    expect(await screen.findByText("Chưa có đơn nào lên hôm nay")).toBeTruthy();
    const tb = screen.getByText("Giá trị TB").closest<HTMLElement>(".kpi");
    expect(within(tb ?? document.body).getByText("—")).toBeTruthy();
    const placed = screen.getByText("Đơn lên").closest<HTMLElement>(".kpi");
    expect(within(placed ?? document.body).getByText("0")).toBeTruthy();
    expect(screen.queryByRole("link", { name: /^Đơn lên/ })).toBeNull(); // 0 đơn: không có gì để mở
  });

  it("đang tải: 'Đang tải số liệu…', không hiện số cũ, chọn kỳ khóa", async () => {
    renderDashboard(fakeReports());
    expect(screen.getByText("Đang tải số liệu…")).toBeTruthy();
    expect(screen.queryByText("1.910.000 ₫")).toBeNull();
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "7 ngày" }).disabled).toBe(true);
    await screen.findByRole("link", { name: /^Đơn lên/ });
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "7 ngày" }).disabled).toBe(false);
  });

  it("lỗi tải 500 → 'Không tải được báo cáo' có nút Thử lại; Thử lại tải được", async () => {
    const f = fakeReports({ steps: [{ status: 500, body: { error: "internal", request_id: "7f3a9c" } }] });
    renderDashboard(f);
    expect(await screen.findByText("Không tải được báo cáo.")).toBeTruthy();
    expect(screen.getByText(/Hệ thống gặp lỗi/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Thử lại" }));
    await screen.findByRole("link", { name: /^Đơn lên/ });
  });

  it("503 retry_later → 'Hệ thống đang bận, thử lại sau ít giây'", async () => {
    const f = fakeReports({
      steps: busySteps(),
    });
    renderDashboard(f);
    expect(await screen.findByText("Hệ thống đang bận, thử lại sau ít giây")).toBeTruthy();
  });

  it("403 → hộp lỗi quyền", async () => {
    renderDashboard(fakeReports({ steps: [{ status: 403, body: { error: "forbidden", request_id: "r" } }] }));
    expect(await screen.findByText("Bạn chưa được cấp quyền làm việc này")).toBeTruthy();
  });

  it("quá 366 ngày ở Tùy chọn: không gọi API, nút Xem khóa", async () => {
    const f = fakeReports();
    renderDashboard(f, "/bao-cao/tong-quan?ky=custom&tu=2026-01-01&den=2027-01-03");
    expect(await screen.findByText(/Khoảng tối đa 366 ngày; đang chọn 368 ngày/)).toBeTruthy();
    expect(f.gets.filter((g) => g.includes("/dashboard"))).toEqual([]);
  });

  it("người chỉ có quyền một Page: dòng 'Phạm vi', không có nút Xuất Excel", async () => {
    renderDashboard(fakeReports({ grants: ONE_PAGE }));
    expect(await screen.findByText(/Phạm vi: chỉ đơn của Page bạn được giao/)).toBeTruthy();
    await screen.findByRole("link", { name: /^Đơn lên/ });
    expect(screen.queryByRole("button", { name: "Xuất Excel" })).toBeNull();
  });
});

describe("Dashboard — chạy nền (BC05)", () => {
  it("run_in_background → thông báo 'Báo cáo nặng, đang chạy nền…', đúng một POST; báo hiệu xong → hiện số", async () => {
    const f = fakeReports({ steps: [BACKGROUND] });
    const { qc } = renderDashboard(f);
    expect(await screen.findByText("Báo cáo nặng, đang chạy nền. Sẽ báo khi xong.")).toBeTruthy();
    await waitFor(() => {
      expect(f.writes.length).toBe(1);
    });
    expect(f.writes[0]?.key).toBeTruthy();
    expect(screen.queryByRole("link", { name: /^Đơn lên/ })).toBeNull();
    await settleRun(f, qc, { status: "ready", result: { ...DASH } });
    await screen.findByRole("link", { name: /^Đơn lên/ });
    expect(within(kpi(/^Đơn lên/)).getByText("8")).toBeTruthy();
    expect(f.writes.length).toBe(1);
  });

  it("lần chạy nền quá nặng → 'Báo cáo quá nặng, thu hẹp khoảng thời gian.'", async () => {
    const f = fakeReports({ steps: [BACKGROUND] });
    const { qc } = renderDashboard(f);
    await settleRun(f, qc, { status: "failed", reason: "too_heavy" });
    expect(await screen.findByText("Báo cáo quá nặng, thu hẹp khoảng thời gian.")).toBeTruthy();
  });
});

/** Mở Tổng quan, bấm Xuất Excel và chờ thông báo "Đang tạo tệp". */
async function startExport() {
  const f = fakeReports();
  const { qc } = renderDashboard(f);
  await screen.findByRole("link", { name: /^Đơn lên/ });
  fireEvent.click(screen.getByRole("button", { name: "Xuất Excel" }));
  await screen.findByText("Đang tạo tệp, xem ở Tệp xuất");
  return { f, qc };
}

describe("Dashboard — Xuất Excel (hành vi (b))", () => {
  it("người có report.export phạm vi toàn shop thấy nút; bấm → một POST xlsx có Idempotency-Key, thông báo 'Đang tạo tệp'", async () => {
    const f = fakeReports();
    renderDashboard(f);
    await screen.findByRole("link", { name: /^Đơn lên/ });
    fireEvent.click(screen.getByRole("button", { name: "Xuất Excel" }));
    expect(await screen.findByText("Đang tạo tệp, xem ở Tệp xuất")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Mở Tệp xuất" }).getAttribute("href")).toBe("/bao-cao/tep-xuat");
    expect(f.writes.length).toBe(1);
    expect(f.writes[0]?.body).toEqual({
      report: "dashboard",
      format: "xlsx",
      params: { from: "2026-11-27", to: "2026-11-27" },
    });
    expect(f.writes[0]?.key).toBeTruthy();
  });

  it("tệp xong (báo hiệu report.run_finished) → 'Tệp … đã sẵn sàng — Tải về'", async () => {
    const { f, qc } = await startExport();
    await settleRun(f, qc, { format: "xlsx", status: "ready" });
    expect(await screen.findByText(/Tệp Tổng quan 27\/11\/2026 đã sẵn sàng/)).toBeTruthy();
    expect(screen.getByRole("button", { name: "Tải về" })).toBeTruthy();
  });

  it("đổi kỳ sau khi bấm xuất: thông báo vẫn nói đúng kỳ lúc bấm, không đổi sang kỳ mới", async () => {
    const { f, qc } = await startExport();
    fireEvent.click(screen.getByRole("button", { name: "7 ngày" }));
    await waitFor(() => {
      expect(f.gets.at(-1)).toContain("from=2026-11-21");
    });
    await settleRun(f, qc, { format: "xlsx", status: "ready", params: { from: "2026-11-27", to: "2026-11-27" } });
    expect(await screen.findByText(/Tệp Tổng quan 27\/11\/2026 đã sẵn sàng/)).toBeTruthy();
    expect(screen.queryByText(/21\/11\/2026 – 27\/11\/2026 đã sẵn sàng/)).toBeNull();
  });

  it("máy chủ từ chối (403 export_forbidden) → câu tiếng Việt riêng", async () => {
    const f = fakeReports();
    f.replies.push({ status: 403, body: { error: "export_forbidden", request_id: "r" } });
    renderDashboard(f);
    await screen.findByRole("link", { name: /^Đơn lên/ });
    fireEvent.click(screen.getByRole("button", { name: "Xuất Excel" }));
    expect(await screen.findByText("Bạn chưa được cấp quyền xuất Excel")).toBeTruthy();
  });

  it("report.export chỉ ở phạm vi page → không có nút", async () => {
    const f = fakeReports({
      grants: [
        ...OWNER.filter((g) => g.permission !== "report.export"),
        { permission: "report.export", scope: { type: "page", id: "p1" } },
      ],
    });
    renderDashboard(f);
    await screen.findByRole("link", { name: /^Đơn lên/ });
    expect(screen.queryByRole("button", { name: "Xuất Excel" })).toBeNull();
  });
});

describe("Dashboard — dữ liệu khó", () => {
  it("tên nhân viên 120 ký tự trong danh sách chi tiết: cắt bằng dấu ba chấm, đủ tên ở title", async () => {
    const f = fakeReports({ drill: { pending_confirm: [orderRow(4, { careByName: LONG_NAME })] } });
    renderDashboard(f, "/bao-cao/tong-quan?o=pending_confirm");
    const nm = await screen.findByTitle(LONG_NAME);
    expect(nm.className).toContain("nm");
  });

  it("tiền 9 chữ số hiện đủ", async () => {
    renderDashboard(fakeReports({ dashboard: { ...DASH, active: amounts(398, 123456789) } }));
    // Ô Doanh số và dòng Tổng của bảng theo kênh.
    expect((await screen.findAllByText("123.456.789 ₫")).length).toBe(2);
  });
});
