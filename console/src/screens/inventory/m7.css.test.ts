// Bố cục của hai màn M7 (Task 273) ở 360 px không cuộn ngang: jsdom không dàn trang nên kiểm các luật CSS giữ cột co
// được (minmax(0, 1fr), min-width: 0, tự xuống dòng mã dài), và khớp từng lớp với mockup đã duyệt docs/mockups/m7/m7.css.
// Kiểm bằng trình duyệt thật ở e2e/mo-so-hang-hoan.phone.spec.ts (viết sẵn, chạy khi xong M7).
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

// Prettier xuống dòng từng khai báo; so sánh sau khi gộp khoảng trắng.
const flat = (t: string) => t.replace(/\s+/g, " ");
const css = flat(readFileSync(join(import.meta.dirname, "m7.css"), "utf8"));
const mock = flat(readFileSync(join(import.meta.dirname, "../../../../docs/mockups/m7/m7.css"), "utf8"));

function rule(src: string, selector: string): string {
  const i = src.indexOf(`${selector} {`);
  if (i < 0) return "";
  return src.slice(i, src.indexOf("}", i) + 1);
}

describe("m7.css", () => {
  it.each([".qrow", ".lrow", ".cand", ".prog"])("%s: cột đầu co được (không đẩy trang rộng ra)", (sel) => {
    const r = rule(css, sel);
    expect(r).toMatch(/minmax\(0, 1fr\)|min-width: 0/);
  });
  it("mã hàng dài tự xuống dòng ở .qrow .sku và .lrow .sku", () => {
    expect(rule(css, ".qrow .sku")).toContain("overflow-wrap: anywhere");
    expect(rule(css, ".lrow .sku")).toContain("overflow-wrap: anywhere");
  });
  it("điện thoại: dòng hàng hoàn xếp một cột (tình trạng chiếm cả dòng)", () => {
    expect(css).toMatch(/@media \(max-width: 759px\)[\s\S]*\.lrow \{ grid-template-columns: minmax\(0, 1fr\) auto; \}/);
  });
  it.each([".prog", ".big-acts", ".qrow", ".cands", ".cand", ".opts", ".opt", ".cond", ".lrow", ".day"])(
    "%s giống nguyên mockup đã duyệt",
    (sel) => {
      expect(rule(css, sel)).toBe(rule(mock, sel));
      expect(rule(css, sel)).not.toBe("");
    },
  );
});
