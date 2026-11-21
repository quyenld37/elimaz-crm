// @vitest-environment jsdom
// Màn Đóng gói (kế hoạch M3 Task 136, ca VC04; mockup docs/mockups/m3/kho.html trạng thái A, B, E; HV23, HV24): chọn 5
// vận đơn hai hãng + tự giao → hai lô nhãn và câu "1 vận đơn tự giao không có nhãn hãng"; in lại không đổi cột
// "Đóng gói"; đóng gói chỉ đi tới ("đã ở bước sau"); lô lỗi thử lại đúng vận đơn của tài khoản đó.
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { LabelBatchCreated, PackingResultList } from "../../api";
import {
  ACC_BEST,
  ACC_VTP,
  WH,
  WH2,
  fiveRows,
  grantsOf,
  installFake,
  orderId,
  renderAt,
  shipId,
  stubDialog,
  type Fake,
} from "./warehouseTestkit";

let fake: Fake;
beforeEach(() => {
  stubDialog();
  fake = installFake();
  fake.rows = fiveRows();
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const B1 = "0195f3b2-0000-7000-8000-00000000bb01";
const B2 = "0195f3b2-0000-7000-8000-00000000bb02";
const B3 = "0195f3b2-0000-7000-8000-00000000bb03";

async function selectAll() {
  renderAt("/warehouse/pack");
  await screen.findByRole("row", { name: /DH0001240/ });
  for (const code of ["DH0001240", "DH0001241", "DH0001242", "DH0001243", "DH0001225"]) {
    fireEvent.click(screen.getByRole("checkbox", { name: `Chọn ${code}` }));
  }
  return screen.getByRole("toolbar", { name: "Thao tác với vận đơn đã chọn" });
}

/** Chọn 5 dòng, bấm In nhãn (máy chủ trả body nếu có), chờ hộp kết quả. */
async function openLabels(body?: LabelBatchCreated) {
  if (body !== undefined) fake.replies.push({ status: 202, body });
  const bar = await selectAll();
  fireEvent.click(within(bar).getByRole("button", { name: "In nhãn" }));
  return screen.findByRole("dialog", { name: "In nhãn · 5 vận đơn" });
}

const created: LabelBatchCreated = {
  batches: [
    { id: B1, carrierAccountId: ACC_VTP, count: 3 },
    { id: B2, carrierAccountId: ACC_BEST, count: 1 },
  ],
  skipped: [{ shipmentId: shipId(25), reason: "no_carrier_label" }],
};

describe("Đóng gói — danh sách", () => {
  it("đọc vận đơn chờ thu gom của kho đầu tiên; cột Nhãn, Đóng gói, Thu hộ theo dữ liệu", async () => {
    renderAt("/warehouse/pack");
    const r40 = await screen.findByRole("row", { name: /DH0001240/ });
    expect(fake.listCalls[0]?.getAll("shipmentStatus")).toEqual(["awaiting_pickup"]);
    expect(fake.listCalls[0]?.get("warehouseId")).toBe(WH);
    expect(within(r40).getByText("Viettel Post · 1234567890140")).toBeTruthy();
    expect(within(r40).getByText(/^Đã in /)).toBeTruthy();
    expect(within(r40).getByText("1.960.000 ₫")).toBeTruthy();
    const r42 = screen.getByRole("row", { name: /DH0001242/ });
    expect(within(r42).getByText("Chưa in")).toBeTruthy();
    expect(within(r42).getByText("Đang đóng gói")).toBeTruthy();
    expect(within(r42).getByText("112.450.000 ₫")).toBeTruthy();
    expect(within(screen.getByRole("row", { name: /DH0001243/ })).getByText("0 ₫")).toBeTruthy();
    const self = screen.getByRole("row", { name: /DH0001225/ });
    expect(within(self).getByText("Không có nhãn hãng")).toBeTruthy();
    expect(within(self).getByText("Shop tự giao · E261121R2W8KN")).toBeTruthy();
  });

  it("tài khoản nhập tay: cột Nhãn hiện 'Không có nhãn hãng'", async () => {
    fake.rows = [fiveRows()[1] ?? fiveRows()[0]].flatMap((r) =>
      r === undefined ? [] : [{ ...r, shipment: r.shipment && { ...r.shipment, mode: "manual" as const } }],
    );
    renderAt("/warehouse/pack");
    const row = await screen.findByRole("row", { name: /DH0001241/ });
    expect(within(row).getByText("Không có nhãn hãng")).toBeTruthy();
  });

  it("bộ lọc: Chưa in nhãn gửi labelPrinted=false; Đã đóng gói gửi packingStatus=packed; đổi kho gửi warehouseId", async () => {
    renderAt("/warehouse/pack");
    await screen.findByRole("row", { name: /DH0001240/ });
    fireEvent.click(screen.getByRole("button", { name: "Chưa in nhãn" }));
    await waitFor(() => {
      expect(fake.listCalls.at(-1)?.get("labelPrinted")).toBe("false");
    });
    fireEvent.click(screen.getByRole("button", { name: "Đã đóng gói" }));
    await waitFor(() => {
      expect(fake.listCalls.at(-1)?.get("packingStatus")).toBe("packed");
    });
    expect(fake.listCalls.at(-1)?.get("labelPrinted")).toBeNull();
    fireEvent.change(screen.getByRole("combobox", { name: "Kho" }), { target: { value: WH2 } });
    await waitFor(() => {
      expect(fake.listCalls.at(-1)?.get("warehouseId")).toBe(WH2);
    });
  });

  it("0 dòng: 'Không có đơn chờ đóng gói'", async () => {
    fake.rows = [];
    renderAt("/warehouse/pack");
    expect(await screen.findByText("Không có đơn chờ đóng gói")).toBeTruthy();
  });

  it("còn trang sau: không coi là hết, có 'Tải thêm'", async () => {
    fake.nextCursor = "c2";
    renderAt("/warehouse/pack");
    expect(await screen.findByRole("button", { name: "Tải thêm" })).toBeTruthy();
    expect(screen.getByText("Đang hiện 5 vận đơn, còn nữa")).toBeTruthy();
  });

  it("chọn 5 dòng: thanh chọn nhiều kể số theo hãng; thiếu quyền in thì không có In nhãn, In phiếu nhặt", async () => {
    const bar = await selectAll();
    expect(within(bar).getByText("Đã chọn 5 vận đơn · Viettel Post 3, Best Express 1, tự giao 1")).toBeTruthy();
    for (const b of ["In nhãn", "In phiếu nhặt", "Đang đóng gói", "Đã đóng gói"]) {
      expect(within(bar).getByRole("button", { name: b })).toBeTruthy();
    }
    cleanup();
    fake.grants = grantsOf("fulfillment.pack");
    const bar2 = await selectAll();
    expect(within(bar2).queryByRole("button", { name: "In nhãn" })).toBeNull();
    expect(within(bar2).queryByRole("button", { name: "In phiếu nhặt" })).toBeNull();
    expect(within(bar2).getByRole("button", { name: "Đã đóng gói" })).toBeTruthy();
  });

  it("In phiếu nhặt mở trang in ở tab mới với mã các vận đơn đã chọn", async () => {
    const open = vi.fn();
    vi.stubGlobal("open", open);
    const bar = await selectAll();
    fireEvent.click(within(bar).getByRole("button", { name: "In phiếu nhặt" }));
    const url = new URL(String(open.mock.calls[0]?.[0]), "http://x");
    expect(url.pathname).toBe("/warehouse/picking-list");
    expect(url.searchParams.getAll("shipmentIds")).toEqual([40, 41, 42, 43, 25].map(shipId));
    expect(url.searchParams.get("warehouseId")).toBe(WH);
  });
});

describe("Đóng gói — in nhãn theo lô (VC04)", () => {
  it("5 vận đơn hai hãng → hai lô, câu tự giao; lô sẵn sàng có tệp PDF; in không đổi cột Đóng gói", async () => {
    fake.batches.set(B1, {
      id: B1,
      carrierAccountId: ACC_VTP,
      status: "ready",
      count: 3,
      url: "https://files.example/vtp.pdf?sig=1",
      createdAt: "2026-11-21T08:20:00Z",
    });
    fake.batches.set(B2, { id: B2, carrierAccountId: ACC_BEST, status: "pending", count: 1, createdAt: "" });
    const dlg = await openLabels(created);
    expect(fake.writes).toHaveLength(1);
    expect(fake.writes[0]).toMatchObject({
      method: "POST",
      path: "/api/v1/label-batches",
      body: { shipmentIds: [40, 41, 42, 43, 25].map(shipId) },
    });
    expect(fake.writes[0]?.key).toMatch(/.+/);
    expect(await within(dlg).findByText("Viettel Post — Kho chính")).toBeTruthy();
    const link = await within(dlg).findByRole("link", { name: "Mở tệp nhãn (PDF)" });
    expect(link.getAttribute("href")).toBe("https://files.example/vtp.pdf?sig=1");
    expect(within(dlg).getByText("Best Express — Kho chính")).toBeTruthy();
    expect(within(dlg).getByText("1 vận đơn · đang lấy nhãn từ hãng…")).toBeTruthy();
    expect(within(dlg).getByText("1 vận đơn tự giao không có nhãn hãng (DH0001225).")).toBeTruthy();
    // In không ghi gì vào đóng gói: chỉ một yêu cầu ghi (lô nhãn), cột Đóng gói giữ nguyên.
    fireEvent.click(within(dlg).getByRole("button", { name: "Đóng" }));
    expect(fake.writes.map((w) => w.path)).toEqual(["/api/v1/label-batches"]);
    expect(within(screen.getByRole("row", { name: /DH0001243/ })).getByText("Đã đóng gói")).toBeTruthy();
    expect(within(screen.getByRole("row", { name: /DH0001242/ })).getByText("Đang đóng gói")).toBeTruthy();
  });

  it("lô lỗi: 'Thử lại lô …' gửi lại đúng vận đơn của tài khoản đó, lô khác giữ nguyên", async () => {
    fake.batches.set(B1, { id: B1, carrierAccountId: ACC_VTP, status: "failed", count: 3, createdAt: "" });
    fake.batches.set(B2, { id: B2, carrierAccountId: ACC_BEST, status: "pending", count: 1, createdAt: "" });
    fake.batches.set(B3, { id: B3, carrierAccountId: ACC_VTP, status: "pending", count: 3, createdAt: "" });
    const dlg = await openLabels(created);
    expect(await within(dlg).findByText("Viettel Post — Kho chính: hãng không trả nhãn.")).toBeTruthy();
    fake.replies.push({
      status: 202,
      body: { batches: [{ id: B3, carrierAccountId: ACC_VTP, count: 3 }], skipped: [] },
    });
    fireEvent.click(within(dlg).getByRole("button", { name: "Thử lại lô Viettel Post — Kho chính" }));
    await waitFor(() => {
      expect(fake.writes).toHaveLength(2);
    });
    expect(fake.writes[1]?.body).toEqual({ shipmentIds: [40, 41, 42].map(shipId) });
    expect(fake.writes[1]?.key).not.toBe(fake.writes[0]?.key);
    expect(await within(dlg).findAllByText("3 vận đơn · đang lấy nhãn từ hãng…")).toHaveLength(1);
    expect(within(dlg).queryByText("Viettel Post — Kho chính: hãng không trả nhãn.")).toBeNull();
    expect(within(dlg).getByText("1 vận đơn · đang lấy nhãn từ hãng…")).toBeTruthy();
  });

  it("đường dẫn tệp không phải http(s) thì không có liên kết", async () => {
    fake.batches.set(B1, {
      id: B1,
      carrierAccountId: ACC_VTP,
      status: "ready",
      count: 3,
      url: "javascript:alert(1)",
      createdAt: "",
    });
    fake.replies.push({
      status: 202,
      body: { batches: [{ id: B1, carrierAccountId: ACC_VTP, count: 3 }], skipped: [] },
    });
    const dlg = await openLabels();
    await within(dlg).findByText("Viettel Post — Kho chính");
    expect(within(dlg).queryByRole("link")).toBeNull();
  });

  it("lỗi API khi in: hiện câu lỗi, không mở hộp", async () => {
    fake.replies.push({ status: 403, body: { error: "forbidden", request_id: "t" } });
    const bar = await selectAll();
    fireEvent.click(within(bar).getByRole("button", { name: "In nhãn" }));
    await waitFor(() => {
      expect(document.querySelector(".box-err")).not.toBeNull();
    });
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});

describe("Đóng gói — chỉ đi tới (HV24)", () => {
  it("Đã đóng gói rồi chọn lại Đang đóng gói → 'đã ở bước sau', dòng đó không đổi", async () => {
    const packed: PackingResultList = {
      items: [40, 41, 42, 43, 25].map((n) => ({ orderId: orderId(n), result: "updated" })),
    };
    fake.replies.push({ status: 200, body: packed });
    let bar = await selectAll();
    // Máy chủ đổi xong thì lần đọc lại danh sách thấy cả năm đơn "Đã đóng gói".
    fake.rows = fiveRows().map((r) => ({ ...r, packingStatus: "packed" as const }));
    fireEvent.click(within(bar).getByRole("button", { name: "Đã đóng gói" }));
    expect(await screen.findByText('Đã đánh dấu 5 đơn "Đã đóng gói".')).toBeTruthy();
    expect(fake.writes[0]).toMatchObject({
      path: "/api/v1/orders/packing",
      body: { orderIds: [40, 41, 42, 43, 25].map(orderId), to: "packed" },
    });
    await waitFor(() => {
      expect(within(screen.getByRole("row", { name: /DH0001241/ })).getByText("Đã đóng gói")).toBeTruthy();
    });
    fireEvent.click(screen.getByRole("checkbox", { name: "Chọn DH0001241" }));
    bar = screen.getByRole("toolbar", { name: "Thao tác với vận đơn đã chọn" });
    fake.replies.push({ status: 200, body: { items: [{ orderId: orderId(41), result: "already_further" }] } });
    fireEvent.click(within(bar).getByRole("button", { name: "Đang đóng gói" }));
    expect(
      await screen.findByText(
        'Đã đánh dấu 0 đơn "Đang đóng gói". 1 đơn đã ở bước sau (DH0001241 · Đã đóng gói), không đổi.',
      ),
    ).toBeTruthy();
    expect(screen.getByRole("link", { name: "Xem đơn" }).getAttribute("href")).toBe(`/don/${orderId(41)}`);
    expect(fake.writes[1]?.body).toEqual({ orderIds: [orderId(41)], to: "picking" });
  });

  it("đơn không đổi được (not_allowed) được kể ra", async () => {
    fake.replies.push({
      status: 200,
      body: {
        items: [
          { orderId: orderId(40), result: "updated" },
          { orderId: orderId(41), result: "not_allowed" },
        ],
      },
    });
    renderAt("/warehouse/pack");
    await screen.findByRole("row", { name: /DH0001240/ });
    fireEvent.click(screen.getByRole("checkbox", { name: "Chọn DH0001240" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "Chọn DH0001241" }));
    fireEvent.click(
      within(screen.getByRole("toolbar", { name: "Thao tác với vận đơn đã chọn" })).getByRole("button", {
        name: "Đang đóng gói",
      }),
    );
    expect(await screen.findByText(/1 đơn không đổi được \(DH0001241\)/)).toBeTruthy();
  });
});
