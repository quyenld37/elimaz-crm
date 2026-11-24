// Luật thuần của màn Chiến dịch giảm giá (kế hoạch M5 Task 458): chữ trạng thái, thao tác theo trạng thái, liên kết
// website, kiểm form và dựng thân gửi API (giờ Việt Nam → ISO).
import { describe, expect, it } from "vitest";
import type { WebCampaign } from "../../api";
import {
  NAME_MAX,
  TARGET_MAX,
  campaignLink,
  draftOf,
  emptyDraft,
  linkText,
  periodText,
  rowActions,
  rowSub,
  sameAsSaved,
  statusChip,
  toInput,
  validateDraft,
  type CampaignDraft,
} from "./campaignRules";

const T1 = "0195f3b2-0000-7000-8000-0000000c0001";
const T2 = "0195f3b2-0000-7000-8000-0000000c0002";

function campaign(p: Partial<WebCampaign> = {}): WebCampaign {
  return {
    id: "0195f3b2-0000-7000-8000-00000000e001",
    name: "Xả hàng thu đông",
    percent: 50,
    scope: "products",
    targetIds: [T1, T2],
    allowCoupon: false,
    clearance: true,
    startsAt: "2026-11-14T17:00:00Z",
    endsAt: "2026-11-25T16:59:00Z",
    state: "enabled",
    status: "active",
    revision: 3,
    linkKey: "0193a7c2",
    path: "/uu-dai/xa-hang-thu-dong-0193a7c2",
    createdAt: "2026-11-10T00:00:00Z",
    updatedAt: "2026-11-10T00:00:00Z",
    ...p,
  };
}

const valid = (p: Partial<CampaignDraft> = {}): CampaignDraft => ({
  ...emptyDraft(),
  name: "  Tết 2027  ",
  percent: "10",
  scope: "categories",
  targets: [{ id: T1, code: "dam-cong-so", name: "Đầm công sở" }],
  start: "2027-01-05T00:00",
  end: "2027-01-25T23:59",
  ...p,
});

describe("hiển thị", () => {
  it("chip theo trạng thái hiển thị", () => {
    expect(statusChip("active")).toEqual({ label: "Đang áp dụng", className: "chip chip-ok" });
    expect(statusChip("scheduled")).toEqual({ label: "Đã hẹn giờ", className: "chip chip-line" });
    expect(statusChip("paused").label).toBe("Tạm dừng");
    expect(statusChip("draft").label).toBe("Bản nháp");
    expect(statusChip("ended").label).toBe("Đã hết hạn");
  });

  it("dòng phụ: phạm vi, hàng cắt mã, dùng kèm mã", () => {
    expect(rowSub(campaign())).toBe("2 sản phẩm · Hàng cắt mã");
    expect(rowSub(campaign({ scope: "categories", clearance: false, allowCoupon: true }))).toBe(
      "2 danh mục · dùng kèm mã giảm giá",
    );
    expect(rowSub(campaign({ scope: "all", targetIds: [], clearance: false }))).toBe("Toàn bộ sản phẩm");
  });

  it("thời gian theo giờ Việt Nam", () => {
    expect(periodText(campaign())).toBe("00:00 15/11 → 23:59 25/11");
  });

  it("liên kết chép: tên miền website + path; chữ báo bỏ https://", () => {
    expect(campaignLink(campaign())).toBe("https://elimaz.com.vn/uu-dai/xa-hang-thu-dong-0193a7c2");
    expect(linkText(campaign())).toBe("elimaz.com.vn/uu-dai/xa-hang-thu-dong-0193a7c2");
  });
});

describe("thao tác theo trạng thái", () => {
  it("đang bật (áp dụng, hẹn giờ): Sửa (tạm dừng để sửa), Tạm dừng, Chép liên kết", () => {
    expect(rowActions(campaign(), true)).toEqual(["pauseEdit", "pause", "copy"]);
    expect(rowActions(campaign({ status: "scheduled" }), true)).toEqual(["pauseEdit", "pause", "copy"]);
  });
  it("đang bật nhưng đã hết hạn: không chép liên kết", () => {
    expect(rowActions(campaign({ status: "ended" }), true)).toEqual(["pauseEdit", "pause"]);
  });
  it("nháp, tạm dừng: Sửa, Kích hoạt, Xóa", () => {
    expect(rowActions(campaign({ state: "draft", status: "draft" }), true)).toEqual(["edit", "enable", "delete"]);
    expect(rowActions(campaign({ state: "paused", status: "paused" }), true)).toEqual(["edit", "enable", "delete"]);
  });
  it("chỉ có quyền xem: chỉ Chép liên kết của chiến dịch đang bật chưa hết hạn", () => {
    expect(rowActions(campaign(), false)).toEqual(["copy"]);
    expect(rowActions(campaign({ status: "ended" }), false)).toEqual([]);
    expect(rowActions(campaign({ state: "paused", status: "paused" }), false)).toEqual([]);
  });
});

describe("form", () => {
  it("hợp lệ → thân API: tên trim, phần trăm số, giờ VN → ISO UTC, mã đích", () => {
    expect(validateDraft(valid())).toEqual({});
    expect(toInput(valid())).toEqual({
      name: "Tết 2027",
      percent: 10,
      scope: "categories",
      targetIds: [T1],
      allowCoupon: false,
      clearance: false,
      startsAt: "2027-01-04T17:00:00.000Z",
      endsAt: "2027-01-25T16:59:00.000Z",
    });
  });

  it("toàn bộ sản phẩm: không gửi đích", () => {
    expect(toInput(valid({ scope: "all", targets: [] }))?.targetIds).toEqual([]);
  });

  it("mỗi ô sai một câu; sai thì không dựng thân", () => {
    const e = validateDraft({ ...emptyDraft(), percent: "100", start: "2027-01-05T00:00", end: "2027-01-05T00:00" });
    expect(e).toEqual({
      name: "Nhập tên chiến dịch",
      percent: "Phần trăm là số nguyên 1–99",
      targets: "Chọn ít nhất một danh mục",
      end: "Kết thúc phải sau bắt đầu",
    });
    expect(toInput({ ...emptyDraft() })).toBeNull();
    expect(validateDraft(valid({ name: "a".repeat(NAME_MAX + 1) })).name).toBe("Tên tối đa 120 ký tự");
    expect(validateDraft(valid({ name: "a".repeat(NAME_MAX) })).name).toBeUndefined();
    expect(validateDraft(valid({ percent: "12,5" })).percent).toBe("Phần trăm là số nguyên 1–99");
    expect(validateDraft(valid({ scope: "products", targets: [] })).targets).toBe("Chọn ít nhất một sản phẩm");
    const many = Array.from({ length: TARGET_MAX + 1 }, (_, i) => ({ id: String(i), code: "", name: "" }));
    expect(validateDraft(valid({ targets: many })).targets).toBe("Tối đa 30 mục");
    expect(validateDraft(valid({ start: "" })).start).toBe("Chọn giờ bắt đầu");
    expect(validateDraft(valid({ end: "" })).end).toBe("Chọn giờ kết thúc");
  });

  it("draftOf ngược với toInput; sameAsSaved so không kể thứ tự đích và dạng ISO", () => {
    const c = campaign({ state: "paused", status: "paused" });
    const d = draftOf(c, [
      { id: T2, code: "b", name: "B" },
      { id: T1, code: "a", name: "A" },
    ]);
    expect(d).toMatchObject({ name: c.name, percent: "50", scope: "products", start: "2026-11-15T00:00" });
    expect(d.end).toBe("2026-11-25T23:59");
    expect(sameAsSaved(d, c)).toBe(true);
    expect(sameAsSaved({ ...d, percent: "40" }, c)).toBe(false);
    expect(sameAsSaved({ ...d, targets: d.targets.slice(1) }, c)).toBe(false);
    expect(sameAsSaved({ ...d, name: "" }, c)).toBe(false);
  });
});
