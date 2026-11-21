// @vitest-environment jsdom
// Nhận hàng hoàn (kế hoạch M3 Task 136, ca TD02, TD04; mockup docs/mockups/m3/kho.html trạng thái D; HV26): danh sách
// phiếu hoàn chờ nhận (carrier_return, partial_delivery); nhập 1 / 0 / 0 khi gửi 2 → nút khóa, "Còn thiếu 1 món chưa
// phân loại"; đủ → gửi ba số mỗi dòng và version; nhận xong hiện ba số máy chủ trả.
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Order, Return } from "../../api";
import { order, orderItem } from "../don/donTestkit";
import { grantsOf, installFake, renderAt, type Fake } from "./warehouseTestkit";

const RA = "0195f3b2-0000-7000-8000-00000000a131";
const RB = "0195f3b2-0000-7000-8000-00000000a130";
const OA = "0195f3b2-0000-7000-8000-00000000d088";
const OB = "0195f3b2-0000-7000-8000-00000000d150";
const SA = "0195f3b2-0000-7000-8000-000000005088";
const IA = "0195f3b2-0000-7000-8000-00000000e101";
const IB = "0195f3b2-0000-7000-8000-00000000e102";

const retA: Return = {
  id: RA,
  code: "TH0000031",
  orderId: OA,
  shipmentId: SA,
  kind: "carrier_return",
  status: "requested",
  refundAmount: 0,
  items: [
    { id: "ri1", orderItemId: IA, qty: 2, refundAmount: 0 },
    { id: "ri2", orderItemId: IB, qty: 1, refundAmount: 0 },
  ],
  createdAt: "2026-11-20T00:00:00Z",
  version: 4,
};
const retB: Return = {
  id: RB,
  code: "TH0000030",
  orderId: OB,
  kind: "partial_delivery",
  status: "requested",
  refundAmount: 0,
  items: [{ id: "ri3", orderItemId: IA, qty: 1, refundAmount: 0 }],
  createdAt: "2026-11-20T00:00:00Z",
  version: 1,
};
const orderA: Order = order({
  id: OA,
  code: "DH0001088",
  items: [
    orderItem({ id: IA, sku: "DAM0123-BE-M", name: "Đầm maxi lụa Hàn cao cấp cổ yếm phối ren hoa", qty: 2 }),
    orderItem({ id: IB, sku: "TUI0090-NAU", name: "Túi da mini", qty: 1 }),
  ],
});

let fake: Fake;
beforeEach(() => {
  fake = installFake();
  fake.grants = grantsOf("order.return.receive", "fulfillment.read");
  fake.returns.set(RA, retA);
  fake.returns.set(RB, retB);
  fake.orders.set(OA, orderA);
  fake.orders.set(OB, order({ id: OB, code: "DH0001150" }));
  fake.shipments.set(SA, {
    id: SA,
    orderId: OA,
    code: "E261120AAAA",
    carrierKey: "best",
    carrierAccountId: "0195f3b2-0000-7000-8000-0000000000c2",
    trackingCode: "BE0098811111VN",
    status: "returned",
    codAmount: 0,
    createdAt: "",
  });
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const card = () => screen.findByRole("region", { name: "Nhận hàng hoàn · TH0000031" });
const box = (c: HTMLElement, name: string) => within(c).getByRole("textbox", { name });

describe("Nhận hàng hoàn — danh sách", () => {
  it("phiếu hoàn chờ nhận của hãng và giao một phần; câu tồn chỉ đổi khi nhận (TD02)", async () => {
    renderAt("/warehouse/returns/receive");
    const list = await screen.findByRole("region", { name: "Phiếu hoàn chờ nhận" });
    expect(await within(list).findByRole("link", { name: "DH0001088 · Best Express" })).toBeTruthy();
    expect(within(list).getByText("2 dòng · 3 món · hãng báo đã hoàn về")).toBeTruthy();
    expect(within(list).getByText("Giao một phần · 1 món về kho")).toBeTruthy();
    expect(within(list).getByText("2 phiếu")).toBeTruthy();
    expect(screen.getByText(/Hãng báo "đã hoàn về shop" chưa làm đổi tồn\./)).toBeTruthy();
    expect(screen.getByText("Chọn một phiếu ở danh sách để đếm hàng và nhận vào kho.")).toBeTruthy();
  });

  it("0 phiếu: câu rỗng", async () => {
    fake.returns.clear();
    renderAt("/warehouse/returns/receive");
    expect(await screen.findByText("Không có phiếu hoàn nào chờ nhận.")).toBeTruthy();
  });
});

describe("Nhận hàng hoàn — theo món (TD04)", () => {
  it("gửi 2, nhập 1 / 0 / 0 → nút khóa, 'Còn thiếu 1 món chưa phân loại'", async () => {
    renderAt(`/warehouse/returns/receive/${RA}`);
    const c = await card();
    expect(within(c).getByText("DH0001088 · Best Express BE0098811111VN · 3 món")).toBeTruthy();
    expect(within(c).getByText("Gửi đi 2")).toBeTruthy();
    fireEvent.change(box(c, "Tốt DAM0123-BE-M"), { target: { value: "1" } });
    fireEvent.change(box(c, "Tốt TUI0090-NAU"), { target: { value: "1" } });
    expect(within(c).getByText("Còn thiếu 1 món chưa phân loại (DAM0123-BE-M).")).toBeTruthy();
    expect(within(c).getByRole<HTMLButtonElement>("button", { name: "Nhận vào kho" }).disabled).toBe(true);
  });

  it("tổng vượt hoặc ô không phải số → nút khóa, câu nói rõ", async () => {
    renderAt(`/warehouse/returns/receive/${RA}`);
    const c = await card();
    fireEvent.change(box(c, "Tốt DAM0123-BE-M"), { target: { value: "3" } });
    fireEvent.change(box(c, "Tốt TUI0090-NAU"), { target: { value: "1" } });
    expect(within(c).getByText("Tổng vượt số trên phiếu (DAM0123-BE-M thừa 1).")).toBeTruthy();
    fireEvent.change(box(c, "Tốt DAM0123-BE-M"), { target: { value: "-1" } });
    expect(within(c).getByText("Chỉ nhập số nguyên không âm (DAM0123-BE-M).")).toBeTruthy();
    expect(within(c).getByRole<HTMLButtonElement>("button", { name: "Nhận vào kho" }).disabled).toBe(true);
  });

  it("1 tốt 1 hỏng / 1 thiếu → kể việc sẽ xảy ra, gửi ba số mỗi dòng và version; xong hiện ba số đã nhận", async () => {
    renderAt(`/warehouse/returns/receive/${RA}`);
    const c = await card();
    fireEvent.change(box(c, "Tốt DAM0123-BE-M"), { target: { value: "1" } });
    fireEvent.change(box(c, "Hỏng DAM0123-BE-M"), { target: { value: "1" } });
    fireEvent.change(box(c, "Thiếu TUI0090-NAU"), { target: { value: "1" } });
    const fx = within(c).getByRole("list", { name: "Nhận xong" });
    expect(within(fx).getByText('1 món tốt về "có thể bán"')).toBeTruthy();
    expect(within(fx).getByText('1 món hỏng vào "lỗi"')).toBeTruthy();
    expect(within(fx).getByText('1 món thiếu ghi thất lạc (trừ tồn) và vào "Cần xem" để khiếu nại hãng')).toBeTruthy();
    const received: Return = {
      ...retA,
      status: "restocked",
      receivedAt: "2026-11-21T09:00:00Z",
      version: 5,
      items: [
        { id: "ri1", orderItemId: IA, qty: 2, refundAmount: 0, sellable: 1, damaged: 1, missing: 0 },
        {
          id: "ri2",
          orderItemId: IB,
          qty: 1,
          refundAmount: 0,
          condition: "missing",
          sellable: 0,
          damaged: 0,
          missing: 1,
        },
      ],
    };
    fake.replies.push({ status: 200, body: received });
    fireEvent.click(within(c).getByRole("button", { name: "Nhận vào kho" }));
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    expect(fake.writes[0]).toMatchObject({
      method: "POST",
      path: `/api/v1/returns/${RA}/receive`,
      body: {
        version: 4,
        lines: [
          { orderItemId: IA, condition: "sellable", sellable: 1, damaged: 1, missing: 0 },
          { orderItemId: IB, condition: "missing", sellable: 0, damaged: 0, missing: 1 },
        ],
      },
    });
    expect(fake.writes[0]?.key).toMatch(/.+/);
    const done = await screen.findByRole("region", { name: "Đã nhận · TH0000031" });
    expect(within(done).getByText("Tốt 1 · Hỏng 1 · Thiếu 0")).toBeTruthy();
    expect(within(done).getByText("Tốt 0 · Hỏng 0 · Thiếu 1")).toBeTruthy();
  });

  it("người khác vừa nhận (409 version_conflict): hiện câu lỗi, không coi là đã nhận", async () => {
    renderAt(`/warehouse/returns/receive/${RA}`);
    const c = await card();
    fireEvent.change(box(c, "Tốt DAM0123-BE-M"), { target: { value: "2" } });
    fireEvent.change(box(c, "Tốt TUI0090-NAU"), { target: { value: "1" } });
    fake.replies.push({ status: 409, body: { error: "version_conflict", request_id: "t" } });
    fireEvent.click(within(c).getByRole("button", { name: "Nhận vào kho" }));
    await waitFor(() => {
      expect(c.querySelector(".box-err")).not.toBeNull();
    });
    expect(screen.queryByRole("region", { name: "Đã nhận · TH0000031" })).toBeNull();
  });

  it("phiếu không còn chờ nhận và chưa nhận (đã hủy): câu riêng, không có ô nhập", async () => {
    fake.returns.set(RA, { ...retA, status: "cancelled" });
    renderAt(`/warehouse/returns/receive/${RA}`);
    expect(await screen.findByText("Phiếu TH0000031 không còn chờ nhận.")).toBeTruthy();
    expect(screen.queryByRole("textbox")).toBeNull();
  });
});
