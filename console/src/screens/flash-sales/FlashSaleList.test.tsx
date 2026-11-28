// @vitest-environment jsdom
// Danh sách đợt flash sale (mockup docs/mockups/m5/flash-sales.html trạng thái A, B, H). Kế hoạch M5 Task 206 Step 1:
// danh sách rỗng và 30 đợt; tạo đợt gửi Idempotency-Key, 503 rồi tự gửi lại cùng khóa; không quyền → không nút ghi; tên
// 120 ký tự xuống dòng không tràn; lọc trạng thái; lỗi tải không vẽ như rỗng.
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import {
  LONG_NAME,
  S1,
  S2,
  S3,
  S4,
  SNEW,
  grantsOf,
  hookFlashFake,
  typeInto,
  renderAt,
  sale,
  type Fake,
} from "./flashTestkit";

let fake: Fake;
hookFlashFake((f) => {
  fake = f;
});
const type = typeInto;
const rowOf = (text: string | RegExp) => screen.getByText(text).closest("tr") as HTMLElement;

describe("Danh sách", () => {
  it("không có đợt → hộp rỗng có nút tạo đợt", async () => {
    fake.sales = [];
    renderAt("/khuyen-mai/flash-sale");
    await screen.findByText("Chưa có đợt flash sale");
    expect(screen.getAllByRole("button", { name: "+ Tạo đợt" }).length).toBeGreaterThan(0);
  });

  it("đủ bốn trạng thái: chip, dòng phụ, nút mở/sửa/xem; đợt tên 120 ký tự có title và gọi lớp xuống dòng", async () => {
    fake.sales = [
      sale({
        id: S1,
        name: "Trưa thứ Sáu đồng giá 199k",
        status: "open",
        itemCount: 4,
        endsAt: "2099-01-01T00:00:00Z",
      }),
      sale({ id: S2, name: LONG_NAME, status: "scheduled", itemCount: 12, startsAt: "2026-11-28T13:00:00Z" }),
      sale({ id: S3, name: "Đêm 12.12 khung 1", status: "draft", itemCount: 6 }),
      sale({ id: S4, name: "Sale 11.11 nửa đêm", status: "closed", closedReason: "ended", itemCount: 6 }),
    ];
    renderAt("/khuyen-mai/flash-sale");
    await screen.findByText("Trưa thứ Sáu đồng giá 199k");
    expect(within(rowOf("Trưa thứ Sáu đồng giá 199k")).getByText("Đang mở")).toBeTruthy();
    expect(within(rowOf(LONG_NAME)).getByText("Đã lên lịch")).toBeTruthy();
    expect(within(rowOf(LONG_NAME)).getByText("12 món · tự mở lúc 20:00 28/11")).toBeTruthy();
    expect(within(rowOf("Đêm 12.12 khung 1")).getByText("6 món · chưa giữ suất nào")).toBeTruthy();
    expect(within(rowOf("Sale 11.11 nửa đêm")).getByText("6 món · hết giờ")).toBeTruthy();
    // Nút: nháp là Sửa, đã đóng là Xem, còn lại Mở; cùng một liên kết sang trang chi tiết.
    expect(within(rowOf("Đêm 12.12 khung 1")).getByRole("link", { name: "Sửa" }).getAttribute("href")).toBe(
      `/khuyen-mai/flash-sale/${S3}`,
    );
    expect(within(rowOf("Sale 11.11 nửa đêm")).getByRole("link", { name: "Xem" })).toBeTruthy();
    expect(within(rowOf("Trưa thứ Sáu đồng giá 199k")).getByRole("link", { name: "Mở" })).toBeTruthy();
    const name = screen.getByText(LONG_NAME);
    expect(name.getAttribute("title")).toBe(LONG_NAME);
    expect(name.className).toContain("clamp2");
  });

  it("30 đợt: trang đầu 20, Xem thêm tải nốt 10", async () => {
    fake.sales = Array.from({ length: 30 }, (_, i) =>
      sale({ id: `0195f3b2-0000-7000-8000-00000000a${String(i).padStart(3, "0")}`, name: `Đợt số ${String(i + 1)}` }),
    );
    renderAt("/khuyen-mai/flash-sale");
    await screen.findByText("Đợt số 1");
    expect(screen.getAllByRole("link", { name: "Sửa" })).toHaveLength(20);
    expect(screen.getByText(/Hiện 20 đợt/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Xem thêm" }));
    await screen.findByText("Đợt số 30");
    expect(screen.getAllByRole("link", { name: "Sửa" })).toHaveLength(30);
    expect(screen.queryByRole("button", { name: "Xem thêm" })).toBeNull();
  });

  it("lọc trạng thái gửi ?status= và nút đang chọn có aria-pressed", async () => {
    fake.sales = [sale({ id: S1, name: "A", status: "open" }), sale({ id: S2, name: "B", status: "draft" })];
    renderAt("/khuyen-mai/flash-sale");
    await screen.findByText("B");
    const bar = screen.getByRole("group", { name: "Lọc theo trạng thái" });
    expect(within(bar).getByRole("button", { name: "Tất cả" }).getAttribute("aria-pressed")).toBe("true");
    fireEvent.click(within(bar).getByRole("button", { name: "Đang mở" }));
    await waitFor(() => {
      expect(screen.queryByText("B")).toBeNull();
    });
    await screen.findByText("A");
    expect(fake.listCalls.at(-1)?.get("status")).toBe("open");
    expect(within(bar).getByRole("button", { name: "Đang mở" }).getAttribute("aria-pressed")).toBe("true");
  });

  it("lỗi tải: hộp lỗi và Thử lại, không vẽ danh sách rỗng", async () => {
    fake.listFails = true;
    renderAt("/khuyen-mai/flash-sale");
    await screen.findByText("Chưa tải được danh sách đợt.");
    expect(screen.queryByText("Chưa có đợt flash sale")).toBeNull();
    fake.listFails = false;
    fireEvent.click(screen.getByRole("button", { name: "Thử lại" }));
    await screen.findByText("Đêm 12.12 khung 1");
  });

  it("không có promotion.flash.manage → không nút Tạo đợt", async () => {
    fake.grants = grantsOf();
    renderAt("/khuyen-mai/flash-sale");
    await screen.findByText("Đêm 12.12 khung 1");
    expect(screen.queryByRole("button", { name: "+ Tạo đợt" })).toBeNull();
  });
});

describe("Tạo đợt", () => {
  const open = async () => {
    renderAt("/khuyen-mai/flash-sale");
    await screen.findByText("Đêm 12.12 khung 1");
    fireEvent.click(screen.getByRole("button", { name: "+ Tạo đợt" }));
    return screen.findByRole("dialog", { name: "Tạo đợt flash sale" });
  };
  const fill = (d: HTMLElement, name: string, start: string, end: string) => {
    type(within(d).getByLabelText("Tên đợt *"), name);
    type(within(d).getByLabelText("Giờ mở (giờ VN) *"), start);
    type(within(d).getByLabelText("Giờ đóng (giờ VN) *"), end);
  };

  it("gửi tên và giờ VN → UTC kèm Idempotency-Key, xong mở trang đợt mới", async () => {
    const d = await open();
    fill(d, "  Đêm 12.12 khung 2  ", "2026-12-12T01:00", "2026-12-12T03:00");
    fireEvent.click(within(d).getByRole("button", { name: "Tạo đợt nháp" }));
    await screen.findByRole("heading", { level: 1, name: /Đêm 12.12 khung 2/ });
    expect(fake.writes).toHaveLength(1);
    const w = fake.writes[0];
    expect(w?.path).toBe("/api/v1/flash-sales");
    expect(w?.key).toBeTruthy();
    expect(w?.body).toEqual({
      name: "Đêm 12.12 khung 2",
      startsAt: "2026-12-11T18:00:00.000Z",
      endsAt: "2026-12-11T20:00:00.000Z",
    });
  });

  it("503 retry_later rồi tự gửi lại đúng cùng Idempotency-Key, chỉ tạo một đợt", async () => {
    const d = await open();
    fill(d, "Đợt đông", "2026-12-12T01:00", "2026-12-12T03:00");
    fake.replies.push({ status: 503, body: { error: "retry_later", request_id: "t" } });
    fireEvent.click(within(d).getByRole("button", { name: "Tạo đợt nháp" }));
    await screen.findByRole("heading", { level: 1, name: /Đợt đông/ }, { timeout: 4000 });
    expect(fake.writes).toHaveLength(2);
    expect(fake.writes[0]?.key).toBe(fake.writes[1]?.key);
    expect(fake.sales.filter((s) => s.id === SNEW)).toHaveLength(1);
  });

  it("giờ đóng không sau giờ mở, hay dài quá 7 ngày → báo lỗi, nút khóa, không gửi", async () => {
    const d = await open();
    fill(d, "Đợt lỗi", "2026-12-12T00:00", "2026-12-11T23:00");
    expect(within(d).getByText("Giờ đóng phải sau giờ mở.")).toBeTruthy();
    expect(within(d).getByRole("button", { name: "Tạo đợt nháp" })).toHaveProperty("disabled", true);
    type(within(d).getByLabelText("Giờ đóng (giờ VN) *"), "2026-12-20T02:00");
    expect(within(d).getByText("Đợt dài tối đa 7 ngày (đang là 8 ngày 2 giờ).")).toBeTruthy();
    expect(fake.writes).toHaveLength(0);
  });

  it("tên 121 ký tự bị chặn", async () => {
    const d = await open();
    fill(d, "a".repeat(121), "2026-12-12T00:00", "2026-12-12T02:00");
    expect(within(d).getByText("Tên tối đa 120 ký tự")).toBeTruthy();
    expect(within(d).getByRole("button", { name: "Tạo đợt nháp" })).toHaveProperty("disabled", true);
  });

  it("máy chủ báo invalid_flash_window → hiện câu lỗi trong hộp, hộp còn mở", async () => {
    const d = await open();
    fill(d, "Đợt", "2026-12-12T00:00", "2026-12-12T02:00");
    fake.replies.push({ status: 422, body: { error: "invalid_flash_window", request_id: "t" } });
    fireEvent.click(within(d).getByRole("button", { name: "Tạo đợt nháp" }));
    await within(d).findByText("Giờ kết thúc phải sau giờ bắt đầu, và một đợt dài tối đa 7 ngày");
  });
});
