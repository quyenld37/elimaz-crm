// Hồ sơ khách /khach/:id (kế hoạch M2 Task M2-29; mockup docs/mockups/m2/khach-ho-so.html trạng thái A, B; DH13, HV28).
// Hiện theo gốc: mở bằng mã hồ sơ con cũng ra hồ sơ của cả nhóm (getCustomer trả theo gốc). SĐT hiện đủ cho người có
// customer.read (màn nội bộ); màn không ghi gì vào log của trình duyệt. Nút ghi hiện theo quyền; máy chủ vẫn chặn.
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link, useParams } from "react-router";
import {
  addCustomerIdentity,
  listCustomerMergeCandidates,
  removeCustomerIdentity,
  updateCustomer,
  type AddCustomerIdentityRequest,
  type Customer,
  type CustomerIdentity,
  type UpdateCustomerRequest,
} from "../../api";
import { ApiError, errorMessage, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { queryKey } from "../../app/queryClient";
import { VN_TIME_ZONE } from "../../app/time";
import { cleanPhone, displayPhone, shortDate } from "../len-don/orderRules";
import { formatMoney } from "../products/productRules";
import { Dialog } from "../staff/staffData";
import { CUSTOMER_TOPIC, customerKey, headers, reloadCustomers, useCustomer, usePerms } from "./customerData";
import { percentText } from "./customerRules";
import { NameField } from "./CreateCustomerDialog";
import { CustomerTimeline, NotesPanel } from "./NotesPanel";
import { TagsEditor } from "./TagsEditor";
import "../don/don.css";
import "./khach.css";

const MAX_IDENTITY = 200;

const KIND_TEXT: Readonly<Record<CustomerIdentity["kind"], string>> = {
  phone: "SĐT",
  email: "Email",
  zalo_uid: "Zalo",
  fb_psid: "Facebook",
  web_account: "Tài khoản website",
};
const SOURCE_TEXT: Readonly<Record<CustomerIdentity["source"], string>> = {
  order: "từ đơn",
  chat: "từ chat (M4 quản)",
  staff: "nhân viên thêm",
  web: "từ website",
  import: "nhập Excel",
};
/** Danh tính nhân viên gỡ được (fb_psid do M4 quản, web_account do website gửi). */
const REMOVABLE = new Set<CustomerIdentity["kind"]>(["phone", "email", "zalo_uid"]);
type AddKind = AddCustomerIdentityRequest["kind"];
const ADD_KINDS: readonly AddKind[] = ["phone", "email", "zalo_uid"];
const isAddKind = (v: string): v is AddKind => ADD_KINDS.some((k) => k === v);

const identityText = (i: CustomerIdentity) =>
  i.kind === "phone" ? displayPhone(i.value) : i.kind === "fb_psid" || i.kind === "web_account" ? "" : i.value;

/** "03/2026" theo giờ Việt Nam. */
function monthYear(iso: string): string {
  const parts = new Intl.DateTimeFormat("en-GB", {
    timeZone: VN_TIME_ZONE,
    month: "2-digit",
    year: "numeric",
  }).formatToParts(new Date(iso));
  const part = (t: string) => parts.find((p) => p.type === t)?.value ?? "";
  return `${part("month")}/${part("year")}`;
}

/** "1990-08-12" → "12/08". */
const birthdayText = (d: string) => d.split("-").slice(1).reverse().join("/");

function EditDialog({ c, onClose }: { c: Customer; onClose: () => void }) {
  const qc = useQueryClient();
  const [name, setName] = useState(c.fullName);
  const [birthday, setBirthday] = useState(c.birthday ?? "");
  const save = useWriteAction((key, b: { id: string; body: UpdateCustomerRequest }) =>
    unwrap(updateCustomer({ path: { id: b.id }, body: b.body, headers: headers(key) })),
  );
  const body: UpdateCustomerRequest = {
    version: c.version,
    ...(name.trim() !== c.fullName ? { fullName: name.trim() } : {}),
    ...(birthday !== (c.birthday ?? "") ? (birthday === "" ? { clearBirthday: true } : { birthday }) : {}),
  };
  const changed = Object.keys(body).length > 1;
  const conflict = save.error instanceof ApiError && save.error.code === "version_conflict";
  const submit = async () => {
    const r = await save.run({ id: c.rootId, body });
    if (!r.ok) return;
    reloadCustomers(qc);
    onClose();
  };
  return (
    <Dialog
      title="Sửa hồ sơ khách"
      onClose={onClose}
      footer={
        <>
          <button type="button" className="btn btn-line" onClick={onClose}>
            Hủy
          </button>
          <button
            type="button"
            className="btn btn-dark"
            disabled={!changed || name.trim() === "" || save.pending}
            onClick={() => {
              void submit();
            }}
          >
            Lưu
          </button>
        </>
      }
    >
      <div className="stack-sm">
        <NameField value={name} onChange={setName} />
        <label className="field">
          <span className="label">Ngày sinh</span>
          <input
            className="input num"
            type="date"
            value={birthday}
            onChange={(e) => {
              setBirthday(e.target.value);
            }}
          />
        </label>
        {conflict ? (
          <div className="box box-err" role="alert">
            <b>Hồ sơ đã thay đổi, tải lại.</b> Có người vừa lưu hồ sơ này. Phần bạn đang sửa chưa được lưu.
            <div className="row" style={{ marginTop: 8 }}>
              <button
                type="button"
                className="btn btn-dark"
                onClick={() => {
                  void qc.invalidateQueries({ queryKey: customerKey(c.rootId) });
                  reloadCustomers(qc);
                  onClose();
                }}
              >
                Tải lại hồ sơ
              </button>
            </div>
          </div>
        ) : (
          save.message !== null && (
            <div className="box box-err" role="alert">
              {save.message}
            </div>
          )
        )}
      </div>
    </Dialog>
  );
}

function Contacts({ c, canWrite }: { c: Customer; canWrite: boolean }) {
  const qc = useQueryClient();
  const [adding, setAdding] = useState(false);
  const [kind, setKind] = useState<AddKind>("phone");
  const [value, setValue] = useState("");
  const remove = useWriteAction((key, b: { id: string; identityId: string }) =>
    unwrap(removeCustomerIdentity({ path: b, headers: headers(key) })),
  );
  const add = useWriteAction((key, b: { id: string; body: AddCustomerIdentityRequest }) =>
    unwrap(addCustomerIdentity({ path: { id: b.id }, body: b.body, headers: headers(key) })),
  );
  const owner = add.error instanceof ApiError && add.error.code === "identity_taken" ? add.error.details.ownerId : null;
  const doRemove = async (identityId: string) => {
    const r = await remove.run({ id: c.rootId, identityId });
    if (r.ok) reloadCustomers(qc);
  };
  const doAdd = async () => {
    const v = kind === "phone" ? cleanPhone(value) : value.trim();
    if (v === "") return;
    const r = await add.run({ id: c.rootId, body: { kind, value: v } });
    if (!r.ok) return;
    setValue("");
    setAdding(false);
    reloadCustomers(qc);
  };
  const message = remove.message ?? add.message;

  return (
    <div className="card">
      <h2>Liên hệ</h2>
      {c.identities.length === 0 && <p className="muted">Chưa có SĐT hay email.</p>}
      <div className="idl">
        {c.identities.map((i) => {
          const shown = identityText(i);
          const meta = [
            KIND_TEXT[i.kind],
            SOURCE_TEXT[i.source],
            ...(i.customerId === c.rootId ? [] : ["hồ sơ đã gộp"]),
          ].join(" · ");
          return (
            <div className="idr" key={i.id}>
              <span className="grow">
                <b className={i.kind === "phone" ? "num" : undefined}>{shown === "" ? KIND_TEXT[i.kind] : shown}</b>
                <br />
                <span className="xs muted">{meta}</span>
              </span>
              {canWrite && REMOVABLE.has(i.kind) && (
                <button
                  type="button"
                  className="btn btn-quiet"
                  aria-label={`Gỡ ${shown}`}
                  disabled={remove.pending}
                  onClick={() => {
                    void doRemove(i.id);
                  }}
                >
                  Gỡ
                </button>
              )}
            </div>
          );
        })}
      </div>
      {canWrite && !adding && (
        <button
          type="button"
          className="btn btn-quiet"
          style={{ marginTop: 8 }}
          onClick={() => {
            setAdding(true);
          }}
        >
          + Thêm SĐT hoặc email
        </button>
      )}
      {adding && (
        <div className="row" style={{ marginTop: 8 }}>
          <span className="select-wrap" style={{ flex: "0 1 120px" }}>
            <select
              aria-label="Loại liên hệ"
              value={kind}
              onChange={(e) => {
                if (isAddKind(e.target.value)) setKind(e.target.value);
              }}
            >
              {ADD_KINDS.map((k) => (
                <option key={k} value={k}>
                  {KIND_TEXT[k]}
                </option>
              ))}
            </select>
          </span>
          <input
            className="input"
            aria-label="Liên hệ mới"
            maxLength={MAX_IDENTITY}
            value={value}
            onChange={(e) => {
              setValue(e.target.value);
            }}
            style={{ flex: "1 1 160px" }}
          />
          <button
            type="button"
            className="btn btn-line"
            onClick={() => {
              setAdding(false);
              add.reset();
            }}
          >
            Hủy
          </button>
          <button
            type="button"
            className="btn btn-dark"
            disabled={add.pending || value.trim() === ""}
            onClick={() => {
              void doAdd();
            }}
          >
            Thêm
          </button>
        </div>
      )}
      {message !== null && (
        <p className="err-text" role="alert">
          {message}
          {typeof owner === "string" && (
            <>
              {" "}
              <Link className="cell-a" to={`/khach/${owner}`}>
                Mở hồ sơ khách đó
              </Link>
            </>
          )}
        </p>
      )}
    </div>
  );
}

function Addresses({ c }: { c: Customer }) {
  return (
    <div className="card">
      <h2>Địa chỉ</h2>
      {c.addresses.length === 0 && <p className="muted">Chưa có địa chỉ.</p>}
      <div className="idl">
        {c.addresses.map((a) => (
          <div className="idr" key={a.id}>
            <span className="grow">
              <b>
                {a.address.name} · <span className="num">{displayPhone(a.address.phone)}</span>
              </b>{" "}
              {a.isDefault && <span className="chip">Mặc định</span>}
              <br />
              <span className="small">
                {[a.address.line, a.address.wardName, a.address.provinceName].filter(Boolean).join(", ")}
              </span>
            </span>
          </div>
        ))}
      </div>
      <p className="hint" style={{ marginTop: 8 }}>
        Địa chỉ mới được lưu khi lên đơn cho khách.
      </p>
    </div>
  );
}

function Members({ c }: { c: Customer }) {
  if (c.memberIds.length < 2) return null;
  const phonesOf = (id: string) =>
    c.identities
      .filter((i) => i.customerId === id && i.kind === "phone")
      .map((i) => displayPhone(i.value))
      .join(", ");
  return (
    <div className="card">
      <h2>Hồ sơ đã gộp</h2>
      <div className="idl">
        {[c.rootId, ...c.memberIds.filter((m) => m !== c.rootId)].map((m) => (
          <div className="idr" key={m}>
            <span className="grow">
              {m === c.rootId ? (
                <>
                  <b>{c.code}</b> · {c.fullName}
                  <br />
                  <span className="xs muted">Hồ sơ đại diện</span>
                </>
              ) : (
                <>
                  <b>Hồ sơ đã gộp</b>
                  <br />
                  <span className="xs muted">{phonesOf(m) === "" ? "Không có SĐT riêng" : `SĐT ${phonesOf(m)}`}</span>
                </>
              )}
            </span>
          </div>
        ))}
      </div>
      <Link className="btn btn-quiet" style={{ marginTop: 8 }} to={`/khach/${c.rootId}/gop#lan-gop`}>
        Các lần gộp
      </Link>
    </div>
  );
}

function Candidates({ c, canMerge }: { c: Customer; canMerge: boolean }) {
  const q = useQuery({
    queryKey: queryKey(CUSTOMER_TOPIC, "merge-candidates", c.rootId),
    queryFn: () => unwrap(listCustomerMergeCandidates({ path: { id: c.rootId } })),
  });
  const items = q.data?.items ?? [];
  if (q.isError) {
    return (
      <p className="err-text" role="alert" style={{ marginBottom: 16 }}>
        {`Chưa đọc được gợi ý "có thể là cùng một khách". ${errorMessage(q.error)}`}
      </p>
    );
  }
  if (items.length === 0) return null;
  return (
    <div className="stack-sm" style={{ marginBottom: 16 }}>
      {items.map((m) => (
        <div className="box box-soft" key={m.rootId}>
          <b>Có thể là cùng một khách:</b>{" "}
          <span>{`${m.code} · ${m.fullName} có SĐT ${m.matchedPhones.map(displayPhone).join(", ")}, trùng SĐT với khách này.`}</span>
          {canMerge && (
            <Link className="btn btn-line" style={{ marginLeft: 8 }} to={`/khach/${c.rootId}/gop?voi=${m.rootId}`}>
              So sánh và gộp
            </Link>
          )}
        </div>
      ))}
    </div>
  );
}

export function CustomerProfile() {
  const { id = "" } = useParams();
  const perms = usePerms();
  const q = useCustomer(id);
  const [editing, setEditing] = useState(false);
  if (q.isError) {
    const gone = q.error instanceof ApiError && q.error.status === 404;
    return (
      <div className="box box-err">
        {gone ? "Không có khách này, hoặc bạn không được xem." : errorMessage(q.error)}{" "}
        <Link className="cell-a" to="/khach">
          Về danh sách khách
        </Link>
      </div>
    );
  }
  const c = q.data;
  if (c === undefined) return <p className="muted">Đang tải…</p>;
  const s = c.stats;
  const canWrite = perms.can("customer.write");
  const sub = [
    c.code,
    `tạo ${monthYear(c.createdAt)}`,
    ...(c.birthday === undefined ? [] : [`ngày sinh ${birthdayText(c.birthday)}`]),
  ];

  return (
    <div>
      <p className="crumb">
        <Link to="/khach">Khách hàng</Link> / {c.code}
      </p>
      <div className="page-h">
        <div className="grow" style={{ maxWidth: 860 }}>
          <div className="row">
            <h1>{c.fullName}</h1>
            <span className={c.lifecycle === "customer" ? "chip chip-ok" : "chip"}>
              {c.lifecycle === "customer" ? "Đã mua" : "Tiềm năng"}
            </span>
          </div>
          <p className="sub">{sub.join(" · ")}</p>
        </div>
        <div className="row">
          {perms.can("customer.merge") && (
            <Link className="btn btn-line" to={`/khach/${c.rootId}/gop`}>
              Gộp với khách khác
            </Link>
          )}
          {canWrite && (
            <button
              type="button"
              className="btn btn-line"
              onClick={() => {
                setEditing(true);
              }}
            >
              Sửa
            </button>
          )}
          {perms.can("order.create") && (
            <Link className="btn btn-dark" to="/don/moi">
              Lên đơn
            </Link>
          )}
        </div>
      </div>

      <Candidates c={c} canMerge={perms.can("customer.merge")} />

      <div className="kpis" style={{ marginBottom: 24 }}>
        <div className="kpi">
          <p className="small muted">Đơn</p>
          <p className="v">{s.orderCount}</p>
          <p className="xs muted">không tính nháp, đã hủy</p>
        </div>
        <div className="kpi">
          <p className="small muted">Thành công / hoàn</p>
          <p className="v">{`${String(s.deliveredCount)} / ${String(s.returnedCount)}`}</p>
          <p className="xs muted">{`tỷ lệ hoàn ${percentText(s.returnRateBp)}`}</p>
        </div>
        <div className="kpi">
          <p className="small muted">Đã chi</p>
          <p className="v">
            <span className="money">{formatMoney(s.totalSpent)}</span>
          </p>
          <p className="xs muted">đơn thành công trừ tiền trả hàng</p>
        </div>
        <div className="kpi">
          <p className="small muted">Mua gần nhất</p>
          <p className="v num">{s.lastOrderAt === undefined ? "—" : shortDate(s.lastOrderAt)}</p>
        </div>
      </div>

      <div className="cols">
        <div className="stack">
          <CustomerTimeline customer={c} />
          {canWrite && <NotesPanel customerId={c.rootId} />}
        </div>
        <div className="stack">
          <TagsEditor customer={c} canWrite={canWrite} />
          <Contacts c={c} canWrite={canWrite} />
          <Addresses c={c} />
          <Members c={c} />
        </div>
      </div>
      {editing && (
        <EditDialog
          c={c}
          onClose={() => {
            setEditing(false);
          }}
        />
      )}
    </div>
  );
}
