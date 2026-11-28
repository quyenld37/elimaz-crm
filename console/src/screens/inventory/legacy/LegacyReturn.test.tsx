// @vitest-environment jsdom
// Màn Hàng hoàn đơn cũ (Task 273; mockup docs/mockups/m7/hang-hoan-don-cu.html, trạng thái A–C).
import { act, cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { LegacyReturn } from "../../../api";
import { renderScreens } from "../../../app/screenTestkit";
import { screens } from "../route";
import { V_DAM, V_QU, W1, grantsAll, newFake, stub, userEvent, type Fake } from "../opening/openingTestkit";

const PERMS = ["inventory.read", "catalog.read", "inventory.legacy_return.create"];
const RET = "0195f3b2-0000-7000-8000-0000000a0001";
const PN = "0195f3b2-0000-7000-8000-0000000d0431";
const PL = "0195f3b2-0000-7000-8000-0000000d0021";
let fake: Fake;

beforeEach(() => {
  fake = stub(newFake(grantsAll(...PERMS)));
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const created = (over: Partial<LegacyReturn> = {}): LegacyReturn => ({
  id: RET,
  warehouseId: W1,
  reference: "VTP12345678",
  stockDocumentId: PN,
  damageDocumentId: PL,
  lines: [
    { variantId: V_DAM, qty: 2, condition: "sellable" },
    { variantId: V_QU, qty: 1, condition: "damaged" },
  ],
  createdBy: { id: "u", name: "Hùng" },
  createdAt: "2026-12-02T03:14:00Z",
  ...over,
});

/** Quét một mã và chờ tra mã xong (fetch giả trả ngay trong vài vi tác vụ). */
async function scan(code: string) {
  await userEvent.type(await screen.findByLabelText("Quét hoặc gõ SKU để thêm món"), `${code}{Enter}`);
  await act(async () => {
    await new Promise((r) => setTimeout(r, 15));
  });
}

describe("A: nhập một kiện", () => {
  it("mã tham chiếu 'vtp 1234 5678' hiện bản chuẩn hóa VTP12345678", async () => {
    renderScreens(screens, "/kho/hang-hoan-cu");
    const input = await screen.findByLabelText("Mã tham chiếu");
    await userEvent.type(input, "vtp 1234 5678");
    expect(screen.getByText(/VTP12345678/)).toBeTruthy();
  });

  it("quét thêm món, quét lại cộng 1, đổi sang Hỏng rồi quét lại có dòng Bán lại được mới", async () => {
    renderScreens(screens, "/kho/hang-hoan-cu");
    await scan("DAM0123-DEN-S");
    await scan("DAM0123-DEN-S");
    expect(await screen.findByLabelText("Số lượng DAM0123-DEN-S")).toHaveProperty("value", "2");
    const group = screen.getByRole("radiogroup", { name: "Tình trạng DAM0123-DEN-S" });
    await userEvent.click(within(group).getByRole("radio", { name: "Hỏng" }));
    await scan("DAM0123-DEN-S");
    expect(screen.getAllByLabelText("Số lượng DAM0123-DEN-S")).toHaveLength(2);
  });

  it("nhập kho: gửi kho, mã chuẩn hóa, dòng; hiện kết quả với liên kết hai phiếu", async () => {
    fake.replies.push({ status: 201, body: created() });
    renderScreens(screens, "/kho/hang-hoan-cu");
    await userEvent.type(await screen.findByLabelText("Mã tham chiếu"), "vtp 1234 5678");
    await scan("DAM0123-DEN-S");
    await scan("QU0789-DEN-M");
    const g = screen.getByRole("radiogroup", { name: "Tình trạng QU0789-DEN-M" });
    await userEvent.click(within(g).getByRole("radio", { name: "Hỏng" }));
    await userEvent.type(screen.getByLabelText("Ghi chú (không bắt buộc)"), "khách trả vì sai size");
    await userEvent.click(screen.getByRole("button", { name: "Nhập kho" }));
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    expect(fake.writes[0]?.path).toBe("/api/v1/inventory/legacy-returns");
    expect(fake.writes[0]?.key).toBeTruthy();
    expect(fake.writes[0]?.body).toEqual({
      warehouseId: W1,
      reference: "VTP12345678",
      note: "khách trả vì sai size",
      lines: [
        { variantId: V_DAM, qty: 1, condition: "sellable" },
        { variantId: V_QU, qty: 1, condition: "damaged" },
      ],
    });
    expect(await screen.findByText(/Đã nhập kiện VTP12345678 vào Kho chính/)).toBeTruthy();
    expect(screen.getByRole("link", { name: /Xem phiếu nhập/ }).getAttribute("href")).toBe(`/kho/phieu/${PN}`);
    expect(screen.getByRole("link", { name: /Xem phiếu chuyển hàng lỗi/ }).getAttribute("href")).toBe(
      `/kho/phieu/${PL}`,
    );
  });

  it("chưa có món hoặc mã tham chiếu → nút Nhập kho khóa kèm lý do", async () => {
    renderScreens(screens, "/kho/hang-hoan-cu");
    const btn = await screen.findByRole("button", { name: "Nhập kho" });
    expect((btn as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByText("Thêm ít nhất một món và mã tham chiếu thì nút mở.")).toBeTruthy();
  });

  it("mã tham chiếu ngắn và số lượng 0 báo ở ô, nút khóa", async () => {
    renderScreens(screens, "/kho/hang-hoan-cu");
    await userEvent.type(await screen.findByLabelText("Mã tham chiếu"), "V1");
    expect(await screen.findByText("Mã tham chiếu từ 3 đến 64 ký tự.")).toBeTruthy();
    await scan("DAM0123-DEN-S");
    fireEvent.change(await screen.findByLabelText("Số lượng DAM0123-DEN-S"), { target: { value: "0" } });
    expect(await screen.findByText(/Số lượng từ 1 đến 1\.000/)).toBeTruthy();
    expect(screen.getByRole("button", { name: "Nhập kho" })).toHaveProperty("disabled", true);
  });

  it("mã lạ → hộp báo nêu mã", async () => {
    renderScreens(screens, "/kho/hang-hoan-cu");
    await scan("2000000009999");
    expect(await screen.findByText("Không thấy mã 2000000009999.")).toBeTruthy();
  });

  it("đang gửi: nút khóa, không gửi hai lần khi bấm lại", async () => {
    let release: () => void = () => undefined;
    fake.hold = new Promise<void>((r) => {
      release = r;
    });
    fake.replies.push({ status: 201, body: created() });
    renderScreens(screens, "/kho/hang-hoan-cu");
    await userEvent.type(await screen.findByLabelText("Mã tham chiếu"), "VTP12345678");
    await scan("DAM0123-DEN-S");
    const btn = screen.getByRole("button", { name: "Nhập kho" });
    await userEvent.click(btn);
    await userEvent.click(btn);
    expect(await screen.findByText("Đang nhập…")).toBeTruthy();
    release();
    await screen.findByText(/Đã nhập kiện/);
    expect(fake.writes).toHaveLength(1);
  });

  it("409 legacy_return_exists → hiện kiện đã nhập rồi kèm giờ nhập và liên kết tới lần nhập cũ", async () => {
    fake.legacyById.set(RET, created());
    fake.replies.push({ status: 409, body: { error: "legacy_return_exists", request_id: "t", existingId: RET } });
    renderScreens(screens, "/kho/hang-hoan-cu");
    await userEvent.type(await screen.findByLabelText("Mã tham chiếu"), "VTP12345678");
    await scan("DAM0123-DEN-S");
    await userEvent.click(screen.getByRole("button", { name: "Nhập kho" }));
    expect(await screen.findByText("Kiện VTP12345678 đã nhập rồi.")).toBeTruthy();
    await waitFor(() => {
      expect(screen.getByText(/Hùng/)).toBeTruthy();
    });
    const link = screen.getByRole("link", { name: "Xem lần nhập cũ" });
    expect(link.getAttribute("href")).toContain("#kien-");
  });
});

describe("C: danh sách theo ngày", () => {
  it("nhóm theo ngày, tổng kiện, cái, hỏng; liên kết phiếu nhập; mặc định 7 ngày gần nhất", async () => {
    fake.legacy = [
      created(),
      created({
        id: "0195f3b2-0000-7000-8000-0000000a0002",
        reference: "GHN-LK8XQ29T",
        stockDocumentId: "0195f3b2-0000-7000-8000-0000000d0430",
        damageDocumentId: undefined,
        lines: [{ variantId: V_DAM, qty: 4, condition: "sellable" }],
        createdAt: "2026-12-02T03:02:00Z",
      }),
      created({
        id: "0195f3b2-0000-7000-8000-0000000a0003",
        reference: "SPXVN0412345678",
        damageDocumentId: undefined,
        lines: [{ variantId: V_DAM, qty: 3, condition: "sellable" }],
        createdAt: "2026-12-03T09:40:00Z",
      }),
    ];
    renderScreens(screens, "/kho/hang-hoan-cu");
    expect(await screen.findByText("3 kiện · 10 cái (1 hỏng) trong 2 ngày")).toBeTruthy();
    expect(screen.getAllByRole("heading", { level: 3 }).map((h) => h.textContent)).toEqual([
      expect.stringContaining("03/12"),
      expect.stringContaining("02/12"),
    ]);
    expect(screen.getByText("VTP12345678")).toBeTruthy();
    const q = fake.attempts.length;
    expect(q).toBe(0);
  });

  it("không có kiện nào → câu rỗng nêu khoảng ngày", async () => {
    renderScreens(screens, "/kho/hang-hoan-cu");
    expect(await screen.findByText(/Chưa nhập kiện nào từ/)).toBeTruthy();
  });

  it("lỗi tải → hộp lỗi có nút Thử lại", async () => {
    fake.failLegacyList = true;
    renderScreens(screens, "/kho/hang-hoan-cu");
    expect(await screen.findByText("Chưa tải được danh sách.")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Thử lại" })).toBeTruthy();
  });
});

it("route: mục menu Hàng hoàn đơn cũ ở nhóm Kho, quyền inventory.legacy_return.create", () => {
  const r = screens.find((s) => s.path === "/kho/hang-hoan-cu");
  expect(r).toMatchObject({ label: "Hàng hoàn đơn cũ", group: "Kho", permission: "inventory.legacy_return.create" });
  expect(r?.hidden).not.toBe(true);
});
