// @vitest-environment jsdom
// Màn Đơn trùng (mockup docs/mockups/m2/don-trung.html, trạng thái A–C; HV25): hai đơn cạnh nhau, sản phẩm chung tô
// nền; "Hủy đơn này" cần xác nhận; "Không trùng" không cần, có thông báo; không tự hủy.
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  O1,
  O2,
  V1,
  V2,
  V3,
  grantsOf,
  installFake,
  listItem,
  order,
  orderItem,
  renderAt,
  stubDialog,
  type Fake,
} from "./donTestkit";

let fake: Fake;
beforeEach(() => {
  stubDialog();
  fake = installFake();
  fake.grants = grantsOf("order.duplicates.resolve");
  fake.duplicates = [
    {
      order: listItem({ id: O1, code: "DH0001198", createdAt: "2026-11-18T02:12:00Z" }),
      otherOrder: listItem({ id: O2, code: "DH0001236", createdAt: "2026-11-18T07:31:00Z" }),
      matchedBy: "customer",
      createdAt: "2026-11-18T07:32:00Z",
    },
  ];
  fake.orders.set(
    O1,
    order({
      id: O1,
      code: "DH0001198",
      items: [orderItem({ variantId: V1 }), orderItem({ id: "x2", variantId: V2, sku: "AO0789-DEN-M", qty: 1 })],
    }),
  );
  fake.orders.set(
    O2,
    order({
      id: O2,
      code: "DH0001236",
      items: [orderItem({ variantId: V1, qty: 2 }), orderItem({ id: "x3", variantId: V3, sku: "TUI0090-NAU", qty: 1 })],
    }),
  );
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("Đơn trùng", () => {
  it("một cặp: cách khớp, số sản phẩm chung, khoảng cách giờ; sản phẩm chung được đánh dấu", async () => {
    renderAt("/don/trung");
    const pair = await screen.findByRole("article", { name: "DH0001198 và DH0001236" });
    expect(within(pair).getByText("Cùng khách")).toBeTruthy();
    expect(await within(pair).findByText("Chung 1 sản phẩm")).toBeTruthy();
    expect(within(pair).getByText("Cách nhau 5 giờ 19 phút")).toBeTruthy();
    await waitFor(() => {
      expect(
        within(pair)
          .getAllByText(/DAM0123-BE-M ×/)
          .every((e) => e.className.includes("same")),
      ).toBe(true);
    });
    expect(within(pair).getByText("AO0789-DEN-M × 1").className).not.toContain("same");
  });

  it("Không trùng: gửi not_duplicate, cặp biến mất, có thông báo", async () => {
    renderAt("/don/trung");
    const pair = await screen.findByRole("article", { name: "DH0001198 và DH0001236" });
    fake.duplicates = [];
    fireEvent.click(within(pair).getByRole("button", { name: "Không trùng" }));
    await waitFor(() => {
      expect(fake.writes.at(-1)).toMatchObject({
        path: `/api/v1/order-duplicates/${O1}/${O2}/resolve`,
        body: { action: "not_duplicate" },
      });
    });
    expect(await screen.findByText("Đã ghi DH0001198 và DH0001236 là không trùng.")).toBeTruthy();
    expect(await screen.findByText("Không có đơn trùng cần xem")).toBeTruthy();
  });

  it("Hủy đơn này cần xác nhận: bên trái cancel_this, bên phải cancel_other", async () => {
    renderAt("/don/trung");
    const pair = await screen.findByRole("article", { name: "DH0001198 và DH0001236" });
    const [left, right] = within(pair).getAllByRole("button", { name: "Hủy đơn này" });
    fireEvent.click(right ?? pair);
    const dlg = await screen.findByRole("dialog", { name: "Hủy đơn DH0001236?" });
    expect(within(dlg).getByText(/Đơn DH0001198 giữ nguyên/)).toBeTruthy();
    expect(fake.writes).toHaveLength(0);
    fireEvent.click(within(dlg).getByRole("button", { name: "Hủy DH0001236" }));
    await waitFor(() => {
      expect(fake.writes.at(-1)?.body).toEqual({ action: "cancel_other" });
    });
    fireEvent.click(left ?? pair);
    fireEvent.click(
      within(await screen.findByRole("dialog", { name: "Hủy đơn DH0001198?" })).getByRole("button", {
        name: "Hủy DH0001198",
      }),
    );
    await waitFor(() => {
      expect(fake.writes.at(-1)?.body).toEqual({ action: "cancel_this" });
    });
  });

  it("không có quyền xử lý: không có nút; không còn cặp: câu trống", async () => {
    fake.grants = grantsOf();
    renderAt("/don/trung");
    const pair = await screen.findByRole("article", { name: "DH0001198 và DH0001236" });
    await new Promise((r) => setTimeout(r, 20));
    expect(within(pair).queryByRole("button")).toBeNull();
    cleanup();
    fake.duplicates = [];
    renderAt("/don/trung");
    expect(await screen.findByText("Không có đơn trùng cần xem")).toBeTruthy();
  });
});
