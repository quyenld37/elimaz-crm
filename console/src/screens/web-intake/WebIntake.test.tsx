// @vitest-environment jsdom
// Màn Nhận đơn web /website/nhan-don-web (kế hoạch M5 Task 462; mockup docs/mockups/m5/web-intake.html trạng thái
// A–D; HV23): đang nhận / đang tạm dừng kèm lý do, người, giờ; tạm dừng cần lý do ≤ 300 và xác nhận (thiếu lý do → nút
// khóa); mở lại cần xác nhận; đang lưu khóa nút; 409 → tải lại và báo; chỉ có quyền xem → nút khóa kèm câu; 403, 422.
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { grantsOf, installFake, intake, renderAt, stubDialog, type Fake } from "./webIntakeTestkit";

let fake: Fake;
beforeEach(() => {
  stubDialog();
  fake = installFake();
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

const written = (n: number) =>
  waitFor(() => {
    expect(fake.writes).toHaveLength(n);
  });
const dialog = () => screen.findByRole("dialog");
/** Bấm nút khi đã bật (quyền tải xong). */
const press = async (name: string) => {
  const b = await screen.findByRole("button", { name });
  await waitFor(() => {
    expect(b).toHaveProperty("disabled", false);
  });
  fireEvent.click(b);
};
const openPause = async () => {
  await press("Tạm dừng nhận đơn");
  return dialog();
};
const REASON = "Kiểm kho cuối tháng, mở lại 18:00";
/** Gõ REASON vào hộp tạm dừng d rồi bấm xác nhận. */
const submitPause = (d: HTMLElement) => {
  fireEvent.change(within(d).getByLabelText("Lý do"), { target: { value: REASON } });
  fireEvent.click(within(d).getByRole("button", { name: "Xác nhận tạm dừng" }));
};

describe("Trạng thái", () => {
  it("đang nhận đơn: tiêu đề, chip, ai mở lại lúc nào", async () => {
    renderAt();
    expect(await screen.findByRole("heading", { name: "Website đang nhận đơn" })).toBeTruthy();
    expect(screen.getByText("Đang nhận đơn")).toBeTruthy();
    expect(screen.getByText("Mở lại lúc 08:00 · 20/11/2026 bởi Nguyễn Thị Lan.")).toBeTruthy();
  });

  it("chưa ai đổi → không có dòng mở lại", async () => {
    fake.state = { paused: false, version: 1 };
    renderAt();
    await screen.findByRole("heading", { name: "Website đang nhận đơn" });
    expect(screen.queryByText(/Mở lại lúc/)).toBeNull();
  });

  it("đang tạm dừng: lý do, người, giờ (có năm)", async () => {
    fake.state = intake({ paused: true, reason: REASON, changedAt: "2026-11-21T06:05:00Z" });
    renderAt();
    expect(await screen.findByRole("heading", { name: "Website đang tạm dừng nhận đơn mới" })).toBeTruthy();
    expect(screen.getByText("Đang tạm dừng")).toBeTruthy();
    expect(screen.getByText(`Lý do: ${REASON}`)).toBeTruthy();
    expect(screen.getByText("Nguyễn Thị Lan · 13:05 · 21/11/2026")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Mở lại nhận đơn" })).toBeTruthy();
  });

  it("403 → câu không có quyền; lỗi khác → Thử lại", async () => {
    fake.readError = { status: 403, body: { error: "forbidden", request_id: "t" } };
    renderAt();
    expect(await screen.findByText("Bạn không có quyền xem trạng thái nhận đơn web.")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Tạm dừng nhận đơn" })).toBeNull();
    cleanup();
    fake.readError = { status: 500, body: { error: "internal", request_id: "t" } };
    renderAt();
    expect(await screen.findByText("Chưa tải được trạng thái nhận đơn web.")).toBeTruthy();
    fake.readError = null;
    fireEvent.click(screen.getByRole("button", { name: "Thử lại" }));
    expect(await screen.findByRole("heading", { name: "Website đang nhận đơn" })).toBeTruthy();
  });
});

describe("Tạm dừng", () => {
  it("thiếu lý do (kể cả chỉ khoảng trắng) → nút xác nhận khóa kèm câu; đủ → gửi paused, lý do đã cắt, version, khóa chống trùng", async () => {
    renderAt();
    const d = await openPause();
    const confirm = within(d).getByRole("button", { name: "Xác nhận tạm dừng" });
    expect(confirm).toHaveProperty("disabled", true);
    expect(within(d).getByText("Nhập lý do để tạm dừng")).toBeTruthy();
    expect(within(d).getByText("0 / 300")).toBeTruthy();
    expect(within(d).getByText(/vẫn vào CRM/)).toBeTruthy();
    const box = within(d).getByLabelText("Lý do");
    expect(box).toHaveProperty("maxLength", 300);
    fireEvent.change(box, { target: { value: "   " } });
    expect(confirm).toHaveProperty("disabled", true);
    fireEvent.change(box, { target: { value: `  ${REASON} ` } });
    expect(within(d).getByText(`${String(REASON.length + 3)} / 300`)).toBeTruthy();
    expect(confirm).toHaveProperty("disabled", false);
    expect(within(d).queryByText("Nhập lý do để tạm dừng")).toBeNull();
    fireEvent.click(confirm);
    await written(1);
    expect(fake.writes[0]?.method).toBe("PUT");
    expect(fake.writes[0]?.body).toEqual({ paused: true, reason: REASON, version: 3 });
    expect(fake.writes[0]?.key).toBeTruthy();
    expect(await screen.findByRole("heading", { name: "Website đang tạm dừng nhận đơn mới" })).toBeTruthy();
    expect(screen.getByText(`Lý do: ${REASON}`)).toBeTruthy();
    expect(screen.getByText("Tôi · 13:05 · 21/11/2026")).toBeTruthy();
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("đang lưu: ô lý do, Đóng, nút gửi đều khóa; bấm lại không gửi thêm", async () => {
    let release: () => void = () => undefined;
    fake.hold = new Promise<void>((r) => {
      release = r;
    });
    renderAt();
    const d = await openPause();
    submitPause(d);
    const saving = await within(d).findByRole("button", { name: "Đang lưu…" });
    expect(saving).toHaveProperty("disabled", true);
    // Nút × ẩn; nút Đóng khóa.
    expect(within(d).getByRole("button", { name: "Đóng" })).toHaveProperty("disabled", true);
    expect(within(d).getByLabelText("Lý do")).toHaveProperty("disabled", true);
    fireEvent.click(saving);
    release();
    await screen.findByRole("heading", { name: "Website đang tạm dừng nhận đơn mới" });
    expect(fake.writes).toHaveLength(1);
  });

  it("422 → câu lỗi trong hộp, giữ lý do", async () => {
    fake.replies.push({ status: 422, body: { error: "invalid_input", request_id: "t" } });
    renderAt();
    const d = await openPause();
    submitPause(d);
    expect(await within(d).findByRole("alert")).toBeTruthy();
    expect(within(d).getByLabelText("Lý do")).toHaveProperty("value", REASON);
  });

  it("409 (người khác vừa đổi) → đóng hộp, tải lại, báo trạng thái mới; Đã hiểu ẩn câu báo", async () => {
    renderAt();
    const d = await openPause();
    fireEvent.change(within(d).getByLabelText("Lý do"), { target: { value: REASON } });
    const readsBefore = fake.reads;
    fake.state = intake({
      paused: true,
      reason: "Sự cố hãng giao hàng",
      changedByName: "Trần Minh Thư",
      changedAt: "2026-11-21T06:04:00Z",
      version: 4,
    });
    fireEvent.click(within(d).getByRole("button", { name: "Xác nhận tạm dừng" }));
    expect(await screen.findByText("Trạng thái vừa được người khác đổi.")).toBeTruthy();
    expect(
      screen.getByText(
        'Đã tải lại: website đang tạm dừng (Trần Minh Thư, 13:04 · 21/11/2026, lý do "Sự cố hãng giao hàng"). Chưa đổi gì thêm.',
      ),
    ).toBeTruthy();
    expect(fake.reads).toBeGreaterThan(readsBefore);
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.getByRole("heading", { name: "Website đang tạm dừng nhận đơn mới" })).toBeTruthy();
    expect(fake.writes).toHaveLength(1);
    fireEvent.click(screen.getByRole("button", { name: "Đã hiểu" }));
    expect(screen.queryByText("Trạng thái vừa được người khác đổi.")).toBeNull();
  });

  it("Đóng → không gửi gì", async () => {
    renderAt();
    const d = await openPause();
    const closers = within(d).getAllByRole("button", { name: "Đóng" });
    expect(closers).toHaveLength(2);
    closers.forEach((b) => {
      expect(b).toHaveProperty("disabled", false);
    });
    fireEvent.click(closers[1] ?? d);
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(fake.writes).toHaveLength(0);
  });
});

describe("Mở lại", () => {
  it("cần xác nhận; gửi paused=false, version", async () => {
    fake.state = intake({ paused: true, reason: REASON, version: 7 });
    renderAt();
    await press("Mở lại nhận đơn");
    const d = await dialog();
    expect(within(d).getByRole("heading", { name: "Mở lại nhận đơn?" })).toBeTruthy();
    expect(fake.writes).toHaveLength(0);
    fireEvent.click(within(d).getByRole("button", { name: "Mở lại nhận đơn" }));
    await written(1);
    expect(fake.writes[0]?.body).toEqual({ paused: false, version: 7 });
    expect(await screen.findByRole("heading", { name: "Website đang nhận đơn" })).toBeTruthy();
    expect(screen.getByText("Mở lại lúc 13:05 · 21/11/2026 bởi Tôi.")).toBeTruthy();
  });
});

describe("Quyền", () => {
  it("chỉ có order.read → nút khóa kèm câu chỉ xem", async () => {
    fake.grants = grantsOf();
    renderAt();
    const b = await screen.findByRole("button", { name: "Tạm dừng nhận đơn" });
    await waitFor(() => {
      expect(b).toHaveProperty("disabled", true);
    });
    expect(screen.getByText('Bạn chỉ xem được (thiếu quyền "Tạm dừng nhận đơn web").')).toBeTruthy();
    fireEvent.click(b);
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("đang tạm dừng mà chỉ xem → nút Mở lại cũng khóa", async () => {
    fake.grants = grantsOf();
    fake.state = intake({ paused: true, reason: REASON });
    renderAt();
    const b = await screen.findByRole("button", { name: "Mở lại nhận đơn" });
    await waitFor(() => {
      expect(b).toHaveProperty("disabled", true);
    });
  });
});
