// API giả của nhà cung cấp cho test vitest (Suppliers, SupplierPicker): fetch giả trả đúng kiểu của client sinh, ghi lại
// mọi lần ghi (khóa chống trùng, thân). Chỉ test nhập tệp này.
import { vi } from "vitest";
import type { Me, Supplier } from "../../api";

export interface SupplierWrite {
  method: string;
  path: string;
  key: string | null;
  body: Record<string, unknown>;
}

export interface FakeSuppliers {
  list: Supplier[];
  /** Tham số của mọi lần gọi listSuppliers. */
  reads: URLSearchParams[];
  writes: SupplierWrite[];
  /** Lỗi trả cho lần ghi kế tiếp (một lần). */
  failNext: { status: number; code: string } | null;
}

let seq = 0;
const newId = () => {
  seq++;
  return `0195f3b2-0000-7000-8000-0000000d${String(seq).padStart(4, "0")}`;
};

export const sup = (name: string, s: Partial<Supplier> = {}): Supplier => ({
  id: newId(),
  name,
  phone: "",
  note: "",
  status: "active",
  version: 1,
  ...s,
});

export const meWith = (...permissions: string[]): Me => ({
  userId: "0195f3b2-0000-7000-8000-000000000001",
  email: "lan@elimaz.vn",
  grants: permissions.map((permission) => ({ permission, scope: { type: "all", id: "" } })),
});

const isRecord = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null;

/** Cài fetch giả: listSuppliers lọc q (chứa, không phân biệt hoa thường) và includeArchived; create, update theo version. */
export function stubSupplierApi(list: Supplier[]): FakeSuppliers {
  const fake: FakeSuppliers = { list, reads: [], writes: [], failNext: null };
  const reply = (status: number, b: unknown) =>
    Promise.resolve(new Response(JSON.stringify(b), { status, headers: { "Content-Type": "application/json" } }));
  vi.stubGlobal(
    "fetch",
    vi.fn(async (req: Request) => {
      const url = new URL(req.url);
      if (!url.pathname.startsWith("/api/v1/inventory/suppliers")) return reply(501, { error: "not_implemented" });
      if (req.method === "GET") {
        fake.reads.push(url.searchParams);
        const q = (url.searchParams.get("q") ?? "").toLowerCase();
        const all = url.searchParams.get("includeArchived") === "true";
        return reply(
          200,
          fake.list.filter((s) => (all || s.status === "active") && s.name.toLowerCase().includes(q)),
        );
      }
      const raw: unknown = await req.json();
      const body = isRecord(raw) ? raw : {};
      fake.writes.push({ method: req.method, path: url.pathname, key: req.headers.get("Idempotency-Key"), body });
      const f = fake.failNext;
      if (f !== null) {
        fake.failNext = null;
        return reply(f.status, { error: f.code, request_id: "t1" });
      }
      if (req.method === "POST") {
        const s = sup(String(body.name).trim(), {
          phone: typeof body.phone === "string" ? body.phone.replace(/[ .-]/g, "") : "",
          note: typeof body.note === "string" ? body.note : "",
        });
        fake.list = [...fake.list, s];
        return reply(201, s);
      }
      const id = url.pathname.split("/").at(-1);
      const old = fake.list.find((s) => s.id === id);
      if (old === undefined) return reply(404, { error: "not_found", request_id: "t1" });
      if (body.version !== old.version) return reply(409, { error: "version_conflict", request_id: "t1" });
      const next: Supplier = { ...old, ...body, version: old.version + 1 };
      fake.list = fake.list.map((s) => (s.id === id ? next : s));
      return reply(200, next);
    }),
  );
  return fake;
}

/** jsdom chưa có showModal/close của <dialog>: bản tối thiểu đặt, bỏ thuộc tính open. */
export function stubDialog() {
  const proto = HTMLDialogElement.prototype;
  proto.showModal = function showModal(this: HTMLDialogElement) {
    this.setAttribute("open", "");
  };
  proto.close = function close(this: HTMLDialogElement) {
    this.removeAttribute("open");
  };
}
