// @vitest-environment jsdom
// Màn Kiểm kho mở sổ (Task 273; mockup docs/mockups/m7/kiem-kho-mo-so.html, trạng thái A–G).
import { act, cleanup, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderScreens } from "../../../app/screenTestkit";
import { screens } from "../route";
import { OPENING, ST, W1, W2, grantsAll, newFake, stub, stubDialog, userEvent, type Fake } from "./openingTestkit";

const PERMS = ["inventory.read", "catalog.read", "inventory.stocktake", "inventory.stocktake.opening"];
let fake: Fake;

beforeEach(() => {
  stubDialog();
  localStorage.clear();
  fake = stub(newFake(grantsAll(...PERMS)));
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  localStorage.clear();
});

const err = (error: string, status: number, extra: Record<string, unknown> = {}) => ({
  status,
  body: { error, request_id: "t", ...extra },
});

describe("A: tạo phiên mở sổ", () => {
  it("ô Kho chỉ có kho được quyền mở sổ theo phạm vi; kho không được quyền không có", async () => {
    fake.grants = [
      ...grantsAll("inventory.read", "catalog.read", "inventory.stocktake"),
      { permission: "inventory.stocktake.opening", scope: { type: "warehouse", id: W2 } },
    ];
    renderScreens(screens, "/kho/mo-so");
    const select = await screen.findByRole("combobox", { name: "Kho" });
    expect(
      within(select)
        .getAllByRole("option")
        .map((o) => o.textContent),
    ).toEqual(["Kho phụ Bình Thạnh"]);
  });

  it("tạo phiên: gửi warehouseId kèm khóa chống trùng, báo số mã phải đếm, nút Vào đếm", async () => {
    fake.replies.push({ status: 201, body: OPENING });
    renderScreens(screens, "/kho/mo-so");
    await userEvent.click(await screen.findByRole("button", { name: "Bắt đầu đếm Kho chính" }));
    expect(await screen.findByText(/412 mã/)).toBeTruthy();
    expect(fake.writes).toHaveLength(1);
    expect(fake.writes[0]?.path).toBe("/api/v1/inventory/opening-stocktakes");
    expect(fake.writes[0]?.body).toEqual({ warehouseId: W1 });
    expect(fake.writes[0]?.key).toBeTruthy();
    expect(screen.getByRole("link", { name: "Vào đếm" }).getAttribute("href")).toBe(`/kho/mo-so/${ST}`);
  });

  it("409 warehouse_not_empty → câu báo và đường sang Kiểm kho thường", async () => {
    fake.replies.push(err("warehouse_not_empty", 409));
    renderScreens(screens, "/kho/mo-so");
    await userEvent.click(await screen.findByRole("button", { name: "Bắt đầu đếm Kho chính" }));
    expect(await screen.findByText("Kho đã có hàng hoặc phiếu kho, không mở sổ được")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Sang Kiểm kho" }).getAttribute("href")).toBe("/kho/kiem-kho");
  });

  it("409 opening_exists → tra phiên đang đếm của kho và có nút Vào phiên", async () => {
    fake.openingList = [OPENING];
    fake.replies.push(err("opening_exists", 409));
    renderScreens(screens, "/kho/mo-so");
    await userEvent.click(await screen.findByRole("button", { name: "Bắt đầu đếm Kho chính" }));
    const link = await screen.findByRole("link", { name: /Vào phiên/ });
    expect(link.getAttribute("href")).toBe(`/kho/mo-so/${ST}`);
  });

  it("409 stocktake_open → câu báo và đường sang Kiểm kho", async () => {
    fake.replies.push(err("stocktake_open", 409));
    renderScreens(screens, "/kho/mo-so");
    await userEvent.click(await screen.findByRole("button", { name: "Bắt đầu đếm Kho chính" }));
    expect(await screen.findByText("Kho đang có phiên kiểm kho chưa xong")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Sang Kiểm kho" })).toBeTruthy();
  });

  it("không được mở sổ kho nào → câu báo, không có nút bắt đầu", async () => {
    fake.grants = grantsAll("inventory.read", "catalog.read", "inventory.stocktake", "inventory.stocktake.opening").map(
      (g) => ({ ...g, scope: { type: "warehouse", id: "0195f3b2-0000-7000-8000-00000000a999" } }),
    );
    renderScreens(screens, "/kho/mo-so");
    expect(await screen.findByText("Bạn chưa được mở sổ kho nào")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Bắt đầu đếm/ })).toBeNull();
  });
});

async function scan(code: string) {
  const input = await screen.findByLabelText("Quét barcode hoặc gõ mã");
  await userEvent.type(input, `${code}{Enter}`);
}

describe("B: đếm bằng quét mã", () => {
  it("tiến độ lấy từ máy chủ: 405 / 412 mã, còn 7 mã chưa đếm", async () => {
    renderScreens(screens, `/kho/mo-so/${ST}`);
    expect(await screen.findByText("405 / 412 mã")).toBeTruthy();
    expect(screen.getAllByText(/Còn 7 mã chưa đếm/).length).toBeGreaterThan(0);
  });

  it("quét 3 lần mã A → 3 yêu cầu +1 với 3 khóa khác nhau; số to là số máy chủ trả", async () => {
    renderScreens(screens, `/kho/mo-so/${ST}`);
    await scan("DAM0123-DEN-S");
    await scan("DAM0123-DEN-S");
    await scan("DAM0123-DEN-S");
    await waitFor(() => {
      expect(fake.writes).toHaveLength(3);
    });
    expect(fake.writes.map((w) => w.body)).toEqual(Array(3).fill({ code: "DAM0123-DEN-S", by: 1 }));
    expect(new Set(fake.writes.map((w) => w.key)).size).toBe(3);
    expect((await screen.findAllByText("Đầm suông công sở")).length).toBeGreaterThan(0);
    await waitFor(() => {
      expect(document.querySelector(".count-big")?.textContent).toBe("3");
    });
  });

  it("nút −1 gửi by −1; nút +1 gửi by +1 (cùng mã vừa quét)", async () => {
    renderScreens(screens, `/kho/mo-so/${ST}`);
    await scan("2000000000015");
    await scan("2000000000015");
    await userEvent.click(await screen.findByRole("button", { name: /Bớt 1 DAM0123-DEN-S/ }));
    await waitFor(() => {
      expect(fake.writes).toHaveLength(3);
    });
    expect(fake.writes[2]?.body).toEqual({ code: "2000000000015", by: -1 });
    await userEvent.click(screen.getByRole("button", { name: /Thêm 1 DAM0123-DEN-S/ }));
    await waitFor(() => {
      expect(fake.writes).toHaveLength(4);
    });
    expect(fake.writes[3]?.body.by).toBe(1);
  });

  it("mất mạng giữa lần 2: báo chờ gửi; có mạng lại gửi lại ĐÚNG khóa cũ; máy chủ đếm 3", async () => {
    renderScreens(screens, `/kho/mo-so/${ST}`);
    await scan("DAM0123-DEN-S");
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    fake.dropNext = 2; // lần quét 2 rớt, và lần gửi lại khi quét tiếp lần 3 cũng rớt
    await scan("DAM0123-DEN-S");
    expect(await screen.findByText(/Mất mạng · 1 lần quét đang chờ gửi/)).toBeTruthy();
    await scan("DAM0123-DEN-S");
    expect(await screen.findByText(/Mất mạng · 2 lần quét đang chờ gửi/)).toBeTruthy();
    act(() => {
      window.dispatchEvent(new Event("online"));
    });
    await waitFor(() => {
      expect(fake.counts.get("0195f3b2-0000-7000-8000-00000000b015")).toBe(3);
    });
    const keys = fake.attempts.map((a) => a.key);
    // k1 (đã lưu), k2 rớt, k2 rớt lần gửi lại, rồi có mạng: k2 (cùng khóa) và k3.
    expect(keys).toHaveLength(5);
    expect(keys[1]).toBe(keys[2]);
    expect(keys[3]).toBe(keys[1]);
    expect(new Set(keys).size).toBe(3);
    await waitFor(() => {
      expect(screen.queryByText(/Mất mạng/)).toBeNull();
    });
  });

  it("mã lạ (422 unknown_code) → hộp báo nêu mã, không cộng gì", async () => {
    renderScreens(screens, `/kho/mo-so/${ST}`);
    await scan("2000000009999");
    expect(await screen.findByText("Không thấy mã 2000000009999.")).toBeTruthy();
    expect(fake.counts.size).toBe(0);
  });

  it("mã trùng hai người bán → chọn ứng viên, gửi lại với SKU của ứng viên bằng khóa mới", async () => {
    fake.replies.push(
      err("ambiguous_code", 422, {
        candidates: [
          { variantId: "v1", sellerId: "0195f3b2-0000-7000-8000-00000000e001", sku: "DAM0123-DEN-S" },
          { variantId: "v2", sellerId: "0195f3b2-0000-7000-8000-00000000e002", sku: "QU0789-DEN-M" },
        ],
      }),
    );
    renderScreens(screens, `/kho/mo-so/${ST}`);
    await scan("8930000000017");
    expect(await screen.findByText(/có ở 2 người bán/)).toBeTruthy();
    const buttons = screen.getAllByRole("button", { name: /^Cộng 1/ });
    expect(buttons).toHaveLength(2);
    const [, second] = buttons;
    if (second === undefined) throw new Error("thiếu nút ứng viên thứ hai");
    await userEvent.click(second);
    await waitFor(() => {
      expect(fake.writes).toHaveLength(2);
    });
    expect(fake.writes[1]?.body).toEqual({ code: "QU0789-DEN-M", by: 1 });
    expect(fake.writes[1]?.key).not.toBe(fake.writes[0]?.key);
  });

  it("bớt dưới 0 (422 negative_count) → câu báo số đếm không âm", async () => {
    renderScreens(screens, `/kho/mo-so/${ST}`);
    await scan("DAM0123-DEN-S");
    await userEvent.click(await screen.findByRole("button", { name: /Bớt 1 DAM0123-DEN-S/ }));
    await userEvent.click(await screen.findByRole("button", { name: /Bớt 1 DAM0123-DEN-S/ }));
    expect(await screen.findByText("Số đếm không được âm")).toBeTruthy();
  });
});

describe("C, E: nút Đăng và xác nhận", () => {
  it("còn 7 mã chưa đếm → nút Đăng khóa kèm lý do", async () => {
    renderScreens(screens, `/kho/mo-so/${ST}`);
    const btn = await screen.findByRole("button", { name: /Đăng mở sổ…/ });
    expect((btn as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByText("Còn 7 mã chưa đếm. Đếm hoặc nhập 0 cho cả 7 mã thì nút mở.")).toBeTruthy();
  });

  it("đủ mã → hộp xác nhận nêu 412 mã và 8.214 cái; Đăng gửi một yêu cầu có khóa", async () => {
    fake.opening = { ...OPENING, linesCounted: 412 };
    fake.replies.push({
      status: 200,
      body: { stocktakeId: ST, linesExpected: 412, countedLines: 412, units: 8214 },
    });
    renderScreens(screens, `/kho/mo-so/${ST}`);
    await userEvent.click(await screen.findByRole("button", { name: /Đăng mở sổ…/ }));
    const dlg = await screen.findByRole("dialog");
    expect(within(dlg).getByText(/412 mã/)).toBeTruthy();
    expect(within(dlg).getByText(/8\.214 cái/)).toBeTruthy();
    fake.opening = { ...fake.opening, status: "applied", appliedAt: "2026-11-30T14:04:00Z" };
    await userEvent.click(within(dlg).getByRole("button", { name: "Đăng" }));
    await waitFor(() => {
      expect(fake.writes.filter((w) => w.path.endsWith("/post"))).toHaveLength(1);
    });
    expect(fake.writes[0]?.key).toBeTruthy();
    expect(await screen.findByText(/Đã mở sổ/)).toBeTruthy();
  });

  it("máy chủ trả 422 uncounted_lines (count 2) → Chưa đăng: còn 2 mã chưa đếm", async () => {
    fake.opening = { ...OPENING, linesCounted: 412 };
    fake.replies.push(err("uncounted_lines", 422, { count: 2 }));
    renderScreens(screens, `/kho/mo-so/${ST}`);
    await userEvent.click(await screen.findByRole("button", { name: /Đăng mở sổ…/ }));
    await userEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Đăng" }));
    expect(await screen.findByText("Chưa đăng: còn 2 mã chưa đếm.")).toBeTruthy();
  });

  it("409 stocktake_not_open (người khác đã đăng) → báo, tải lại phiên, hiện kết quả", async () => {
    fake.opening = { ...OPENING, linesCounted: 412 };
    fake.replies.push(err("stocktake_not_open", 409));
    renderScreens(screens, `/kho/mo-so/${ST}`);
    await userEvent.click(await screen.findByRole("button", { name: /Đăng mở sổ…/ }));
    fake.opening = { ...fake.opening, status: "applied", appliedAt: "2026-11-30T14:04:00Z" };
    await userEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Đăng" }));
    expect(await screen.findByText(/Đã mở sổ/)).toBeTruthy();
  });

  it("409 warehouse_not_empty lúc đăng → câu báo và nút Hủy phiên…", async () => {
    fake.opening = { ...OPENING, linesCounted: 412 };
    fake.replies.push(err("warehouse_not_empty", 409));
    renderScreens(screens, `/kho/mo-so/${ST}`);
    await userEvent.click(await screen.findByRole("button", { name: /Đăng mở sổ…/ }));
    await userEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Đăng" }));
    expect(await screen.findByText(/Không đăng được/)).toBeTruthy();
    expect(screen.getByRole("button", { name: /Hủy phiên/ })).toBeTruthy();
  });
});

describe("Nhập 0 cho mã chưa có hàng", () => {
  it("tra mã rồi ghi số đếm 0 bằng recordStocktakeCount của đúng biến thể", async () => {
    renderScreens(screens, `/kho/mo-so/${ST}`);
    const input = await screen.findByLabelText("Mã không có hàng trên kệ");
    await userEvent.type(input, "QU0789-DEN-M");
    fake.replies.push({
      status: 200,
      body: { variantId: "0195f3b2-0000-7000-8000-00000000b213", counted: 0 },
    });
    await userEvent.click(screen.getByRole("button", { name: "Nhập 0" }));
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    expect(fake.writes[0]?.path).toBe(`/api/v1/inventory/stocktakes/${ST}/lines/0195f3b2-0000-7000-8000-00000000b213`);
    expect(fake.writes[0]?.body).toEqual({ counted: 0 });
  });
});

describe("F: đã đăng và kiểm lại", () => {
  beforeEach(() => {
    fake.opening = { ...OPENING, linesCounted: 412, status: "applied", appliedAt: "2026-11-30T14:04:00Z" };
  });

  it("kiểm khớp → chip Khớp; đường sang Phiếu kho để sửa bằng phiếu điều chỉnh", async () => {
    renderScreens(screens, `/kho/mo-so/${ST}`);
    await userEvent.click(await screen.findByRole("button", { name: "Kiểm lại" }));
    expect(await screen.findByText("Khớp")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Sang Phiếu kho" }).getAttribute("href")).toBe("/kho/phieu");
  });

  it("kiểm lệch 2 mã → bảng mã, đếm, sổ, tồn hiện tại", async () => {
    fake.check = {
      ok: false,
      changedSince: false,
      mismatches: [
        { variantId: "a", sku: "QU0789-DEN-M", counted: 5, ledgerOpening: 5, onHand: 3 },
        { variantId: "b", sku: "SET0012-XAM-M", counted: 11, ledgerOpening: 10, onHand: 10 },
      ],
    };
    renderScreens(screens, `/kho/mo-so/${ST}`);
    await userEvent.click(await screen.findByRole("button", { name: "Kiểm lại" }));
    expect(await screen.findByText("Lệch 2 mã")).toBeTruthy();
    const row = screen.getByText("SET0012-XAM-M").closest("tr");
    expect(row?.textContent).toContain("11");
    expect(row?.textContent).toContain("10");
  });

  it("kho đã có phiếu khác sau giờ mở bán (changedSince) → chip Sổ khớp, nói rõ chỉ so sổ", async () => {
    fake.check = { ok: true, changedSince: true, mismatches: [] };
    renderScreens(screens, `/kho/mo-so/${ST}`);
    await userEvent.click(await screen.findByRole("button", { name: "Kiểm lại" }));
    expect(await screen.findByText("Sổ khớp")).toBeTruthy();
    expect(screen.getByText(/không so tồn hiện tại/)).toBeTruthy();
  });
});

describe("G: quyền và lỗi tải", () => {
  it("403 khi mở phiên → câu lý do; lỗi tải có nút Thử lại", async () => {
    vi.unstubAllGlobals();
    vi.stubGlobal("fetch", (req: Request) => {
      const path = new URL(req.url).pathname;
      if (path === "/api/v1/me")
        return Promise.resolve(Response.json({ userId: "u", email: "a@b.c", grants: fake.grants }));
      if (path === "/api/v1/inventory/warehouses") return Promise.resolve(Response.json([]));
      return Promise.resolve(Response.json({ error: "forbidden", request_id: "t" }, { status: 403 }));
    });
    renderScreens(screens, `/kho/mo-so/${ST}`);
    expect(await screen.findByText("Bạn chưa được cấp quyền làm việc này")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Thử lại" })).toBeTruthy();
  });
});

it("route đã đăng ký (ẩn khỏi menu, HV01)", () => {
  const r = screens.find((s) => s.path === "/kho/mo-so");
  expect(r?.hidden).toBe(true);
  expect(r?.permission).toBe("inventory.stocktake.opening");
});
