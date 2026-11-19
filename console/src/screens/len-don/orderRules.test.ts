// Luật chữ và dựng thân yêu cầu của màn Lên đơn (kế hoạch M2 Task M2-27; mockup docs/mockups/m2/len-don.html).
import { describe, expect, it } from "vitest";
import { COUPON_REASON_MESSAGES } from "../../app/errors";
import {
  EMPTY_ADDRESS,
  TEXT_NEED_ADDRESS,
  TEXT_NEW_CUSTOMER_NAME,
  TEXT_PARTIAL_ADDRESS,
  TEXT_NEED_CUSTOMER_AND_LINES,
  addressState,
  buildRequest,
  cleanPhone,
  couponReasonText,
  customerQuery,
  displayPhone,
  shortDate,
  maskPhone,
  saveBlock,
  type FormState,
} from "./orderRules";

describe("customerQuery (HV05, luật tìm của customer.Search)", () => {
  it.each([
    ["", { kind: "empty" }],
    ["   ", { kind: "empty" }],
    ["567", { kind: "short", text: "Gõ ít nhất 4 số cuối" }],
    ["0.12", { kind: "short", text: "Gõ ít nhất 4 số cuối" }],
    ["a", { kind: "short", text: "Gõ ít nhất 2 ký tự của tên" }],
    ["5678", { kind: "query", q: "5678" }],
    ["0912 345 678", { kind: "query", q: "0912 345 678" }],
    ["KH000231", { kind: "query", q: "KH000231" }],
    [" thu ha ", { kind: "query", q: "thu ha" }],
  ])("%j → %j", (text, want) => {
    expect(customerQuery(text)).toEqual(want);
  });
});

describe("maskPhone (gợi ý che giữa SĐT tới khi chọn)", () => {
  it("chỉ hiện 4 số cuối", () => {
    expect(maskPhone("+84912345678")).toBe("•••• •• 5678");
    expect(maskPhone("0912.345.678")).toBe("•••• •• 5678");
  });
  it("số quá ngắn: che hết", () => {
    expect(maskPhone("123")).toBe("••••");
  });
});

describe("displayPhone, shortDate", () => {
  it("SĐT +84 → 0xxx xxx xxx; dạng khác giữ nguyên", () => {
    expect(displayPhone("+84912345678")).toBe("0912 345 678");
    expect(displayPhone("0912345678")).toBe("0912 345 678");
    expect(displayPhone("+6591234567")).toBe("+6591234567");
  });
  it("ngày theo giờ Việt Nam", () => {
    expect(shortDate("2026-11-17T18:30:00Z")).toBe("18/11");
    expect(shortDate("x")).toBe("");
  });
});

describe("cleanPhone", () => {
  it("bỏ khoảng trắng, chấm, gạch; giữ + đầu", () => {
    expect(cleanPhone("0987 654.321")).toBe("0987654321");
    expect(cleanPhone("+84-987-654-321")).toBe("+84987654321");
  });
});

describe("couponReasonText: câu cho từng lý do (bảng trạng thái F của mockup)", () => {
  it("đủ 10 lý do của CouponRejectReason, câu đúng mockup", () => {
    expect(COUPON_REASON_MESSAGES).toEqual({
      not_found: "Không có mã này",
      disabled: "Mã đã bị tắt",
      not_started: "Mã chưa tới ngày dùng",
      expired: "Mã đã hết hạn",
      exhausted: "Mã đã hết lượt dùng",
      min_subtotal: "Đơn chưa đủ giá trị tối thiểu của mã",
      channel: "Mã không dùng cho kênh này",
      bound_other: "Mã dành riêng cho khách khác",
      customer_limit: "Khách đã dùng hết lượt của mã này",
      seller: "Mã không áp cho sản phẩm của người bán này",
    });
  });
  it("lý do lạ → câu chung của coupon_rejected", () => {
    expect(couponReasonText("expired")).toBe("Mã đã hết hạn");
    expect(couponReasonText("la")).toBe("Mã giảm giá không dùng được cho đơn này");
    expect(couponReasonText(undefined)).toBe("Mã giảm giá không dùng được cho đơn này");
  });
});

const V1 = "0195f3b2-0000-7000-8000-00000000b001";
const C1 = "0195f3b2-0000-7000-8000-00000000c001";
const S1 = "0195f3b2-0000-7000-8000-00000000f001";

function form(p: Partial<FormState> = {}): FormState {
  return {
    customer: { kind: "existing", id: C1 },
    lines: [{ variantId: V1, qty: 2, lineDiscount: 100000 }],
    address: { name: "Nguyễn Văn Tùng", phone: "0987 654 321", line: "Số 12", provinceCode: "01", wardCode: "00001" },
    phoneIsCustomers: false,
    orderDiscount: 0,
    couponCode: "",
    shippingFee: 30000,
    trafficSourceId: S1,
    carrierNote: "",
    ...p,
  };
}

describe("addressState", () => {
  it("đủ, rỗng, thiếu", () => {
    expect(addressState(form().address)).toBe("complete");
    expect(addressState({ name: "", phone: "", line: "", provinceCode: "", wardCode: "" })).toBe("empty");
    expect(addressState({ ...form().address, wardCode: "" })).toBe("partial");
    expect(addressState({ ...form().address, line: " " })).toBe("partial");
  });
});

describe("saveBlock (nút mờ kèm lý do, HV04)", () => {
  it("chưa khách hoặc 0 dòng → mọi nút mờ", () => {
    expect(saveBlock(form({ customer: null }), "draft")).toBe(TEXT_NEED_CUSTOMER_AND_LINES);
    expect(saveBlock(form({ lines: [] }), "new")).toBe(TEXT_NEED_CUSTOMER_AND_LINES);
  });
  it("xác nhận cần địa chỉ đủ tỉnh, xã (máy chủ trả address_incomplete); nháp, mới thì không", () => {
    const f = form({ address: { ...form().address, provinceCode: "", wardCode: "" } });
    expect(saveBlock(f, "confirmed")).toBe(TEXT_NEED_ADDRESS);
    expect(saveBlock(f, "new")).toBeNull();
    expect(saveBlock(f, "draft")).toBeNull();
  });
  it("địa chỉ rỗng hẳn: lưu mới, nháp được", () => {
    const f = form({ address: { name: "", phone: "", line: "", provinceCode: "", wardCode: "" } });
    expect(saveBlock(f, "new")).toBeNull();
    expect(saveBlock(f, "confirmed")).toBe(TEXT_NEED_ADDRESS);
  });
  it("có người nhận mà thiếu số nhà: mọi nút cần đủ người nhận, SĐT, số nhà", () => {
    const f = form({ address: { ...form().address, line: "" } });
    expect(saveBlock(f, "draft")).toBe(TEXT_PARTIAL_ADDRESS);
    expect(saveBlock(f, "confirmed")).toBe(TEXT_NEED_ADDRESS);
  });
  it("khách mới cần tên người nhận làm tên khách", () => {
    const f = form({ customer: { kind: "new" }, address: { ...form().address, name: "" } });
    expect(saveBlock(f, "draft")).toBe(TEXT_PARTIAL_ADDRESS);
    const g = form({ customer: { kind: "new" }, address: EMPTY_ADDRESS });
    expect(saveBlock(g, "draft")).toBe(TEXT_NEW_CUSTOMER_NAME);
  });
  it("đủ → không chặn", () => {
    expect(saveBlock(form(), "confirmed")).toBeNull();
  });
});

describe("buildRequest", () => {
  it("khách có sẵn, đủ địa chỉ, mã, nguồn, ghi chú hãng", () => {
    expect(
      buildRequest(form({ couponCode: " thu10 ", carrierNote: " Gọi trước ", orderDiscount: 5000 }), "confirmed"),
    ).toEqual({
      customerId: C1,
      intent: "confirmed",
      items: [{ variantId: V1, qty: 2, lineDiscount: 100000 }],
      shipAddress: {
        name: "Nguyễn Văn Tùng",
        phone: "0987654321",
        line: "Số 12",
        provinceCode: "01",
        wardCode: "00001",
      },
      shippingFee: 30000,
      orderDiscount: 5000,
      couponCode: "THU10",
      trafficSourceId: S1,
      carrierNote: "Gọi trước",
      channel: "manual",
    });
  });
  it("khách mới: tên người nhận làm tên khách, SĐT người nhận, cờ SĐT của khách", () => {
    const b = buildRequest(form({ customer: { kind: "new" }, phoneIsCustomers: true }), "new");
    expect(b.customerId).toBeUndefined();
    expect(b.customer).toEqual({ fullName: "Nguyễn Văn Tùng", phone: "0987654321", phoneIsCustomers: true });
  });
  it("địa chỉ rỗng → không gửi shipAddress; dòng không giảm → không gửi lineDiscount; thiếu tỉnh xã → không gửi mã", () => {
    const b = buildRequest(
      form({
        address: { name: "", phone: "", line: "", provinceCode: "", wardCode: "" },
        lines: [{ variantId: V1, qty: 1, lineDiscount: 0 }],
        trafficSourceId: "",
      }),
      "draft",
    );
    expect(b.shipAddress).toBeUndefined();
    expect(b.items).toEqual([{ variantId: V1, qty: 1 }]);
    expect(b.trafficSourceId).toBeUndefined();
    expect(b.orderDiscount).toBeUndefined();
    const c = buildRequest(form({ address: { ...form().address, wardCode: "" } }), "new");
    expect(c.shipAddress).toEqual({ name: "Nguyễn Văn Tùng", phone: "0987654321", line: "Số 12", provinceCode: "01" });
  });
});
