// API giả và dữ liệu mẫu cho test vitest của khung bên phải Hộp chat (Task 176). Chỉ test nhập tệp này. Trả đúng kiểu
// client sinh. OrderForm của M2 được thay bằng bản giả trong test của OrderPanel (vi.mock), nên API giả không cần các
// đường của form lên đơn.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import type {
  AssignableUser,
  CallLog,
  ChatOrderRef,
  ConversationCustomerCard,
  ConversationDetail,
  ConversationOrderContext,
  ConversationTag,
  Customer,
  Grant,
  Order,
  Reminder,
} from "../../../api";
import { NOT_FOUND, emptyWrites, reply, stubApi, type WriteQueue } from "../../../app/screenTestkit";
import { stubDialog } from "../../don/donTestkit";
import { order } from "../../len-don/orderTestkit";
import { ME, P1, account, conv, convId } from "../inboxTestkit";

export { ME, P1 };
export const CONV = convId(1);
export const convIdOther = convId(2);
export const CUST = "0195f3b2-0000-7000-8000-00000002c001";
export const OTHER_CUST = "0195f3b2-0000-7000-8000-00000002c002";
export const U_AN = "0195f3b2-0000-7000-8000-0000000000e3";
export const DRAFT = "0195f3b2-0000-7000-8000-00000004d001";
export const SRC_LIVE = "0195f3b2-0000-7000-8000-00000005f001";
export const TAG_DONE = "0195f3b2-0000-7000-8000-00000003d001";
export const TAG_SIZE = "0195f3b2-0000-7000-8000-00000003d002";

/** Grant của quyền trên Page P1 (phạm vi "page"). */
export const onPage = (permission: string, page = P1): Grant => ({ permission, scope: { type: "page", id: page } });
export const shop = (permission: string): Grant => ({ permission, scope: { type: "all", id: "" } });
/** Nhân viên bán hàng: đọc, trả lời trên Page P1; xem khách; lên, sửa, xác nhận đơn toàn shop. */
export const SALE: Grant[] = [
  onPage("conversation.read"),
  onPage("conversation.reply"),
  shop("customer.read"),
  shop("order.create"),
  shop("order.update"),
  shop("order.confirm"),
];

export function orderRef(p: Partial<ChatOrderRef> = {}): ChatOrderRef {
  return {
    id: "0195f3b2-0000-7000-8000-00000004a001",
    code: "DH0001231",
    status: "shipping",
    grandTotal: 690000,
    createdAt: "2026-11-18T03:00:00Z",
    version: 1,
    ...p,
  };
}

export function card(p: Partial<ConversationCustomerCard["customer"]> = {}): ConversationCustomerCard {
  return {
    customer: {
      id: CUST,
      rootId: CUST,
      name: "Trần Thị Thu Hà",
      nameSource: "staff",
      doNotContact: false,
      phones: ["+84912345678"],
      orderCount: 12,
      deliveredCount: 8,
      returnedCount: 2,
      returnRateBp: 2000,
      totalSpent: 8450000,
      ...p,
    },
    recentOrders: [orderRef()],
    openDrafts: [],
  };
}

export function detail(p: Partial<ConversationDetail["conversation"]> = {}): ConversationDetail {
  return {
    conversation: conv(1, { customer: { id: CUST, name: "Trần Thị Thu Hà" }, ...p }),
    page: account(P1, "ELIMAZ Official"),
    sendPolicy: { allowed: true },
    humanAgentAvailable: false,
    blockedByUser: false,
    doNotContact: false,
  };
}

export function profile(p: Partial<Customer> = {}): Customer {
  return {
    id: CUST,
    rootId: CUST,
    code: "KH000231",
    fullName: "Trần Thị Thu Hà",
    lifecycle: "customer",
    stats: { orderCount: 12, deliveredCount: 8, returnedCount: 2, returnRateBp: 2000, totalSpent: 8450000 },
    memberIds: [CUST],
    identities: [],
    addresses: [],
    tags: [],
    version: 1,
    createdAt: "2026-11-01T02:00:00Z",
    ...p,
  };
}

export function draftOrder(p: Partial<Order> = {}): Order {
  return order({
    id: DRAFT,
    code: "DH0001238",
    status: "draft",
    customerId: CUST,
    subtotal: 2580000,
    grandTotal: 2610000,
    codAmount: 2610000,
    version: 3,
    ...p,
  });
}

export function reminder(p: Partial<Reminder> = {}): Reminder {
  return {
    id: "0195f3b2-0000-7000-8000-00000006a001",
    conversationId: CONV,
    userId: ME,
    remindAt: "2026-11-26T08:00:00Z",
    note: "Gọi lại xác nhận size",
    status: "pending",
    source: "manual",
    version: 1,
    createdAt: "2026-11-26T02:00:00Z",
    ...p,
  };
}

export function call(p: Partial<CallLog> = {}): CallLog {
  return {
    id: "0195f3b2-0000-7000-8000-00000007a001",
    customerId: CUST,
    outcome: "no_answer",
    createdBy: { id: U_AN, name: "Trần Minh An" },
    createdAt: "2026-11-17T03:00:00Z",
    ...p,
  };
}

export interface SideFake extends WriteQueue {
  grants: Grant[];
  detail: ConversationDetail;
  card: ConversationCustomerCard;
  context: ConversationOrderContext;
  profile: Customer;
  tags: ConversationTag[];
  users: AssignableUser[];
  reminders: Reminder[];
  calls: CallLog[];
  orders: Order[];
  /** Đường của mọi yêu cầu GET đã nhận. */
  reads: string[];
}

export function sideFake(p: Partial<SideFake> = {}): SideFake {
  return {
    ...emptyWrites(),
    grants: SALE,
    detail: detail(),
    card: card(),
    context: { channelAccountId: P1, conversationId: CONV, customerId: CUST, sellerId: P1, trafficSourceId: SRC_LIVE },
    profile: profile(),
    tags: [
      { id: TAG_SIZE, name: "Hỏi size", color: "soft", sort: 1, version: 1 },
      { id: TAG_DONE, name: "Đã lên đơn", color: "success", sort: 2, version: 1 },
    ],
    users: [
      { id: U_AN, name: "Trần Minh An" },
      { id: ME, name: "Nguyễn Thị Lan" },
    ],
    reminders: [],
    calls: [],
    orders: [],
    reads: [],
    ...p,
  };
}

function readApi(f: SideFake, path: string): Response {
  f.reads.push(path);
  const base = `/api/v1/conversations/${CONV}`;
  switch (path) {
    case "/api/v1/me":
      return reply({ userId: ME, email: "lan@elimaz.vn", grants: f.grants });
    case base:
      return reply(f.detail);
    case `${base}/customer-card`:
      return reply(f.card);
    case `${base}/order-context`:
      return reply(f.context);
    case `/api/v1/customers/${CUST}`:
      return reply(f.profile);
    case `/api/v1/customers/${CUST}/calls`:
      return reply({ items: f.calls });
    case `/api/v1/channels/${P1}/assignable-users`:
      return reply({ items: f.users });
    case "/api/v1/conversation-tags":
      return reply({ items: f.tags });
    case "/api/v1/me/reminders":
      return reply({ items: f.reminders });
    default: {
      const o = f.orders.find((x) => path === `/api/v1/orders/${x.id}`);
      return o === undefined ? reply(NOT_FOUND, 404) : reply(o);
    }
  }
}

/** Câu trả lời mặc định của yêu cầu ghi: 204 cho nhãn, không liên hệ, nhịp tim; bản ghi cho phần còn lại. */
function writeApi(f: SideFake, path: string, body: Record<string, unknown>): Response {
  if (path.includes("/tags/") || path.endsWith("/do-not-contact") || path.endsWith("/presence")) {
    return new Response(null, { status: 204 });
  }
  if (path.endsWith("/claim") || path.endsWith("/assign")) return reply(f.detail);
  if (path.endsWith("/reminders"))
    return reply(reminder({ note: typeof body.note === "string" ? body.note : "" }), 201);
  if (path.endsWith("/done")) return reply(reminder({ status: "done" }));
  if (path.endsWith("/calls")) return reply(call({ outcome: "callback" }), 201);
  if (path.endsWith("/confirm")) return reply(draftOrder({ status: "confirmed", version: 5 }));
  return reply(NOT_FOUND, 404);
}

/** Thay fetch bằng API giả của khung bên phải; hộp thoại <dialog> mở được trong jsdom. */
export function stubSide(f: SideFake): SideFake {
  stubDialog();
  return stubApi(f, readApi, writeApi);
}

/** Dựng một phần của khung bên phải trong QueryClient và router bộ nhớ riêng. */
export function renderSide(ui: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>,
  );
}
