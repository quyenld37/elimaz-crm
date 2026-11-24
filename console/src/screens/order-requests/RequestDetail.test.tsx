// @vitest-environment jsdom
// Chi tiết Yêu cầu về đơn hàng /don/yeu-cau/:id (kế hoạch M5 Task 207; mockup docs/mockups/m5/order-requests.html trạng
// thái B–F): nội dung khách gửi hiện nguyên chữ; trả lời "khách thấy" / "nội bộ" gửi đúng cờ; đổi trạng thái theo sơ
// đồ của máy chủ (hộp Cần bổ sung, Từ chối, Hoàn tất; Đã xác nhận đổi có hướng dẫn gửi trả); 409 báo và tải lại, giữ
// chữ đang gõ; chỉ xem; yêu cầu đã đóng; ảnh riêng tư ba trạng thái.
import { act, cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { onRealtimeChanged } from "../../app/queryClient";
import { order } from "../don/donTestkit";
import { ORD, RQ1, grantsOf, installFake, renderAt, request, stubDialog, type Fake } from "./requestTestkit";

let fake: Fake;
beforeEach(() => {
  stubDialog();
  fake = installFake();
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const PATH = `/don/yeu-cau/${RQ1}`;
const type = (el: HTMLElement, v: string) => {
  fireEvent.change(el, { target: { value: v } });
};
const open = async () => {
  renderAt(PATH);
  await screen.findByRole("heading", { level: 1, name: "Yêu cầu đổi hàng YC0000148" });
};
const choose = (status: string) => {
  fireEvent.change(screen.getByRole("combobox", { name: "Trạng thái tiếp theo" }), { target: { value: status } });
};
/** Giá trị của ô nhập, ô chữ, ô chọn. */
const valueOf = (el: HTMLElement) =>
  el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement || el instanceof HTMLSelectElement
    ? el.value
    : "";
/** Khung tin (.msg) chứa chữ text. */
const msgOf = (text: string): HTMLElement => {
  const m = screen.getByText(text).closest(".msg");
  if (!(m instanceof HTMLElement)) throw new Error(`không thấy khung tin của "${text}"`);
  return m;
};
const save = () => {
  fireEvent.click(screen.getByRole("button", { name: "Lưu xử lý" }));
};

describe("Nội dung yêu cầu", () => {
  it("lời nhắn có <script> hiện nguyên chữ, không thành thẻ; dòng hàng, lý do bằng chữ; tin hai phía có nhãn", async () => {
    const { container } = renderAt(PATH);
    expect(
      await screen.findByText("Váy mặc chật ở ngực, cho mình đổi sang size L nhé. <script>alert('x')</script>"),
    ).toBeTruthy();
    expect(container.querySelector("script")).toBeNull();
    expect(screen.getByText("Đầm suông cổ vuông")).toBeTruthy();
    expect(screen.getByText("Chật, nhỏ")).toBeTruthy();
    expect(screen.getByText("Lỗi sản phẩm")).toBeTruthy();
    expect(screen.getByText("Bung đường may ở vai")).toBeTruthy();
    expect(screen.getByText("Biến thể khác (xem ở trang đơn)")).toBeTruthy();
    const staff = msgOf("Chào chị, ELIMAZ đã nhận yêu cầu.");
    expect(staff.className).toContain("is-staff");
    expect(within(staff).getByText("Khách thấy")).toBeTruthy();
    const note = msgOf("Kho còn 4 chiếc Đen / L.");
    expect(note.className).toContain("is-int");
    expect(within(note).getByText("Nội bộ · khách không thấy")).toBeTruthy();
  });

  it("đơn và khách đọc từ đơn (tên dài xuống dòng, tổng tiền); nút mở đơn", async () => {
    fake.order = order({
      id: ORD,
      code: "DH0001312",
      grandTotal: 1_270_000,
      customer: { id: "c", rootId: "c", code: "KH1", fullName: "NguyễnThịThuHàShopThờiTrangCôngSởXuấtKhẩuHàNội" },
    });
    await open();
    const name = await screen.findByText("NguyễnThịThuHàShopThờiTrangCôngSởXuấtKhẩuHàNội");
    expect(name.className).toContain("wrap-any");
    expect(screen.getByText("1.270.000 ₫")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Mở đơn DH0001312" }).getAttribute("href")).toBe(`/don/${ORD}`);
  });

  it("ảnh: sẵn sàng mở tab mới bằng link ký; đang xử lý, bị từ chối, mất hiện riêng; link không phải http(s) không mở", async () => {
    fake.details.set(
      RQ1,
      request({
        files: [
          { fileId: "f1", status: "ready", url: "https://files.elimaz.test/priv/a.jpg?sig=1" },
          { fileId: "f2", status: "processing" },
          { fileId: "f3", status: "rejected" },
          { fileId: "f4", status: "missing" },
          { fileId: "f5", status: "ready", url: "javascript:alert(1)" },
        ],
      }),
    );
    await open();
    expect(screen.getByRole("heading", { name: "Ảnh khách gửi (5)" })).toBeTruthy();
    const links = screen.getAllByRole("link", { name: "Mở ảnh" });
    expect(links).toHaveLength(1);
    expect(links[0]?.getAttribute("href")).toBe("https://files.elimaz.test/priv/a.jpg?sig=1");
    expect(links[0]?.getAttribute("target")).toBe("_blank");
    expect(links[0]?.getAttribute("rel")).toBe("noopener noreferrer");
    expect(screen.getByText("Đang xử lý ảnh…")).toBeTruthy();
    expect(screen.getByText("Ảnh bị từ chối")).toBeTruthy();
    expect(screen.getByText("Ảnh không còn")).toBeTruthy();
    expect(screen.getAllByText("Không mở được")).toHaveLength(1);
  });

  it("báo hiệu của đúng yêu cầu này tải lại; của yêu cầu khác thì không", async () => {
    const { qc } = renderAt(PATH);
    await screen.findByText("Kho còn 4 chiếc Đen / L.");
    fake.details.set(RQ1, request({ status: "in_review", version: 2 }));
    act(() => {
      onRealtimeChanged(qc, { topic: "order_request.changed", entity: { type: "order_request", id: "khac" } });
    });
    await new Promise((r) => setTimeout(r, 30));
    expect(within(screen.getByRole("banner")).getByText("Đã tiếp nhận")).toBeTruthy();
    act(() => {
      onRealtimeChanged(qc, { topic: "order_request.changed", entity: { type: "order_request", id: RQ1 } });
    });
    expect(await within(screen.getByRole("banner")).findByText("Đang kiểm tra")).toBeTruthy();
  });
});

describe("Trả lời khách", () => {
  it("Khách thấy gửi visibleToCustomer true, Ghi chú nội bộ gửi false; mỗi lần có Idempotency-Key; dưới 5 ký tự không gửi được", async () => {
    await open();
    const box = screen.getByRole("textbox", { name: "Viết tin" });
    const send = screen.getByRole("button", { name: "Gửi" });
    type(box, "Dạ");
    expect(send.hasAttribute("disabled")).toBe(true);
    type(box, "  Bên em kiểm tra ảnh và báo lại chị trong hôm nay ạ.  ");
    expect(screen.getByText("51 / 1000")).toBeTruthy();
    fireEvent.click(send);
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    expect(fake.writes[0]?.path).toBe(`/api/v1/order-requests/${RQ1}/messages`);
    expect(fake.writes[0]?.body).toEqual({
      body: "Bên em kiểm tra ảnh và báo lại chị trong hôm nay ạ.",
      visibleToCustomer: true,
    });
    expect(fake.writes[0]?.key).toBeTruthy();
    await waitFor(() => {
      expect(valueOf(screen.getByRole("textbox", { name: "Viết tin" }))).toBe("");
    });
    fireEvent.click(screen.getByRole("radio", { name: "Ghi chú nội bộ" }));
    type(screen.getByRole("textbox", { name: "Viết tin" }), "Giữ 1 áo Trắng / S cho khách.");
    fireEvent.click(screen.getByRole("button", { name: "Gửi" }));
    await waitFor(() => {
      expect(fake.writes).toHaveLength(2);
    });
    expect(fake.writes[1]?.body).toEqual({ body: "Giữ 1 áo Trắng / S cho khách.", visibleToCustomer: false });
    expect(fake.writes[1]?.key).not.toBe(fake.writes[0]?.key);
  });

  it("đang gửi thì khóa nút và ô chọn trạng thái", async () => {
    let release: () => void = () => undefined;
    fake.hold = new Promise((r) => {
      release = r;
    });
    await open();
    type(screen.getByRole("textbox", { name: "Viết tin" }), "Bên em đang kiểm tra ạ.");
    fireEvent.click(screen.getByRole("button", { name: "Gửi" }));
    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Đang gửi…" }).hasAttribute("disabled")).toBe(true);
    });
    expect(screen.getByRole("combobox", { name: "Trạng thái tiếp theo" }).hasAttribute("disabled")).toBe(true);
    expect(screen.getByRole("button", { name: "Lưu xử lý" }).hasAttribute("disabled")).toBe(true);
    act(() => {
      release();
    });
    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Gửi" })).toBeTruthy();
    });
  });
});

describe("Đổi trạng thái", () => {
  it("chỉ hiện trạng thái kế tiếp máy chủ cho; Đã xác nhận đổi chỉ cho yêu cầu đổi hàng", async () => {
    await open();
    const opts = within(screen.getByRole("combobox", { name: "Trạng thái tiếp theo" }))
      .getAllByRole("option")
      .map((o) => o.textContent);
    expect(opts).toEqual([
      "Giữ nguyên, chỉ trả lời",
      "Đang kiểm tra",
      "Cần bổ sung",
      "Đã xác nhận đổi",
      "Hoàn tất",
      "Từ chối",
    ]);
    cleanup();
    fake.details.set(RQ1, request({ kind: "return", status: "in_review" }));
    renderAt(PATH);
    const sel = await screen.findByRole("combobox", { name: "Trạng thái tiếp theo" });
    expect(within(sel).queryByRole("option", { name: "Đã xác nhận đổi" })).toBeNull();
    expect(within(sel).getByRole("option", { name: "Cần bổ sung" })).toBeTruthy();
  });

  it("Đang kiểm tra lưu ngay, gửi version đã đọc và lời nhắn nếu có", async () => {
    await open();
    expect(screen.getByRole("button", { name: "Lưu xử lý" }).hasAttribute("disabled")).toBe(true);
    choose("in_review");
    type(screen.getByRole("textbox", { name: "Lời nhắn cho khách" }), "Bên em đang kiểm tra ạ.");
    save();
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    expect(fake.writes[0]?.path).toBe(`/api/v1/order-requests/${RQ1}/status`);
    expect(fake.writes[0]?.body).toEqual({ status: "in_review", version: 1, message: "Bên em đang kiểm tra ạ." });
    expect(fake.writes[0]?.key).toBeTruthy();
  });

  it("Cần bổ sung mở hộp, lời nhắn bắt buộc từ 5 ký tự", async () => {
    await open();
    choose("needs_info");
    save();
    const dlg = await screen.findByRole("dialog", { name: "Cần khách bổ sung" });
    const go = within(dlg).getByRole("button", { name: "Gửi và chuyển trạng thái" });
    type(within(dlg).getByRole("textbox", { name: "Lời nhắn cho khách *" }), "Ảnh");
    expect(within(dlg).getByText("Lời nhắn cần ít nhất 5 ký tự.")).toBeTruthy();
    expect(go.hasAttribute("disabled")).toBe(true);
    type(within(dlg).getByRole("textbox", { name: "Lời nhắn cho khách *" }), "Chị chụp thêm ảnh đường may giúp em.");
    fireEvent.click(go);
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    expect(fake.writes[0]?.body).toEqual({
      status: "needs_info",
      version: 1,
      message: "Chị chụp thêm ảnh đường may giúp em.",
    });
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
  });

  it("Từ chối hỏi xác nhận, lý do bắt buộc ≤ 500, gửi outcome.reason", async () => {
    await open();
    choose("rejected");
    save();
    const dlg = await screen.findByRole("dialog", { name: "Từ chối yêu cầu này?" });
    expect(within(dlg).getByText(/không mở lại được/)).toBeTruthy();
    const go = within(dlg).getByRole("button", { name: "Từ chối yêu cầu" });
    expect(go.hasAttribute("disabled")).toBe(true);
    type(within(dlg).getByRole("textbox", { name: "Lý do từ chối *" }), "x".repeat(501));
    expect(go.hasAttribute("disabled")).toBe(true);
    type(within(dlg).getByRole("textbox", { name: "Lý do từ chối *" }), "Đã quá 7 ngày kể từ ngày nhận.");
    expect(within(dlg).getByText("30 / 500")).toBeTruthy();
    fireEvent.click(go);
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    expect(fake.writes[0]?.body).toEqual({
      status: "rejected",
      version: 1,
      outcome: { reason: "Đã quá 7 ngày kể từ ngày nhận." },
    });
  });

  it("Hoàn tất hỏi xác nhận, mã vận đơn tùy chọn ≤ 60", async () => {
    fake.details.set(RQ1, request({ status: "items_received" }));
    await open();
    choose("resolved");
    save();
    const dlg = await screen.findByRole("dialog", { name: "Hoàn tất yêu cầu này?" });
    const go = within(dlg).getByRole("button", { name: "Hoàn tất yêu cầu" });
    type(within(dlg).getByRole("textbox", { name: "Mã vận đơn đơn đổi (nếu có)" }), "V".repeat(61));
    expect(go.hasAttribute("disabled")).toBe(true);
    type(within(dlg).getByRole("textbox", { name: "Mã vận đơn đơn đổi (nếu có)" }), " VTP2611208877 ");
    fireEvent.click(go);
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    expect(fake.writes[0]?.body).toEqual({
      status: "resolved",
      version: 1,
      outcome: { trackingCode: "VTP2611208877" },
    });
  });

  it("Hoàn tất không mã vận đơn: không gửi outcome", async () => {
    await open();
    choose("resolved");
    save();
    const dlg = await screen.findByRole("dialog", { name: "Hoàn tất yêu cầu này?" });
    fireEvent.click(within(dlg).getByRole("button", { name: "Hoàn tất yêu cầu" }));
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    expect(fake.writes[0]?.body).toEqual({ status: "resolved", version: 1 });
  });

  it("Đã xác nhận đổi: địa chỉ, mã kiện, hạn gửi kiểm trước khi lưu; gửi shipBack với ngày", async () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2026-11-21T03:00:00Z"));
    await open();
    choose("confirmed");
    const addr = screen.getByRole("textbox", { name: "Địa chỉ nhận hàng *" });
    const code = screen.getByRole("textbox", { name: "Mã ghi trên kiện hàng *" });
    const date = screen.getByLabelText("Gửi trước ngày (giờ Việt Nam) *");
    type(addr, "Kho ELIM");
    type(code, "YC");
    type(date, "2026-11-20");
    expect(screen.getByText("Địa chỉ nhận hàng cần 10–300 ký tự.")).toBeTruthy();
    expect(screen.getByText("Mã ghi trên kiện cần 3–40 ký tự.")).toBeTruthy();
    expect(screen.getByText("Hạn gửi hàng không được là ngày đã qua.")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Lưu xử lý" }).hasAttribute("disabled")).toBe(true);
    type(addr, "Kho ELIMAZ, 18 ngõ 64 Nguyễn Lương Bằng, Đống Đa, Hà Nội");
    type(code, "YC148-DOI");
    type(date, "2026-11-21");
    save();
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    expect(fake.writes[0]?.body).toEqual({
      status: "confirmed",
      version: 1,
      shipBack: {
        address: "Kho ELIMAZ, 18 ngõ 64 Nguyễn Lương Bằng, Đống Đa, Hà Nội",
        packageCode: "YC148-DOI",
        deadline: "2026-11-21",
      },
    });
  });

  it("409: báo người khác vừa đổi, tải lại yêu cầu, giữ lời nhắn đang gõ, không tự gửi lại", async () => {
    await open();
    choose("in_review");
    type(screen.getByRole("textbox", { name: "Lời nhắn cho khách" }), "Bên em đang kiểm tra ạ.");
    fake.replies.push({ status: 409, body: { error: "version_conflict", request_id: "t" } });
    fake.details.set(RQ1, request({ status: "in_review", version: 2 }));
    save();
    expect(await screen.findByText("Yêu cầu vừa được người khác đổi trạng thái.")).toBeTruthy();
    expect(await within(screen.getByRole("banner")).findByText("Đang kiểm tra")).toBeTruthy();
    expect(valueOf(screen.getByRole("textbox", { name: "Lời nhắn cho khách" }))).toBe("Bên em đang kiểm tra ạ.");
    expect(fake.writes).toHaveLength(1);
    // Trạng thái đã chọn không còn hợp lệ (in_review → in_review): ô chọn về "Giữ nguyên".
    expect(valueOf(screen.getByRole("combobox", { name: "Trạng thái tiếp theo" }))).toBe("");
    fireEvent.click(screen.getByRole("button", { name: "Đã hiểu" }));
    expect(screen.queryByText("Yêu cầu vừa được người khác đổi trạng thái.")).toBeNull();
  });

  it("lỗi khác (422) hiện câu lỗi trong thẻ", async () => {
    await open();
    choose("in_review");
    fake.replies.push({ status: 422, body: { error: "invalid_transition", request_id: "t" } });
    save();
    expect(await screen.findByText(/Tải lại để xem trạng thái mới/)).toBeTruthy();
  });
});

describe("Quyền và trạng thái đóng", () => {
  it("chỉ có quyền xem: không ô viết tin, không đổi trạng thái, có câu giải thích", async () => {
    fake.grants = grantsOf("order.read");
    await open();
    expect(await screen.findByText(/Bạn chỉ có quyền xem yêu cầu/)).toBeTruthy();
    expect(screen.queryByRole("textbox", { name: "Viết tin" })).toBeNull();
    expect(screen.queryByRole("combobox", { name: "Trạng thái tiếp theo" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Lưu xử lý" })).toBeNull();
  });

  it("không có order.read: không đọc đơn, mã đơn không thành liên kết", async () => {
    fake.grants = grantsOf("order.requests.manage");
    await open();
    expect(screen.queryByRole("link", { name: /Mở đơn/ })).toBeNull();
    expect(screen.getAllByText("DH0001312").length).toBeGreaterThan(0);
    expect(fake.orderReads).toBe(0);
  });

  it("đã đóng (Từ chối): lý do khách thấy, không ô viết tin, không đổi trạng thái", async () => {
    fake.details.set(
      RQ1,
      request({
        status: "rejected",
        outcome: { reason: "Đã quá 7 ngày kể từ ngày nhận." },
        resolvedAt: "2026-11-21T05:00:00Z",
      }),
    );
    await open();
    expect(screen.getByText(/Yêu cầu đã đóng/)).toBeTruthy();
    expect(screen.getByText(/Đã quá 7 ngày kể từ ngày nhận\./)).toBeTruthy();
    expect(screen.queryByRole("textbox", { name: "Viết tin" })).toBeNull();
    expect(screen.queryByRole("combobox", { name: "Trạng thái tiếp theo" })).toBeNull();
  });

  it("khách đã hủy: câu riêng, không cần xử lý", async () => {
    fake.details.set(RQ1, request({ status: "cancelled" }));
    await open();
    expect(screen.getByText(/Khách đã rút yêu cầu trên website/)).toBeTruthy();
  });

  it("đã xác nhận đổi: hiện hướng dẫn gửi trả khách thấy", async () => {
    fake.details.set(
      RQ1,
      request({
        status: "confirmed",
        shipBack: { address: "Kho ELIMAZ, 18 ngõ 64", packageCode: "YC141-DOI", deadline: "2026-11-25T16:59:59Z" },
      }),
    );
    await open();
    expect(screen.getByText("Khách gửi hàng về")).toBeTruthy();
    expect(screen.getByText("YC141-DOI")).toBeTruthy();
    expect(screen.getByText("23:59 25/11")).toBeTruthy();
  });

  it("yêu cầu hủy đơn đã gửi hãng: nút mở đơn để hủy vận đơn", async () => {
    fake.details.set(RQ1, request({ kind: "cancel", reasonCode: "CHANGED_MIND", lines: [] }));
    fake.order = order({ id: ORD, code: "DH0001312", currentShipmentId: "s1" });
    renderAt(PATH);
    const link = await screen.findByRole("link", { name: "Mở đơn để hủy vận đơn" });
    expect(link.getAttribute("href")).toBe(`/don/${ORD}`);
  });

  it("yêu cầu không có: báo không tìm thấy, không vẽ thẻ xử lý", async () => {
    renderAt("/don/yeu-cau/khong-co");
    expect(await screen.findByText(/Chưa tải được yêu cầu/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Lưu xử lý" })).toBeNull();
  });
});
