// @vitest-environment jsdom
// Màn Quà sinh nhật: đầu trang (lượt chạy cuối, số tổng, Tạo mã ngay), tab Cần nhắn (chép tin, mở Zalo, Đã nhắn một và
// nhiều mã), quyền chỉ xem, 403, quà đang tắt (mockup docs/mockups/m5/birthday-gifts.html trạng thái A; kế hoạch M5
// Task 209 + Task 450). SĐT đầy đủ không hiện trên bảng, không vào console hay localStorage.
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  LONG_NAME,
  PATH,
  customer,
  gift,
  grantsOf,
  installAt,
  renderAt,
  teardown,
  run,
  settings,
  type Fake,
} from "./birthdayTestkit";

let fake: Fake;
let clip: ReturnType<typeof vi.fn>;
const logs: unknown[][] = [];
beforeEach(() => {
  fake = installAt();
  fake.todo = [
    gift(3, { fullName: "Nguyễn Thị Lan", phone: "+84912345678", code: "SN4E1C95" }),
    gift(4, { fullName: LONG_NAME }),
    gift(0),
    gift(-2),
  ];
  fake.customers = [customer(1, "due"), customer(2, "used"), customer(3, "stopped")];
  clip = vi.fn(() => Promise.resolve());
  Object.defineProperty(navigator, "clipboard", { value: { writeText: clip }, configurable: true });
  for (const m of ["log", "info", "warn", "error", "debug"] as const)
    vi.spyOn(console, m).mockImplementation((...a: unknown[]) => {
      logs.push(a);
    });
});
afterEach(() => {
  teardown();
  logs.length = 0;
  localStorage.clear();
});

const row = (name: string | RegExp) => screen.findByRole("row", { name });

describe("Đầu trang", () => {
  it("lượt chạy cuối, cài đặt đang bật, bốn số tổng", async () => {
    renderAt(PATH);
    const info = await screen.findByTestId("bg-run");
    expect(within(info).getByText("Đang bật")).toBeTruthy();
    expect(info.textContent).toContain("Giảm 50.000 ₫ · gửi trước 7 ngày · dùng trong 14 ngày");
    expect(info.textContent).toContain("Lần chạy gần nhất 09:00 24/11 (theo lịch): 6 mã mới, 0 lỗi.");
    const kpis = await screen.findByTestId("bg-kpis");
    await waitFor(() => {
      expect(within(kpis).getByTestId("kpi-todo").textContent).toContain("4");
    });
    expect(within(kpis).getByTestId("kpi-due").textContent).toContain("1");
    expect(within(kpis).getByTestId("kpi-used").textContent).toContain("1");
    expect(within(kpis).getByTestId("kpi-saved").textContent).toContain("3");
    expect(within(kpis).getByTestId("kpi-saved").textContent).toContain("1 khách đã hủy nhận");
    expect(screen.getByRole("tab", { name: "Cần nhắn (4)" }).getAttribute("aria-selected")).toBe("true");
  });

  it("chưa chạy lần nào; lượt đang chạy", async () => {
    fake.settings = settings();
    renderAt(PATH);
    expect((await screen.findByTestId("bg-run")).textContent).toContain("Chưa có lượt tạo mã nào.");
    cleanup();
    fake.settings = settings({ lastRun: run({ finishedAt: undefined, trigger: "manual" }) });
    renderAt(PATH);
    expect((await screen.findByTestId("bg-run")).textContent).toContain("Lượt bấm tay bắt đầu 09:00 24/11 đang chạy.");
  });

  it("quà đang tắt: báo rõ, khách Đến hạn ghi quà đang tắt", async () => {
    fake.settings = settings({ enabled: false });
    renderAt(PATH);
    const info = await screen.findByTestId("bg-run");
    expect(within(info).getByText("Đang tắt")).toBeTruthy();
    expect(info.textContent).toContain("CRM không tạo mã mới");
    fireEvent.click(screen.getByRole("tab", { name: "Tất cả khách" }));
    const r = await row(/Khách 1/);
    expect(within(r).getByText("Đến hạn · quà đang tắt")).toBeTruthy();
  });

  it("Tạo mã ngay: gửi có khóa chống trùng, báo số mã; lượt khác đang chạy → 409 hiện hộp", async () => {
    renderAt(PATH);
    fireEvent.click(await screen.findByRole("button", { name: "Tạo mã ngay" }));
    expect(await screen.findByText("Đã chạy xong: 2 mã mới, 0 lỗi.")).toBeTruthy();
    expect(fake.writes[0]).toMatchObject({ method: "POST", path: "/api/v1/birthday-runs" });
    expect(fake.writes[0]?.key).toBeTruthy();
    fake.replies.push({ status: 409, body: { error: "another_run_in_progress", request_id: "t" } });
    fireEvent.click(screen.getByRole("button", { name: "Tạo mã ngay" }));
    expect(await screen.findByText("Đang có một lần tạo mã khác chạy.")).toBeTruthy();
  });

  it("chỉ promotion.read: không nút ghi, có câu chỉ xem", async () => {
    fake.grants = grantsOf();
    renderAt(PATH);
    await row(/Nguyễn Thị Lan/);
    expect(screen.queryByRole("button", { name: "Tạo mã ngay" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Đã nhắn" })).toBeNull();
    expect(screen.queryByRole("checkbox")).toBeNull();
    expect(screen.getByText(/Bạn chỉ xem được/)).toBeTruthy();
    expect(screen.getAllByRole("button", { name: /Chép tin/ })).toHaveLength(4);
  });

  it("403 của API: hiện câu không quyền, không vẽ như danh sách rỗng", async () => {
    fake.forbidden = true;
    renderAt(PATH);
    expect((await screen.findAllByText("Bạn không có quyền xem khuyến mãi.")).length).toBeGreaterThan(0);
    expect(screen.queryByText("Không có mã nào cần nhắn tay")).toBeNull();
  });
});

describe("Tab Cần nhắn", () => {
  it("bốn mã, sinh nhật còn/hôm nay/đã qua, số che giữa, tên dài, Zalo mở số đầy đủ", async () => {
    renderAt(PATH);
    const lan = await row(/Nguyễn Thị Lan/);
    expect(within(lan).getByText("0912 *** 678")).toBeTruthy();
    expect(within(lan).getByText("SN4E1C95")).toBeTruthy();
    expect(within(lan).getByText("01/12/2026")).toBeTruthy();
    expect(lan.textContent).toContain("24/11 · còn 3 ngày");
    expect(
      within(lan)
        .getByRole("link", { name: /Mở Zalo/ })
        .getAttribute("href"),
    ).toBe("https://zalo.me/0912345678");
    expect((await row(/Khách số 0/)).textContent).toContain("hôm nay");
    expect((await row(/Khách số -2/)).textContent).toContain("đã qua 2 ngày");
    expect(screen.getByText(LONG_NAME).getAttribute("title")).toBe(LONG_NAME);
    expect(document.body.textContent).not.toContain("0912345678");
  });

  it("Chép tin: chép đúng tin, báo; không ghi SĐT vào console, localStorage", async () => {
    renderAt(PATH);
    const lan = await row(/Nguyễn Thị Lan/);
    fireEvent.click(within(lan).getByRole("button", { name: /Chép tin/ }));
    await waitFor(() => {
      expect(clip).toHaveBeenCalledTimes(1);
    });
    expect(clip.mock.calls[0]?.[0]).toContain("Mừng sinh nhật Lan! ELIMAZ tặng bạn mã SN4E1C95 giảm 50.000 ₫.");
    expect(await screen.findByText('Đã chép tin cho Nguyễn Thị Lan. Dán vào Zalo rồi bấm "Đã nhắn".')).toBeTruthy();
    expect(JSON.stringify(logs)).not.toContain("912345678");
    expect(JSON.stringify(Object.keys(localStorage).map((k) => localStorage.getItem(k)))).not.toContain("912345678");
  });

  it("Đã nhắn một mã: gửi đúng mã, dòng biến mất", async () => {
    renderAt(PATH);
    const lan = await row(/Nguyễn Thị Lan/);
    fireEvent.click(within(lan).getByRole("button", { name: "Đã nhắn" }));
    expect(await screen.findByText("Đã ghi đã nhắn 1 mã.")).toBeTruthy();
    expect(fake.writes[0]).toMatchObject({ path: "/api/v1/birthday-gifts/messaged", body: { codes: ["SN4E1C95"] } });
    await waitFor(() => {
      expect(screen.queryByRole("row", { name: /Nguyễn Thị Lan/ })).toBeNull();
    });
  });

  it("chọn nhiều rồi đánh dấu; mã người khác đã ghi trước được báo riêng", async () => {
    renderAt(PATH);
    await row(/Nguyễn Thị Lan/);
    fireEvent.click(screen.getByRole("checkbox", { name: "Chọn Nguyễn Thị Lan" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "Chọn Khách số 0" }));
    expect(screen.getByText("Đã chọn 2 khách")).toBeTruthy();
    fake.replies.push({ status: 200, body: { marked: ["SN4E1C95"] } });
    fireEvent.click(screen.getByRole("button", { name: "Đánh dấu đã nhắn" }));
    expect(await screen.findByText("Đã ghi đã nhắn 1 mã; 1 mã đã được ghi trước đó.")).toBeTruthy();
    expect(fake.writes[0]?.body).toEqual({ codes: ["SN4E1C95", "SNCODE00"] });
    expect(screen.queryByText(/Đã chọn/)).toBeNull();
  });

  it("chọn cả trang, bỏ chọn", async () => {
    renderAt(PATH);
    await row(/Nguyễn Thị Lan/);
    fireEvent.click(screen.getByRole("checkbox", { name: "Chọn cả trang" }));
    expect(screen.getByText("Đã chọn 4 khách")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Bỏ chọn" }));
    expect(screen.queryByText(/Đã chọn/)).toBeNull();
  });

  it("không mã nào: câu trống", async () => {
    fake.todo = [];
    renderAt(PATH);
    expect(await screen.findByText("Không có mã nào cần nhắn tay")).toBeTruthy();
    expect(screen.getByRole("tab", { name: "Cần nhắn (0)" })).toBeTruthy();
  });
});
