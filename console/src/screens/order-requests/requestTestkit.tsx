// API giả (fetch) và cách dựng cho test vitest của màn Yêu cầu về đơn hàng. Chỉ test nhập tệp này. Trả đúng kiểu của
// client sinh (OrderRequest, OrderRequestSummary của Task 195; Order của M2).
import type { Grant, Order, OrderRequest, OrderRequestSummary } from "../../api";
import {
  NOT_FOUND,
  ORDER_PAGE,
  emptyWrites,
  meReply,
  page,
  renderScreens,
  reply,
  stubApi,
  type WriteQueue,
} from "../../app/screenTestkit";
import { order } from "../don/donTestkit";
import { screens } from "./route";

export { stubDialog } from "../don/donTestkit";

export const RQ1 = "0195f3b2-0000-7000-8000-00000000a201";
export const RQ2 = "0195f3b2-0000-7000-8000-00000000a202";
export const ORD = "0195f3b2-0000-7000-8000-00000000d001";
export const VAR1 = "0195f3b2-0000-7000-8000-00000000b001";
export const VAR2 = "0195f3b2-0000-7000-8000-00000000b002";
export const LONG_NAME = "NguyễnThịThuHàShopThờiTrangCôngSởXuấtKhẩuHàNộiChiNhánhSố0205";

/** Một dòng danh sách: YC0000148 đổi hàng, đã tiếp nhận (lấy từ request()); sửa bằng p. */
export function summary(p: Partial<OrderRequestSummary> = {}): OrderRequestSummary {
  const { id, ref, orderId, orderCode, kind, status, reasonCode, createdAt, version } = request();
  return { id, ref, orderId, orderCode, kind, status, reasonCode, createdAt, updatedAt: createdAt, version, ...p };
}

/** Chi tiết YC0000148: 2 dòng (một dòng muốn đổi biến thể khác), lời nhắn có <script>, 3 ảnh, 2 tin nhân viên. */
export function request(p: Partial<OrderRequest> = {}): OrderRequest {
  return {
    id: RQ1,
    ref: "YC0000148",
    orderId: ORD,
    orderCode: "DH0001312",
    kind: "exchange",
    status: "received",
    reasonCode: "SIZE",
    customerNote: "Váy mặc chật ở ngực, cho mình đổi sang size L nhé. <script>alert('x')</script>",
    conditionConfirmed: true,
    lines: [
      {
        variantId: VAR1,
        qty: 1,
        reason: "TOO_SMALL",
        wantVariantId: VAR2,
        sku: "DS2201-DEN-M",
        name: "Đầm suông cổ vuông",
      },
      {
        variantId: VAR2,
        qty: 1,
        reason: "DEFECT",
        note: "Bung đường may ở vai",
        sku: "SM1408-TRA-S",
        name: "Áo sơ mi lụa",
      },
    ],
    messages: [
      {
        id: "m1",
        authorType: "staff",
        authorName: "Lê Hồng Nhung",
        body: "Chào chị, ELIMAZ đã nhận yêu cầu.",
        visibleToCustomer: true,
        createdAt: "2026-11-21T03:50:00Z",
      },
      {
        id: "m2",
        authorType: "staff",
        authorName: "Trần Minh Thư",
        body: "Kho còn 4 chiếc Đen / L.",
        visibleToCustomer: false,
        createdAt: "2026-11-21T03:52:00Z",
      },
    ],
    files: [
      { fileId: "f1", status: "ready", url: "https://files.elimaz.test/priv/a.jpg?sig=1" },
      { fileId: "f2", status: "processing" },
      { fileId: "f3", status: "rejected" },
    ],
    createdAt: "2026-11-21T03:42:00Z",
    updatedAt: "2026-11-21T03:52:00Z",
    version: 1,
    ...p,
  };
}

export const grantsOf = (...perms: string[]): Grant[] =>
  ["order.requests.read", ...perms].map((permission) => ({ permission, scope: { type: "all", id: "" } }));

export interface Fake extends WriteQueue {
  grants: Grant[];
  /** Mọi yêu cầu; listOrderRequests trả theo trang `limit`, con trỏ là vị trí bắt đầu. */
  rows: OrderRequestSummary[];
  listCalls: URLSearchParams[];
  /** Số lần đọc getOrder. */
  orderReads: number;
  details: Map<string, OrderRequest>;
  order: Order;
  /** Lỗi trả cho mọi lần đọc danh sách (ví dụ 403). */
  listError: { status: number; body: unknown } | null;
}

function listPage(fake: Fake, q: URLSearchParams): Response {
  fake.listCalls.push(q);
  if (fake.listError !== null) return reply(fake.listError.body, fake.listError.status);
  const status = q.get("status");
  const kind = q.get("kind");
  const all = fake.rows.filter((r) => (status === null || r.status === status) && (kind === null || r.kind === kind));
  return reply(page(all, q));
}

function read(fake: Fake, p: string, q: URLSearchParams): Response {
  if (p === "/api/v1/me") return meReply(fake.grants);
  if (p === "/api/v1/order-requests") return listPage(fake, q);
  const one = /^\/api\/v1\/order-requests\/([^/]+)$/.exec(p);
  if (one !== null) {
    const r = fake.details.get(one[1] ?? "");
    return r === undefined ? reply(NOT_FOUND, 404) : reply(r);
  }
  if (p === `/api/v1/orders/${fake.order.id}`) {
    fake.orderReads++;
    return reply(fake.order);
  }
  return reply(NOT_FOUND, 404);
}

function writeReply(fake: Fake, p: string, body: Record<string, unknown>): Response {
  const m = /^\/api\/v1\/order-requests\/([^/]+)\/(messages|status)$/.exec(p);
  const r = fake.details.get(m?.[1] ?? "");
  if (m === null || r === undefined) return reply(NOT_FOUND, 404);
  if (m[2] === "messages") {
    return reply(
      {
        id: "new",
        authorType: "staff",
        body: body.body,
        visibleToCustomer: body.visibleToCustomer,
        createdAt: "2026-11-21T04:00:00Z",
      },
      201,
    );
  }
  const next: OrderRequest = { ...r, status: r.status, version: r.version + 1 };
  fake.details.set(r.id, next);
  return reply(next);
}

export function installFake(): Fake {
  const fake: Fake = {
    grants: grantsOf("order.requests.manage", "order.read"),
    rows: [summary()],
    listCalls: [],
    orderReads: 0,
    details: new Map([[RQ1, request()]]),
    order: order({ id: ORD, code: "DH0001312", grandTotal: 1_270_000 }),
    listError: null,
    ...emptyWrites(),
  };
  return stubApi(fake, read, writeReply);
}

export const renderAt = (path: string) => renderScreens(screens, path, ORDER_PAGE);
