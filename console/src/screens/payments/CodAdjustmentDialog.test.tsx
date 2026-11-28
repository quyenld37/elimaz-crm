// @vitest-environment jsdom
// Sổ COD và điều chỉnh COD trên khung COD của vận đơn (kế hoạch M3 Task 138; mockup docs/mockups/m3/tien-tren-don.html
// trạng thái C, D; HV35): sổ chỉ ghi thêm, có dòng đảo; bước 1 chọn cách, số, lý do; bước 2 trước/sau, cảnh báo ngăn
// âm vẫn cho xác nhận; 409 preview_stale → bản xem trước mới; không có cash.adjust thì không có nút.
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { CodAdjustmentPreview, CodBalances, CodTransfer } from "../../api";
import { stubDialog } from "../don/donTestkit";
import {
  SENDER,
  SH1,
  WH,
  confirmedOrder,
  grantsAt,
  installFake,
  renderPanel,
  shipment,
  type Fake,
} from "../shipping/shippingTestkit";

const T_COLLECT = "0195f3b2-0000-7000-8000-0000000c0002";
const T_REV = "0195f3b2-0000-7000-8000-0000000c0003";

let fake: Fake;
beforeEach(() => {
  fake = installFake();
  stubDialog();
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const bal = (p: Partial<CodBalances> = {}): CodBalances => ({
  toCollect: 0,
  carrierHolding: 90_000,
  shopReceived: 1_168_000,
  carrierFee: 32_000,
  salesClearing: -1_290_000,
  ...p,
});

function tr(p: Partial<CodTransfer>): CodTransfer {
  return {
    id: T_COLLECT,
    shipmentId: SH1,
    kind: "collected",
    occurredAt: "2026-11-20T02:15:00Z",
    entries: [
      { account: "to_collect", amount: -1_960_000 },
      { account: "carrier_holding", amount: 1_960_000 },
    ],
    actor: { id: "", name: "Hãng" },
    ...p,
  };
}

function setup(perms: string[] = [], transfers?: CodTransfer[]) {
  fake.grants = grantsAt(WH, ...SENDER, "cash.cod.read", ...perms);
  fake.shipments.set(
    SH1,
    shipment({ status: "delivered", label: "Thành công", createState: "created", trackingCode: "1234567890001" }),
  );
  fake.cod = {
    balances: bal(),
    transfers: transfers ?? [
      tr({
        id: T_REV,
        kind: "reversal",
        occurredAt: "2026-11-22T09:40:00Z",
        reversesTransferId: "x",
        reason: "Nhân viên sửa trạng thái từ Thành công sang Đang chuyển hoàn",
        actor: { id: "u1", name: "Nguyễn Thị Lan" },
        entries: [
          { account: "to_collect", amount: 1_960_000 },
          { account: "carrier_holding", amount: -1_960_000 },
        ],
      }),
      tr({}),
    ],
  };
  renderPanel(confirmedOrder({ currentShipmentId: SH1 }));
}

function preview(p: Partial<CodAdjustmentPreview> = {}): CodAdjustmentPreview {
  return {
    before: bal(),
    after: bal({ carrierHolding: -30_000, carrierFee: 152_000 }),
    transfer: {
      kind: "adjustment",
      entries: [
        { account: "carrier_holding", amount: -120_000 },
        { account: "carrier_fee", amount: 120_000 },
      ],
    },
    warnings: ['Ngăn "Hãng đang giữ" sẽ âm −30.000 ₫ (đang giữ 90.000 ₫, chuyển 120.000 ₫)'],
    token: "c-1",
    ...p,
  };
}

const codBox = () => screen.findByRole("region", { name: "Tiền thu hộ (COD)" });

async function openAdjust() {
  const b = await within(await codBox()).findByRole("button", { name: "Điều chỉnh COD" });
  fireEvent.click(b);
  return screen.findByRole("dialog", { name: "Điều chỉnh COD · DH0001235" });
}

describe("Sổ COD của vận đơn", () => {
  it("mới nhất trên đầu; dòng đảo có lý do và người làm; chỉ ghi thêm", async () => {
    setup();
    const box = await codBox();
    const ledger = await within(box).findByRole("list", { name: "Sổ COD" });
    const rows = within(ledger).getAllByRole("listitem");
    expect(rows[0]?.textContent).toContain("Đảo bút toán");
    expect(rows[0]?.textContent).toContain("Nhân viên sửa trạng thái từ Thành công sang Đang chuyển hoàn");
    expect(rows[0]?.textContent).toContain("Nguyễn Thị Lan");
    expect(rows[1]?.textContent).toContain("Hãng thu 1.960.000 ₫ · Đang đi thu → Hãng đang giữ");
    expect(box.textContent).toContain("Chỉ ghi thêm");
    expect(within(box).queryByRole("button", { name: "Điều chỉnh COD" })).toBeNull();
  });
});

describe("Điều chỉnh COD (hai bước)", () => {
  it("chuyển ngăn: bước 1 → xem trước trước/sau, cảnh báo ngăn âm vẫn xác nhận được → gửi token", async () => {
    setup(["cash.adjust"]);
    const dlg = await openAdjust();
    expect(dlg.textContent).toContain("Bước 1/2 · Viettel Post — Kho chính 1234567890001");
    fireEvent.click(within(dlg).getByRole("radio", { name: "Chuyển tiền giữa hai ngăn" }));
    fireEvent.change(within(dlg).getByRole("combobox", { name: "Từ ngăn" }), { target: { value: "carrier_holding" } });
    fireEvent.change(within(dlg).getByRole("combobox", { name: "Sang ngăn" }), { target: { value: "carrier_fee" } });
    fireEvent.change(within(dlg).getByRole("textbox", { name: "Số tiền" }), { target: { value: "120.000" } });
    const look = within(dlg).getByRole<HTMLButtonElement>("button", { name: "Xem trước" });
    expect(look.disabled).toBe(true);
    fireEvent.change(within(dlg).getByRole("textbox", { name: "Lý do" }), { target: { value: "Phí hãng thu thiếu" } });
    fake.replies.push({ status: 200, body: preview() });
    fireEvent.click(look);
    expect(await within(dlg).findByText(/Bước 2\/2 · chưa ghi gì/)).toBeTruthy();
    expect(fake.writes.at(-1)).toMatchObject({
      path: "/api/v1/cod/adjustments/preview",
      body: {
        shipmentId: SH1,
        mode: "move",
        from: "carrier_holding",
        to: "carrier_fee",
        amount: 120_000,
        reason: "Phí hãng thu thiếu",
      },
    });
    const row = within(dlg).getByRole("row", { name: /Hãng đang giữ/ });
    expect(row.textContent).toContain("90.000 ₫");
    expect(row.textContent).toContain("−30.000 ₫");
    expect(within(dlg).getByRole("row", { name: /Phí hãng/ }).textContent).toContain("152.000 ₫");
    expect(dlg.textContent).toContain('Ngăn "Hãng đang giữ" sẽ âm −30.000 ₫');
    const ok = within(dlg).getByRole<HTMLButtonElement>("button", { name: "Xác nhận điều chỉnh" });
    expect(ok.disabled).toBe(false);
    fake.replies.push({ status: 201, body: tr({ id: "new", kind: "adjustment" }) });
    fireEvent.click(ok);
    await waitFor(() => {
      expect(fake.writes.at(-1)).toMatchObject({
        path: "/api/v1/cod/adjustments",
        body: { mode: "move", amount: 120_000, token: "c-1" },
      });
    });
    await waitFor(() => {
      expect(screen.queryByRole("dialog", { name: "Điều chỉnh COD · DH0001235" })).toBeNull();
    });
  });

  it("409 preview_stale: hiện bản xem trước mới kèm câu, xác nhận lại gửi token mới", async () => {
    setup(["cash.adjust"]);
    const dlg = await openAdjust();
    fireEvent.change(within(dlg).getByRole("combobox", { name: "Bút toán cần đảo" }), { target: { value: T_COLLECT } });
    fireEvent.change(within(dlg).getByRole("textbox", { name: "Lý do" }), { target: { value: "Ghi nhầm" } });
    fake.replies.push({ status: 200, body: preview({ warnings: [] }) });
    fireEvent.click(within(dlg).getByRole("button", { name: "Xem trước" }));
    await within(dlg).findByRole("button", { name: "Xác nhận điều chỉnh" });
    expect(fake.writes.at(-1)?.body).toMatchObject({ mode: "reverse", reversesTransferId: T_COLLECT });
    fake.replies.push(
      { status: 409, body: { error: "preview_stale", request_id: "t" } },
      { status: 200, body: preview({ token: "c-2", warnings: [] }) },
    );
    fireEvent.click(within(dlg).getByRole("button", { name: "Xác nhận điều chỉnh" }));
    expect(await within(dlg).findByText(/Số liệu vừa đổi, đây là bản xem trước mới/)).toBeTruthy();
    fake.replies.push({ status: 201, body: tr({ id: "new", kind: "reversal" }) });
    fireEvent.click(within(dlg).getByRole("button", { name: "Xác nhận điều chỉnh" }));
    await waitFor(() => {
      expect(fake.writes.at(-1)?.body).toMatchObject({ token: "c-2", reversesTransferId: T_COLLECT });
    });
  });

  it("bút toán đã bị đảo và bút toán đảo không có trong danh sách đảo", async () => {
    setup(["cash.adjust"], [tr({ id: T_REV, kind: "reversal", reversesTransferId: T_COLLECT }), tr({})]);
    const dlg = await openAdjust();
    const sel = within(dlg).getByRole<HTMLSelectElement>("combobox", { name: "Bút toán cần đảo" });
    const values = Array.from(sel.options).map((o) => o.value);
    expect(values).not.toContain(T_COLLECT);
    expect(values).not.toContain(T_REV);
  });
});
