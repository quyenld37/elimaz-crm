// @vitest-environment jsdom
// Gọi điện và ghi kết quả cuộc gọi (Task 176; mockup khung-ben-phai.html trạng thái A, C; HV30; CH17 phía màn hình).
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { CallButton } from "./CallButton";
import { CONV, CUST, call, card, renderSide, sideFake, stubSide } from "./sidePanelTestkit";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

/** Giờ địa phương ngày mai 15:00 cho ô datetime-local. */
function tomorrowAt15(): string {
  const d = new Date(Date.now() + 24 * 3600_000);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${String(d.getFullYear())}-${p(d.getMonth() + 1)}-${p(d.getDate())}T15:00`;
}

async function openDialog() {
  fireEvent.click(screen.getByRole("button", { name: "Ghi kết quả cuộc gọi" }));
  return screen.findByRole("dialog", { name: "Kết quả cuộc gọi" });
}

/** Dựng nút gọi của thẻ mẫu, mở hộp thoại, chọn kết quả outcome; trả API giả và hộp thoại. */
async function openWith(outcome: string) {
  const f = stubSide(sideFake());
  renderSide(<CallButton card={card()} conversationId={CONV} canReply />);
  const dlg = await openDialog();
  fireEvent.click(within(dlg).getByRole("radio", { name: outcome }));
  return { f, dlg };
}

describe("nút gọi", () => {
  it("thẻ có SĐT → liên kết tel: và lần gọi trước", async () => {
    stubSide(sideFake({ calls: [call()] }));
    renderSide(<CallButton card={card()} conversationId={CONV} canReply />);
    expect(screen.getByRole("link", { name: "Gọi 0912 345 678" }).getAttribute("href")).toBe("tel:0912345678");
    expect(await screen.findByText(/Lần gọi trước: 17\/11 · Không nghe máy · Trần Minh An/)).toBeTruthy();
  });

  it("khách không có SĐT (hay người xem không có customer.read) → không có nút gọi", () => {
    stubSide(sideFake());
    renderSide(<CallButton card={card({ phones: [] })} conversationId={CONV} canReply />);
    expect(screen.queryByRole("link", { name: /^Gọi / })).toBeNull();
    const c = card();
    delete c.customer.phones;
    cleanup();
    renderSide(<CallButton card={c} conversationId={CONV} canReply />);
    expect(screen.queryByRole("link", { name: /^Gọi / })).toBeNull();
  });

  it("không có conversation.reply → không có nút ghi kết quả", () => {
    stubSide(sideFake());
    renderSide(<CallButton card={card()} conversationId={CONV} canReply={false} />);
    expect(screen.queryByRole("button", { name: "Ghi kết quả cuộc gọi" })).toBeNull();
  });
});

describe("hộp thoại kết quả cuộc gọi (HV30)", () => {
  it("sáu kết quả", async () => {
    stubSide(sideFake());
    renderSide(<CallButton card={card()} conversationId={CONV} canReply />);
    const dlg = await openDialog();
    expect(
      within(dlg)
        .getAllByRole("radio")
        .map((r) => r.closest("label")?.textContent.trim()),
    ).toEqual(["Đã chốt", "Không nghe máy", "Hẹn gọi lại", "Sai số", "Khách từ chối", "Khác"]);
  });

  it('"Hẹn gọi lại" không chọn giờ → báo lỗi tại ô, không gửi', async () => {
    const { f, dlg } = await openWith("Hẹn gọi lại");
    fireEvent.click(within(dlg).getByRole("button", { name: "Lưu" }));
    expect(await within(dlg).findByText("Chọn giờ gọi lại.")).toBeTruthy();
    expect(within(dlg).getByLabelText("Gọi lại lúc *").getAttribute("aria-invalid")).toBe("true");
    expect(f.writes).toHaveLength(0);
  });

  it("đủ giờ → createCallLog đúng thân (kết quả, giờ, hội thoại, ghi chú) rồi đóng", async () => {
    const { f, dlg } = await openWith("Hẹn gọi lại");
    const at = tomorrowAt15();
    fireEvent.change(within(dlg).getByLabelText("Gọi lại lúc *"), { target: { value: at } });
    fireEvent.change(within(dlg).getByLabelText("Ghi chú"), { target: { value: "Khách đang họp" } });
    fireEvent.click(within(dlg).getByRole("button", { name: "Lưu" }));
    await waitFor(() => {
      expect(f.writes).toHaveLength(1);
    });
    expect(f.writes[0]).toMatchObject({
      method: "POST",
      path: `/api/v1/customers/${CUST}/calls`,
      body: {
        outcome: "callback",
        callbackAt: new Date(at).toISOString(),
        conversationId: CONV,
        note: "Khách đang họp",
      },
    });
    await waitFor(() => {
      expect(screen.queryByRole("dialog", { name: "Kết quả cuộc gọi" })).toBeNull();
    });
  });

  it("kết quả khác hẹn gọi lại không có ô giờ và không gửi callbackAt", async () => {
    const { f, dlg } = await openWith("Không nghe máy");
    expect(within(dlg).queryByLabelText("Gọi lại lúc *")).toBeNull();
    fireEvent.click(within(dlg).getByRole("button", { name: "Lưu" }));
    await waitFor(() => {
      expect(f.writes[0]?.body).toEqual({ outcome: "no_answer", conversationId: CONV });
    });
  });

  it("chưa chọn kết quả → nút Lưu khóa", async () => {
    stubSide(sideFake());
    renderSide(<CallButton card={card()} conversationId={CONV} canReply />);
    const dlg = await openDialog();
    expect(within(dlg).getByRole<HTMLButtonElement>("button", { name: "Lưu" }).disabled).toBe(true);
  });
});
