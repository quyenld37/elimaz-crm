// @vitest-environment jsdom
// Cả khung bên phải (Task 176; mockup khung-ben-phai.html trạng thái A): thẻ khách, nhãn (CH07), nhắc việc, gọi điện,
// người phụ trách; quyền theo Page. OrderForm của M2 thay bằng bản giả (khung lên đơn có test riêng).
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import SidePanel from "./SidePanel";
import {
  CONV,
  TAG_DONE,
  TAG_SIZE,
  convIdOther,
  detail,
  onPage,
  reminder,
  renderSide,
  shop,
  sideFake,
  stubSide,
} from "./sidePanelTestkit";

vi.mock("../../len-don/OrderForm", async (orig) => ({
  ...(await orig<typeof import("../../len-don/OrderForm")>()),
  OrderForm: () => <p>Form lên đơn giả</p>,
}));

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const section = (name: string) => screen.getByRole("region", { name });

function tomorrowAt(h: number): string {
  const d = new Date(Date.now() + 24 * 3600_000);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${String(d.getFullYear())}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(h)}:00`;
}

describe("khung bên phải đủ phần (trạng thái A)", () => {
  it("Task 501: CH08 newLead → chip 'Khách tiềm năng mới' ở thẻ khách; HV02 nút '‹ Hội thoại' (chỉ điện thoại) về #hoi-thoai", async () => {
    stubSide(sideFake({ detail: detail({ newLead: true }) }));
    renderSide(<SidePanel conversationId={CONV} />);
    const khach = within(await screen.findByRole("region", { name: "Khách" }));
    expect((await khach.findByText("Khách tiềm năng mới")).classList.contains("chip")).toBe(true);
    const back = screen.getByRole("link", { name: "‹ Hội thoại" });
    expect(back.getAttribute("href")).toBe("#hoi-thoai");
    expect(back.classList.contains("only-phone")).toBe(true);
  });

  it("Task 501: không newLead → không có chip", async () => {
    stubSide(sideFake());
    renderSide(<SidePanel conversationId={CONV} />);
    expect(await screen.findByText("Trần Thị Thu Hà", { selector: "b" })).toBeTruthy();
    expect(screen.queryByText("Khách tiềm năng mới")).toBeNull();
  });

  it("thẻ khách, đơn gần đây, nhãn, người phụ trách, nhắc việc, gọi điện", async () => {
    stubSide(
      sideFake({ detail: detail({ tags: [{ id: TAG_SIZE, name: "Hỏi size", color: "soft", sort: 1, version: 1 }] }) }),
    );
    renderSide(<SidePanel conversationId={CONV} />);
    expect(await screen.findByText("Trần Thị Thu Hà", { selector: "b" })).toBeTruthy();
    for (const name of ["Khách", "Nhãn", "Người phụ trách", "Nhắc việc", "Gọi điện", "Lên đơn"]) {
      expect(section(name)).toBeTruthy();
    }
    expect(within(section("Nhãn")).getByText("Hỏi size")).toBeTruthy();
  });

  it("thẻ khách lỗi → câu lỗi, không vẽ như khách rỗng", async () => {
    const f = stubSide(sideFake());
    f.card = { ...f.card };
    vi.stubGlobal(
      "fetch",
      vi.fn((req: Request) =>
        Promise.resolve(
          new URL(req.url).pathname.endsWith("/customer-card")
            ? Response.json({ error: "forbidden", request_id: "t" }, { status: 403 })
            : Response.json({ error: "not_found", request_id: "t" }, { status: 404 }),
        ),
      ),
    );
    renderSide(<SidePanel conversationId={CONV} />);
    expect(await screen.findByRole("alert")).toBeTruthy();
    expect(screen.queryByText("Khách mới, chưa có đơn")).toBeNull();
  });
});

describe("nhãn (CH07)", () => {
  it("gỡ nhãn gửi DELETE; + Nhãn chỉ liệt kê nhãn chưa gắn, chọn gửi PUT", async () => {
    const f = stubSide(
      sideFake({ detail: detail({ tags: [{ id: TAG_SIZE, name: "Hỏi size", color: "soft", sort: 1, version: 1 }] }) }),
    );
    renderSide(<SidePanel conversationId={CONV} />);
    const tags = await screen.findByRole("region", { name: "Nhãn" });
    fireEvent.click(await within(tags).findByRole("button", { name: "Gỡ nhãn Hỏi size" }));
    await waitFor(() => {
      expect(f.writes[0]).toMatchObject({ method: "DELETE", path: `/api/v1/conversations/${CONV}/tags/${TAG_SIZE}` });
    });
    fireEvent.click(within(tags).getByRole("button", { name: "+ Nhãn" }));
    const menu = await within(tags).findByRole("listbox", { name: "Chọn nhãn" });
    expect(within(menu).queryByRole("option", { name: "Hỏi size" })).toBeNull();
    fireEvent.click(within(menu).getByRole("option", { name: "Đã lên đơn" }));
    await waitFor(() => {
      expect(f.writes[1]).toMatchObject({ method: "PUT", path: `/api/v1/conversations/${CONV}/tags/${TAG_DONE}` });
    });
  });

  it("chỉ xem (không conversation.reply) → không gắn, không gỡ, không nhắc việc", async () => {
    stubSide(
      sideFake({
        grants: [onPage("conversation.read"), shop("customer.read")],
        detail: detail({ tags: [{ id: TAG_SIZE, name: "Hỏi size", color: "soft", sort: 1, version: 1 }] }),
      }),
    );
    renderSide(<SidePanel conversationId={CONV} />);
    const tags = await screen.findByRole("region", { name: "Nhãn" });
    await within(tags).findByText("Hỏi size");
    expect(within(tags).queryByRole("button", { name: "+ Nhãn" })).toBeNull();
    expect(within(tags).queryByRole("button", { name: "Gỡ nhãn Hỏi size" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Nhắc tôi lúc…" })).toBeNull();
  });
});

describe("nhắc việc", () => {
  it("chỉ hiện nhắc của hội thoại này; Xong gửi completeReminder", async () => {
    const mine = reminder();
    const f = stubSide(
      sideFake({
        reminders: [
          mine,
          reminder({ id: "0195f3b2-0000-7000-8000-00000006a002", conversationId: convIdOther, note: "Việc khác" }),
        ],
      }),
    );
    renderSide(<SidePanel conversationId={CONV} />);
    const box = await screen.findByRole("region", { name: "Nhắc việc" });
    expect(await within(box).findByText(/Gọi lại xác nhận size/)).toBeTruthy();
    expect(within(box).queryByText(/Việc khác/)).toBeNull();
    fireEvent.click(within(box).getByRole("button", { name: "Xong" }));
    await waitFor(() => {
      expect(f.writes[0]).toMatchObject({ method: "POST", path: `/api/v1/reminders/${mine.id}/done` });
    });
  });

  it("Nhắc tôi lúc… gửi createReminder với giờ đã chọn và ghi chú; thiếu giờ báo tại ô", async () => {
    const f = stubSide(sideFake());
    renderSide(<SidePanel conversationId={CONV} />);
    const box = await screen.findByRole("region", { name: "Nhắc việc" });
    fireEvent.click(await within(box).findByRole("button", { name: "Nhắc tôi lúc…" }));
    fireEvent.click(within(box).getByRole("button", { name: "Lưu nhắc" }));
    expect(await within(box).findByText("Chọn giờ nhắc.")).toBeTruthy();
    expect(f.writes).toHaveLength(0);
    const at = tomorrowAt(9);
    fireEvent.change(within(box).getByLabelText("Nhắc lúc *"), { target: { value: at } });
    fireEvent.change(within(box).getByLabelText("Việc cần làm"), { target: { value: "Gọi chốt size" } });
    fireEvent.click(within(box).getByRole("button", { name: "Lưu nhắc" }));
    await waitFor(() => {
      expect(f.writes[0]).toMatchObject({
        path: `/api/v1/conversations/${CONV}/reminders`,
        body: { remindAt: new Date(at).toISOString(), note: "Gọi chốt size" },
      });
    });
  });
});
