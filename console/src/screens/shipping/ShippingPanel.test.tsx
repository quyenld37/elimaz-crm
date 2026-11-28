// @vitest-environment jsdom
// Khối Vận chuyển trên đơn (kế hoạch M3 Task 133; mockup docs/mockups/m3/don-van-chuyen.html trạng thái A, B, C, G, H;
// HV07–HV11, HV16): so phí, chọn rẻ nhất, báo phí lại, gửi hãng một lần, kết quả lệnh tạo, tài khoản nhập mã tay.
import { act, cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { onRealtimeChanged } from "../../app/queryClient";
import {
  A_BEST,
  A_BEST2,
  A_GHN,
  A_SELF,
  A_VTP,
  LONG_ACCOUNT,
  O1,
  Q_BEST,
  Q_VTP,
  REQ1,
  REQ2,
  SENDER,
  SH1,
  WH,
  WH2,
  account,
  confirmedOrder,
  grantsAt,
  installFake,
  quote,
  renderPanel,
  shipment,
  type Fake,
} from "./shippingTestkit";
import { TEXT_WAIT_CREDIT } from "./ShippingPanel";

let fake: Fake;
beforeEach(() => {
  fake = installFake();
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const panel = () => screen.getByRole("region", { name: "Vận chuyển" });
const rows = () => within(panel()).getByRole("radiogroup", { name: "Chọn tài khoản hãng" });
const quoteWrites = () => fake.writes.filter((w) => w.path === `/api/v1/orders/${O1}/shipping-quotes`);
const shipWrites = () => fake.writes.filter((w) => w.path === `/api/v1/orders/${O1}/shipments`);
const STALE = { status: 409, body: { error: "quote_stale", request_id: "t" } };
const BUSY = { status: 503, body: { error: "retry_later", request_id: "t" }, headers: { "Retry-After": "0" } };

/** Lần so phí gần nhất có sẵn: VTP 32.000, Best 35.500, Best phụ lỗi "hãng không trả lời". */
function latestQuotes(p: { validUntil?: string } = {}) {
  fake.quotes.set("", {
    requestId: REQ1,
    items: [
      quote(p),
      quote({
        quoteId: Q_BEST,
        carrierAccountId: A_BEST,
        accountName: "Best Express — Kho chính",
        carrierKey: "best",
        feeTotal: 35_500,
        service: "STD",
        serviceName: "Tiêu chuẩn",
        expectedDays: 3,
        ...p,
      }),
      {
        carrierAccountId: A_BEST2,
        accountName: LONG_ACCOUNT,
        carrierKey: "best",
        status: "error",
        errorCode: "timeout",
      },
    ],
  });
}

function manualAccounts() {
  fake.accounts = [
    account(),
    account({ id: A_SELF, carrierKey: "self", name: "Shop tự giao (nội thành)" }),
    account({ id: A_VTP, carrierKey: "vtp", mode: "api", name: "Viettel Post — Kho chính" }),
    account({ id: "0195f3b2-0000-7000-8000-00000000ac08", name: "GHN tạm dừng", status: "paused" }),
    account({ id: "0195f3b2-0000-7000-8000-00000000ac09", warehouseId: WH2, name: "GHN kho khác" }),
  ];
}

async function sendButton(name: RegExp | string) {
  return within(panel()).findByRole("button", { name });
}

/** Mở khối, đổi sang dòng Best Express (35.500 ₫) rồi bấm gửi. */
async function sendBest() {
  renderPanel(confirmedOrder());
  await sendButton(/^Gửi hãng · Viettel Post/);
  fireEvent.click(within(rows()).getByRole("radio", { name: /Best Express — Kho chính/ }));
  fireEvent.click(await sendButton("Gửi hãng · Best Express · 35.500 ₫"));
}

describe("So phí (VC02, HV07, HV08)", () => {
  it("2 giá và 1 lỗi: không ô nào hiện 0 ₫, lỗi có chữ, chọn sẵn giá rẻ nhất, tài khoản nhập tay hiện Không báo phí", async () => {
    latestQuotes();
    manualAccounts();
    renderPanel(confirmedOrder());
    const btn = await sendButton("Gửi hãng · Viettel Post · 32.000 ₫");
    const g = rows();
    const radios = within(g).getAllByRole("radio");
    expect(radios).toHaveLength(5);
    expect(g.textContent).not.toContain("0 ₫ ");
    expect(within(g).queryByText(/^0\s?₫$/)).toBeNull();
    const vtp = within(g).getByRole("radio", { name: /Viettel Post — Kho chính/ });
    expect(vtp).toHaveProperty("checked", true);
    expect(within(g).getByText("32.000 ₫")).toBeTruthy();
    expect(within(g).getByText("35.500 ₫")).toBeTruthy();
    expect(within(g).getByText(/rẻ nhất/)).toBeTruthy();
    const err = within(g).getByRole("radio", { name: new RegExp(LONG_ACCOUNT) });
    expect(err).toHaveProperty("disabled", true);
    expect(within(g).getByText("Không có giá")).toBeTruthy();
    expect(within(g).getByText("Hãng không trả lời")).toBeTruthy();
    expect(within(g).getAllByText("Không báo phí")).toHaveLength(2);
    // Tài khoản kho khác, tài khoản api đã có trong bảng so phí không thêm dòng.
    expect(within(g).queryByText("GHN kho khác")).toBeNull();
    expect(btn).toHaveProperty("disabled", false);
    // Mở đơn đã có lần so phí: không tự hỏi phí lại (HV07: một lần).
    expect(quoteWrites()).toHaveLength(0);
  });

  it("chưa từng so phí: tự hỏi phí một lần, dòng đang hỏi; báo hiệu shipping_quote.ready của đơn này thì đọc lại", async () => {
    fake.quotes.set(REQ2, {
      requestId: REQ2,
      items: [
        { carrierAccountId: A_VTP, accountName: "Viettel Post — Kho chính", carrierKey: "vtp", status: "pending" },
      ],
    });
    const { qc } = renderPanel(confirmedOrder());
    expect(await within(panel()).findByText("Đang hỏi hãng…")).toBeTruthy();
    expect(quoteWrites()).toHaveLength(1);
    expect(quoteWrites()[0]?.key).toBeTruthy();
    expect(within(rows()).getByRole("radio")).toHaveProperty("disabled", true);
    fake.quotes.set(REQ2, { requestId: REQ2, items: [quote()] });
    act(() => {
      onRealtimeChanged(qc, { topic: "shipping_quote.ready", entity: { type: "order", id: "khác" } });
    });
    act(() => {
      onRealtimeChanged(qc, { topic: "shipping_quote.ready", entity: { type: "order", id: O1 } });
    });
    await sendButton("Gửi hãng · Viettel Post · 32.000 ₫");
    expect(quoteWrites()).toHaveLength(1);
  });

  it("báo phí quá hạn: nút gửi thành Báo phí lại; bấm thì hỏi phí lại với cùng cân nặng, tùy chọn", async () => {
    latestQuotes({ validUntil: new Date(Date.now() - 60_000).toISOString() });
    renderPanel(confirmedOrder());
    expect(await within(panel()).findByText(/Giá đã cũ/)).toBeTruthy();
    const btn = await sendButton("Báo phí lại");
    expect(within(panel()).queryByRole("button", { name: /^Gửi hãng/ })).toBeNull();
    expect(within(panel()).getByText(/Giá đã cũ/)).toBeTruthy();
    fireEvent.click(btn);
    await waitFor(() => {
      expect(quoteWrites()).toHaveLength(1);
    });
    // Cùng thân như lần so phí lúc mở: khai giá = tổng đơn; không cân nặng (theo cài đặt), không options (tùy chọn
    // mặc định của tài khoản — ghi chú hãng của đơn không điền sẵn vì options có ô nào là bỏ hết mặc định).
    expect(quoteWrites()[0]?.body).toEqual({ declaredValue: 1_860_000 });
    expect(shipWrites()).toHaveLength(0);
  });

  it("tự điền ghi chú cho hãng: cảnh báo bỏ tùy chọn mặc định, ô trống không còn ghi Theo tài khoản; gửi options đó", async () => {
    latestQuotes();
    renderPanel(confirmedOrder());
    await sendButton(/^Gửi hãng · Viettel Post/);
    expect(within(panel()).queryByText(/không dùng tùy chọn mặc định của tài khoản hãng/)).toBeNull();
    expect(within(panel()).getByText(/Ghi chú hãng của đơn: "Hàng sỉ, kiểm số lượng khi nhận."/)).toBeTruthy();
    expect(within(panel()).getByRole("option", { name: "Theo tài khoản" })).toBeTruthy();
    fireEvent.change(within(panel()).getByLabelText(/Ghi chú cho hãng/), { target: { value: " Gọi trước " } });
    expect(within(panel()).getByText(/không dùng tùy chọn mặc định của tài khoản hãng/)).toBeTruthy();
    expect(within(panel()).queryByRole("option", { name: "Theo tài khoản" })).toBeNull();
    expect(within(panel()).getByPlaceholderText("Không thu")).toBeTruthy();
    fireEvent.click(await sendButton("Báo phí lại"));
    await waitFor(() => {
      expect(quoteWrites()).toHaveLength(1);
    });
    expect(quoteWrites()[0]?.body).toEqual({ declaredValue: 1_860_000, options: { note: "Gọi trước" } });
  });

  it("đổi cân nặng sau khi so phí: nút thành Báo phí lại, gửi đúng cân nặng mới", async () => {
    latestQuotes();
    renderPanel(confirmedOrder());
    await sendButton(/^Gửi hãng · Viettel Post/);
    fireEvent.change(within(panel()).getByLabelText("Cân nặng"), { target: { value: "1500" } });
    const btn = await sendButton("Báo phí lại");
    expect(within(panel()).getByText(/so phí lại/)).toBeTruthy();
    fireEvent.click(btn);
    await waitFor(() => {
      expect(quoteWrites()).toHaveLength(1);
    });
    expect(quoteWrites()[0]?.body.weightGrams).toBe(1500);
  });
});

describe("Gửi hãng (VC03, HV10)", () => {
  it("gửi giá đã chọn: thân có tài khoản, báo phí, dịch vụ; bấm hai lần nhanh chỉ một yêu cầu", async () => {
    latestQuotes();
    fake.created = shipment();
    renderPanel(confirmedOrder());
    const btn = await sendButton("Gửi hãng · Viettel Post · 32.000 ₫");
    fireEvent.click(btn);
    fireEvent.click(btn);
    await waitFor(() => {
      expect(shipWrites()).toHaveLength(1);
    });
    const w = shipWrites()[0];
    expect(w?.key).toBeTruthy();
    expect(w?.body).toMatchObject({ carrierAccountId: A_VTP, quoteId: Q_VTP, service: "VCN" });
    expect(await within(panel()).findByText("Đang gửi hãng…")).toBeTruthy();
    expect(shipWrites()).toHaveLength(1);
  });

  it("chọn dòng khác rồi gửi: đúng tài khoản đó", async () => {
    latestQuotes();
    await sendBest();
    await waitFor(() => {
      expect(shipWrites()).toHaveLength(1);
    });
    expect(shipWrites()[0]?.body).toMatchObject({ carrierAccountId: A_BEST, quoteId: Q_BEST, service: "STD" });
  });

  it("409 quote_stale: tự báo phí lại, giữ tài khoản đã chọn, không gửi lần hai", async () => {
    latestQuotes();
    fake.replies.push(STALE);
    fake.quotes.set(REQ2, {
      requestId: REQ2,
      items: [
        quote({ quoteId: "0195f3b2-0000-7000-8000-00000000ad11", feeTotal: 31_000 }),
        quote({
          quoteId: "0195f3b2-0000-7000-8000-00000000ad12",
          carrierAccountId: A_BEST,
          accountName: "Best Express — Kho chính",
          carrierKey: "best",
          feeTotal: 36_000,
          service: "STD",
        }),
      ],
    });
    await sendBest();
    await waitFor(() => {
      expect(quoteWrites()).toHaveLength(1);
    });
    const btn = await sendButton("Gửi hãng · Best Express · 36.000 ₫");
    expect(within(rows()).getByRole("radio", { name: /Best Express — Kho chính/ })).toHaveProperty("checked", true);
    expect(within(panel()).getByText(/đã báo phí lại/)).toBeTruthy();
    expect(shipWrites()).toHaveLength(1);
    fireEvent.click(btn);
    await waitFor(() => {
      expect(shipWrites()).toHaveLength(2);
    });
    expect(shipWrites()[1]?.body.quoteId).toBe("0195f3b2-0000-7000-8000-00000000ad12");
  });

  it("503 retry_later hai lần rồi 202: thành công, cùng khóa, không hiện lỗi", async () => {
    latestQuotes();
    fake.replies.push(BUSY, BUSY);
    renderPanel(confirmedOrder());
    fireEvent.click(await sendButton(/^Gửi hãng · Viettel Post/));
    expect(await within(panel()).findByText("Đang gửi hãng…")).toBeTruthy();
    const ws = shipWrites();
    expect(ws).toHaveLength(3);
    expect(new Set(ws.map((w) => w.key)).size).toBe(1);
    expect(within(panel()).queryByText(/bận/)).toBeNull();
    expect(within(panel()).queryByText(/lỗi/i)).toBeNull();
  });

  it("lỗi dứt khoát (địa chỉ chưa khớp): câu tiếng Việt, nút gửi vẫn bấm lại được", async () => {
    latestQuotes();
    fake.replies.push({ status: 422, body: { error: "address_unmapped", request_id: "t" } });
    renderPanel(confirmedOrder());
    fireEvent.click(await sendButton(/^Gửi hãng · Viettel Post/));
    expect(await within(panel()).findByText(/Hãng chưa nhận địa chỉ này/)).toBeTruthy();
    expect(await sendButton(/^Gửi hãng · Viettel Post/)).toHaveProperty("disabled", false);
  });

  it("khách hay hoàn: băng gợi ý ngay trên nút Gửi hãng, không chặn (HV09)", async () => {
    latestQuotes();
    fake.stats = {
      delivered: 3,
      returned: 2,
      returnRateBp: 4000,
      risk: "high",
      hint: "Nên xin cọc hoặc gọi xác nhận trước khi gửi.",
    };
    renderPanel(confirmedOrder());
    const btn = await sendButton(/^Gửi hãng · Viettel Post/);
    const box = await within(panel()).findByText("Khách hay hoàn");
    const banner = box.closest(".box");
    expect(banner?.textContent).toContain("Nhận 3 · Hoàn 2, 40%");
    expect(banner?.textContent).toContain("Nên xin cọc");
    expect(banner?.nextElementSibling).toBe(btn.closest(".act") ?? btn);
    expect(btn).toHaveProperty("disabled", false);
  });

  it("khách không hay hoàn: không có băng", async () => {
    latestQuotes();
    renderPanel(confirmedOrder());
    await sendButton(/^Gửi hãng · Viettel Post/);
    expect(within(panel()).queryByText("Khách hay hoàn")).toBeNull();
  });

  it("COD 0 ₫: câu hãng chỉ giao, không thu tiền", async () => {
    latestQuotes();
    renderPanel(confirmedOrder({ codAmount: 0 }));
    await sendButton(/^Gửi hãng · Viettel Post/);
    expect(within(panel()).getByText(/Hãng chỉ giao, không thu tiền/)).toBeTruthy();
  });

  it("đơn đổi chưa có phiếu cấn trừ: nút gửi mờ kèm lý do; có phiếu (đã thu > 0) thì gửi được (Task 116)", async () => {
    latestQuotes();
    const R9 = "0195f3b2-0000-7000-8000-00000000a199";
    fake.returns.set(R9, {
      id: R9,
      code: "TH0000031",
      orderId: "0195f3b2-0000-7000-8000-00000000d0ff",
      kind: "exchange",
      status: "requested",
      refundAmount: 177_049,
      items: [],
      createdAt: "2026-11-20T00:00:00Z",
      version: 1,
    });
    const ex = { orderType: "exchange" as const, replacedReturnId: R9, grandTotal: 180_000 };
    const view = renderPanel(confirmedOrder({ ...ex, prepaidTotal: 0, codAmount: 180_000 }));
    const btn = await sendButton("Gửi hãng · Viettel Post · 32.000 ₫");
    await waitFor(() => {
      expect(btn).toHaveProperty("disabled", true);
    });
    expect(panel().textContent).toContain(TEXT_WAIT_CREDIT);
    view.unmount();
    renderPanel(confirmedOrder({ ...ex, prepaidTotal: 177_049, codAmount: 2_951 }));
    const ok = await sendButton("Gửi hãng · Viettel Post · 32.000 ₫");
    await waitFor(() => {
      expect(ok).toHaveProperty("disabled", false);
    });
  });

  it("không có quyền gửi hãng ở kho của đơn: nút mờ kèm lý do (trạng thái H)", async () => {
    latestQuotes();
    fake.grants = grantsAt(WH2, ...SENDER);
    renderPanel(confirmedOrder());
    const btn = await sendButton("Gửi hãng");
    expect(btn).toHaveProperty("disabled", true);
    expect(await within(panel()).findByText(/Bạn không có quyền gửi hãng ở kho của đơn này/)).toBeTruthy();
    expect(quoteWrites()).toHaveLength(0);
  });
});

describe("Tài khoản nhập mã tay, tự giao (VC14, HV16)", () => {
  it("chọn tài khoản nhập tay: dán mã, phí; Lưu mã vận đơn gửi trackingCode, feeActual, không quoteId", async () => {
    latestQuotes();
    manualAccounts();
    renderPanel(confirmedOrder());
    await sendButton(/^Gửi hãng · Viettel Post/);
    fireEvent.click(within(rows()).getByRole("radio", { name: /GHN — nhập mã tay/ }));
    const save = await sendButton("Lưu mã vận đơn");
    expect(save).toHaveProperty("disabled", true);
    fireEvent.change(within(panel()).getByLabelText(/Mã vận đơn của GHN/), { target: { value: " GHN5KX92MQ " } });
    fireEvent.change(within(panel()).getByLabelText(/Phí GHN báo/), { target: { value: "28.000" } });
    fireEvent.click(save);
    await waitFor(() => {
      expect(shipWrites()).toHaveLength(1);
    });
    expect(shipWrites()[0]?.body).toEqual({ carrierAccountId: A_GHN, trackingCode: "GHN5KX92MQ", feeActual: 28_000 });
  });

  it("mã trùng: câu tracking_code_conflict", async () => {
    manualAccounts();
    fake.replies.push({ status: 409, body: { error: "tracking_code_conflict", request_id: "t" } });
    renderPanel(confirmedOrder());
    fireEvent.click(await within(panel()).findByRole("radio", { name: /GHN — nhập mã tay/ }));
    fireEvent.change(within(panel()).getByLabelText(/Mã vận đơn của GHN/), { target: { value: "GHN1" } });
    fireEvent.click(await sendButton("Lưu mã vận đơn"));
    expect(await within(panel()).findByText(/Mã vận đơn của hãng đã dùng cho vận đơn khác/)).toBeTruthy();
  });

  it("chỉ có tài khoản nhập tay (chưa có tài khoản API): chọn sẵn tài khoản duy nhất, không báo phí", async () => {
    fake.accounts = [account()];
    fake.replies.push({ status: 202, body: { requestId: REQ2, accounts: [] } });
    renderPanel(confirmedOrder());
    expect(await within(panel()).findByRole("radio", { name: /GHN — nhập mã tay/ })).toHaveProperty("checked", true);
    expect(within(panel()).getByText("Không báo phí")).toBeTruthy();
    expect(within(panel()).getByText(/Kho này chưa có tài khoản hãng báo phí được/)).toBeTruthy();
  });

  it("tự giao: một nút tạo vận đơn, không ô mã", async () => {
    manualAccounts();
    renderPanel(confirmedOrder());
    fireEvent.click(await within(panel()).findByRole("radio", { name: /Shop tự giao/ }));
    expect(within(panel()).queryByLabelText(/Mã vận đơn/)).toBeNull();
    fireEvent.click(await sendButton("Tạo vận đơn tự giao"));
    await waitFor(() => {
      expect(shipWrites()).toHaveLength(1);
    });
    expect(shipWrites()[0]?.body).toEqual({ carrierAccountId: A_SELF });
  });

  it("vai Bán hàng (gửi hãng ở kho của đơn, không có quyền xem kết nối hãng): thấy tài khoản nhập tay của kho đơn, dán mã được (Task 449)", async () => {
    latestQuotes();
    manualAccounts();
    fake.grants = grantsAt(WH, "fulfillment.quote", "fulfillment.ship");
    renderPanel(confirmedOrder());
    const ghn = await within(panel()).findByRole("radio", { name: /GHN — nhập mã tay/ });
    expect(within(rows()).getByRole("radio", { name: /Shop tự giao/ })).toBeTruthy();
    expect(within(rows()).queryByRole("radio", { name: /GHN kho khác/ })).toBeNull();
    expect(within(rows()).queryByRole("radio", { name: /GHN tạm dừng/ })).toBeNull();
    expect(within(panel()).queryByText(/chỉ hiện với người được xem kết nối hãng/)).toBeNull();
    expect(fake.accountReads).toEqual([`/api/v1/orders/${O1}/shipping-accounts`]);
    fireEvent.click(ghn);
    fireEvent.change(within(panel()).getByLabelText(/Mã vận đơn của GHN/), { target: { value: "GHN77" } });
    fireEvent.click(await sendButton("Lưu mã vận đơn"));
    await waitFor(() => {
      expect(shipWrites()).toHaveLength(1);
    });
    expect(shipWrites()[0]?.body).toEqual({ carrierAccountId: A_GHN, trackingCode: "GHN77" });
  });

  it("không có quyền gửi hãng ở kho của đơn: không đọc tài khoản gửi được, không dòng nhập tay", async () => {
    latestQuotes();
    manualAccounts();
    fake.grants = grantsAt(WH2, ...SENDER);
    renderPanel(confirmedOrder());
    expect(await sendButton("Gửi hãng")).toHaveProperty("disabled", true);
    expect(within(panel()).queryByRole("radio", { name: /GHN — nhập mã tay/ })).toBeNull();
    expect(fake.accountReads).toEqual([]);
  });

  it("đọc tài khoản gửi được lỗi: câu lỗi riêng, không coi như kho không có tài khoản", async () => {
    latestQuotes();
    manualAccounts();
    fake.grants = grantsAt(WH, "fulfillment.quote", "fulfillment.ship");
    const base = vi.mocked(fetch).getMockImplementation();
    vi.mocked(fetch).mockImplementation(async (req) =>
      req instanceof Request && req.url.endsWith("/shipping-accounts")
        ? Response.json({ error: "internal", request_id: "t" }, { status: 500 })
        : (base?.(req) ?? Response.json({})),
    );
    renderPanel(confirmedOrder());
    expect(await within(panel()).findByText("Chưa đọc được tài khoản nhập mã tay của kho.")).toBeTruthy();
  });
});

describe("Kết quả lệnh tạo (VC16, HV11)", () => {
  const withShipment = (p: Parameters<typeof shipment>[0]) => {
    fake.shipments.set(SH1, shipment(p));
    return confirmedOrder({ currentShipmentId: SH1 });
  };

  it("pending: Đang gửi hãng…, có mã CRM, không nút gửi", async () => {
    renderPanel(withShipment({}));
    expect(await within(panel()).findByText("Đang gửi hãng…")).toBeTruthy();
    expect(within(panel()).getByText("E261121K7Q2MX")).toBeTruthy();
    expect(within(panel()).queryByRole("button")).toBeNull();
  });

  it("unknown: hai nút, không có Gửi lại; Hãng đã tạo đòi mã và ghi chú", async () => {
    const o = withShipment({ createState: "unknown", needsReview: true });
    renderPanel(o);
    expect(await within(panel()).findByText("Không rõ hãng đã tạo chưa")).toBeTruthy();
    expect(within(panel()).queryByRole("button", { name: /Gửi lại/ })).toBeNull();
    const made = within(panel()).getByRole("button", { name: "Hãng đã tạo" });
    const notMade = within(panel()).getByRole("button", { name: "Hãng chưa tạo" });
    expect(made).toHaveProperty("disabled", true);
    expect(within(panel()).getByText("Nhập mã vận đơn của hãng")).toBeTruthy();
    fireEvent.change(within(panel()).getByLabelText(/Mã vận đơn của hãng/), { target: { value: "1234567890123" } });
    expect(made).toHaveProperty("disabled", true);
    fireEvent.change(within(panel()).getByLabelText(/Ghi chú/), { target: { value: "Đã kiểm trên trang VTP" } });
    expect(made).toHaveProperty("disabled", false);
    expect(notMade).toHaveProperty("disabled", false);
    fireEvent.click(made);
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    expect(fake.writes[0]?.path).toBe(`/api/v1/shipments/${SH1}/resolve-create`);
    expect(fake.writes[0]?.body).toEqual({
      result: "created",
      trackingCode: "1234567890123",
      reason: "Đã kiểm trên trang VTP",
    });
  });

  it("unknown → Hãng chưa tạo: chỉ cần ghi chú, không gửi mã", async () => {
    renderPanel(withShipment({ createState: "unknown" }));
    fireEvent.change(await within(panel()).findByLabelText(/Ghi chú/), {
      target: { value: "Không thấy trên trang hãng" },
    });
    fireEvent.change(within(panel()).getByLabelText(/Mã vận đơn của hãng/), { target: { value: "123" } });
    fireEvent.click(within(panel()).getByRole("button", { name: "Hãng chưa tạo" }));
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    expect(fake.writes[0]?.body).toEqual({ result: "not_created", reason: "Không thấy trên trang hãng" });
  });

  it("not_sent: Chưa gửi được + Gửi lại do người bấm (CRM không tự gửi)", async () => {
    renderPanel(withShipment({ createState: "not_sent" }));
    const btn = await within(panel()).findByRole("button", { name: "Gửi lại" });
    expect(within(panel()).getByText("Chưa gửi được")).toBeTruthy();
    expect(fake.writes).toHaveLength(0);
    fireEvent.click(btn);
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    expect(fake.writes[0]?.path).toBe(`/api/v1/shipments/${SH1}/resend`);
    expect(fake.writes[0]?.key).toBeTruthy();
  });

  it("not_sent, người xem không có quyền gửi: Gửi lại mờ kèm lý do", async () => {
    fake.grants = grantsAt(WH2, ...SENDER);
    renderPanel(withShipment({ createState: "not_sent" }));
    expect(await within(panel()).findByRole("button", { name: "Gửi lại" })).toHaveProperty("disabled", true);
  });

  it("hãng từ chối (vận đơn đã gỡ khỏi đơn): băng Hãng từ chối kèm mã, rồi so phí lại được", async () => {
    latestQuotes();
    fake.orderShipments = [
      shipment({
        code: "E261121P3D8RA",
        status: "cancelled",
        createState: "rejected",
        cancelKind: "create_rejected",
        subStatus: "invalid_phone",
      }),
    ];
    const onEdit = vi.fn();
    renderPanel(confirmedOrder(), onEdit);
    expect(await within(panel()).findByText(/Hãng từ chối: invalid_phone/)).toBeTruthy();
    expect(within(panel()).getByText(/E261121P3D8RA/)).toBeTruthy();
    fireEvent.click(within(panel()).getByRole("button", { name: "Sửa đơn" }));
    expect(onEdit).toHaveBeenCalledOnce();
    await sendButton(/^Gửi hãng · Viettel Post/);
  });

  it("vận đơn đã tạo: khối đã gửi có mã hãng, nhãn trạng thái; báo hiệu shipment.changed đúng vận đơn thì đọc lại", async () => {
    const o = withShipment({
      status: "awaiting_pickup",
      createState: "created",
      trackingCode: "1234567890123",
      label: "Chờ thu gom",
      fee: 32_000,
    });
    const { qc } = renderPanel(o);
    expect(await within(panel()).findByText("1234567890123")).toBeTruthy();
    expect(within(panel()).getByText("Chờ thu gom")).toBeTruthy();
    fake.shipments.set(
      SH1,
      shipment({ status: "shipping", createState: "created", trackingCode: "1234567890123", label: "Đang chuyển" }),
    );
    act(() => {
      onRealtimeChanged(qc, { topic: "shipment.changed", entity: { type: "shipment", id: SH1 } });
    });
    expect(await within(panel()).findByText("Đang chuyển")).toBeTruthy();
  });
});

describe("Khi nào khối hiện", () => {
  it("đơn mới chưa xác nhận, không vận đơn: không có khối", async () => {
    renderPanel(confirmedOrder({ status: "new", label: "new" }));
    await act(async () => {
      await Promise.resolve();
    });
    expect(screen.queryByRole("region", { name: "Vận chuyển" })).toBeNull();
  });
});

describe("Hãng khách chọn trên website (Task 490)", () => {
  it("khách chọn Best (đắt hơn): dòng Khách chọn có hãng, dịch vụ, phí; chọn sẵn Best thay vì rẻ nhất; không tự gửi", async () => {
    latestQuotes();
    renderPanel(
      confirmedOrder({
        channel: "web",
        webShipping: {
          accountId: A_BEST,
          carrierKey: "best",
          carrierName: "Best Express",
          service: "STD",
          fee: 35_500,
          estimated: false,
        },
      }),
    );
    await sendButton("Gửi hãng · Best Express · 35.500 ₫");
    expect(within(panel()).getByText("Khách chọn: Best Express · STD · 35.500 ₫")).toBeTruthy();
    expect(within(rows()).getByRole("radio", { name: /Best Express — Kho chính/ })).toHaveProperty("checked", true);
    expect(shipWrites()).toHaveLength(0);
  });

  it("hãng khách chọn không có trong bảng so phí: vẫn hiện dòng Khách chọn, chọn sẵn rẻ nhất như cũ", async () => {
    latestQuotes();
    renderPanel(
      confirmedOrder({
        webShipping: { accountId: A_GHN, carrierKey: "ghn", carrierName: "GHN", fee: 29_000, estimated: false },
      }),
    );
    await sendButton("Gửi hãng · Viettel Post · 32.000 ₫");
    expect(within(panel()).getByText("Khách chọn: GHN · 29.000 ₫")).toBeTruthy();
  });

  it("phí dự phòng, đơn từ hàng đợi không còn khớp: câu riêng, nhắc kiểm lại", async () => {
    latestQuotes();
    renderPanel(confirmedOrder({ webShipping: { fee: 35_000, estimated: true, problem: "fee_mismatch" } }));
    await sendButton("Gửi hãng · Viettel Post · 32.000 ₫");
    expect(
      within(panel()).getByText("Khách chọn: phí giao dự phòng của shop (không chọn hãng) · 35.000 ₫"),
    ).toBeTruthy();
    expect(within(panel()).getByText(/Lựa chọn không còn khớp lúc CRM nhận đơn/)).toBeTruthy();
  });

  it("đơn không có lựa chọn của khách: không có dòng Khách chọn", async () => {
    latestQuotes();
    renderPanel(confirmedOrder());
    await sendButton("Gửi hãng · Viettel Post · 32.000 ₫");
    expect(within(panel()).queryByText(/Khách chọn/)).toBeNull();
  });
});
