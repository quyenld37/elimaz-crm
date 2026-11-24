// @vitest-environment jsdom
// Màn Góp ý /khach/gop-y (kế hoạch M5 Task 461; mockup docs/mockups/m5/feedback.html trạng thái A, C; HV22): lọc trạng
// thái, chủ đề, khách; dòng hiện chủ đề, mức bằng chữ, nội dung thuần, người gửi, liên kết hồ sơ; dòng mở đổi trạng
// thái, ghi chú (chỉ gửi trường đổi); 409 tải lại giữ ghi chú; 403 có câu riêng; còn trang sau thì không ghi "hết".
import { act, cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  CUST,
  FB1,
  LONG_NAME,
  SCRIPT_BODY,
  feedback,
  grantsOf,
  installFake,
  renderAt,
  type Fake,
} from "./feedbackTestkit";

let fake: Fake;
beforeEach(() => {
  fake = installFake();
  fake.rows = [
    feedback(),
    feedback({
      id: "f2",
      topic: "website",
      mood: 5,
      body: SCRIPT_BODY,
      name: "Lan",
      contact: "lan.nguyen@example.com",
      customerId: undefined,
      customerName: undefined,
      orderRef: undefined,
      page: undefined,
      device: undefined,
      note: undefined,
      createdAt: "2026-11-21T02:40:00Z",
    }),
    feedback({
      id: "f3",
      topic: "product",
      mood: 4,
      body: "Áo sơ mi lụa mặc mát.",
      name: undefined,
      contact: undefined,
      customerId: undefined,
      customerName: undefined,
      state: "seen",
    }),
    feedback({
      id: "f4",
      topic: "service",
      mood: 1,
      body: "Nhắn page 2 ngày không ai trả lời.",
      name: LONG_NAME,
      contact: "0987 654 321",
      customerId: undefined,
      customerName: undefined,
      state: "answered",
    }),
    feedback({ id: "f5", topic: "other", mood: undefined, body: "Shop có mở ở Đà Nẵng không ạ?", state: "answered" }),
  ];
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

/** Dòng tóm tắt đầu tiên khớp chữ (dòng mở bên dưới cũng chứa nội dung). */
const row = async (text: string) => {
  const rows = await screen.findAllByRole("row", { name: new RegExp(text) });
  const first = rows[0];
  if (first === undefined) throw new Error(`không có dòng ${text}`);
  return first;
};
/** Chờ API giả nhận đủ n yêu cầu ghi. */
const written = (n: number) =>
  waitFor(() => {
    expect(fake.writes).toHaveLength(n);
  });
const openRow = async (text: string) => {
  fireEvent.click(within(await row(text)).getByRole("button", { name: "Mở" }));
  return screen.findByRole("region", { name: "Chi tiết góp ý" });
};

describe("Danh sách góp ý", () => {
  it("mỗi dòng: giờ, chủ đề, mức bằng chữ, nội dung thuần, người gửi, liên kết hồ sơ, trạng thái", async () => {
    renderAt("/khach/gop-y");
    const r1 = await row("Shipper giao trễ");
    expect(within(r1).getByText("10:12 21/11")).toBeTruthy();
    expect(within(r1).getByText("Giao hàng")).toBeTruthy();
    expect(within(r1).getByText("2/5 · Không hài lòng")).toBeTruthy();
    expect(within(r1).getByText("Trần Thị Thu Hà · 0912 345 678")).toBeTruthy();
    expect(within(r1).getByRole("link", { name: "Hồ sơ KH000231" }).getAttribute("href")).toBe(`/khach/${CUST}`);
    expect(within(r1).getByText("Mới")).toBeTruthy();

    // <script> hiện nguyên chữ, không thành phần tử.
    const r2 = await row("Trang mới dễ xem");
    expect(within(r2).getByText(SCRIPT_BODY)).toBeTruthy();
    expect(document.querySelector("script")).toBeNull();
    expect(within(r2).getByText("Trang web")).toBeTruthy();
    expect(within(r2).getByText("5/5 · Rất hài lòng")).toBeTruthy();
    expect(within(r2).getByText("Không gắn hồ sơ")).toBeTruthy();
    expect(within(r2).queryByRole("link")).toBeNull();

    const r3 = await row("Áo sơ mi lụa");
    expect(within(r3).getByText("Sản phẩm")).toBeTruthy();
    expect(within(r3).getByText("4/5 · Hài lòng")).toBeTruthy();
    expect(within(r3).getByText("Không để lại")).toBeTruthy();
    expect(within(r3).getByText("Đã xem")).toBeTruthy();

    const r4 = await row("Nhắn page");
    expect(within(r4).getByText("Tư vấn, chăm sóc")).toBeTruthy();
    expect(within(r4).getByText("1/5 · Rất không hài lòng")).toBeTruthy();
    expect(within(r4).getByText(`${LONG_NAME} · 0987 654 321`).className).toContain("wrap-any");
    expect(within(r4).getByText("Đã phản hồi")).toBeTruthy();

    const r5 = await row("Đà Nẵng");
    expect(within(r5).getByText("Khác")).toBeTruthy();
    expect(within(r5).getByText("—")).toBeTruthy();
  });

  it("không có quyền customer.read: tên khách gắn hiện chữ, không liên kết", async () => {
    fake.grants = grantsOf("customer.feedback.manage");
    renderAt("/khach/gop-y");
    const r1 = await row("Shipper giao trễ");
    expect(within(r1).getByText("Hồ sơ KH000231")).toBeTruthy();
    expect(within(r1).queryByRole("link")).toBeNull();
  });

  it("lọc trạng thái bằng thanh nhãn, chủ đề bằng ô chọn: gửi state, topic lên API", async () => {
    renderAt("/khach/gop-y");
    await row("Shipper giao trễ");
    expect(fake.listCalls[0]?.get("state")).toBeNull();
    expect(screen.getByRole("button", { name: /^Tất cả/ }).getAttribute("aria-pressed")).toBe("true");
    fireEvent.click(screen.getByRole("button", { name: /^Đã xem/ }));
    await waitFor(() => {
      expect(fake.listCalls.at(-1)?.get("state")).toBe("seen");
    });
    expect(await row("Áo sơ mi lụa")).toBeTruthy();
    expect(screen.queryByRole("row", { name: /Shipper/ })).toBeNull();
    fireEvent.change(screen.getByRole("combobox", { name: "Chủ đề" }), { target: { value: "delivery" } });
    await waitFor(() => {
      expect(fake.listCalls.at(-1)?.get("topic")).toBe("delivery");
    });
    expect(await screen.findByText("Chưa có góp ý nào trong mục này")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Xem tất cả góp ý" }));
    expect(await row("Shipper giao trễ")).toBeTruthy();
    expect(fake.listCalls.at(-1)?.get("topic")).toBeNull();
    expect(fake.listCalls.at(-1)?.get("state")).toBeNull();
  });

  it("?khach= lọc theo khách (customerId), có nút bỏ lọc", async () => {
    renderAt(`/khach/gop-y?khach=${CUST}`);
    await row("Shipper giao trễ");
    expect(fake.listCalls.at(-1)?.get("customerId")).toBe(CUST);
    expect(screen.queryByRole("row", { name: /Trang mới/ })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Bỏ lọc khách" }));
    expect(await row("Trang mới dễ xem")).toBeTruthy();
    expect(fake.listCalls.at(-1)?.get("customerId")).toBeNull();
  });

  it("120 góp ý: trang 50, còn trang sau thì không ghi hết; tải thêm tới khi hết", async () => {
    fake.rows = Array.from({ length: 120 }, (_, i) =>
      feedback({ id: `x${String(i)}`, body: `Góp ý số #${String(i)}#` }),
    );
    renderAt("/khach/gop-y");
    await row("#0#");
    expect(screen.getByText("50 góp ý, còn nữa")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Tải thêm" }));
    await row("#99#");
    expect(screen.getByText("100 góp ý, còn nữa")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Tải thêm" }));
    await row("#119#");
    expect(screen.getByText("120 góp ý")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Tải thêm" })).toBeNull();
    expect(new Set(fake.listCalls.map((q) => q.get("cursor"))).size).toBe(3);
  });

  it("API trả 403: câu không có quyền, không vẽ như rỗng", async () => {
    fake.listError = { status: 403, body: { error: "forbidden", request_id: "t" } };
    renderAt("/khach/gop-y");
    expect(await screen.findByText("Bạn không có quyền xem góp ý của khách.")).toBeTruthy();
    expect(screen.queryByText("Chưa có góp ý nào trong mục này")).toBeNull();
  });

  it("lỗi khác: câu lỗi và nút thử lại", async () => {
    fake.listError = { status: 500, body: { error: "internal", request_id: "r9" } };
    renderAt("/khach/gop-y");
    expect(await screen.findByText("Chưa tải được danh sách góp ý.")).toBeTruthy();
    fake.listError = null;
    fireEvent.click(screen.getByRole("button", { name: "Thử lại" }));
    expect(await row("Shipper giao trễ")).toBeTruthy();
  });
});

describe("Dòng mở", () => {
  it("hiện đủ nội dung, mã đơn, trang, thiết bị, ghi chú; một dòng mở một lúc; Đóng", async () => {
    renderAt("/khach/gop-y");
    const d = await openRow("Shipper giao trễ");
    expect(within(d).getByText("Shipper giao trễ 2 ngày, không gọi trước, hộp bị móp một góc.")).toBeTruthy();
    expect(within(d).getByText(/Mã đơn: W0001312/)).toBeTruthy();
    expect(within(d).getByText(/Trang khách đang xem: \/don-hang\/W0001312/)).toBeTruthy();
    expect(within(d).getByText(/Thiết bị: iPhone · Safari 18/)).toBeTruthy();
    const note = within(d).getByRole<HTMLTextAreaElement>("textbox", { name: "Ghi chú nội bộ" });
    expect(note.value).toBe("Đã báo Viettel Post.");
    expect(within(d).getByText("20 / 1000 · khách không thấy")).toBeTruthy();
    // Trạng thái "Mới": nút sang hai trạng thái còn lại.
    expect(within(d).getByRole("button", { name: "Đánh dấu đã xem" })).toBeTruthy();
    expect(within(d).getByRole("button", { name: "Đã phản hồi khách" })).toBeTruthy();
    expect(within(d).queryByRole("button", { name: "Đánh dấu chưa xem" })).toBeNull();
    expect(within(d).getByRole<HTMLButtonElement>("button", { name: "Lưu ghi chú" }).disabled).toBe(true);

    // Mở dòng khác thì dòng trước đóng.
    const d2 = await openRow("Trang mới dễ xem");
    expect(within(d2).getByText(SCRIPT_BODY)).toBeTruthy();
    expect(within(d2).queryByText(/Mã đơn/)).toBeNull();
    expect(screen.getAllByRole("region", { name: "Chi tiết góp ý" })).toHaveLength(1);
    fireEvent.click(within(await row("Trang mới dễ xem")).getByRole("button", { name: "Đóng" }));
    expect(screen.queryByRole("region", { name: "Chi tiết góp ý" })).toBeNull();
  });

  it("đổi trạng thái chỉ gửi version và state, có Idempotency-Key; dòng cập nhật", async () => {
    renderAt("/khach/gop-y");
    const d = await openRow("Shipper giao trễ");
    fireEvent.change(within(d).getByRole("textbox", { name: "Ghi chú nội bộ" }), { target: { value: "đang gõ" } });
    fireEvent.click(within(d).getByRole("button", { name: "Đã phản hồi khách" }));
    await written(1);
    const w = fake.writes[0];
    expect(w?.method).toBe("PATCH");
    expect(w?.path).toBe(`/api/v1/feedback/${FB1}`);
    expect(w?.body).toEqual({ version: 1, state: "answered" });
    expect(w?.key).toBeTruthy();
    expect(await within(await row("Shipper giao trễ")).findByText("Đã phản hồi")).toBeTruthy();
    const d2 = screen.getByRole("region", { name: "Chi tiết góp ý" });
    expect(within(d2).getByRole("button", { name: "Đánh dấu chưa xem" })).toBeTruthy();
    expect(within(d2).getByRole("button", { name: "Đánh dấu đã xem" })).toBeTruthy();
    // Ghi chú đang gõ vẫn giữ.
    expect(within(d2).getByRole<HTMLTextAreaElement>("textbox", { name: "Ghi chú nội bộ" }).value).toBe("đang gõ");
  });

  it("lưu ghi chú chỉ gửi version và note; xóa hết là note rỗng; quá 1000 ký tự không gõ được", async () => {
    renderAt("/khach/gop-y");
    const d = await openRow("Shipper giao trễ");
    const note = within(d).getByRole<HTMLTextAreaElement>("textbox", { name: "Ghi chú nội bộ" });
    expect(note.maxLength).toBe(1000);
    fireEvent.change(note, { target: { value: "Đã gọi lại khách" } });
    fireEvent.click(within(d).getByRole("button", { name: "Lưu ghi chú" }));
    await written(1);
    expect(fake.writes[0]?.body).toEqual({ version: 1, note: "Đã gọi lại khách" });
    expect(await within(d).findByText("Đã lưu ghi chú.")).toBeTruthy();
    await waitFor(() => {
      expect(within(d).getByRole<HTMLButtonElement>("button", { name: "Lưu ghi chú" }).disabled).toBe(true);
    });
    fireEvent.change(note, { target: { value: "  " } });
    fireEvent.click(within(d).getByRole("button", { name: "Lưu ghi chú" }));
    await written(2);
    expect(fake.writes[1]?.body).toEqual({ version: 2, note: "" });
  });

  it("đang lưu: khóa ô ghi chú và mọi nút của dòng", async () => {
    let release: () => void = () => undefined;
    fake.hold = new Promise((r) => {
      release = r;
    });
    renderAt("/khach/gop-y");
    const d = await openRow("Shipper giao trễ");
    fireEvent.click(within(d).getByRole("button", { name: "Đánh dấu đã xem" }));
    await waitFor(() => {
      expect(within(d).getByRole<HTMLTextAreaElement>("textbox", { name: "Ghi chú nội bộ" }).disabled).toBe(true);
    });
    for (const b of within(d).getAllByRole<HTMLButtonElement>("button")) expect(b.disabled).toBe(true);
    fireEvent.click(within(d).getByRole("button", { name: "Đã phản hồi khách" }));
    expect(fake.writes).toHaveLength(1);
    await act(async () => {
      release();
      await Promise.resolve();
    });
    expect(await within(await row("Shipper giao trễ")).findByText("Đã xem")).toBeTruthy();
  });

  it("409: tải lại, báo trạng thái mới, người sửa, giờ; giữ ghi chú đang gõ; lần lưu sau dùng version mới", async () => {
    renderAt("/khach/gop-y");
    const d = await openRow("Shipper giao trễ");
    // Người khác vừa đổi sang "Đã phản hồi".
    fake.rows[0] = feedback({
      state: "answered",
      version: 2,
      updatedByName: "Lê Hồng Nhung",
      updatedAt: "2026-11-21T03:30:00Z",
    });
    fake.replies.push({ status: 409, body: { error: "version_conflict", request_id: "t" } });
    const note = within(d).getByRole<HTMLTextAreaElement>("textbox", { name: "Ghi chú nội bộ" });
    fireEvent.change(note, { target: { value: "Ghi chú của tôi" } });
    fireEvent.click(within(d).getByRole("button", { name: "Lưu ghi chú" }));
    const box = await within(d).findByText("Góp ý vừa được người khác đổi.");
    const text = box.parentElement?.textContent ?? "";
    await waitFor(() => {
      expect(box.parentElement?.textContent).toContain('"Đã phản hồi" (Lê Hồng Nhung, 10:30 21/11)');
    });
    expect(text).toContain("Ghi chú bạn đang gõ vẫn giữ trong ô");
    expect(note.value).toBe("Ghi chú của tôi");
    expect(within(await row("Shipper giao trễ")).getByText("Đã phản hồi")).toBeTruthy();
    fireEvent.click(within(d).getByRole("button", { name: "Lưu ghi chú" }));
    await written(2);
    expect(fake.writes[1]?.body).toEqual({ version: 2, note: "Ghi chú của tôi" });
    expect(fake.writes[1]?.key).not.toBe(fake.writes[0]?.key);
  });

  it("chỉ có quyền xem: ghi chú hiện chữ, không có nút, không có ô", async () => {
    fake.grants = grantsOf("customer.read");
    renderAt("/khach/gop-y");
    const d = await openRow("Shipper giao trễ");
    expect(within(d).getByText("Đã báo Viettel Post.")).toBeTruthy();
    expect(within(d).queryByRole("textbox")).toBeNull();
    expect(within(d).queryAllByRole("button")).toHaveLength(0);
  });

  it("sửa bị 403: câu không có quyền", async () => {
    fake.replies.push({ status: 403, body: { error: "forbidden", request_id: "t" } });
    renderAt("/khach/gop-y");
    const d = await openRow("Shipper giao trễ");
    fireEvent.click(within(d).getByRole("button", { name: "Đánh dấu đã xem" }));
    expect(await within(d).findByText("Bạn chưa được cấp quyền làm việc này")).toBeTruthy();
  });

  it("không ghi nội dung, liên lạc vào log trình duyệt hay localStorage", async () => {
    const spies = (["log", "info", "warn", "error", "debug"] as const).map((m) => vi.spyOn(console, m));
    const store = vi.spyOn(Storage.prototype, "setItem");
    renderAt("/khach/gop-y");
    const d = await openRow("Shipper giao trễ");
    fireEvent.change(within(d).getByRole("textbox", { name: "Ghi chú nội bộ" }), { target: { value: "Gọi 0912" } });
    fireEvent.click(within(d).getByRole("button", { name: "Lưu ghi chú" }));
    await written(1);
    for (const s of spies) expect(s).not.toHaveBeenCalled();
    expect(store).not.toHaveBeenCalled();
  });
});

describe("Số trên thanh nhãn (Task 486)", () => {
  it("mỗi nhãn có số góp ý của cả shop; đổi trạng thái một góp ý thì số đọc lại", async () => {
    renderAt("/khach/gop-y");
    for (const name of ["Mới 2", "Đã xem 1", "Đã phản hồi 2", "Tất cả 5"]) {
      expect(await screen.findByRole("button", { name })).toBeTruthy();
    }
    const d = await openRow("Shipper giao trễ");
    fireEvent.click(within(d).getByRole("button", { name: "Đã phản hồi khách" }));
    await written(1);
    expect(await screen.findByRole("button", { name: "Đã phản hồi 3" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Mới 1" })).toBeTruthy();
    expect(fake.countReads).toBe(2);
  });
});
