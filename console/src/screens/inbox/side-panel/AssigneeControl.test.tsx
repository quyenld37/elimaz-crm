// @vitest-environment jsdom
// Người phụ trách (Task 176; mockup khung-ben-phai.html trạng thái A, C; HV31; CH06 phía màn hình).
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AssigneeControl } from "./AssigneeControl";
import { CONV, ME, P1, SALE, U_AN, detail, onPage, renderSide, sideFake, stubSide } from "./sidePanelTestkit";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const LEAD = [...SALE, onPage("conversation.assign")];

describe("nhận hội thoại", () => {
  it('chưa ai nhận → "Chưa ai nhận" và nút "Nhận" gửi claimConversation một lần có khóa', async () => {
    const f = stubSide(sideFake());
    renderSide(<AssigneeControl detail={f.detail} grants={SALE} meId={ME} />);
    expect(screen.getByText("Chưa ai nhận")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Nhận" }));
    await waitFor(() => {
      expect(f.writes).toHaveLength(1);
    });
    expect(f.writes[0]).toMatchObject({ method: "POST", path: `/api/v1/conversations/${CONV}/claim` });
    expect(f.writes[0]?.key).not.toBeNull();
  });

  it("người khác vừa nhận → câu báo already_assigned tiếng Việt", async () => {
    const f = stubSide(sideFake());
    f.replies.push({ status: 409, body: { error: "already_assigned", request_id: "t" } });
    renderSide(<AssigneeControl detail={f.detail} grants={SALE} meId={ME} />);
    fireEvent.click(screen.getByRole("button", { name: "Nhận" }));
    expect(await screen.findByText(/Hội thoại đã có người khác nhận/)).toBeTruthy();
  });

  it("đã có người nhận → hiện tên, không có nút Nhận; của tôi ghi (bạn)", () => {
    const f = stubSide(sideFake({ detail: detail({ assignee: { id: ME, name: "Nguyễn Thị Lan" } }) }));
    renderSide(<AssigneeControl detail={f.detail} grants={SALE} meId={ME} />);
    expect(screen.getByText("Nguyễn Thị Lan (bạn)")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Nhận" })).toBeNull();
  });

  it("không có conversation.reply (chỉ xem) → không có nút Nhận", () => {
    const f = stubSide(sideFake());
    renderSide(<AssigneeControl detail={f.detail} grants={[onPage("conversation.read")]} meId={ME} />);
    expect(screen.queryByRole("button", { name: "Nhận" })).toBeNull();
  });
});

describe("giao cho người khác (HV31)", () => {
  it('không có conversation.assign → không có "Giao cho…", "Trả lại"', () => {
    const f = stubSide(sideFake({ detail: detail({ assignee: { id: ME, name: "Nguyễn Thị Lan" } }) }));
    renderSide(<AssigneeControl detail={f.detail} grants={SALE} meId={ME} />);
    expect(screen.queryByRole("button", { name: "Giao cho…" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Trả lại (bỏ nhận)" })).toBeNull();
  });

  it("conversation.assign trên Page khác → không có Giao cho… (quyền theo Page)", () => {
    const f = stubSide(sideFake());
    renderSide(<AssigneeControl detail={f.detail} grants={[...SALE, onPage("conversation.assign", U_AN)]} meId={ME} />);
    expect(screen.queryByRole("button", { name: "Giao cho…" })).toBeNull();
  });

  it("Giao cho… liệt kê nhân viên của Page; chọn rồi Giao gửi userId và expectedVersion", async () => {
    const f = stubSide(sideFake({ detail: detail({ version: 7 }) }));
    renderSide(<AssigneeControl detail={f.detail} grants={LEAD} meId={ME} />);
    fireEvent.click(screen.getByRole("button", { name: "Giao cho…" }));
    const dlg = await screen.findByRole("dialog", { name: "Giao cho" });
    expect(f.reads).toContain(`/api/v1/channels/${P1}/assignable-users`);
    fireEvent.click(await within(dlg).findByRole("radio", { name: "Trần Minh An" }));
    expect(within(dlg).getByRole("radio", { name: "Nguyễn Thị Lan (bạn)" })).toBeTruthy();
    fireEvent.click(within(dlg).getByRole("button", { name: "Giao" }));
    await waitFor(() => {
      expect(f.writes).toHaveLength(1);
    });
    expect(f.writes[0]).toMatchObject({
      path: `/api/v1/conversations/${CONV}/assign`,
      body: { userId: U_AN, expectedVersion: 7 },
    });
  });

  it("tìm nhân viên theo tên lọc danh sách", async () => {
    const f = stubSide(sideFake());
    renderSide(<AssigneeControl detail={f.detail} grants={LEAD} meId={ME} />);
    fireEvent.click(screen.getByRole("button", { name: "Giao cho…" }));
    const dlg = await screen.findByRole("dialog", { name: "Giao cho" });
    await within(dlg).findByRole("radio", { name: "Trần Minh An" });
    fireEvent.change(within(dlg).getByRole("searchbox", { name: "Tìm nhân viên" }), { target: { value: "lan" } });
    expect(within(dlg).queryByRole("radio", { name: "Trần Minh An" })).toBeNull();
    expect(within(dlg).getByRole("radio", { name: "Nguyễn Thị Lan (bạn)" })).toBeTruthy();
  });

  it("Trả lại (bỏ nhận) gửi userId null", async () => {
    const f = stubSide(sideFake({ detail: detail({ assignee: { id: ME, name: "Nguyễn Thị Lan" }, version: 2 }) }));
    renderSide(<AssigneeControl detail={f.detail} grants={LEAD} meId={ME} />);
    fireEvent.click(screen.getByRole("button", { name: "Trả lại (bỏ nhận)" }));
    await waitFor(() => {
      expect(f.writes[0]?.body).toEqual({ userId: null, expectedVersion: 2 });
    });
  });
});
