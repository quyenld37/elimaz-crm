// @vitest-environment jsdom
// Tab Tất cả khách: tám trạng thái, lọc trạng thái và tháng (mặc định tháng hiện tại), tìm, tải thêm theo con trỏ, xuất
// CSV (mockup docs/mockups/m5/birthday-gifts.html trạng thái B; kế hoạch M5 Task 450, Task 468).
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { STATES } from "./birthdayRules";
import { PATH, customer, grantsOf, installAt, renderAt, teardown, type Fake } from "./birthdayTestkit";

let fake: Fake;
beforeEach(() => {
  fake = installAt();
  fake.customers = STATES.map((s, i) =>
    customer(
      i,
      s,
      s === "used"
        ? { code: `SNX${String(i)}`, mail: true, usedOrderCode: "DH0001288" }
        : s === "given"
          ? { code: `SNX${String(i)}`, mail: true }
          : {},
    ),
  );
});
afterEach(() => {
  teardown();
});

const open = async () => {
  renderAt(`${PATH}?tab=khach`);
  return screen.findByRole("row", { name: /Khách 0/ });
};
const lastCall = () => fake.customerCalls.at(-1);

describe("Tất cả khách", () => {
  it("mỗi trạng thái một dòng, chữ như mockup; lọc tháng mặc định tháng hiện tại", async () => {
    await open();
    expect(lastCall()?.get("month")).toBe("11");
    for (const [i, label] of [
      [0, "Cần nhắn"],
      [1, "Đến hạn, chờ tạo mã"],
      [2, "Chờ đơn giao thành công"],
      [3, "Đã gửi mã"],
      [4, "Chờ ngày gửi"],
      [5, "Đã dùng · DH0001288"],
      [6, "Hết hạn, chưa dùng"],
      [7, "Đã hủy nhận"],
    ] as const) {
      const r = screen.getByText(`Khách ${String(i)}`).closest("tr");
      expect(r === null ? null : within(r).getByText(label)).toBeTruthy();
    }
    const used = screen.getByRole("row", { name: /Khách 5/ });
    expect(within(used).getByText("SNX5")).toBeTruthy();
    expect(within(used).getByText("Zalo · Email")).toBeTruthy();
    expect(within(used).getByText("0987 *** 305")).toBeTruthy();
    expect(within(used).getByText("28/11")).toBeTruthy();
    expect(within(used).getByText("21/11")).toBeTruthy();
    expect(screen.getByRole("row", { name: /Khách 7/ }).className).toContain("is-muted");
    expect(screen.getByRole("option", { name: "Cần nhắn (1)" })).toBeTruthy();
  });

  it("lọc trạng thái, mọi tháng, tìm số gõ 0 đầu gửi nguyên cho máy chủ", async () => {
    await open();
    fireEvent.change(screen.getByRole("combobox", { name: "Trạng thái" }), { target: { value: "used" } });
    await waitFor(() => {
      expect(lastCall()?.get("state")).toBe("used");
    });
    fireEvent.change(screen.getByRole("combobox", { name: "Tháng sinh" }), { target: { value: "0" } });
    await waitFor(() => {
      expect(lastCall()?.get("month")).toBeNull();
    });
    fireEvent.change(screen.getByRole("searchbox", { name: "Tìm khách" }), { target: { value: " 0987654305 " } });
    await waitFor(
      () => {
        expect(lastCall()?.get("q")).toBe("0987654305");
      },
      { timeout: 2000 },
    );
  });

  it("còn trang sau: hiện số đang hiện / tổng và Tải thêm", async () => {
    fake.customers = Array.from({ length: 60 }, (_, i) => customer(i, "waiting"));
    await open();
    expect(screen.getByText("Đang hiện 50 / 60 khách")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Tải thêm" }));
    expect(await screen.findByText("Đang hiện 60 / 60 khách")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Tải thêm" })).toBeNull();
  });

  it("không khách khớp: câu trống", async () => {
    fake.customers = [];
    renderAt(`${PATH}?tab=khach`);
    expect(await screen.findByText("Không có khách nào khớp")).toBeTruthy();
  });

  it("Xuất CSV: gửi bộ lọc đang chọn, tải tệp, báo số dòng; gửi lại cùng khóa → câu riêng", async () => {
    const create = vi.fn(() => "blob:x");
    vi.stubGlobal("URL", Object.assign(URL, { createObjectURL: create, revokeObjectURL: vi.fn() }));
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => undefined);
    await open();
    fireEvent.change(screen.getByRole("combobox", { name: "Trạng thái" }), { target: { value: "given" } });
    fireEvent.click(screen.getByRole("button", { name: "Xuất CSV" }));
    expect(await screen.findByText("Đã xuất 8 dòng vào tệp sinh-nhat.csv.")).toBeTruthy();
    expect(fake.writes[0]).toMatchObject({
      path: "/api/v1/birthday-customers/export",
      body: { state: "given", month: 11 },
    });
    expect(create).toHaveBeenCalledTimes(1);
    expect(click).toHaveBeenCalledTimes(1);
    fake.replies.push({ status: 409, body: { error: "already_processed", request_id: "t" } });
    fireEvent.click(screen.getByRole("button", { name: "Xuất CSV" }));
    expect(await screen.findByText(/Tệp đã xuất ở lần bấm trước/)).toBeTruthy();
  });

  it("chỉ promotion.read: không nút Xuất CSV", async () => {
    fake.grants = grantsOf();
    await open();
    expect(screen.queryByRole("button", { name: "Xuất CSV" })).toBeNull();
  });
});
