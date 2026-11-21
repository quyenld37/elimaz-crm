// @vitest-environment jsdom
// Màn Đơn trùng (mockup docs/mockups/m2/don-trung.html, trạng thái A–C; HV25): hai đơn cạnh nhau, sản phẩm chung tô
// nền; "Hủy đơn này" cần xác nhận, gửi version của đơn bị hủy; đơn đã gửi hãng: câu "yêu cầu hủy" (Task 432); "Không
// trùng" không cần, có thông báo; không tự hủy.
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Order } from "../../api";
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
/** Thân trả của resolve: cặp sau xử lý, đơn bị hủy (O1 với cancel_this, O2 với cancel_other) ở trạng thái status. */
function resolved(action: "cancel_this" | "cancel_other", status: "cancelled" | "cancel_requested") {
  const p = fake.duplicates[0];
  if (p === undefined) throw new Error("thiếu cặp");
  return action === "cancel_this"
    ? { ...p, order: { ...p.order, status }, ...(status === "cancelled" ? { resolution: "cancelled" } : {}) }
    : { ...p, otherOrder: { ...p.otherOrder, status }, ...(status === "cancelled" ? { resolution: "cancelled" } : {}) };
}

function patchOrder(id: string, p: Partial<Order>) {
  const o = fake.orders.get(id);
  if (o === undefined) throw new Error("thiếu đơn");
  fake.orders.set(id, { ...o, ...p });
}

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

  it("Hủy đơn này cần xác nhận: bên trái cancel_this, bên phải cancel_other, kèm version của đơn bị hủy", async () => {
    fake.replies.push({ status: 200, body: resolved("cancel_other", "cancelled") });
    fake.replies.push({ status: 200, body: resolved("cancel_this", "cancelled") });
    patchOrder(O1, { version: 7 });
    renderAt("/don/trung");
    const pair = await screen.findByRole("article", { name: "DH0001198 và DH0001236" });
    await within(pair).findByText("Chung 1 sản phẩm");
    const [left, right] = within(pair).getAllByRole("button", { name: "Hủy đơn này" });
    fireEvent.click(right ?? pair);
    const dlg = await screen.findByRole("dialog", { name: "Hủy đơn DH0001236?" });
    expect(within(dlg).getByText(/Hàng của đơn trả về có thể bán ngay; .*Đơn DH0001198 giữ nguyên/)).toBeTruthy();
    expect(fake.writes).toHaveLength(0);
    fireEvent.click(within(dlg).getByRole("button", { name: "Hủy DH0001236" }));
    await waitFor(() => {
      expect(fake.writes.at(-1)?.body).toEqual({ action: "cancel_other", version: 3 });
    });
    expect(await screen.findByText("Đã hủy DH0001236.")).toBeTruthy();
    fireEvent.click(left ?? pair);
    fireEvent.click(
      within(await screen.findByRole("dialog", { name: "Hủy đơn DH0001198?" })).getByRole("button", {
        name: "Hủy DH0001198",
      }),
    );
    await waitFor(() => {
      expect(fake.writes.at(-1)?.body).toEqual({ action: "cancel_this", version: 7 });
    });
  });

  it("đơn đã gửi hãng: hộp nói yêu cầu hủy, hàng chưa về kho; thông báo đã gửi yêu cầu, cặp còn", async () => {
    patchOrder(O2, { status: "confirmed", currentShipmentId: "s-1" });
    fake.replies.push({ status: 200, body: resolved("cancel_other", "cancel_requested") });
    renderAt("/don/trung");
    const pair = await screen.findByRole("article", { name: "DH0001198 và DH0001236" });
    await within(pair).findByText("Chung 1 sản phẩm");
    const [, right] = within(pair).getAllByRole("button", { name: "Hủy đơn này" });
    fireEvent.click(right ?? pair);
    const dlg = await screen.findByRole("dialog", { name: "Hủy đơn DH0001236?" });
    expect(
      within(dlg).getByText(/Đơn đã gửi hãng\. Đơn sẽ thành "Đang yêu cầu hủy".*hàng chưa trả về kho/),
    ).toBeTruthy();
    expect(within(dlg).queryByText(/có thể bán ngay/)).toBeNull();
    fireEvent.click(within(dlg).getByRole("button", { name: "Gửi yêu cầu hủy DH0001236" }));
    expect(await screen.findByText(/^Đã gửi yêu cầu hủy DH0001236\. Hàng chưa về kho/)).toBeTruthy();
    expect(screen.queryByText("Đã hủy DH0001236.")).toBeNull();
    expect(screen.getByRole("article", { name: "DH0001198 và DH0001236" })).toBeTruthy();
  });

  it("409 người khác vừa sửa đơn: câu báo, không thông báo đã hủy, đọc lại hai đơn", async () => {
    fake.replies.push({ status: 409, body: { error: "version_conflict", request_id: "r" } });
    renderAt("/don/trung");
    const pair = await screen.findByRole("article", { name: "DH0001198 và DH0001236" });
    await within(pair).findByText("Chung 1 sản phẩm");
    const before = fake.getCalls.filter((id) => id === O2).length;
    const [, right] = within(pair).getAllByRole("button", { name: "Hủy đơn này" });
    fireEvent.click(right ?? pair);
    fireEvent.click(
      within(await screen.findByRole("dialog", { name: "Hủy đơn DH0001236?" })).getByRole("button", {
        name: "Hủy DH0001236",
      }),
    );
    expect(await screen.findByText(/Người khác vừa sửa mục này/)).toBeTruthy();
    expect(screen.queryByText("Đã hủy DH0001236.")).toBeNull();
    await waitFor(() => {
      expect(fake.getCalls.filter((id) => id === O2).length).toBeGreaterThan(before);
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
