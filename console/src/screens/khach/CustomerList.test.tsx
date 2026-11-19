// @vitest-environment jsdom
// Danh sách khách (mockup docs/mockups/m2/khach-danh-sach.html trạng thái A–C; HV26). Kế hoạch M2 Task M2-29, Step 1:
// tìm "5678" hiện cả hai khách có đuôi đó.
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  C1,
  C2,
  C3,
  LONG_COMPANY,
  T3,
  grantsOf,
  installFake,
  nth,
  listItem,
  renderAt,
  stubDialog,
  type Fake,
} from "./khachTestkit";

let fake: Fake;
beforeEach(() => {
  stubDialog();
  fake = installFake();
  fake.list = [
    listItem(),
    listItem({ id: C2, code: "KH000877", fullName: LONG_COMPANY, phones: ["+84903111222"] }),
    listItem({
      id: C3,
      code: "KH004410",
      fullName: "Lê Hà My",
      phones: ["+84355125678"],
      stats: { orderCount: 1, deliveredCount: 0, returnedCount: 0, totalSpent: 0, returnRateBp: 0 },
    }),
    listItem({
      id: "0195f3b2-0000-7000-8000-00000000c004",
      code: "KH004511",
      fullName: "Mai (fb)",
      phones: [],
      stats: { orderCount: 0, deliveredCount: 0, returnedCount: 0, totalSpent: 0, returnRateBp: 0 },
    }),
  ];
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const search = () => screen.getByRole("searchbox", { name: "Tìm khách" });

describe("Danh sách khách", () => {
  it("khách mới nhất: mã, SĐT đủ, số đơn, đã chi, mua gần nhất; khách chưa có SĐT ghi Chưa có", async () => {
    renderAt("/khach");
    const row = await screen.findByRole("row", { name: /KH000231/ });
    expect(within(row).getByRole("link", { name: "Trần Thị Thu Hà" }).getAttribute("href")).toBe(`/khach/${C1}`);
    expect(within(row).getByText("0912 345 678")).toBeTruthy();
    expect(within(row).getByText("12")).toBeTruthy();
    expect(within(row).getByText("8.450.000 ₫")).toBeTruthy();
    expect(within(row).getByText("18/11")).toBeTruthy();
    expect(within(screen.getByRole("row", { name: /KH000877/ })).getByText(LONG_COMPANY)).toBeTruthy();
    expect(within(screen.getByRole("row", { name: /KH004511/ })).getByText("Chưa có")).toBeTruthy();
    expect(fake.listCalls[0]?.get("q")).toBeNull();
  });

  it('tìm "5678" hiện cả hai khách có đuôi đó, đuôi số được đánh dấu, cột Khớp theo', async () => {
    renderAt("/khach");
    await screen.findByRole("row", { name: /KH000231/ });
    fireEvent.change(search(), { target: { value: "5678" } });
    await waitFor(() => {
      expect(fake.listCalls.at(-1)?.get("q")).toBe("5678");
    });
    const rows = await screen.findAllByRole("row", { name: /Đuôi SĐT/ });
    expect(rows).toHaveLength(2);
    expect(within(nth(rows, 0)).getByText("Trần Thị Thu Hà")).toBeTruthy();
    expect(within(nth(rows, 1)).getByText("Lê Hà My")).toBeTruthy();
    const marks = screen.getAllByText("5 678", { selector: "mark" });
    expect(marks).toHaveLength(2);
    expect(screen.queryByText(LONG_COMPANY)).toBeNull();
  });

  it("gõ 3 số: nhắc gõ ít nhất 4 số cuối, không gọi tìm", async () => {
    renderAt("/khach");
    await screen.findByRole("row", { name: /KH000231/ });
    const before = fake.listCalls.length;
    fireEvent.change(search(), { target: { value: "567" } });
    expect(await screen.findByText("Gõ ít nhất 4 số cuối")).toBeTruthy();
    await new Promise((r) => setTimeout(r, 400));
    expect(fake.listCalls.length).toBe(before);
  });

  it("tìm không ra: câu báo có chữ đã gõ và gợi ý tìm bằng 4 số cuối", async () => {
    renderAt("/khach");
    await screen.findByRole("row", { name: /KH000231/ });
    fireEvent.change(search(), { target: { value: "nguyen thao vy" } });
    expect(await screen.findByRole("heading", { name: 'Không tìm thấy "nguyen thao vy"' })).toBeTruthy();
  });

  it("chưa có khách nào: trang trống có lời nhắc", async () => {
    fake.list = [];
    renderAt("/khach");
    expect(await screen.findByRole("heading", { name: "Chưa có khách nào" })).toBeTruthy();
  });

  it("lọc nhãn gửi tagId", async () => {
    renderAt("/khach");
    await screen.findByRole("row", { name: /KH000231/ });
    fireEvent.change(await screen.findByRole("combobox", { name: "Nhãn" }), { target: { value: T3 } });
    await waitFor(() => {
      expect(fake.listCalls.at(-1)?.get("tagId")).toBe(T3);
    });
  });

  it("chỉ người có quyền thấy nút Nhãn, Nhập Excel, Thêm khách", async () => {
    renderAt("/khach");
    await screen.findByRole("row", { name: /KH000231/ });
    expect(screen.queryByRole("button", { name: "Nhãn" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Nhập Excel" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Thêm khách" })).toBeNull();
    cleanup();
    fake.grants = grantsOf("customer.write", "customer.tags.manage", "customer.import", "files.upload");
    renderAt("/khach");
    expect(await screen.findByRole("button", { name: "Nhãn" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Nhập Excel" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Thêm khách" })).toBeTruthy();
  });

  it("thêm khách: gửi tên và SĐT đã làm sạch kèm khóa; SĐT đã thuộc khách khác → liên kết tới khách đó", async () => {
    fake.grants = grantsOf("customer.write");
    fake.replies.push({ status: 409, body: { error: "identity_taken", request_id: "r", ownerId: C2 } });
    renderAt("/khach");
    fireEvent.click(await screen.findByRole("button", { name: "Thêm khách" }));
    const dlg = screen.getByRole("dialog", { name: "Thêm khách" });
    fireEvent.change(within(dlg).getByLabelText("Họ tên"), { target: { value: "Ngô Lan" } });
    fireEvent.change(within(dlg).getByLabelText("SĐT"), { target: { value: "0903 111 222" } });
    fireEvent.click(within(dlg).getByRole("button", { name: "Thêm khách" }));
    expect(await within(dlg).findByRole("link", { name: "Mở hồ sơ khách đó" })).toBeTruthy();
    expect(within(dlg).getByRole("link", { name: "Mở hồ sơ khách đó" }).getAttribute("href")).toBe(`/khach/${C2}`);
    const w = fake.writes.at(-1);
    expect(w?.path).toBe("/api/v1/customers");
    expect(w?.key).not.toBeNull();
    expect(w?.body).toEqual({ fullName: "Ngô Lan", phone: "0903111222" });
  });
});
