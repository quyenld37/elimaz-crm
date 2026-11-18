// Luật hiển thị của màn Sức khỏe hệ thống (kế hoạch M1 Task 53; mockup docs/mockups/m1/suc-khoe.html).
import { describe, expect, it } from "vitest";
import type { PlatformHealth } from "../../api";
import {
  STALE_AFTER_S,
  allClear,
  ageText,
  beatText,
  clock,
  clockDay,
  count,
  downAlertAt,
  hoursText,
  processRows,
} from "./healthRules";

const health = (extra: Partial<PlatformHealth> = {}): PlatformHealth => ({
  processes: [
    { role: "api", lastHeartbeatAgeS: 4 },
    { role: "ingest", lastHeartbeatAgeS: 6 },
    { role: "worker", lastHeartbeatAgeS: 5 },
  ],
  queue: { oldestPendingAgeS: 0, pending: 0, dead: 0, inDoubt: 0 },
  ingest: { spoolFiles: 0 },
  backup: {},
  errors: [],
  alerts: [],
  ...extra,
});

describe("ageText", () => {
  it("giây, phút, giờ, ngày tròn xuống", () => {
    expect(ageText(0)).toBe("0 giây");
    expect(ageText(45)).toBe("45 giây");
    expect(ageText(60)).toBe("1 phút");
    expect(ageText(200)).toBe("3 phút");
    expect(ageText(3599)).toBe("59 phút");
    expect(ageText(3600)).toBe("1 giờ");
    expect(ageText(86_399)).toBe("23 giờ");
    expect(ageText(86_400 * 5 + 10)).toBe("5 ngày");
  });
});

describe("beatText", () => {
  it("dưới một giờ ghi số giây như mockup (200 giây), từ một giờ như ageText", () => {
    expect(beatText(4)).toBe("4 giây");
    expect(beatText(200)).toBe("200 giây");
    expect(beatText(3599)).toBe("3.599 giây");
    expect(beatText(7200)).toBe("2 giờ");
  });
});

describe("hoursText", () => {
  it("dưới 24 giờ ghi giờ, từ 24 giờ ghi ngày", () => {
    expect(hoursText(0)).toBe("0 giờ");
    expect(hoursText(6)).toBe("6 giờ");
    expect(hoursText(23)).toBe("23 giờ");
    expect(hoursText(24)).toBe("1 ngày");
    expect(hoursText(5 * 24 + 3)).toBe("5 ngày");
  });
});

describe("count", () => {
  it("dấu chấm ngăn nghìn", () => {
    expect(count(0)).toBe("0");
    expect(count(1204)).toBe("1.204");
    expect(count(1_234_567)).toBe("1.234.567");
  });
});

describe("clock, clockDay", () => {
  it("giờ phút (giây) và ngày tháng theo giờ máy", () => {
    const d = new Date(2026, 10, 18, 14, 2, 5);
    expect(clock(d)).toBe("14:02");
    expect(clock(d, true)).toBe("14:02:05");
    expect(clockDay(new Date(2026, 0, 7, 9, 5))).toBe("09:05 07/01");
  });
});

describe("processRows", () => {
  it("gộp theo vai trò lấy nhịp sống mới nhất; quá 90 giây là ngừng", () => {
    const rows = processRows([
      { role: "worker", lastHeartbeatAgeS: 4000 }, // tiến trình cũ của lần deploy trước
      { role: "worker", lastHeartbeatAgeS: 200 },
      { role: "api", lastHeartbeatAgeS: STALE_AFTER_S },
      { role: "ingest", lastHeartbeatAgeS: STALE_AFTER_S + 1 },
    ]);
    expect(rows).toEqual([
      { role: "api", ageS: 90, down: false },
      { role: "worker", ageS: 200, down: true },
      { role: "ingest", ageS: 91, down: true },
    ]);
  });
  it("worker, ingest thiếu dòng vẫn có ô (ngừng, chưa có nhịp sống); vai trò khác sau, theo tên", () => {
    const rows = processRows([
      { role: "imager", lastHeartbeatAgeS: 3 },
      { role: "api", lastHeartbeatAgeS: 1 },
      { role: "beta", lastHeartbeatAgeS: 2 },
    ]);
    expect(rows).toEqual([
      { role: "api", ageS: 1, down: false },
      { role: "worker", ageS: null, down: true },
      { role: "ingest", ageS: null, down: true },
      { role: "beta", ageS: 2, down: false },
      { role: "imager", ageS: 3, down: false },
    ]);
  });
  it("không có api thì không thêm ô api", () => {
    expect(processRows([]).map((r) => r.role)).toEqual(["worker", "ingest"]);
  });
});

describe("downAlertAt", () => {
  it("cảnh báo gấp mới nhất của vai trò (khóa <vai trò>-heartbeat)", () => {
    const alerts: PlatformHealth["alerts"] = [
      { key: "worker-heartbeat", severity: "urgent", sentAt: "2026-11-18T07:00:00Z" },
      { key: "worker-heartbeat", severity: "urgent", sentAt: "2026-11-18T07:29:00Z" },
      { key: "ingest-heartbeat", severity: "urgent", sentAt: "2026-11-18T07:40:00Z" },
      { key: "worker-heartbeat", severity: "normal", sentAt: "2026-11-18T08:00:00Z" },
    ];
    expect(downAlertAt(alerts, "worker")?.toISOString()).toBe("2026-11-18T07:29:00.000Z");
    expect(downAlertAt(alerts, "api")).toBeUndefined();
  });
});

describe("allClear", () => {
  it("chỉ khi mọi tiến trình chạy, không việc hỏng hay không chắc, không cảnh báo, không lỗi", () => {
    expect(allClear(health())).toBe(true);
    expect(allClear(health({ processes: [{ role: "api", lastHeartbeatAgeS: 1 }] }))).toBe(false);
    expect(allClear(health({ queue: { oldestPendingAgeS: 0, pending: 0, dead: 1, inDoubt: 0 } }))).toBe(false);
    expect(allClear(health({ queue: { oldestPendingAgeS: 0, pending: 0, dead: 0, inDoubt: 1 } }))).toBe(false);
    expect(allClear(health({ alerts: [{ key: "x", severity: "normal", sentAt: "2026-11-18T07:00:00Z" }] }))).toBe(
      false,
    );
    const e = {
      fingerprint: "f",
      name: "n",
      role: "api",
      message: "m",
      count: 1,
      firstSeenAt: "2026-11-18T07:00:00Z",
      lastSeenAt: "2026-11-18T07:00:00Z",
    };
    expect(allClear(health({ errors: [e] }))).toBe(false);
    // Việc đang chờ không làm mất trạng thái bình thường.
    expect(allClear(health({ queue: { oldestPendingAgeS: 30, pending: 12, dead: 0, inDoubt: 0 } }))).toBe(true);
  });
});
