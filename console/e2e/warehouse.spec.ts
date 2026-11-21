// Màn Kho M3 (kế hoạch M3 Task 136; mockup docs/mockups/m3/kho.html) ở 1512 × 767, 390 × 844 và 360 × 800, API giả:
// Đóng gói (5 vận đơn hai hãng + tự giao, thanh chọn nhiều, hộp In nhãn), phiếu nhặt, Nhận hàng hoàn (ba ô Tốt /
// Hỏng / Thiếu). Hành vi kiểm ở vitest của src/screens/warehouse; ở đây kiểm bố cục không tràn (ô nhập nằm trọn trong
// thẻ, không cuộn ngang) và chụp test-results/kho-*.png để so với docs/mockups/m3/png/kho-{1512,390,360}.png.
import type { Page } from "@playwright/test";
import type { LabelBatch, OrderListItem, PickingList, Return } from "../src/api";
import { OWNER_GRANTS, expect, json, noHorizontalScroll, test, type FakeApi } from "./fixtures";

const id = (n: number) => `0195f3b2-0000-7000-8000-0000000d${String(n).padStart(4, "0")}`;
const WH = id(9001);
const ACC_VTP = id(9101);
const ACC_BEST = id(9102);
const GRANTS = [
  ...OWNER_GRANTS,
  ...[
    "order.read",
    "order.return.receive",
    "fulfillment.read",
    "fulfillment.print",
    "fulfillment.pack",
    "fulfillment.carrier_accounts.read",
  ].map((permission) => ({ permission, scope: { type: "all" as const, id: "" } })),
];

function row(
  n: number,
  p: Partial<OrderListItem>,
  ship: Partial<NonNullable<OrderListItem["shipment"]>>,
): OrderListItem {
  return {
    id: id(n),
    code: `DH00012${String(n)}`,
    label: "awaiting_pickup",
    status: "confirmed",
    customerId: id(1),
    customerName: "Khách",
    grandTotal: 690_000,
    codAmount: 690_000,
    itemCount: 1,
    createdAt: "2026-11-21T07:00:00Z",
    duplicateSuspect: false,
    packingStatus: "none",
    shipment: {
      id: id(n + 500),
      code: `E26112100${String(n)}`,
      carrierKey: "vtp",
      carrierAccountId: ACC_VTP,
      mode: "api",
      trackingCode: `12345678901${String(n)}`,
      status: "awaiting_pickup",
      ...ship,
    },
    ...p,
  };
}

const ROWS: OrderListItem[] = [
  row(40, { itemCount: 3, codAmount: 1_960_000 }, { labelPrintedAt: "2026-11-21T08:12:00Z" }),
  row(41, {}, {}),
  row(42, { itemCount: 64, codAmount: 112_450_000, packingStatus: "picking" }, {}),
  row(
    43,
    { itemCount: 2, codAmount: 0, packingStatus: "packed" },
    {
      carrierKey: "best",
      carrierAccountId: ACC_BEST,
      trackingCode: "BE0098812737VN",
    },
  ),
  row(25, { codAmount: 450_000 }, { carrierKey: "self", mode: "manual", trackingCode: "E261121R2W8KN" }),
  row(
    44,
    { packingStatus: "packed" },
    { carrierKey: "spx", trackingCode: "SPXVN0612345678", labelPrintedAt: "2026-11-21T07:02:00Z" },
  ),
];

const LONG = "Đầm maxi lụa Hàn cao cấp cổ yếm phối ren hoa thủ công tay phồng nhẹ dáng xòe xếp ly nhiều tầng";
const PICKING: PickingList = {
  lines: [
    {
      sku: "AO0101-DEN-M",
      name: "Áo thun cổ tròn basic",
      variant: "Đen · M",
      qty: 21,
      orderCodes: ["DH0001240", "DH0001242"],
    },
    {
      sku: "DAM0201-BE-L",
      name: "Đầm lụa be dáng suông",
      variant: "Be · L",
      qty: 3,
      orderCodes: ["DH0001240", "DH0001241"],
    },
    { sku: "DAM0123-BE-M", name: LONG, variant: "Be · M", qty: 44, orderCodes: ["DH0001242"] },
    { sku: "TUI0090-NAU", name: "Túi da mini", variant: "Nâu", qty: 3, orderCodes: ["DH0001243", "DH0001225"] },
  ],
  orders: [
    {
      code: "DH0001240",
      shipmentCode: "E1",
      trackingCode: "1234567890123",
      carrier: "vtp",
      itemCount: 3,
      codAmount: 1_960_000,
    },
    {
      code: "DH0001242",
      shipmentCode: "E2",
      trackingCode: "1234567890125",
      carrier: "vtp",
      itemCount: 64,
      codAmount: 112_450_000,
    },
    { code: "DH0001225", shipmentCode: "E261121R2W8KN", carrier: "self", itemCount: 1, codAmount: 450_000 },
  ],
};

const RET: Return = {
  id: id(7031),
  code: "TH0000031",
  orderId: id(88),
  shipmentId: id(588),
  kind: "carrier_return",
  status: "requested",
  refundAmount: 0,
  items: [
    { id: id(7101), orderItemId: id(7201), qty: 2, refundAmount: 0 },
    { id: id(7102), orderItemId: id(7202), qty: 1, refundAmount: 0 },
  ],
  createdAt: "2026-11-20T00:00:00Z",
  version: 3,
};

async function warehouseApi(api: FakeApi) {
  await api.live({ grants: GRANTS });
  await api.on("**/api/v1/inventory/warehouses", (r) =>
    json(r, 200, [{ id: WH, code: "MAIN", name: "Kho chính", status: "active" }]),
  );
  await api.on(/\/api\/v1\/orders(\?|$)/, (r) => json(r, 200, { items: ROWS, nextCursor: "c2" }));
  await api.on(/\/api\/v1\/carrier-accounts(\?|$)/, (r) =>
    json(r, 200, {
      items: [
        { id: ACC_VTP, carrierKey: "vtp", name: "Viettel Post — Kho chính" },
        { id: ACC_BEST, carrierKey: "best", name: "Best Express — Kho chính" },
      ].map((a) => ({
        ...a,
        mode: "api",
        environment: "production",
        status: "active",
        warehouseId: WH,
        isDefaultForWarehouse: false,
        showOnWeb: false,
        webSort: 0,
        hasCredentials: true,
        needsRewrap: false,
        version: 1,
      })),
    }),
  );
  await api.on("**/api/v1/label-batches", (r) =>
    json(r, 202, {
      batches: [
        { id: id(8001), carrierAccountId: ACC_VTP, count: 3 },
        { id: id(8002), carrierAccountId: ACC_BEST, count: 1 },
      ],
      skipped: [{ shipmentId: id(525), reason: "no_carrier_label" }],
    }),
  );
  await api.on(/\/api\/v1\/label-batches\/[^/]+$/, (r) => {
    const bid = new URL(r.request().url()).pathname.split("/").pop() ?? "";
    const b: LabelBatch =
      bid === id(8001)
        ? {
            id: bid,
            carrierAccountId: ACC_VTP,
            status: "ready",
            count: 3,
            url: "https://files.example/vtp.pdf",
            createdAt: "",
          }
        : { id: bid, carrierAccountId: ACC_BEST, status: "pending", count: 1, createdAt: "" };
    return json(r, 200, b);
  });
  await api.on(/\/api\/v1\/picking-list(\?|$)/, (r) => json(r, 200, PICKING));
  await api.on(/\/api\/v1\/returns(\?|$)/, (r) => {
    const kind = new URL(r.request().url()).searchParams.get("kind");
    return json(r, 200, { items: kind === "carrier_return" ? [RET] : [] });
  });
  await api.on(/\/api\/v1\/returns\/[^/]+$/, (r) => json(r, 200, RET));
  await api.on(/\/api\/v1\/orders\/[^/?]+$/, (r) =>
    json(r, 200, {
      id: id(88),
      code: "DH0001088",
      status: "returned",
      customerId: id(1),
      subtotal: 0,
      shippingFee: 0,
      grandTotal: 0,
      codAmount: 0,
      createdAt: "2026-11-18T00:00:00Z",
      items: [
        {
          id: id(7201),
          position: 1,
          variantId: id(7301),
          sku: "DAM0123-BE-M",
          name: LONG,
          qty: 2,
          unitPrice: 690_000,
          netAmount: 1_380_000,
        },
        {
          id: id(7202),
          position: 2,
          variantId: id(7302),
          sku: "TUI0090-NAU",
          name: "Túi da mini",
          qty: 1,
          unitPrice: 290_000,
          netAmount: 290_000,
        },
      ],
    }),
  );
  await api.on(/\/api\/v1\/shipments\/[^/]+$/, (r) =>
    json(r, 200, {
      id: id(588),
      orderId: id(88),
      code: "E261120AAAA",
      carrierKey: "best",
      carrierAccountId: ACC_BEST,
      trackingCode: "BE0098811111VN",
      status: "returned",
      codAmount: 0,
      createdAt: "",
    }),
  );
}

/** Mọi ô nhập của thẻ nằm trọn trong thẻ (không tràn ngang). */
async function inputsInside(page: Page, card: string) {
  const out = await page.evaluate((sel) => {
    const c = document.querySelector(sel);
    if (c === null) return ["không thấy thẻ"];
    const box = c.getBoundingClientRect();
    return [...c.querySelectorAll("input")]
      .map((i) => i.getBoundingClientRect())
      .filter((r) => r.left < box.left - 0.5 || r.right > box.right + 0.5 || r.width < 32)
      .map((r) => `${String(r.left)}–${String(r.right)} ngoài ${String(box.left)}–${String(box.right)}`);
  }, card);
  expect(out).toEqual([]);
}

for (const vp of [
  { width: 1512, height: 767 },
  { width: 390, height: 844 },
  { width: 360, height: 800 },
]) {
  test.describe(`${String(vp.width)} × ${String(vp.height)}`, () => {
    test.use({ viewport: vp });

    test("Đóng gói: chọn 5 dòng, thanh chọn nhiều, hộp In nhãn không tràn", async ({ page, api }) => {
      await warehouseApi(api);
      await page.goto("/warehouse/pack");
      await expect(page.getByRole("row", { name: /DH0001240/ })).toBeVisible();
      for (const c of ["DH0001240", "DH0001241", "DH0001242", "DH0001243", "DH0001225"]) {
        await page.getByRole("checkbox", { name: `Chọn ${c}` }).check();
      }
      await expect(page.getByText("Đã chọn 5 vận đơn · Viettel Post 3, Best Express 1, tự giao 1")).toBeVisible();
      await noHorizontalScroll(page);
      await page.screenshot({ fullPage: true, path: `test-results/kho-dong-goi-${String(vp.width)}.png` });
      await page.getByRole("toolbar").getByRole("button", { name: "In nhãn" }).click();
      const dlg = page.getByRole("dialog", { name: "In nhãn · 5 vận đơn" });
      await expect(dlg.getByRole("link", { name: "Mở tệp nhãn (PDF)" })).toBeVisible();
      await expect(dlg.getByText("1 vận đơn tự giao không có nhãn hãng (DH0001225).")).toBeVisible();
      await noHorizontalScroll(page);
      await page.screenshot({ path: `test-results/kho-in-nhan-${String(vp.width)}.png` });
    });

    test("Phiếu nhặt: tên dài không tràn", async ({ page, api }) => {
      await warehouseApi(api);
      await page.goto(`/warehouse/picking-list?shipmentIds=${id(540)}&shipmentIds=${id(542)}&warehouseId=${WH}`);
      await expect(page.getByRole("heading", { name: "Phiếu nhặt hàng · Kho chính" })).toBeVisible();
      await noHorizontalScroll(page);
      await page.screenshot({ fullPage: true, path: `test-results/kho-phieu-nhat-${String(vp.width)}.png` });
    });

    test("Nhận hàng hoàn: ba ô mỗi dòng nằm trọn trong thẻ, nút khóa khi thiếu", async ({ page, api }) => {
      await warehouseApi(api);
      await page.goto(`/warehouse/returns/receive/${RET.id}`);
      const card = page.getByRole("region", { name: "Nhận hàng hoàn · TH0000031" });
      await card.getByRole("textbox", { name: "Tốt DAM0123-BE-M" }).fill("1");
      await card.getByRole("textbox", { name: "Tốt TUI0090-NAU" }).fill("1");
      await expect(card.getByText("Còn thiếu 1 món chưa phân loại (DAM0123-BE-M).")).toBeVisible();
      await expect(card.getByRole("button", { name: "Nhận vào kho" })).toBeDisabled();
      await expect(page.getByRole("link", { name: "DH0001088 · Best Express" })).toBeVisible();
      await inputsInside(page, 'section[aria-label="Nhận hàng hoàn · TH0000031"]');
      await noHorizontalScroll(page);
      await page.screenshot({ fullPage: true, path: `test-results/kho-nhan-hoan-${String(vp.width)}.png` });
    });
  });
}
