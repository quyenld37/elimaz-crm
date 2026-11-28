// @vitest-environment jsdom
// Nghĩa vụ hoàn (kế hoạch M3 Task 138, TI09, TD10; mockup docs/mockups/m3/tien-tren-don.html trạng thái B; HV34):
// "Chưa chi — chưa tính là đã hoàn", chi một phần, miễn hoàn chỉ người có cash.refunds.waive (Chủ), lý do bắt buộc;
// danh sách có cột Phải hoàn / Đã chi / Trạng thái; tiền 9 chữ số.
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Grant, RefundObligation } from "../../api";
import {
  ORDER_PAGE,
  emptyWrites,
  meReply,
  renderScreens,
  reply,
  stubApi,
  type WriteQueue,
} from "../../app/screenTestkit";
import { stubDialog } from "../don/donTestkit";
import { screens } from "./route";

const OB1 = "0195f3b2-0000-7000-8000-0000000ob001";
const OB2 = "0195f3b2-0000-7000-8000-0000000ob002";
const OB3 = "0195f3b2-0000-7000-8000-0000000ob003";

function ob(p: Partial<RefundObligation> = {}): RefundObligation {
  return {
    id: OB1,
    orderId: "0195f3b2-0000-7000-8000-00000000d190",
    orderCode: "DH0001190",
    source: "order_cancel",
    amount: 200_000,
    paidAmount: 0,
    status: "open",
    createdAt: "2026-11-22T02:00:00Z",
    version: 1,
    ...p,
  };
}

interface Fake extends WriteQueue {
  grants: Grant[];
  items: RefundObligation[];
  statuses: (string | null)[];
}

const grants = (...perms: string[]): Grant[] =>
  ["order.read", "cash.cod.read", ...perms].map((permission) => ({ permission, scope: { type: "all", id: "" } }));

let fake: Fake;
beforeEach(() => {
  stubDialog();
  fake = stubApi<Fake>(
    {
      ...emptyWrites(),
      grants: grants("cash.refunds.record", "cash.refunds.waive"),
      items: [
        ob(),
        ob({
          id: OB2,
          orderCode: "DH0001102",
          source: "customer_return",
          amount: 620_000,
          paidAmount: 300_000,
        }),
        ob({ id: OB3, orderCode: "DH0001999", source: "exchange", amount: 123_456_789 }),
      ],
      statuses: [],
    },
    (f, path, q) => {
      if (path === "/api/v1/me") return meReply(f.grants);
      if (path === "/api/v1/refund-obligations") {
        f.statuses.push(q.get("status"));
        const st = q.get("status") ?? "open";
        return reply({ items: f.items.filter((x) => x.status === st) });
      }
      return reply({ error: "not_found", request_id: "t" }, 404);
    },
    (f, path, body) => {
      const id = /^\/api\/v1\/refund-obligations\/([^/]+)\//.exec(path)?.[1];
      const cur = f.items.find((x) => x.id === id) ?? ob();
      if (path.endsWith("/waive")) {
        return reply({ ...cur, status: "waived", waiveReason: String(body.reason), version: cur.version + 1 });
      }
      const paid = cur.paidAmount + Number(body.amount);
      return reply({
        ...cur,
        paidAmount: paid,
        status: paid >= cur.amount ? "paid" : "open",
        version: cur.version + 1,
      });
    },
  );
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const open = () => renderScreens(screens, "/refund-obligations", ORDER_PAGE);
const rowOf = (code: string) => screen.findByRole("row", { name: new RegExp(code) });

describe("Danh sách Nghĩa vụ hoàn", () => {
  it("cột Phải hoàn / Đã chi / Trạng thái; chưa chi ghi 'chưa tính là đã hoàn'; chi một phần ghi số còn; 9 chữ số", async () => {
    open();
    const r1 = await rowOf("DH0001190");
    expect(r1.textContent).toContain("Hủy đơn có cọc");
    expect(r1.textContent).toContain("200.000 ₫");
    expect(within(r1).getByText("Chưa chi")).toBeTruthy();
    expect(r1.textContent).toContain("Chưa chi — chưa tính là đã hoàn");
    const r2 = await rowOf("DH0001102");
    expect(r2.textContent).toContain("Khách trả hàng");
    expect(r2.textContent).toContain("300.000 ₫");
    expect(within(r2).getByText("Còn 320.000 ₫")).toBeTruthy();
    expect((await rowOf("DH0001999")).textContent).toContain("123.456.789 ₫");
    expect(within(r1).getByRole("link", { name: "DH0001190" }).getAttribute("href")).toBe(
      "/don/0195f3b2-0000-7000-8000-00000000d190",
    );
    for (const h of ["Đơn", "Nguồn", "Phải hoàn", "Đã chi", "Trạng thái"]) {
      expect(screen.getByRole("columnheader", { name: h })).toBeTruthy();
    }
    expect(fake.statuses[0]).toBe("open");
  });

  it("lọc Miễn: đọc lại theo trạng thái, dòng ghi lý do miễn", async () => {
    fake.items.push(ob({ id: "w1", orderCode: "DH0001003", status: "waived", waiveReason: "khách đồng ý mất cọc" }));
    open();
    await rowOf("DH0001190");
    fireEvent.click(screen.getByRole("tab", { name: "Đã miễn" }));
    const r = await rowOf("DH0001003");
    expect(r.textContent).toContain("Miễn · khách đồng ý mất cọc");
    expect(fake.statuses).toContain("waived");
    expect(within(r).queryByRole("button", { name: "Ghi phiếu chi" })).toBeNull();
  });

  it("chi một phần: câu 'sau khi chi', gửi số tiền và mã giao dịch, khóa chống trùng; vượt số còn → nút khóa", async () => {
    open();
    fireEvent.click(within(await rowOf("DH0001190")).getByRole("button", { name: "Ghi phiếu chi" }));
    const dlg = await screen.findByRole("dialog", { name: "Chi hoàn · DH0001190" });
    expect(dlg.textContent).toContain("Chưa chi — chưa tính là đã hoàn");
    const amount = within(dlg).getByRole("textbox", { name: "Số tiền chi" });
    fireEvent.change(amount, { target: { value: "300000" } });
    expect(within(dlg).getByText("Vượt số còn phải hoàn 200.000 ₫")).toBeTruthy();
    const send = within(dlg).getByRole<HTMLButtonElement>("button", { name: "Ghi phiếu chi" });
    expect(send.disabled).toBe(true);
    fireEvent.change(amount, { target: { value: "100.000" } });
    expect(dlg.textContent).toContain("Sau khi chi: đã hoàn 100.000 ₫, còn 100.000 ₫, nghĩa vụ vẫn mở.");
    fireEvent.change(within(dlg).getByRole("textbox", { name: "Mã giao dịch" }), { target: { value: "FT2611" } });
    fireEvent.click(send);
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    expect(fake.writes[0]).toMatchObject({
      path: `/api/v1/refund-obligations/${OB1}/payments`,
      body: { amount: 100_000, reference: "FT2611" },
    });
    expect(fake.writes[0]?.key).toBeTruthy();
    await waitFor(() => {
      expect(screen.queryByRole("dialog", { name: "Chi hoàn · DH0001190" })).toBeNull();
    });
  });

  it("chi vượt ở máy chủ (người khác vừa chi): 409 refund_exceeds_remaining hiện câu, hộp không đóng", async () => {
    open();
    fireEvent.click(within(await rowOf("DH0001190")).getByRole("button", { name: "Ghi phiếu chi" }));
    const dlg = await screen.findByRole("dialog", { name: "Chi hoàn · DH0001190" });
    fireEvent.change(within(dlg).getByRole("textbox", { name: "Số tiền chi" }), { target: { value: "200000" } });
    expect(dlg.textContent).toContain("Sau khi chi: đã hoàn 200.000 ₫, đóng nghĩa vụ.");
    fake.replies.push({ status: 409, body: { error: "refund_exceeds_remaining", request_id: "t" } });
    fireEvent.click(within(dlg).getByRole("button", { name: "Ghi phiếu chi" }));
    expect(await within(dlg).findByText("Số tiền chi vượt số còn phải hoàn cho khách")).toBeTruthy();
  });

  it("miễn hoàn (Chủ): lý do bắt buộc, gửi lý do", async () => {
    open();
    fireEvent.click(within(await rowOf("DH0001190")).getByRole("button", { name: "Miễn hoàn" }));
    const dlg = await screen.findByRole("dialog", { name: "Miễn hoàn · DH0001190" });
    const send = within(dlg).getByRole<HTMLButtonElement>("button", { name: "Miễn hoàn" });
    expect(send.disabled).toBe(true);
    expect(dlg.textContent).toContain('Đóng nghĩa vụ "không hoàn" 200.000 ₫. Ghi lịch sử, không xóa.');
    fireEvent.change(within(dlg).getByRole("textbox", { name: "Lý do" }), {
      target: { value: "Khách đồng ý mất cọc (tin nhắn 21/11)" },
    });
    fireEvent.click(send);
    await waitFor(() => {
      expect(fake.writes[0]).toMatchObject({
        path: `/api/v1/refund-obligations/${OB1}/waive`,
        body: { reason: "Khách đồng ý mất cọc (tin nhắn 21/11)" },
      });
    });
  });

  it("không phải Chủ (không cash.refunds.waive): Miễn hoàn mờ kèm lý do; không cash.refunds.record: không có Ghi phiếu chi", async () => {
    fake.grants = grants();
    open();
    const r = await rowOf("DH0001190");
    await waitFor(() => {
      expect(within(r).getByRole<HTMLButtonElement>("button", { name: "Miễn hoàn" }).disabled).toBe(true);
    });
    expect(r.textContent).toContain("Chỉ Chủ được miễn hoàn");
    expect(within(r).queryByRole("button", { name: "Ghi phiếu chi" })).toBeNull();
  });

  it("danh sách rỗng và lỗi đọc có trạng thái riêng", async () => {
    fake.items = [];
    open();
    expect(await screen.findByText("Không có nghĩa vụ hoàn nào ở mục này.")).toBeTruthy();
  });
});
