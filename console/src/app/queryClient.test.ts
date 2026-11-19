// Lớp lấy dữ liệu (TanStack Query) nối với báo hiệu realtime (kế hoạch M1 Task 48): changed của một chủ đề tải lại
// đúng các truy vấn đang mở của chủ đề đó; resync tải lại mọi truy vấn đang mở; truy vấn không ai xem chỉ bị đánh dấu cũ.
import { QueryObserver, type QueryClient, type QueryKey } from "@tanstack/react-query";
import { afterEach, describe, expect, it } from "vitest";
import { ApiError, NetworkError } from "./errors";
import {
  TOPICS,
  createQueryClient,
  onRealtimeChanged,
  onRealtimeResync,
  queryKey,
  shouldRetry,
  subscribeChanged,
  type RealtimeMeta,
} from "./queryClient";

const clients: QueryClient[] = [];
afterEach(() => {
  for (const c of clients.splice(0)) c.clear();
});

function harness() {
  const qc = createQueryClient();
  clients.push(qc);
  const calls = new Map<string, number>();
  const fnFor = (key: QueryKey) => () => {
    const k = JSON.stringify(key);
    calls.set(k, (calls.get(k) ?? 0) + 1);
    return Promise.resolve(k);
  };
  const unsubs: (() => void)[] = [];
  const watch = async (key: QueryKey, meta?: RealtimeMeta) => {
    const obs = new QueryObserver(qc, { queryKey: key, queryFn: fnFor(key), meta });
    unsubs.push(obs.subscribe(() => undefined));
    await qc.query({ queryKey: key, queryFn: fnFor(key), meta });
  };
  const cache = async (key: QueryKey) => {
    await qc.query({ queryKey: key, queryFn: fnFor(key) });
  };
  const count = (key: QueryKey) => calls.get(JSON.stringify(key)) ?? 0;
  const settle = () => new Promise((r) => setTimeout(r, 20));
  return { qc, watch, cache, count, settle, unsubs };
}

describe("queryKey", () => {
  it("chủ đề đứng đầu khóa", () => {
    expect(queryKey(TOPICS.product, "list", { q: "đầm" })).toEqual(["catalog.product", "list", { q: "đầm" }]);
  });

  it("đủ ba chủ đề của M1", () => {
    expect(TOPICS).toEqual({ product: "catalog.product", balance: "inventory.balance", file: "files.file" });
  });
});

describe("onRealtimeChanged", () => {
  it("catalog.product đổi → tải lại truy vấn đang mở của catalog.product, không đụng inventory.balance", async () => {
    const h = harness();
    const products = queryKey(TOPICS.product, "list");
    const balances = queryKey(TOPICS.balance, "list");
    await h.watch(products);
    await h.watch(balances);
    const p0 = h.count(products);
    const b0 = h.count(balances);
    onRealtimeChanged(h.qc, { topic: "catalog.product", entity: { type: "product", id: "p1" } });
    await h.settle();
    expect(h.count(products)).toBe(p0 + 1);
    expect(h.count(balances)).toBe(b0);
  });

  it("truy vấn không ai xem: không tải lại, chỉ đánh dấu cũ", async () => {
    const h = harness();
    const k = queryKey(TOPICS.file, "f1");
    await h.cache(k);
    const c0 = h.count(k);
    onRealtimeChanged(h.qc, { topic: "files.file", entity: { type: "file", id: "f1" } });
    await h.settle();
    expect(h.count(k)).toBe(c0);
    expect(h.qc.getQueryState(k)?.isInvalidated).toBe(true);
  });

  it("chủ đề lạ → không tải lại gì", async () => {
    const h = harness();
    const k = queryKey(TOPICS.product, "list");
    await h.watch(k);
    const c0 = h.count(k);
    onRealtimeChanged(h.qc, { topic: "conversation.message", entity: { type: "message", id: "m" } });
    await h.settle();
    expect(h.count(k)).toBe(c0);
  });
});

describe("onRealtimeChanged theo đối tượng (Task 85)", () => {
  const ORDER = "order.changed";
  const changed = (id: string) => ({ topic: ORDER, entity: { type: "order", id } });

  it("meta realtimeEntity: chỉ tải lại khi báo hiệu đúng đối tượng; truy vấn không meta vẫn tải lại", async () => {
    const h = harness();
    const x = queryKey(ORDER, "detail", "X");
    const y = queryKey(ORDER, "detail", "Y");
    const plain = queryKey(ORDER, "duplicates");
    await h.watch(x, { realtimeEntity: "X" });
    await h.watch(y, { realtimeEntity: "Y" });
    await h.watch(plain);
    const [x0, y0, p0] = [h.count(x), h.count(y), h.count(plain)];
    onRealtimeChanged(h.qc, changed("X"));
    await h.settle();
    expect(h.count(x)).toBe(x0 + 1);
    expect(h.count(y)).toBe(y0);
    expect(h.count(plain)).toBe(p0 + 1);
  });

  it("meta realtimeManual: changed không tải lại (màn tự xử lý), resync vẫn tải lại", async () => {
    const h = harness();
    const list = queryKey(ORDER, "list", {});
    await h.watch(list, { realtimeManual: true });
    const c0 = h.count(list);
    onRealtimeChanged(h.qc, changed("X"));
    await h.settle();
    expect(h.count(list)).toBe(c0);
    onRealtimeResync(h.qc);
    await h.settle();
    expect(h.count(list)).toBe(c0 + 1);
  });

  it("subscribeChanged nhận mọi báo hiệu của đúng QueryClient tới khi hủy", () => {
    const h = harness();
    const other = createQueryClient();
    clients.push(other);
    const got: string[] = [];
    const off = subscribeChanged(h.qc, (e) => got.push(e.entity.id));
    onRealtimeChanged(h.qc, changed("X"));
    onRealtimeChanged(other, changed("Z"));
    off();
    onRealtimeChanged(h.qc, changed("Y"));
    expect(got).toEqual(["X"]);
  });
});

describe("onRealtimeResync", () => {
  it("tải lại mọi truy vấn đang mở", async () => {
    const h = harness();
    const a = queryKey(TOPICS.product, "list");
    const b = queryKey(TOPICS.balance, "list");
    await h.watch(a);
    await h.watch(b);
    const a0 = h.count(a);
    const b0 = h.count(b);
    onRealtimeResync(h.qc);
    await h.settle();
    expect(h.count(a)).toBe(a0 + 1);
    expect(h.count(b)).toBe(b0 + 1);
  });
});

describe("shouldRetry", () => {
  it("4xx không thử lại; mạng, 5xx thử lại tối đa 2 lần", () => {
    expect(shouldRetry(0, new ApiError({ status: 404, code: "not_found", details: {} }))).toBe(false);
    expect(shouldRetry(0, new ApiError({ status: 403, code: "forbidden", details: {} }))).toBe(false);
    expect(shouldRetry(0, new ApiError({ status: 503, code: "retry_later", details: {} }))).toBe(true);
    expect(shouldRetry(0, new NetworkError())).toBe(true);
    expect(shouldRetry(2, new NetworkError())).toBe(false);
  });
});
