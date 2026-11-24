// @vitest-environment jsdom
// Màn Nhật ký thao tác /nhat-ky-thao-tac (kế hoạch M5 Task 462; mockup docs/mockups/m5/audit.html trạng thái A, B;
// HV24, HV25): mới nhất trước; người làm nhân viên, khóa dịch vụ, hệ thống; chỉ tên trường; lọc thao tác, người, khoảng
// ngày; xem thêm theo con trỏ (3 trang không trùng, còn nextCursor thì chưa "hết"); rỗng; 403.
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { KEY, LAN, installFake, log, many, renderAt, type Fake } from "./auditTestkit";

let fake: Fake;
beforeEach(() => {
  fake = installFake();
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

const bodyRows = () => screen.getAllByRole("row").slice(1);
const lastCall = () => {
  const q = fake.listCalls.at(-1);
  if (q === undefined) throw new Error("chưa gọi listAuditLogs");
  return q;
};

describe("Nhật ký thao tác", () => {
  it("mỗi dòng: giờ, người làm và loại, thao tác, mục, trường đổi (không giá trị)", async () => {
    fake.rows = [
      log(),
      log({
        id: "r2",
        actorType: "service",
        actorId: KEY,
        actorName: "Website ELIMAZ — bản chính",
        action: "order.request.create",
        entityType: "order_request",
        entityId: "0195f3b2-0000-7000-8000-00000000aa01",
        entityName: "YC-0000144",
        changedFields: [],
        createdAt: "2026-11-21T03:42:00Z",
      }),
      log({
        id: "r3",
        actorType: "system",
        actorId: undefined,
        actorName: undefined,
        action: "promotion.birthday.run",
        entityType: "promotion.birthday_run",
        entityId: "run-1",
        changedFields: [],
        createdAt: "2026-11-21T02:00:00Z",
      }),
      log({
        id: "r4",
        action: "ma.moi.chua.nhan",
        entityType: "loai_moi",
        entityId: "x1",
        changedFields: ["ship_address"],
        createdAt: "2026-11-21T01:00:00Z",
      }),
    ];
    renderAt();
    await screen.findByRole("table");
    const [r1, r2, r3, r4] = bodyRows();
    if (r1 === undefined || r2 === undefined || r3 === undefined || r4 === undefined) throw new Error("thiếu dòng");
    expect(within(r1).getByText("13:05 · 21/11")).toBeTruthy();
    expect(within(r1).getByText("Nguyễn Thị Lan")).toBeTruthy();
    expect(within(r1).getByText("Nhân viên")).toBeTruthy();
    expect(within(r1).getByText("Nhận đơn web")).toBeTruthy();
    expect(within(r1).getByText("tạm dừng, lý do")).toBeTruthy();
    // Thực thể toàn shop: không hiện mã shop.
    expect(r1.textContent).not.toContain("0000000000ff");

    expect(within(r2).getByText("Website ELIMAZ — bản chính")).toBeTruthy();
    expect(within(r2).getByText("Khóa dịch vụ")).toBeTruthy();
    expect(within(r2).getByText("Tạo yêu cầu về đơn hàng")).toBeTruthy();
    expect(within(r2).getByText("Yêu cầu")).toBeTruthy();
    // Có tên dễ đọc (Task 488) → hiện tên thay mã.
    expect(within(r2).getByText("YC-0000144").className).toContain("wrap-any");
    expect(r2.textContent).not.toContain("00000000aa01");
    expect(within(r2).getByText("—")).toBeTruthy();

    expect(within(r3).getAllByText("Hệ thống")).toHaveLength(2);
    expect(within(r3).getByText("Tạo mã quà sinh nhật")).toBeTruthy();

    // Mã chưa có nhãn hiện nguyên mã.
    expect(within(r4).getByText("ma.moi.chua.nhan")).toBeTruthy();
    expect(within(r4).getByText("loai_moi")).toBeTruthy();
    expect(within(r4).getByText("ship_address")).toBeTruthy();
    expect(screen.getByText("4 dòng")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Xem thêm" })).toBeNull();
  });

  it("120 dòng → 3 trang theo con trỏ, không trùng, không sót; còn trang thì ghi 'còn nữa'", async () => {
    fake.rows = many(120);
    renderAt();
    await screen.findByText("50 dòng · còn nữa");
    expect(bodyRows()).toHaveLength(50);
    fireEvent.click(screen.getByRole("button", { name: "Xem thêm" }));
    await screen.findByText("100 dòng · còn nữa");
    expect(lastCall().get("cursor")).toBe("50");
    fireEvent.click(screen.getByRole("button", { name: "Xem thêm" }));
    await screen.findByText("120 dòng");
    expect(screen.queryByRole("button", { name: "Xem thêm" })).toBeNull();
    const times = bodyRows().map((r) => r.textContent);
    expect(times).toHaveLength(120);
    expect(new Set(times).size).toBe(120);
    expect(fake.listCalls.map((q) => q.get("cursor"))).toEqual([null, "50", "100"]);
  });

  it("lọc thao tác gửi đúng mã; lọc người lấy từ listAuditActors (cả người chưa có dòng, đã khóa), gửi actorId", async () => {
    fake.rows = [
      log(),
      log({
        id: "r2",
        actorType: "service",
        actorId: KEY,
        actorName: "Website ELIMAZ",
        action: "customer.feedback.submit",
        entityType: "customer.feedback",
        changedFields: [],
      }),
    ];
    renderAt();
    await screen.findByRole("table");
    fireEvent.change(screen.getByRole("combobox", { name: "Hành động" }), {
      target: { value: "customer.feedback.submit" },
    });
    await waitFor(() => {
      expect(lastCall().get("action")).toBe("customer.feedback.submit");
    });
    await waitFor(() => {
      expect(bodyRows()).toHaveLength(1);
    });
    fireEvent.change(screen.getByRole("combobox", { name: "Hành động" }), { target: { value: "" } });
    await waitFor(() => {
      expect(bodyRows()).toHaveLength(2);
    });

    const who = screen.getByRole("combobox", { name: "Người làm" });
    expect(await within(who).findByRole("option", { name: "Nguyễn Thị Lan" })).toBeTruthy();
    expect(await within(who).findByRole("option", { name: "Khóa: Website ELIMAZ" })).toBeTruthy();
    // Người chưa có dòng nào trong trang đã tải vẫn có trong ô; đã khóa ghi rõ.
    expect(within(who).getByRole("option", { name: "Trần Văn Cũ (đã khóa)" })).toBeTruthy();
    fireEvent.change(who, { target: { value: LAN } });
    await waitFor(() => {
      expect(lastCall().get("actorId")).toBe(LAN);
    });
    await waitFor(() => {
      expect(bodyRows()).toHaveLength(1);
    });
    // Người đang chọn vẫn còn trong ô dù danh sách chỉ còn dòng của người đó; người khác cũng còn để đổi.
    expect(within(who).getByRole("option", { name: "Khóa: Website ELIMAZ" })).toBeTruthy();
    expect(who).toHaveProperty("value", LAN);
  });

  it("chưa tải được người làm → ô chỉ còn 'Mọi người', nhật ký vẫn hiện", async () => {
    fake.actorsError = { status: 500, body: { error: "internal", request_id: "x" } };
    renderAt();
    await screen.findByRole("table");
    const who = screen.getByRole("combobox", { name: "Người làm" });
    expect(
      within(who)
        .getAllByRole("option")
        .map((o) => o.textContent),
    ).toEqual(["Mọi người"]);
  });

  it("khoảng ngày → from, to theo giờ VN (lấy trọn ngày cuối); ngày đầu sau ngày cuối → câu lỗi, không gọi API", async () => {
    renderAt();
    await screen.findByRole("table");
    fireEvent.change(screen.getByLabelText("Từ ngày"), { target: { value: "2026-11-21" } });
    fireEvent.change(screen.getByLabelText("Đến ngày"), { target: { value: "2026-11-21" } });
    await waitFor(() => {
      expect(lastCall().get("to")).toBe("2026-11-21T17:00:00.000Z");
    });
    expect(lastCall().get("from")).toBe("2026-11-20T17:00:00.000Z");
    const calls = fake.listCalls.length;
    fireEvent.change(screen.getByLabelText("Từ ngày"), { target: { value: "2026-11-23" } });
    expect(await screen.findByText('"Từ ngày" phải trước hoặc bằng "Đến ngày".')).toBeTruthy();
    expect(fake.listCalls).toHaveLength(calls);
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("không có dòng khớp bộ lọc → câu rỗng riêng; chưa có dòng nào → câu khác", async () => {
    fake.rows = [];
    renderAt();
    expect(await screen.findByText("Chưa có thao tác nào")).toBeTruthy();
    fireEvent.change(screen.getByRole("combobox", { name: "Hành động" }), { target: { value: "order.cancel" } });
    expect(await screen.findByText("Không có thao tác phù hợp")).toBeTruthy();
    expect(screen.getByText("Đổi hành động, người làm hoặc khoảng ngày.")).toBeTruthy();
  });

  it("403 → câu không có quyền, không vẽ như rỗng; lỗi khác → Thử lại", async () => {
    fake.listError = { status: 403, body: { error: "forbidden", request_id: "t" } };
    renderAt();
    expect(await screen.findByText("Bạn không có quyền xem nhật ký thao tác.")).toBeTruthy();
    expect(screen.queryByText("Chưa có thao tác nào")).toBeNull();
    cleanup();
    fake.listError = { status: 500, body: { error: "internal", request_id: "t" } };
    renderAt();
    expect(await screen.findByText("Chưa tải được nhật ký thao tác.")).toBeTruthy();
    fake.listError = null;
    fireEvent.click(screen.getByRole("button", { name: "Thử lại" }));
    expect(await screen.findByRole("table")).toBeTruthy();
  });
});
