// Luật thuần của màn Khách: đơn vị của quy tắc nhãn, màu nhãn, câu lỗi nhập, đánh dấu đuôi SĐT, lần gộp từ dòng thời
// gian và hạn hoàn tác 30 ngày (Q14).
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import type { TimelineItem } from "../../api";
import {
  IMPORT_ERROR_TEXT,
  TAG_COLORS,
  TAG_FIELDS,
  importErrorText,
  markTail,
  mergeEntries,
  tagChip,
  toDisplayValue,
  toServerValue,
  undoDeadline,
  undoState,
} from "./customerRules";

const goSource = (file: string) =>
  readFileSync(new URL(`../../../../internal/modules/customer/${file}`, import.meta.url), "utf8");

describe("quy tắc nhãn", () => {
  it("đủ 6 chỉ số máy chủ nhận, đúng tên trường của TagCondition", () => {
    expect(TAG_FIELDS.map((f) => f.field).sort()).toEqual(
      [
        "delivered_count",
        "days_since_last_order",
        "order_count",
        "return_rate_bp",
        "returned_count",
        "total_spent",
      ].sort(),
    );
  });

  it("tỷ lệ hoàn nhập theo %, máy chủ nhận phần vạn; chỉ số khác là số nguyên", () => {
    expect(toServerValue("return_rate_bp", "30")).toBe(3000);
    expect(toServerValue("return_rate_bp", "12,5")).toBe(1250);
    expect(toServerValue("return_rate_bp", "12.34")).toBe(1234);
    expect(toServerValue("return_rate_bp", "100")).toBe(10000);
    expect(toServerValue("return_rate_bp", "101")).toBeNull();
    expect(toServerValue("return_rate_bp", "1,234")).toBeNull();
    expect(toServerValue("total_spent", "5.000.000")).toBe(5_000_000);
    expect(toServerValue("order_count", "2")).toBe(2);
    expect(toServerValue("order_count", "2,5")).toBeNull();
    expect(toServerValue("order_count", "-1")).toBeNull();
    expect(toServerValue("order_count", "")).toBeNull();
    expect(toServerValue("total_spent", "9007199254740992")).toBeNull();
    expect(toDisplayValue("return_rate_bp", 3000)).toBe("30");
    expect(toDisplayValue("return_rate_bp", 1250)).toBe("12,5");
    expect(toDisplayValue("total_spent", 5_000_000)).toBe("5.000.000");
    expect(toDisplayValue("order_count", 2)).toBe("2");
  });

  it("10 khóa màu đúng TagColors() của máy chủ; mỗi màu ra một lớp chip có sẵn", () => {
    const m = /TagColors\(\) \[\]string \{\s*return \[\]string\{([^}]*)\}/.exec(goSource("tags.go"));
    const server = [...(m?.[1] ?? "").matchAll(/"([a-z]+)"/g)].map((x) => x[1]);
    expect(TAG_COLORS.map((c) => c.key)).toEqual(server);
    for (const c of TAG_COLORS) expect(tagChip(c.key)).toMatch(/^chip( chip-(line|err|ok|dark))?$/);
    expect(tagChip("khong-co")).toBe("chip chip-line");
  });
});

describe("nhập khách", () => {
  it("mọi mã lỗi dòng và lỗi tệp của máy chủ có câu tiếng Việt", () => {
    const codes = [...goSource("importer_rows.go").matchAll(/row\w+\s*=\s*"([a-z_]+)"/g)].map((x) => x[1]);
    const fileCodes = [...goSource("importer.go").matchAll(/codeImport\w+\s*=\s*"([a-z_]+)"/g)].map((x) => x[1]);
    expect(codes.length).toBeGreaterThan(5);
    for (const c of [...codes, ...fileCodes]) {
      expect(c !== undefined && Object.hasOwn(IMPORT_ERROR_TEXT, c), `thiếu câu cho ${String(c)}`).toBe(true);
    }
    expect(importErrorText("invalid_phone")).toBe("SĐT không hợp lệ");
    expect(importErrorText("ma_moi")).toBe("Dòng không hợp lệ (ma_moi)");
  });
});

describe("đánh dấu đuôi SĐT khớp", () => {
  it("đánh dấu đúng n chữ số cuối, kể cả khoảng trắng xen giữa", () => {
    expect(markTail("0912 345 678", 4)).toEqual(["0912 34", "5 678"]);
    expect(markTail("0912 345 678", 3)).toEqual(["0912 345 ", "678"]);
    expect(markTail("0912 345 678", 10)).toEqual(["", "0912 345 678"]);
    expect(markTail("0912 345 678", 0)).toEqual(["0912 345 678", ""]);
  });
});

describe("lần gộp", () => {
  const items: TimelineItem[] = [
    {
      at: "2026-11-18T08:40:00Z",
      kind: "merge",
      refId: "m2",
      actor: { id: "u", name: "Lan" },
      summary: { survivor_id: "c2", merged_id: "c1", matched_by: "staff" },
    },
    { at: "2026-11-10T01:00:00Z", kind: "note", refId: "n1", note: "x" },
    { at: "2026-11-05T01:00:00Z", kind: "merge_undone", refId: "m0", summary: { survivor_id: "c1", merged_id: "c9" } },
    {
      at: "2026-11-01T08:00:00Z",
      kind: "merge",
      refId: "m1",
      actor: { id: "u", name: "Lan" },
      summary: { survivor_id: "c1", merged_id: "c3", matched_by: "phone" },
    },
    { at: "2026-10-01T08:00:00Z", kind: "merge", refId: "m0", summary: { survivor_id: "c1", merged_id: "c9" } },
  ];

  it("lấy mục merge, gắn lúc hoàn tác từ mục merge_undone cùng refId; mới nhất trước", () => {
    expect(mergeEntries(items)).toEqual([
      { id: "m2", at: "2026-11-18T08:40:00Z", survivorId: "c2", mergedId: "c1", actor: "Lan", undoneAt: undefined },
      { id: "m1", at: "2026-11-01T08:00:00Z", survivorId: "c1", mergedId: "c3", actor: "Lan", undoneAt: undefined },
      {
        id: "m0",
        at: "2026-10-01T08:00:00Z",
        survivorId: "c1",
        mergedId: "c9",
        actor: "",
        undoneAt: "2026-11-05T01:00:00Z",
      },
    ]);
  });

  it("hạn hoàn tác 30 ngày tính từ lúc gộp", () => {
    const now = Date.parse("2026-11-19T00:00:00Z");
    expect(undoDeadline("2026-11-18T08:40:00Z").toISOString()).toBe("2026-12-18T08:40:00.000Z");
    const [m2, m1, m0] = mergeEntries(items);
    expect(m2 && undoState(m2, now)).toBe("open");
    expect(m1 && undoState(m1, now)).toBe("open");
    expect(m0 && undoState(m0, now)).toBe("undone");
    expect(m1 && undoState(m1, Date.parse("2026-12-01T08:00:01Z"))).toBe("expired");
  });
});
