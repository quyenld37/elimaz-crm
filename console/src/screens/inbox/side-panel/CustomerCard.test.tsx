// @vitest-environment jsdom
// Thẻ khách của khung bên phải (Task 176; mockup docs/mockups/m4/khung-ben-phai.html trạng thái A, B, C; HV24, HV29;
// DH03 phía màn hình).
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { CustomerCard } from "./CustomerCard";
import { CUST, OTHER_CUST, card, orderRef, profile, renderSide, sideFake, stubSide } from "./sidePanelTestkit";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const tile = (name: string) => screen.getByRole("group", { name });

describe("chỉ số của khách cũ (DH03)", () => {
  it("12 đơn, 8 nhận, 2 hoàn, returnRateBP 2000 → Hoàn 2 · 20%, Nhận hàng 8 · 80%, tổng chi", () => {
    stubSide(sideFake());
    renderSide(<CustomerCard card={card()} profile={profile()} canWriteCustomer={false} />);
    expect(tile("Số đơn").textContent).toContain("12");
    expect(tile("Tổng chi").textContent).toContain("8.450.000 ₫");
    expect(tile("Nhận hàng").textContent).toContain("8 · 80%");
    expect(tile("Hoàn").textContent).toContain("2 · 20%");
    expect(tile("Hoàn").className).not.toContain("is-bad");
  });

  it("returnRateBP 1250 → 12% (làm tròn xuống, không tính lại từ số đơn)", () => {
    stubSide(sideFake());
    renderSide(
      <CustomerCard
        card={card({ returnedCount: 1, deliveredCount: 7, returnRateBp: 1250 })}
        canWriteCustomer={false}
      />,
    );
    expect(tile("Hoàn").textContent).toContain("1 · 12%");
    expect(tile("Nhận hàng").textContent).toContain("7 · 87%");
  });

  it('khách có nhãn "Hay hoàn" của M2 → ô Hoàn tô đỏ, nhãn khách hiện cạnh tên (HV24)', () => {
    stubSide(sideFake());
    renderSide(
      <CustomerCard
        card={card()}
        profile={profile({
          tags: [
            { tag: { id: "t1", name: "Khách quen", color: "gray", version: 1 }, source: "staff" },
            { tag: { id: "t2", name: "Hay hoàn", color: "red", version: 1 }, source: "rule" },
          ],
        })}
        canWriteCustomer={false}
      />,
    );
    expect(tile("Hoàn").className).toContain("is-bad");
    expect(screen.getByText("Hay hoàn").className).toContain("chip-err");
    expect(screen.getByText("KH000231", { exact: false })).toBeTruthy();
  });

  it('5 đơn gần đây có liên kết tới đơn; "Xem cả 12 đơn" mở hồ sơ', () => {
    stubSide(sideFake());
    const five = [1, 2, 3, 4, 5].map((i) =>
      orderRef({ id: `0195f3b2-0000-7000-8000-00000004a00${String(i)}`, code: `DH000123${String(i)}` }),
    );
    renderSide(<CustomerCard card={{ ...card(), recentOrders: five }} profile={profile()} canWriteCustomer={false} />);
    const list = screen.getByRole("list", { name: "Đơn gần đây" });
    expect(within(list).getAllByRole("link")).toHaveLength(5);
    expect(
      within(list)
        .getByRole("link", { name: /DH0001231/ })
        .getAttribute("href"),
    ).toBe("/don/0195f3b2-0000-7000-8000-00000004a001");
    expect(screen.getByRole("link", { name: "Xem cả 12 đơn" }).getAttribute("href")).toBe(`/khach/${CUST}`);
  });
});

describe("khách mới", () => {
  it('0 đơn → "Khách mới, chưa có đơn", không có ô số, không có đơn gần đây', () => {
    stubSide(sideFake());
    renderSide(
      <CustomerCard
        card={{
          ...card({ orderCount: 0, deliveredCount: 0, returnedCount: 0, returnRateBp: 0, totalSpent: 0, phones: [] }),
          recentOrders: [],
        }}
        canWriteCustomer={false}
      />,
    );
    expect(screen.getByText("Khách mới, chưa có đơn")).toBeTruthy();
    expect(screen.queryByRole("group", { name: "Hoàn" })).toBeNull();
    expect(screen.queryByRole("list", { name: "Đơn gần đây" })).toBeNull();
    expect(screen.getByText(/chưa có SĐT/)).toBeTruthy();
  });

  it("tên 60 ký tự không dấu cách nằm trong ô cắt chữ (không tràn khung)", () => {
    stubSide(sideFake());
    const name = "NguyễnThịThuHàShopThờiTrangCôngSởXuấtKhẩuHàNộiChiNhánhSố0205";
    renderSide(<CustomerCard card={card({ name })} canWriteCustomer={false} />);
    expect(screen.getByText(name).className).toContain("ellip");
  });
});

describe("gợi ý cùng một khách (M2)", () => {
  it("có mergeSuggestionCustomerId → hộp gợi ý, nút mở hai hồ sơ; không tự gộp", () => {
    stubSide(sideFake());
    renderSide(
      <CustomerCard
        card={{ ...card(), mergeSuggestionCustomerId: OTHER_CUST }}
        profile={profile()}
        canWriteCustomer={false}
      />,
    );
    expect(screen.getByText("Có thể là cùng một khách.")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Xem hai hồ sơ" }).getAttribute("href")).toBe(
      `/khach/${CUST}/gop?voi=${OTHER_CUST}`,
    );
  });
});

describe("không liên hệ (HV29)", () => {
  it("không có customer.write → không có công tắc không liên hệ", () => {
    stubSide(sideFake());
    renderSide(<CustomerCard card={card({ doNotContact: true })} canWriteCustomer={false} />);
    expect(screen.getByText("Không liên hệ.")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Bỏ cờ không liên hệ" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Đặt không liên hệ" })).toBeNull();
  });

  it("có customer.write: bỏ cờ gửi on=false một lần có khóa chống trùng", async () => {
    const f = stubSide(sideFake());
    renderSide(<CustomerCard card={card({ doNotContact: true })} canWriteCustomer />);
    fireEvent.click(screen.getByRole("button", { name: "Bỏ cờ không liên hệ" }));
    await waitFor(() => {
      expect(f.writes).toHaveLength(1);
    });
    expect(f.writes[0]).toMatchObject({
      method: "PUT",
      path: `/api/v1/customers/${CUST}/do-not-contact`,
      body: { on: false },
    });
    expect(f.writes[0]?.key).not.toBeNull();
  });

  it("có customer.write, khách chưa có cờ: nút đặt cờ gửi on=true", async () => {
    const f = stubSide(sideFake());
    renderSide(<CustomerCard card={card()} canWriteCustomer />);
    fireEvent.click(screen.getByRole("button", { name: "Đặt không liên hệ" }));
    await waitFor(() => {
      expect(f.writes[0]?.body).toEqual({ on: true });
    });
  });
});
