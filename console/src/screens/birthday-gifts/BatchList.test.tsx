// @vitest-environment jsdom
// Tab Đợt mã (kế hoạch M5 Task 209 Step 1): danh sách 60 đợt (tải thêm), chi tiết 30 mã với các trạng thái (đã dùng, đã
// tặng, chưa tặng, hết hạn), đợt rỗng, không quyền → 403 hiện thông báo (mockup trạng thái D; WB05).
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { PATH, batch, code, installAt, renderAt, teardown, type Fake } from "./birthdayTestkit";

let fake: Fake;
beforeEach(() => {
  fake = installAt();
  fake.batches = Array.from({ length: 60 }, (_, i) => batch(i + 1));
});
afterEach(() => {
  teardown();
});

describe("Đợt mã", () => {
  it("60 đợt quà sinh nhật: 50 đầu, Tải thêm đủ 60; lọc loại gửi purpose", async () => {
    renderAt(`${PATH}?tab=dot-ma`);
    await screen.findByRole("row", { name: /Sinh nhật 1\/11\/2026/ });
    expect(fake.batchCalls[0]?.get("purpose")).toBe("birthday");
    expect(screen.getByText("50 đợt")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Tải thêm" }));
    expect(await screen.findByText("60 đợt")).toBeTruthy();
    fireEvent.change(screen.getByRole("combobox", { name: "Loại đợt" }), { target: { value: "imported" } });
    await waitFor(() => {
      expect(fake.batchCalls.at(-1)?.get("purpose")).toBe("imported");
    });
    expect(await screen.findByText("Chưa có đợt mã nào")).toBeTruthy();
  });

  it("danh sách đợt: cột Đã tặng, Đã dùng, Hết hạn theo số máy chủ; đợt cũ chưa có số → 0", async () => {
    fake.batches = [batch(26, { quantity: 11, givenCount: 11, usedCount: 5, expiredCount: 6 }), batch(20)];
    renderAt(`${PATH}?tab=dot-ma`);
    const r = await screen.findByRole("row", { name: /Sinh nhật 26\/11\/2026/ });
    const cell = (row: HTMLElement, label: string) => row.querySelector(`td[data-label="${label}"]`)?.textContent;
    expect([cell(r, "Mã"), cell(r, "Đã tặng"), cell(r, "Đã dùng"), cell(r, "Hết hạn")]).toEqual(["11", "11", "5", "6"]);
    const old = screen.getByRole("row", { name: /Sinh nhật 20\/11\/2026/ });
    expect([cell(old, "Đã tặng"), cell(old, "Đã dùng"), cell(old, "Hết hạn")]).toEqual(["0", "0", "0"]);
    expect(screen.getByRole("columnheader", { name: "Đã tặng" })).toBeTruthy();
  });

  it("chi tiết đợt: SĐT gắn mã đã che, mã đã dùng ghi mã đơn mở được đơn; mã không gắn số → —", async () => {
    fake.batches = [batch(1, { quantity: 2 })];
    fake.codes = [
      code(1, {
        usedCount: 1,
        boundPhoneMasked: "0905 *** 781",
        usedOrderCode: "DH0001288",
        usedOrderId: "0195f3b2-0000-7000-8000-0000000f0001",
      }),
      code(2, { bound: false }),
    ];
    renderAt(`${PATH}?tab=dot-ma&dot=${batch(1).id}`);
    const used = await screen.findByRole("row", { name: /SNB001/ });
    expect(within(used).getByText("0905 *** 781")).toBeTruthy();
    const link = within(used).getByRole("link", { name: "DH0001288" });
    expect(link.getAttribute("href")).toBe("/don/0195f3b2-0000-7000-8000-0000000f0001");
    const free = screen.getByRole("row", { name: /SNB002/ });
    expect(free.querySelector('td[data-label="Gắn với số"]')?.textContent).toBe("—");
    expect(screen.getByRole("columnheader", { name: "Gắn với số" })).toBeTruthy();
  });

  it("chi tiết đợt: số đã tặng/dùng/hết hạn và 30 mã ba trạng thái", async () => {
    const b = batch(1, { quantity: 30, givenCount: 20, usedCount: 10, expiredCount: 0 });
    fake.batches = [b];
    fake.codes = Array.from({ length: 30 }, (_, i) => {
      if (i < 10) return code(i, { usedCount: 1, givenAt: "2026-11-08T03:00:00Z" });
      if (i < 20) return code(i, { givenAt: "2026-11-08T03:00:00Z" });
      return code(i);
    });
    renderAt(`${PATH}?tab=dot-ma`);
    const r = await screen.findByRole("row", { name: /Sinh nhật 1\/11\/2026/ });
    fireEvent.click(within(r).getByRole("button", { name: "Mở" }));
    const detail = await screen.findByTestId("bg-batch");
    await waitFor(() => {
      expect(within(detail).getAllByText("Đã dùng")).toHaveLength(10);
    });
    expect(within(detail).getByText("Sinh nhật 1/11/2026 · 30 mã")).toBeTruthy();
    expect(within(detail).getByText("Đã tặng 20 · đã dùng 10 · hết hạn chưa dùng 0")).toBeTruthy();
    expect(within(detail).getAllByText("Đã tặng 08/11")).toHaveLength(10);
    expect(within(detail).getAllByText("Chưa tặng")).toHaveLength(10);
    expect(within(detail).getByText("SNB000")).toBeTruthy();
    expect(within(detail).getAllByText("21/12/2026").length).toBeGreaterThan(0);
    expect(within(detail).getByText(/Giảm 50.000 ₫/)).toBeTruthy();
  });

  it("mã hết hạn chưa dùng: dòng mờ", async () => {
    fake.batches = [batch(1, { quantity: 1 })];
    fake.codes = [code(1, { validTo: "2026-11-09T17:00:00Z" })];
    renderAt(`${PATH}?tab=dot-ma&dot=${batch(1).id}`);
    const r = await screen.findByRole("row", { name: /SNB001/ });
    expect(within(r).getByText("Hết hạn, chưa dùng")).toBeTruthy();
    expect(r.className).toContain("is-muted");
  });

  it("đợt rỗng: không nút Mở, câu không có khách đến hạn", async () => {
    fake.batches = [batch(20, { quantity: 0 })];
    renderAt(`${PATH}?tab=dot-ma`);
    const r = await screen.findByRole("row", { name: /Sinh nhật 20\/11\/2026/ });
    expect(within(r).queryByRole("button", { name: "Mở" })).toBeNull();
    expect(within(r).getByText("Không có khách đến hạn")).toBeTruthy();
  });

  it("không quyền (403): hiện thông báo", async () => {
    fake.forbidden = true;
    renderAt(`${PATH}?tab=dot-ma`);
    expect((await screen.findAllByText("Bạn không có quyền xem khuyến mãi.")).length).toBeGreaterThan(0);
    expect(screen.queryByText("Chưa có đợt mã nào")).toBeNull();
  });
});
