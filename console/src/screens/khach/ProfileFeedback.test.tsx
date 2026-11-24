// @vitest-environment jsdom
// Khối "Góp ý" trong hồ sơ khách (kế hoạch M5 Task 461; mockup docs/mockups/m5/feedback.html trạng thái B, C): góp ý
// của cả nhóm khách (customerId = gốc), mới nhất trước; còn trang sau thì không ghi tổng; không quyền thì không có khối.
import { cleanup, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { feedback } from "../feedback/feedbackTestkit";
import { C1, customer, grantsOf, installFake, renderAt, type Fake } from "./khachTestkit";

let fake: Fake;
beforeEach(() => {
  fake = installFake();
  fake.grants = grantsOf("customer.feedback.read");
  fake.customers.set(C1, customer());
  fake.feedback = [
    feedback({ customerId: C1 }),
    feedback({
      id: "f2",
      customerId: C1,
      topic: "product",
      mood: 5,
      body: "Đầm suông mặc rất đẹp <b>x</b>",
      state: "answered",
      createdAt: "2026-10-02T03:00:00Z",
    }),
    feedback({ id: "f3", customerId: "khac", body: "Của khách khác" }),
  ];
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const block = async () => {
  const h = await screen.findByRole("heading", { name: /^Góp ý/ });
  const card = h.closest("section");
  if (card === null) throw new Error("không có khối Góp ý");
  return card;
};

describe("Khối Góp ý trong hồ sơ khách", () => {
  it("góp ý của nhóm khách: ngày, chủ đề · mức, trạng thái, nội dung thuần; số khi đã hết; nút sang màn Góp ý", async () => {
    renderAt(`/khach/${C1}`);
    const b = await block();
    expect(await within(b).findByRole("heading", { name: "Góp ý (2)" })).toBeTruthy();
    const items = await within(b).findAllByRole("listitem");
    expect(items).toHaveLength(2);
    const [first, second] = items;
    expect(first?.textContent).toContain("21/11");
    expect(first?.textContent).toContain("Giao hàng · 2/5");
    expect(within(first ?? b).getByText("Mới")).toBeTruthy();
    expect(within(second ?? b).getByText("Sản phẩm · 5/5")).toBeTruthy();
    expect(within(second ?? b).getByText("Đã phản hồi")).toBeTruthy();
    expect(within(second ?? b).getByText("Đầm suông mặc rất đẹp <b>x</b>")).toBeTruthy();
    expect(within(b).getByRole("link", { name: "Xem ở Góp ý" }).getAttribute("href")).toBe(`/khach/gop-y?khach=${C1}`);
    expect(within(b).getByText("Gồm góp ý của mọi hồ sơ đã gộp vào khách này.")).toBeTruthy();
    const q = fake.feedbackCalls[0];
    expect(q?.get("customerId")).toBe(C1);
    expect(q?.get("limit")).toBe("5");
  });

  it("còn trang sau: không ghi tổng, ghi 5 gần nhất", async () => {
    fake.feedback = Array.from({ length: 7 }, (_, i) => feedback({ id: `x${String(i)}`, customerId: C1 }));
    renderAt(`/khach/${C1}`);
    const b = await block();
    expect(await within(b).findByRole("heading", { name: "Góp ý · 5 gần nhất" })).toBeTruthy();
    expect(within(b).getAllByRole("listitem")).toHaveLength(5);
  });

  it("góp ý không có mức: chỉ chủ đề", async () => {
    fake.feedback = [feedback({ customerId: C1, topic: "other", mood: undefined })];
    renderAt(`/khach/${C1}`);
    const b = await block();
    expect(await within(b).findByText("Khác")).toBeTruthy();
  });

  it("chưa có góp ý: một dòng", async () => {
    fake.feedback = [];
    renderAt(`/khach/${C1}`);
    const b = await block();
    expect(await within(b).findByText("Khách chưa gửi góp ý nào.")).toBeTruthy();
    expect(within(b).getByRole("heading", { name: "Góp ý (0)" })).toBeTruthy();
  });

  it("API lỗi 403: câu không có quyền trong khối, hồ sơ vẫn mở", async () => {
    fake.feedbackError = { status: 403, body: { error: "forbidden", request_id: "t" } };
    renderAt(`/khach/${C1}`);
    const b = await block();
    expect(await within(b).findByText("Bạn không có quyền xem góp ý của khách.")).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Trần Thị Thu Hà" })).toBeTruthy();
  });

  it("không có quyền xem góp ý: không có khối, không gọi API", async () => {
    fake.grants = grantsOf();
    renderAt(`/khach/${C1}`);
    expect(await screen.findByRole("heading", { name: "Trần Thị Thu Hà" })).toBeTruthy();
    expect(screen.queryByRole("heading", { name: /^Góp ý/ })).toBeNull();
    expect(fake.feedbackCalls).toHaveLength(0);
  });
});
