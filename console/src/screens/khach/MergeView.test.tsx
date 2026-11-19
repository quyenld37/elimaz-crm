// @vitest-environment jsdom
// Gộp khách và các lần gộp (mockup docs/mockups/m2/khach-gop.html trạng thái A–C; HV29, HV30; DH13, DH14). Kế hoạch
// M2 Task M2-29, Step 1: gộp phải chọn hồ sơ giữ lại và xác nhận lần hai; merge_not_latest → câu báo có liên kết.
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { C1, C2, C3, customer, grantsOf, installFake, nth, renderAt, stubDialog, type Fake } from "./khachTestkit";

let fake: Fake;
const TUNG = customer({
  id: C2,
  rootId: C2,
  code: "KH001877",
  fullName: "Nguyễn Văn Tùng",
  memberIds: [C2],
  identities: [
    {
      id: "i9",
      customerId: C2,
      kind: "phone",
      value: "+84987654321",
      source: "order",
      createdAt: "2026-05-01T00:00:00Z",
    },
  ],
  addresses: [],
  tags: [],
  stats: { orderCount: 2, deliveredCount: 2, returnedCount: 0, totalSpent: 980_000, returnRateBp: 0 },
  version: 1,
});

beforeEach(() => {
  stubDialog();
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2026-11-19T03:00:00Z"));
  fake = installFake();
  fake.grants = grantsOf("customer.merge");
  fake.customers.set(C1, customer());
  fake.customers.set(C2, TUNG);
  fake.candidates = [
    {
      rootId: C2,
      code: "KH001877",
      fullName: "Nguyễn Văn Tùng",
      matchedPhones: ["+84987654321"],
      stats: TUNG.stats,
    },
  ];
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const mergeButton = () => screen.getByRole("button", { name: /^Gộp vào|^Chọn hồ sơ giữ lại/ });

describe("Gộp hai khách", () => {
  it("hai hồ sơ cạnh nhau; chưa chọn hồ sơ giữ lại thì nút gộp mờ", async () => {
    renderAt(`/khach/${C1}/gop?voi=${C2}`);
    expect(await screen.findByRole("heading", { name: "Nguyễn Văn Tùng" })).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Trần Thị Thu Hà" })).toBeTruthy();
    expect(screen.getAllByText("0987 654 321", { selector: ".same" }).length).toBeGreaterThan(0);
    const radios = screen.getAllByRole("radio", { name: "Giữ hồ sơ này làm đại diện" });
    expect(radios).toHaveLength(2);
    expect(radios.every((r) => !(r as HTMLInputElement).checked)).toBe(true);
    expect((mergeButton() as HTMLButtonElement).disabled).toBe(true);
  });

  it("chọn giữ hồ sơ bên phải, bấm Gộp vào… chỉ mở hộp xác nhận; Quay lại không gửi; Gộp gửi đúng chiều rồi mở hồ sơ giữ lại", async () => {
    renderAt(`/khach/${C1}/gop?voi=${C2}`);
    await screen.findByRole("heading", { name: "Nguyễn Văn Tùng" });
    const radios = screen.getAllByRole("radio", { name: "Giữ hồ sơ này làm đại diện" });
    fireEvent.click(nth(radios, 1));
    fireEvent.click(screen.getByRole("button", { name: "Gộp vào KH001877…" }));
    let dlg = screen.getByRole("dialog", { name: "Gộp KH000231 vào KH001877?" });
    expect(within(dlg).getByText(/Hoàn tác được tới 19\/12\/2026/)).toBeTruthy();
    expect(fake.writes).toHaveLength(0);
    fireEvent.click(within(dlg).getByRole("button", { name: "Quay lại" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(fake.writes).toHaveLength(0);
    fake.replies.push({
      status: 200,
      body: {
        id: "m9",
        survivorId: C2,
        mergedId: C1,
        movedIds: [C1, C3],
        matchedBy: "phone",
        actor: { id: "u", name: "Lan" },
        createdAt: "2026-11-19T03:00:00Z",
      },
    });
    fireEvent.click(screen.getByRole("button", { name: "Gộp vào KH001877…" }));
    dlg = screen.getByRole("dialog", { name: "Gộp KH000231 vào KH001877?" });
    fireEvent.click(within(dlg).getByRole("button", { name: "Gộp" }));
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    expect(fake.writes[0]).toMatchObject({
      method: "POST",
      path: `/api/v1/customers/${C2}/merge`,
      body: { mergedId: C1, matchedBy: "phone" },
    });
    expect(fake.writes[0]?.key).not.toBeNull();
    expect(await screen.findByRole("heading", { name: "Nguyễn Văn Tùng", level: 1 })).toBeTruthy();
  });

  it("chưa có hồ sơ thứ hai: chọn từ gợi ý trùng SĐT hoặc tìm khách (gộp theo nhân viên chọn → matchedBy staff)", async () => {
    fake.list = [
      {
        id: C2,
        code: "KH001877",
        fullName: "Nguyễn Văn Tùng",
        phones: ["+84987654321"],
        stats: TUNG.stats,
      },
    ];
    renderAt(`/khach/${C1}/gop`);
    expect(await screen.findByRole("button", { name: /KH001877 · Nguyễn Văn Tùng/ })).toBeTruthy();
    fireEvent.change(screen.getByRole("searchbox", { name: "Tìm khách để gộp" }), { target: { value: "4321" } });
    const hit = await screen.findByRole("button", { name: /Chọn KH001877/ });
    fireEvent.click(hit);
    await screen.findByRole("heading", { name: "Nguyễn Văn Tùng" });
    fireEvent.click(nth(screen.getAllByRole("radio", { name: "Giữ hồ sơ này làm đại diện" }), 0));
    fireEvent.click(screen.getByRole("button", { name: "Gộp vào KH000231…" }));
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Gộp" }));
    await waitFor(() => {
      expect(fake.writes[0]).toMatchObject({
        path: `/api/v1/customers/${C1}/merge`,
        body: { mergedId: C2, matchedBy: "staff" },
      });
    });
  });

  it("không có quyền gộp: không có phần so sánh, không nút gộp; vẫn xem được các lần gộp", async () => {
    fake.grants = grantsOf();
    renderAt(`/khach/${C1}/gop?voi=${C2}`);
    expect(await screen.findByRole("heading", { name: "Các lần gộp" })).toBeTruthy();
    expect(screen.queryByRole("radio")).toBeNull();
    expect(screen.queryByRole("button", { name: /^Gộp vào/ })).toBeNull();
  });
});

/** Mở trang gộp của C1, chờ bảng lần gộp, bấm Hoàn tác ở dòng của lần gộp mergeId. */
async function undoRow(mergeId: string) {
  renderAt(`/khach/${C1}/gop`);
  await screen.findAllByRole("button", { name: "Hoàn tác" });
  const row = document.getElementById(`gop-${mergeId}`);
  if (row === null) throw new Error(`không thấy dòng ${mergeId}`);
  fireEvent.click(within(row).getByRole("button", { name: "Hoàn tác" }));
}

describe("Các lần gộp", () => {
  beforeEach(() => {
    fake.timeline = [
      {
        at: "2026-11-18T08:40:00Z",
        kind: "merge",
        refId: "m2",
        actor: { id: "u", name: "Nguyễn Thị Lan" },
        summary: { survivor_id: C2, merged_id: C1, matched_by: "staff" },
      },
      {
        at: "2026-11-01T08:00:00Z",
        kind: "merge",
        refId: "m1",
        actor: { id: "u", name: "Nguyễn Thị Lan" },
        summary: { survivor_id: C1, merged_id: C3, matched_by: "phone" },
      },
      {
        at: "2026-10-02T02:30:00Z",
        kind: "merge",
        refId: "m0",
        actor: { id: "u", name: "Lê Văn Đức" },
        summary: { survivor_id: C1, merged_id: "x", matched_by: "staff" },
      },
    ];
  });

  it("dòng quá 30 ngày ghi lý do, không có nút; dòng còn hạn có nút Hoàn tác", async () => {
    renderAt(`/khach/${C1}/gop`);
    const h = await screen.findByRole("heading", { name: "Các lần gộp" });
    await screen.findAllByRole("button", { name: "Hoàn tác" });
    const tbl = h.parentElement?.querySelector("table");
    if (!(tbl instanceof HTMLElement)) throw new Error("không thấy bảng");
    const rows = within(tbl).getAllByRole("row").slice(1);
    expect(rows).toHaveLength(3);
    expect(within(nth(rows, 0)).getByRole("button", { name: "Hoàn tác" })).toBeTruthy();
    expect(within(nth(rows, 2)).getByText("Quá 30 ngày, không hoàn tác được.")).toBeTruthy();
    expect(within(nth(rows, 2)).queryByRole("button")).toBeNull();
  });

  it("hoàn tác bị merge_not_latest: câu báo kèm liên kết tới lần gộp phải hoàn tác trước", async () => {
    fake.replies.push({ status: 409, body: { error: "merge_not_latest", request_id: "r", blockingMergeId: "m2" } });
    await undoRow("m1");
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toMatch(/Chưa hoàn tác được\./);
    expect(alert.textContent).toMatch(/Hoàn tác lần gộp 15:40 · 18\/11 trước/);
    expect(within(alert).getByRole("link", { name: "Mở lần gộp đó" }).getAttribute("href")).toBe("#gop-m2");
    expect(fake.writes.at(-1)).toMatchObject({ method: "POST", path: "/api/v1/customer-merges/m1/undo" });
  });

  it("hoàn tác bị merge_window_expired: câu báo quá 30 ngày", async () => {
    fake.replies.push({ status: 422, body: { error: "merge_window_expired", request_id: "r" } });
    await undoRow("m2");
    expect((await screen.findByRole("alert")).textContent).toMatch(/Đã quá 30 ngày, không hoàn tác gộp được/);
  });

  it("hoàn tác xong: thông báo, đọc lại các lần gộp", async () => {
    fake.replies.push({
      status: 200,
      body: {
        id: "m2",
        survivorId: C2,
        mergedId: C1,
        movedIds: [C1],
        matchedBy: "staff",
        actor: { id: "u", name: "Lan" },
        createdAt: "2026-11-18T08:40:00Z",
        undoneAt: "2026-11-19T03:00:00Z",
      },
    });
    await undoRow("m2");
    expect(await screen.findByRole("status")).toBeTruthy();
    expect(screen.getByRole("status").textContent).toMatch(/Đã hoàn tác lần gộp 15:40 · 18\/11/);
  });
});
