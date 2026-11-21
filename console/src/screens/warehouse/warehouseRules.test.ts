// @vitest-environment jsdom
// Quy tắc thuần của màn Kho (HV23, HV24): câu thanh chọn nhiều, câu sau đóng gói, câu vận đơn bỏ qua khi in nhãn,
// đường dẫn tệp nhãn an toàn.
import { describe, expect, it } from "vitest";
import { packRow } from "./warehouseTestkit";
import { packingMessage, safeFileUrl, selectionText, skippedMessages } from "./warehouseRules";

describe("selectionText", () => {
  it("gom theo hãng theo thứ tự xuất hiện; tự giao viết thường", () => {
    const rows = [packRow(1), packRow(2, {}, { carrierKey: "self" }), packRow(3), packRow(4, {}, { carrierKey: "lạ" })];
    expect(selectionText(rows)).toBe("Đã chọn 4 vận đơn · Viettel Post 2, tự giao 1, lạ 1");
  });
});

describe("packingMessage", () => {
  it("chỉ cập nhật: một câu", () => {
    const r = packRow(1);
    expect(packingMessage("packed", [{ orderId: r.id, result: "updated" }], new Map([[r.id, r]]))).toBe(
      'Đã đánh dấu 1 đơn "Đã đóng gói".',
    );
  });
  it("kể đơn đã ở bước sau kèm trạng thái, đơn không đổi được", () => {
    const a = packRow(1, { packingStatus: "packed" });
    const b = packRow(2);
    const m = packingMessage(
      "picking",
      [
        { orderId: a.id, result: "already_further" },
        { orderId: b.id, result: "not_allowed" },
      ],
      new Map([
        [a.id, a],
        [b.id, b],
      ]),
    );
    expect(m).toBe(
      'Đã đánh dấu 0 đơn "Đang đóng gói". 1 đơn đã ở bước sau (DH0001201 · Đã đóng gói), không đổi. 1 đơn không đổi được (DH0001202): không còn chờ thu gom hoặc ngoài kho bạn được giao.',
    );
  });
});

describe("skippedMessages", () => {
  it("gom theo lý do: tự giao, nhập tay, chưa có mã hãng, đã hủy", () => {
    const carrier: Record<string, string> = { s1: "self", s2: "vtp", s3: "vtp", s4: "best", s5: "self" };
    const code = (id: string) => `DH-${id}`;
    expect(
      skippedMessages(
        [
          { shipmentId: "s1", reason: "no_carrier_label" },
          { shipmentId: "s2", reason: "no_carrier_label" },
          { shipmentId: "s3", reason: "no_tracking_code" },
          { shipmentId: "s4", reason: "cancelled" },
          { shipmentId: "s5", reason: "no_carrier_label" },
        ],
        (id) => carrier[id] ?? "",
        code,
      ),
    ).toEqual([
      "2 vận đơn tự giao không có nhãn hãng (DH-s1, DH-s5).",
      "1 vận đơn nhập tay không có nhãn hãng (DH-s2).",
      "1 vận đơn chưa có mã của hãng, chưa in được (DH-s3).",
      "1 vận đơn đã hủy, không in (DH-s4).",
    ]);
  });
});

describe("safeFileUrl", () => {
  it.each([
    ["https://f.example/a.pdf?x=1", "https://f.example/a.pdf?x=1"],
    ["javascript:alert(1)", null],
    ["data:text/html,x", null],
  ])("%j → %j", (u, want) => {
    expect(safeFileUrl(u)).toBe(want);
  });
  it("không có đường dẫn → null", () => {
    expect(safeFileUrl(undefined)).toBeNull();
  });
});
