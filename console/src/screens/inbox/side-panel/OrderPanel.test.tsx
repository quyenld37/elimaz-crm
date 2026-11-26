// @vitest-environment jsdom
// Lên đơn và hoàn tất đơn nháp cạnh khung chat (Task 176; mockup khung-ben-phai.html trạng thái B, C; HV25, HV27, HV28;
// LV04 phía màn hình; 01 mục 6). OrderForm của M2 thay bằng bản giả để xem đúng prop khung truyền vào.
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Order } from "../../../api";
import type { OrderFormProps } from "../../len-don/OrderForm";
import { OrderPanel } from "./OrderPanel";
import {
  CONV,
  CUST,
  DRAFT,
  SALE,
  SRC_LIVE,
  TAG_DONE,
  draftOrder,
  orderRef,
  renderSide,
  shop,
  onPage,
  sideFake,
  stubSide,
} from "./sidePanelTestkit";

const form = vi.hoisted(() => ({ props: null as OrderFormProps | null }));

vi.mock("../../len-don/OrderForm", async (orig) => ({
  ...(await orig<typeof import("../../len-don/OrderForm")>()),
  OrderForm: (p: OrderFormProps) => {
    form.props = p;
    return <p>Form lên đơn giả</p>;
  },
}));

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  form.props = null;
});

const CUSTOMER = { id: CUST, code: "KH000231", fullName: "Trần Thị Thu Hà" };
const created: Order = draftOrder({
  id: "0195f3b2-0000-7000-8000-00000004e001",
  code: "DH0001240",
  status: "confirmed",
});

function renderPanel(drafts = [] as ReturnType<typeof orderRef>[], grants = SALE) {
  const f = stubSide(sideFake({ grants, orders: [draftOrder()] }));
  renderSide(
    <OrderPanel conversationId={CONV} customer={CUSTOMER} drafts={drafts} grants={grants} pageId={f.detail.page.id} />,
  );
  return f;
}

const props = async () => {
  await waitFor(() => {
    expect(form.props).not.toBeNull();
  });
  const p = form.props;
  if (p === null) throw new Error("chưa có form");
  return p;
};

describe("lên đơn trong khung chat (01 mục 6)", () => {
  it("mở lên đơn → OrderForm nhận hội thoại, nguồn đơn của bối cảnh, khách của hội thoại; gửi nhịp tim drafting_order", async () => {
    const f = renderPanel();
    fireEvent.click(screen.getByRole("button", { name: "Lên đơn" }));
    const p = await props();
    expect(p.conversationId).toBe(CONV);
    expect(p.initialTrafficSourceId).toBe(SRC_LIVE);
    expect(p.initialCustomer).toEqual(CUSTOMER);
    expect(p.edit).toBeUndefined();
    await waitFor(() => {
      expect(f.writes.some((w) => w.path === `/api/v1/conversations/${CONV}/presence`)).toBe(true);
    });
    expect(f.writes.find((w) => w.path.endsWith("/presence"))?.body).toEqual({ state: "drafting_order" });
  });

  it('tạo đơn xong → gợi ý "Gắn nhãn Đã lên đơn" (không tự gắn); bấm → addConversationTag đúng một lần (HV27)', async () => {
    const f = renderPanel();
    fireEvent.click(screen.getByRole("button", { name: "Lên đơn" }));
    const p = await props();
    p.onCreated?.(created);
    const chip = await screen.findByRole("button", { name: 'Gắn nhãn "Đã lên đơn"' });
    expect(screen.getByText(/Đã lưu DH0001240/)).toBeTruthy();
    expect(f.writes.filter((w) => w.path.includes("/tags/"))).toHaveLength(0);
    fireEvent.click(chip);
    await waitFor(() => {
      expect(f.writes.filter((w) => w.path.includes("/tags/"))).toHaveLength(1);
    });
    expect(f.writes.find((w) => w.path.includes("/tags/"))).toMatchObject({
      method: "PUT",
      path: `/api/v1/conversations/${CONV}/tags/${TAG_DONE}`,
    });
    await waitFor(() => {
      expect(screen.queryByRole("button", { name: 'Gắn nhãn "Đã lên đơn"' })).toBeNull();
    });
  });

  it('"Bỏ qua" đóng gợi ý mà không gắn nhãn', async () => {
    const f = renderPanel();
    fireEvent.click(screen.getByRole("button", { name: "Lên đơn" }));
    (await props()).onCreated?.(created);
    fireEvent.click(await screen.findByRole("button", { name: "Bỏ qua" }));
    expect(screen.queryByRole("button", { name: 'Gắn nhãn "Đã lên đơn"' })).toBeNull();
    expect(f.writes.filter((w) => w.path.includes("/tags/"))).toHaveLength(0);
  });

  it("không có order.create → khung lên đơn khóa sẵn kèm lý do, không có nút Lên đơn (HV28)", () => {
    renderPanel([], [onPage("conversation.read"), onPage("conversation.reply")]);
    expect(screen.getByText("Bạn chưa lên đơn được ở đây.")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Lên đơn" })).toBeNull();
  });
});

describe("hoàn tất đơn nháp từ Live (LV04, HV25)", () => {
  const draftRef = orderRef({
    id: DRAFT,
    code: "DH0001238",
    status: "draft",
    grandTotal: 2580000,
    draftExpiresAt: "2026-11-20T13:00:00Z",
    version: 3,
  });

  /** Mở hoàn tất đơn nháp, chờ form và nhịp tim (để câu trả lời xếp sẵn rơi đúng vào lần xác nhận). */
  async function openDraft() {
    const f = renderPanel([draftRef]);
    fireEvent.click(screen.getByRole("button", { name: "Hoàn tất DH0001238" }));
    const p = await props();
    await waitFor(() => {
      expect(f.writes.some((w) => w.path.endsWith("/presence"))).toBe(true);
    });
    return { f, p };
  }

  it("đơn nháp hiện trong Đơn nháp đang mở; Hoàn tất mở form sửa đơn đó, lưu xong confirmOrder với version mới nhất", async () => {
    const f = renderPanel([draftRef]);
    expect(screen.getByText(/DH0001238/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Hoàn tất DH0001238" }));
    const p = await props();
    expect(p.edit?.order.id).toBe(DRAFT);
    expect(p.edit?.confirmAfterSave).toBe(true);
    p.edit?.onSaved(draftOrder({ version: 4 }));
    await waitFor(() => {
      expect(f.writes.some((w) => w.path === `/api/v1/orders/${DRAFT}/confirm`)).toBe(true);
    });
    expect(f.writes.find((w) => w.path.endsWith("/confirm"))?.body).toEqual({ version: 4 });
    expect(await screen.findByRole("button", { name: 'Gắn nhãn "Đã lên đơn"' })).toBeTruthy();
  });

  it('xác nhận gặp 409 version_conflict → "Có người vừa sửa, tải lại", không tự gửi lại', async () => {
    const { f, p } = await openDraft();
    f.replies.push({ status: 409, body: { error: "version_conflict", request_id: "t" } });
    p.edit?.onSaved(draftOrder({ version: 4 }));
    expect(await screen.findByText(/Có người vừa sửa, tải lại/)).toBeTruthy();
    expect(f.writes.filter((w) => w.path.endsWith("/confirm"))).toHaveLength(1);
    expect(screen.getByRole("button", { name: "Tải lại đơn nháp" })).toBeTruthy();
  });

  it("đã lưu thay đổi mà xác nhận bị từ chối (422 needs_approval) → câu báo, form dựng lại từ đơn vừa lưu (version mới)", async () => {
    const { f, p } = await openDraft();
    f.replies.push({ status: 422, body: { error: "needs_approval", request_id: "t" } });
    p.edit?.onSaved(draftOrder({ version: 4 }));
    expect(await screen.findByRole("alert")).toBeTruthy();
    await waitFor(() => {
      expect(form.props?.edit?.order.version).toBe(4);
    });
  });

  it("không có order.confirm → không có nút Hoàn tất (máy chủ cần order.update và order.confirm)", () => {
    renderPanel([draftRef], [onPage("conversation.read"), shop("order.create"), shop("order.update")]);
    expect(screen.getByText(/DH0001238/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Hoàn tất DH0001238" })).toBeNull();
  });
});
