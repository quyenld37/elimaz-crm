// Quy luật thuần của màn Inbox (Task 174): gộp lần tải theo id, bộ lọc trên URL, chữ số chưa đọc, giờ, nhãn.
import { describe, expect, it } from "vitest";
import {
  DEFAULT_FILTERS,
  isDefault,
  listQuery,
  mergeRows,
  readFilters,
  timeText,
  unreadText,
  visibleTags,
  writeFilters,
} from "./inboxRules";
import { conv, tag } from "./inboxTestkit";

describe("mergeRows", () => {
  it("cùng id ở hai lần tải → một dòng, lấy bản tải sau; sắp (lastMessageAt, id) giảm dần", () => {
    const a = conv(1, { lastMessageAt: "2026-11-26T08:00:00Z", unreadCount: 0 });
    const b = conv(2, { lastMessageAt: "2026-11-26T09:00:00Z" });
    const c = conv(3, { lastMessageAt: "2026-11-26T09:00:00Z" });
    const a2 = { ...a, lastMessageAt: "2026-11-26T10:00:00Z", unreadCount: 2 };
    const rows = mergeRows([
      { at: 100, rows: [b, a, c] },
      { at: 200, rows: [a2] },
    ]);
    expect(rows.map((r) => r.id)).toEqual([a.id, c.id, b.id]);
    expect(rows[0]?.unreadCount).toBe(2);
  });

  it("bản cũ hơn (tải trước) không đè bản mới", () => {
    const a = conv(1, { unreadCount: 5 });
    const old = { ...a, unreadCount: 1 };
    expect(
      mergeRows([
        { at: 300, rows: [a] },
        { at: 100, rows: [old] },
      ])[0]?.unreadCount,
    ).toBe(5);
  });
});

describe("bộ lọc trên URL", () => {
  it("mặc định: Đang mở, mọi người, mọi nhãn, tin và comment; không ghi tham số nào", () => {
    expect(readFilters(new URLSearchParams())).toEqual(DEFAULT_FILTERS);
    expect(writeFilters(DEFAULT_FILTERS).toString()).toBe("");
    expect(isDefault(DEFAULT_FILTERS)).toBe(true);
  });

  it("đọc lại đúng mọi trường đã ghi; giá trị lạ về mặc định", () => {
    const f = {
      page: "p1",
      status: "all",
      assignee: "me",
      tag: "t1",
      kind: "comment",
      unread: true,
      q: "5678",
    } as const;
    expect(readFilters(writeFilters(f))).toEqual(f);
    expect(isDefault(f)).toBe(false);
    expect(readFilters(new URLSearchParams("status=xyz&kind=abc&unread=2"))).toEqual(DEFAULT_FILTERS);
  });

  it("listQuery: Của tôi → assignee=me, nhãn → tagId, mọi trạng thái → không status, tìm dưới 2 ký tự không gửi", () => {
    expect(listQuery(DEFAULT_FILTERS)).toEqual({ status: "open", limit: 50 });
    expect(
      listQuery({
        ...DEFAULT_FILTERS,
        status: "all",
        assignee: "me",
        tag: "t1",
        page: "p1",
        unread: true,
        q: " 5678 ",
      }),
    ).toEqual({ assignee: "me", tagId: "t1", pageId: "p1", unread: true, q: "5678", limit: 50 });
    expect(listQuery({ ...DEFAULT_FILTERS, kind: "inbox", q: "a" })).toEqual({
      status: "open",
      kind: "inbox",
      limit: 50,
    });
    // Chỉ chữ số: từ 4 số cuối SĐT mới gửi (máy chủ trả 422 query_too_short với "123").
    expect(listQuery({ ...DEFAULT_FILTERS, q: "123" }).q).toBeUndefined();
    expect(listQuery({ ...DEFAULT_FILTERS, q: "5678" }).q).toBe("5678");
    expect(listQuery({ ...DEFAULT_FILTERS, q: "Lan" }).q).toBe("Lan");
  });
});

describe("chữ hiển thị", () => {
  it("số chưa đọc: trên 99 → 99+", () => {
    expect(unreadText(9)).toBe("9");
    expect(unreadText(99)).toBe("99");
    expect(unreadText(120)).toBe("99+");
  });

  it("giờ: vừa xong, n phút, giờ trong ngày, Hôm qua, ngày/tháng (giờ Việt Nam)", () => {
    const now = Date.parse("2026-11-26T03:00:00Z"); // 10:00 giờ VN
    expect(timeText("2026-11-26T02:59:40Z", now)).toBe("vừa xong");
    expect(timeText("2026-11-26T02:58:00Z", now)).toBe("2 phút");
    expect(timeText("2026-11-26T00:14:00Z", now)).toBe("07:14");
    expect(timeText("2026-11-25T10:00:00Z", now)).toBe("Hôm qua");
    expect(timeText("2026-11-20T10:00:00Z", now)).toBe("20/11");
  });

  it("nhãn: tối đa 3, còn lại +n", () => {
    const tags = Array.from({ length: 14 }, (_, i) => tag(i + 1));
    const v = visibleTags(tags);
    expect(v.shown).toHaveLength(3);
    expect(v.more).toBe(11);
    expect(visibleTags([]).more).toBe(0);
  });
});
