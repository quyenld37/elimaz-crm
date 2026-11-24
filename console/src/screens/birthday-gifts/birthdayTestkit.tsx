// API giả (fetch) và cách dựng cho test vitest của màn Quà sinh nhật. Chỉ test nhập tệp này. Trả đúng kiểu của client
// sinh (BirthdaySettings, BirthdayGift, BirthdayCustomer của Task 468; CouponBatch, Coupon của Task 197).
import { cleanup } from "@testing-library/react";
import { vi } from "vitest";
import type {
  BirthdayCustomer,
  BirthdayCustomerState,
  BirthdayGift,
  BirthdayRun,
  BirthdaySettings,
  Coupon,
  CouponBatch,
  Grant,
} from "../../api";
import {
  NOT_FOUND,
  emptyWrites,
  meReply,
  page,
  renderScreens,
  reply,
  stubApi,
  type WriteQueue,
} from "../../app/screenTestkit";
import { coupon, grantsOf } from "../ma-giam-gia/couponTestkit";
import { STATES } from "./birthdayRules";
import { screens } from "./route";

export { grantsOf };
export const PATH = "/khuyen-mai/qua-sinh-nhat";
export const FORBIDDEN = { error: "forbidden", request_id: "t" };
/** Tên dài không dấu cách (dữ liệu khó của mockup). */
export const LONG_NAME = "NguyễnThịThuHàShopThờiTrangCôngSởXuấtKhẩuHàNộiChiNhánhSố0205";

const SETTINGS: BirthdaySettings = {
  enabled: true,
  amount: 50_000,
  minOrder: 0,
  daysBefore: 7,
  validDays: 14,
  minLeadDays: 30,
  version: 3,
};
/** Cài đặt đang bật: 50.000 ₫, gửi trước 7 ngày, dùng 14 ngày, lưu trước 30 ngày; sửa bằng p. */
export const settings = (p: Partial<BirthdaySettings> = {}): BirthdaySettings => Object.assign({}, SETTINGS, p);

const RUN: BirthdayRun = {
  id: "0195f3b2-0000-7000-8000-0000000b0001",
  trigger: "schedule",
  startedAt: "2026-11-24T02:00:00Z",
  finishedAt: "2026-11-24T02:00:05Z",
  given: 6,
  failed: 0,
};
/** Lượt theo lịch 09:00 24/11 (giờ VN), 6 mã, 0 lỗi; sửa bằng p. */
export const run = (p: Partial<BirthdayRun> = {}): BirthdayRun => Object.assign({}, RUN, p);

const uuid = (n: number) => `0195f3b2-0000-7000-8000-${String(n).padStart(12, "0")}`;

/** Quà chưa trao thứ n (SĐT +8491234567n). */
export function gift(n: number, p: Partial<BirthdayGift> = {}): BirthdayGift {
  return {
    customerId: uuid(n),
    code: `SNCODE${String(n).padStart(2, "0")}`,
    fullName: `Khách số ${String(n)}`,
    phone: `+849123456${String(n).padStart(2, "0")}`,
    day: 24,
    month: 11,
    zalo: false,
    mail: false,
    amount: 50_000,
    minOrder: 0,
    validFrom: "2026-11-16T17:00:00Z",
    validTo: "2026-12-01T17:00:00Z",
    birthdayIn: n,
    ...p,
  };
}

export function customer(n: number, state: BirthdayCustomerState, p: Partial<BirthdayCustomer> = {}): BirthdayCustomer {
  return {
    customerId: uuid(100 + n),
    fullName: `Khách ${String(n)}`,
    phone: `+849876543${String(n).padStart(2, "0")}`,
    day: 28,
    month: 11,
    zalo: true,
    mail: false,
    state,
    birthdayIn: 4,
    sendOn: "2026-11-20T17:00:00Z",
    savedAt: "2026-09-01T00:00:00Z",
    ...p,
  };
}

export function batch(n: number, p: Partial<CouponBatch> = {}): CouponBatch {
  return {
    id: uuid(500 + n),
    name: `Sinh nhật ${String(n)}/11/2026`,
    prefix: "SN",
    quantity: 6,
    purpose: "birthday",
    template: {
      name: "Quà sinh nhật",
      discountKind: "fixed",
      value: 50_000,
      validFrom: "2026-11-07T17:00:00Z",
      validTo: "2026-11-21T17:00:00Z",
    },
    createdAt: "2026-11-08T02:00:00Z",
    ...p,
  };
}

export interface Fake extends WriteQueue {
  grants: Grant[];
  settings: BirthdaySettings;
  todo: BirthdayGift[];
  customers: BirthdayCustomer[];
  customerCalls: URLSearchParams[];
  batches: CouponBatch[];
  batchCalls: URLSearchParams[];
  codes: Coupon[];
  /** Số "Sắp gửi · 7 ngày tới" và quà của năm máy chủ trả ở listBirthdayCustomers. */
  upcomingWeek: number;
  yearGifts: { year: number; issued: number; used: number };
  /** Mọi GET của promotion trả 403 (vai trò mất quyền promotion.read). */
  forbidden: boolean;
}

function customersReply(fake: Fake, q: URLSearchParams): Response {
  fake.customerCalls.push(q);
  const state = q.get("state") ?? "all";
  const month = Number(q.get("month") ?? "0");
  const text = q.get("q") ?? "";
  const byQ = fake.customers.filter((c) => text === "" || c.fullName.includes(text) || c.phone.includes(text));
  const inMonth = byQ.filter((c) => month === 0 || c.month === month);
  const shown = inMonth.filter((c) => state === "all" || c.state === state);
  const stateCounts = Object.fromEntries(STATES.map((s) => [s, inMonth.filter((c) => c.state === s).length]));
  const monthCounts = Array.from({ length: 12 }, (_, i) => byQ.filter((c) => c.month === i + 1).length);
  return reply({
    ...page(shown, q),
    total: shown.length,
    stateCounts,
    monthCounts,
    upcomingWeek: fake.upcomingWeek,
    yearGifts: fake.yearGifts,
  });
}

function read(fake: Fake, p: string, q: URLSearchParams): Response {
  if (p === "/api/v1/me") return meReply(fake.grants);
  if (fake.forbidden) return reply(FORBIDDEN, 403);
  if (p === "/api/v1/birthday-settings") return reply(fake.settings);
  if (p === "/api/v1/birthday-gifts/todo") return reply({ items: fake.todo });
  if (p === "/api/v1/birthday-customers") return customersReply(fake, q);
  if (p === "/api/v1/coupon-batches") {
    fake.batchCalls.push(q);
    return reply(
      page(
        fake.batches.filter((b) => b.purpose === (q.get("purpose") ?? b.purpose)),
        q,
      ),
    );
  }
  if (p === "/api/v1/coupons") return reply(page(fake.codes, q));
  const one = /^\/api\/v1\/coupon-batches\/([^/]+)$/.exec(p);
  const b = fake.batches.find((x) => x.id === one?.[1]);
  return b === undefined ? reply(NOT_FOUND, 404) : reply(b);
}

function write(fake: Fake, p: string, body: Record<string, unknown>): Response {
  if (p === "/api/v1/birthday-settings") {
    fake.settings = { ...fake.settings, ...body, version: fake.settings.version + 1 };
    return reply(fake.settings);
  }
  if (p === "/api/v1/birthday-runs") return reply(run({ trigger: "manual", given: 2 }));
  if (p === "/api/v1/birthday-gifts/messaged") {
    const codes = Array.isArray(body.codes) ? body.codes.filter((c) => typeof c === "string") : [];
    fake.todo = fake.todo.filter((g) => !codes.includes(g.code));
    return reply({ marked: codes });
  }
  if (p === "/api/v1/birthday-customers/export")
    return reply({ filename: "sinh-nhat.csv", rows: fake.customers.length, csv: "﻿Tên,SĐT\n" });
  return reply(NOT_FOUND, 404);
}

/** Đồng hồ của test: 10:00 24/11/2026 giờ VN (chỉ giả Date, hẹn giờ vẫn thật), rồi cài API giả. */
export function installAt(): Fake {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2026-11-24T03:00:00Z"));
  return installFake();
}

/** Dọn sau mỗi test: gỡ màn, đồng hồ, fetch giả, spy. */
export function teardown() {
  cleanup();
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
}

export function installFake(): Fake {
  const fake: Fake = {
    grants: grantsOf("promotion.birthday.manage"),
    settings: settings({ lastRun: run() }),
    todo: [],
    customers: [],
    customerCalls: [],
    batches: [],
    batchCalls: [],
    codes: [],
    upcomingWeek: 0,
    yearGifts: { year: 2026, issued: 0, used: 0 },
    forbidden: false,
    ...emptyWrites(),
  };
  return stubApi(fake, read, write);
}

/** Mã thứ n của đợt (Coupon của listCoupons?batchId). */
export const code = (n: number, p: Partial<Coupon> = {}): Coupon =>
  coupon({
    id: uuid(900 + n),
    code: `SNB${String(n).padStart(3, "0")}`,
    batchId: uuid(501),
    discountKind: "fixed",
    value: 50_000,
    usedCount: 0,
    maxUses: 1,
    bound: true,
    validFrom: "2026-11-07T17:00:00Z",
    validTo: "2026-12-21T17:00:00Z",
    ...p,
  });

export const renderAt = (path: string) => renderScreens(screens, path);
