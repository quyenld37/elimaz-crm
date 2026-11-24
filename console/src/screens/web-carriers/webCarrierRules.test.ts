// Luật thuần của thẻ Hãng trên website (Task 208).
import { describe, expect, it } from "vitest";
import { account } from "../carriers/carriersTestkit";
import {
  blockReason,
  formOf,
  isLastActive,
  nextWebSort,
  readiness,
  moveOrder,
  savedBy,
  shipmentCounts,
  sameForm,
  toRequest,
  validateForm,
  webRows,
} from "./webCarrierRules";

describe("webRows", () => {
  it("đang hiện xếp webSort rồi id; bỏ tài khoản đã thu hồi", () => {
    const rows = webRows([
      account(3, { showOnWeb: true, webSort: 2 }),
      account(1, { showOnWeb: true, webSort: 2 }),
      account(2, { showOnWeb: true, webSort: 1 }),
      account(4),
      account(5, { status: "revoked", showOnWeb: true }),
      account(6, { status: "revoked" }),
    ]);
    expect(rows.shown.map((a) => a.name)).toEqual(["Tài khoản 2", "Tài khoản 1", "Tài khoản 3"]);
    expect(rows.hidden.map((a) => a.name)).toEqual(["Tài khoản 4"]);
  });
});

describe("blockReason", () => {
  it("chỉ active bật được; tạm ngừng, cần cấp quyền lại, nháp có câu lý do", () => {
    expect(blockReason({ status: "active" })).toBeNull();
    expect(blockReason({ status: "paused" })).toContain("Tạm ngừng");
    expect(blockReason({ status: "disabled" })).toContain("Tạm ngừng");
    expect(blockReason({ status: "auth_failed" })).toContain("Cần cấp quyền lại");
    expect(blockReason({ status: "draft" })).toContain("Chưa hoạt động");
    expect(blockReason({ status: "checking" })).toContain("Chưa hoạt động");
  });
});

describe("isLastActive", () => {
  it("hãng active cuối cùng đang hiện; hãng tạm ngừng không tính", () => {
    const a = account(1, { showOnWeb: true });
    const paused = account(2, { showOnWeb: true, status: "paused" });
    expect(isLastActive([a, paused], a)).toBe(true);
    expect(isLastActive([a, paused], paused)).toBe(false);
    expect(isLastActive([a, account(3, { showOnWeb: true })], a)).toBe(false);
  });
});

describe("nextWebSort, moveOrder", () => {
  it("bật thêm: đứng cuối; rỗng → 1; không quá 1000", () => {
    expect(nextWebSort([])).toBe(1);
    expect(nextWebSort([account(1, { webSort: 4 }), account(2, { webSort: 2 })])).toBe(5);
    expect(nextWebSort([account(1, { webSort: 1000 })])).toBe(1000);
  });

  it("đổi chỗ hai dòng kề: trả cả danh sách theo thứ tự mới; ra ngoài danh sách → []", () => {
    const shown = [account(1, { webSort: 1 }), account(2, { webSort: 2 }), account(3, { webSort: 3 })];
    expect(moveOrder(shown, 1, -1)).toEqual([shown[1]?.id, shown[0]?.id, shown[2]?.id]);
    expect(moveOrder(shown, 0, 1)).toEqual([shown[1]?.id, shown[0]?.id, shown[2]?.id]);
    expect(moveOrder(shown, 2, 1)).toEqual([]);
    expect(moveOrder(shown, 0, -1)).toEqual([]);
    expect(moveOrder([], 0, 1)).toEqual([]);
  });
});

describe("số vận đơn, người lưu (Task 488)", () => {
  const st = (id: string, n: number) => ({
    accountId: id,
    carrierKey: "vtp",
    name: id,
    status: "active",
    webSort: 1,
    preferred: false,
    shipments30d: n,
  });
  it("số vận đơn gồm cả hãng chưa hiện", () => {
    const m = shipmentCounts({ version: 1, carriers: [st("a", 3)], hiddenCarriers: [st("b", 7)] });
    expect([...m]).toEqual([
      ["a", 3],
      ["b", 7],
    ]);
    expect(shipmentCounts(undefined).size).toBe(0);
  });
  it("người, giờ lưu chỉ khi bản tải lại mới hơn bản gửi; thiếu tên → chỉ giờ; thiếu giờ → null", () => {
    const t = (iso: string) => `T(${iso})`;
    const s = { version: 6, carriers: [], hiddenCarriers: [], updatedAt: "x", updatedByName: "Thư" };
    expect(savedBy(s, 5, t)).toBe("Thư, T(x)");
    expect(savedBy(s, 6, t)).toBeNull();
    expect(savedBy({ ...s, updatedByName: undefined }, 5, t)).toBe("lần lưu cuối lúc T(x)");
    expect(savedBy({ ...s, updatedAt: undefined }, 5, t)).toBeNull();
    expect(savedBy(undefined, 5, t)).toBeNull();
  });
});

describe("form chọn sẵn, phí dự phòng", () => {
  const S = { version: 4, carriers: [], hiddenCarriers: [] };

  it("đọc cài đặt: chưa có → rẻ nhất, tắt phí dự phòng; có → điền số", () => {
    expect(formOf(S)).toEqual({ preferred: "", useFallback: false, fee: "" });
    expect(formOf({ ...S, preferredAccountId: "a", fallbackFee: 30000 })).toEqual({
      preferred: "a",
      useFallback: true,
      fee: "30.000",
    });
  });

  it("gửi: không chọn sẵn, tắt phí → chỉ version; bật phí 30.000 → 30000", () => {
    expect(toRequest({ preferred: "", useFallback: false, fee: "30.000" }, 4)).toEqual({ version: 4 });
    expect(toRequest({ preferred: "a", useFallback: true, fee: "30.000" }, 4)).toEqual({
      version: 4,
      preferredAccountId: "a",
      fallbackFee: 30000,
    });
    expect(toRequest({ preferred: "", useFallback: true, fee: "0" }, 4)).toEqual({ version: 4, fallbackFee: 0 });
  });

  it("kiểm: chọn sẵn phải đang hiện; phí sai dạng, quá 500.000 báo lỗi", () => {
    expect(validateForm({ preferred: "x", useFallback: false, fee: "" }, ["a"]).preferred).toContain("đang hiện");
    expect(validateForm({ preferred: "a", useFallback: false, fee: "abc" }, ["a"])).toEqual({});
    expect(validateForm({ preferred: "", useFallback: true, fee: "abc" }, []).fee).toContain("Nhập số tiền");
    expect(validateForm({ preferred: "", useFallback: true, fee: "" }, []).fee).toContain("Nhập số tiền");
    expect(validateForm({ preferred: "", useFallback: true, fee: "500.001" }, []).fee).toContain("500.000");
    expect(validateForm({ preferred: "", useFallback: true, fee: "500.000" }, [])).toEqual({});
  });

  it("sameForm: phí khi tắt không tính; 30000 = 30.000", () => {
    const a = { preferred: "", useFallback: true, fee: "30.000" };
    expect(sameForm(a, { ...a, fee: "30000" })).toBe(true);
    expect(sameForm(a, { ...a, fee: "31.000" })).toBe(false);
    expect(sameForm({ ...a, useFallback: false }, { ...a, useFallback: false, fee: "1" })).toBe(true);
  });

  it("readiness: cần một hãng active đang hiện và phí dự phòng", () => {
    expect(readiness([account(1, { showOnWeb: true, status: "paused" })], S)).toEqual({
      carrier: false,
      fallback: false,
    });
    expect(readiness([account(1, { showOnWeb: true })], { ...S, fallbackFee: 0 })).toEqual({
      carrier: true,
      fallback: true,
    });
  });
});
