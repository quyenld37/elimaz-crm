// Gộp khách /khach/:id/gop (kế hoạch M2 Task M2-29; mockup docs/mockups/m2/khach-gop.html trạng thái A–C; HV29, HV30,
// DH13, DH14). Hồ sơ thứ hai lấy từ ?voi= (nút "So sánh và gộp" của gợi ý trùng SĐT), hoặc chọn ở trang này từ gợi ý
// hay ô tìm khách (?theo=staff: nhân viên tự chọn). Phải chọn hồ sơ giữ lại làm đại diện (không chọn sẵn) rồi xác nhận
// lần hai mới gửi mergeCustomer. Hệ thống không bao giờ tự gộp. Chỉ người có customer.merge thấy phần gộp; các lần gộp
// (MergeHistory) ai xem được khách cũng xem được.
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router";
import {
  listCustomerMergeCandidates,
  listCustomers,
  mergeCustomer,
  type Customer,
  type MergeCustomerRequest,
} from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { queryKey } from "../../app/queryClient";
import { VN_TIME_ZONE } from "../../app/time";
import { SEARCH_DELAY_MS, useDebounced } from "../inventory/Balances";
import { customerQuery, displayPhone } from "../len-don/orderRules";
import { formatMoney } from "../products/productRules";
import { Dialog } from "../staff/staffData";
import { CUSTOMER_TOPIC, headers, reloadCustomers, useCustomer, usePerms } from "./customerData";
import { undoDeadline } from "./customerRules";
import { MergeHistory } from "./MergeHistory";
import "../don/don.css";
import "./khach.css";

const SEARCH_LIMIT = 8;
const digits = (s: string) => s.replace(/\D/g, "");

function dateText(d: Date): string {
  return new Intl.DateTimeFormat("vi-VN", {
    timeZone: VN_TIME_ZONE,
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
  }).format(d);
}

function Side({
  c,
  same,
  picked,
  onPick,
}: {
  c: Customer;
  same: ReadonlySet<string>;
  picked: boolean;
  onPick: () => void;
}) {
  const phones = c.identities.filter((i) => i.kind === "phone").map((i) => i.value);
  const addr = c.addresses.find((a) => a.isDefault) ?? c.addresses[0];
  const s = c.stats;
  const phone = (p: string) =>
    same.has(digits(p)) ? (
      <span className="same num nowrap">{displayPhone(p)}</span>
    ) : (
      <span className="num nowrap">{displayPhone(p)}</span>
    );
  return (
    <div className={picked ? "side-c is-pick" : "side-c"}>
      <label className="check">
        <input type="radio" name="giu" checked={picked} onChange={onPick} /> <b>Giữ hồ sơ này làm đại diện</b>
      </label>
      <div className="row">
        <h2>{c.fullName}</h2>
        <span className="chip">{c.code}</span>
      </div>
      <dl className="kv">
        <dt>SĐT</dt>
        <dd>
          {phones.length === 0
            ? "Chưa có"
            : phones.map((p, i) => (
                <span key={p}>
                  {i > 0 && " · "}
                  {phone(p)}
                </span>
              ))}
        </dd>
        <dt>Địa chỉ giao gần đây</dt>
        <dd>
          {addr === undefined ? (
            "—"
          ) : (
            <>
              {addr.address.name} · {phone(addr.address.phone)}
            </>
          )}
        </dd>
        <dt>Đơn</dt>
        <dd>{`${String(s.orderCount)} (${String(s.deliveredCount)} thành công, ${String(s.returnedCount)} hoàn)`}</dd>
        <dt>Đã chi</dt>
        <dd>
          <span className="money">{formatMoney(s.totalSpent)}</span>
        </dd>
        <dt>Nhãn</dt>
        <dd>{c.tags.length === 0 ? "—" : c.tags.map((t) => t.tag.name).join(", ")}</dd>
        <dt>Hồ sơ trong nhóm</dt>
        <dd>{c.memberIds.length}</dd>
      </dl>
    </div>
  );
}

/** Chọn hồ sơ thứ hai khi trang mở không có ?voi=: gợi ý trùng SĐT hoặc tìm khách. */
function PickOther({ c, onPick }: { c: Customer; onPick: (id: string, byStaff: boolean) => void }) {
  const [text, setText] = useState("");
  const typed = customerQuery(text);
  const term = useDebounced(typed.kind === "query" ? typed.q : "", SEARCH_DELAY_MS);
  const cands = useQuery({
    queryKey: queryKey(CUSTOMER_TOPIC, "merge-candidates", c.rootId),
    queryFn: () => unwrap(listCustomerMergeCandidates({ path: { id: c.rootId } })),
  });
  const found = useQuery({
    queryKey: queryKey(CUSTOMER_TOPIC, "search", term),
    enabled: term !== "",
    queryFn: () => unwrap(listCustomers({ query: { q: term, limit: SEARCH_LIMIT } })),
  });
  const hits = term === "" ? [] : (found.data?.items ?? []).filter((x) => x.id !== c.rootId);
  return (
    <div className="card stack-sm">
      <h2>Chọn khách để gộp với {c.code}</h2>
      {(cands.data?.items ?? []).length > 0 && <p className="small muted">Có thể là cùng một khách (trùng SĐT):</p>}
      {(cands.data?.items ?? []).map((m) => (
        <button
          key={m.rootId}
          type="button"
          className="btn btn-line"
          style={{ justifySelf: "start" }}
          onClick={() => {
            onPick(m.rootId, false);
          }}
        >
          {`${m.code} · ${m.fullName}`}
        </button>
      ))}
      <input
        className="input is-search"
        type="search"
        aria-label="Tìm khách để gộp"
        placeholder="4 số cuối SĐT trở lên, tên không dấu hoặc mã KH"
        maxLength={100}
        value={text}
        onChange={(e) => {
          setText(e.target.value);
        }}
      />
      {typed.kind === "short" && <p className="field-err">{typed.text}</p>}
      {(cands.isError || found.isError) && (
        <p className="err-text" role="alert">
          {errorMessage(cands.isError ? cands.error : found.error)}
        </p>
      )}
      {found.isSuccess && term !== "" && hits.length === 0 && <p className="muted">Không tìm thấy khách khác.</p>}
      {hits.map((h) => (
        <button
          key={h.id}
          type="button"
          className="btn btn-quiet"
          style={{ justifySelf: "start" }}
          onClick={() => {
            onPick(h.id, true);
          }}
        >
          {`Chọn ${h.code} · ${h.fullName}${h.phones[0] === undefined ? "" : ` · ${displayPhone(h.phones[0])}`}`}
        </button>
      ))}
    </div>
  );
}

function Compare({ a, otherId, byStaff }: { a: Customer; otherId: string; byStaff: boolean }) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const other = useCustomer(otherId);
  const cands = useQuery({
    queryKey: queryKey(CUSTOMER_TOPIC, "merge-candidates", a.rootId),
    queryFn: () => unwrap(listCustomerMergeCandidates({ path: { id: a.rootId } })),
  });
  const [keep, setKeep] = useState<string | null>(null);
  const [confirm, setConfirm] = useState(false);
  const merge = useWriteAction((key, b: { id: string; body: MergeCustomerRequest }) =>
    unwrap(mergeCustomer({ path: { id: b.id }, body: b.body, headers: headers(key) })),
  );
  const [now] = useState(() => Date.now());

  if (other.isError) return <div className="box box-err">{errorMessage(other.error)}</div>;
  const b = other.data;
  if (b === undefined) return <p className="muted">Đang tải…</p>;
  if (b.rootId === a.rootId) return <div className="box box-info">Hai hồ sơ đã là một khách.</div>;

  const candidate = (cands.data?.items ?? []).find((m) => m.rootId === b.rootId);
  const same = new Set((candidate?.matchedPhones ?? []).map(digits));
  const phonesA = new Set(a.identities.filter((i) => i.kind === "phone").map((i) => digits(i.value)));
  for (const i of b.identities) if (i.kind === "phone" && phonesA.has(digits(i.value))) same.add(digits(i.value));
  const matchedBy: MergeCustomerRequest["matchedBy"] = !byStaff && candidate !== undefined ? "phone" : "staff";

  const survivor = keep === null ? null : keep === a.rootId ? a : b;
  const merged = survivor === null ? null : survivor === a ? b : a;

  const back = () => {
    setConfirm(false);
  };
  const submit = async () => {
    if (survivor === null || merged === null) return;
    const r = await merge.run({ id: survivor.rootId, body: { mergedId: merged.rootId, matchedBy } });
    if (!r.ok) return;
    setConfirm(false);
    reloadCustomers(qc);
    void navigate(`/khach/${survivor.rootId}`);
  };

  return (
    <>
      <div className="split">
        {[a, b].map((c) => (
          <Side
            key={c.rootId}
            c={c}
            same={same}
            picked={keep === c.rootId}
            onPick={() => {
              setKeep(c.rootId);
            }}
          />
        ))}
      </div>
      <div className="savebar is-sticky" style={{ marginTop: 16 }}>
        <Link className="btn btn-line" to={`/khach/${a.rootId}`}>
          Không gộp
        </Link>
        <button
          type="button"
          className="btn btn-dark"
          disabled={survivor === null}
          onClick={() => {
            merge.reset();
            setConfirm(true);
          }}
        >
          {survivor === null ? "Chọn hồ sơ giữ lại" : `Gộp vào ${survivor.code}…`}
        </button>
      </div>
      {confirm && survivor !== null && merged !== null && (
        <Dialog
          title={`Gộp ${merged.code} vào ${survivor.code}?`}
          onClose={back}
          footer={
            <>
              <button type="button" className="btn btn-line" onClick={back}>
                Quay lại
              </button>
              <button
                type="button"
                className="btn btn-dark"
                disabled={merge.pending}
                onClick={() => {
                  void submit();
                }}
              >
                Gộp
              </button>
            </>
          }
        >
          <p>
            {`Sau khi gộp: hồ sơ, đơn, SĐT, địa chỉ của ${merged.fullName} hiện chung ở hồ sơ ${survivor.fullName}; chỉ số tính lại cho cả nhóm. Không dữ liệu nào bị xóa.`}
          </p>
          <p className="small muted">
            {`Hoàn tác được tới ${dateText(undoDeadline(new Date(now).toISOString()))} nếu chưa có lần gộp nào khác chạm hai hồ sơ này.`}
          </p>
          {merge.message !== null && (
            <div className="box box-err" role="alert">
              {merge.message}
            </div>
          )}
        </Dialog>
      )}
    </>
  );
}

export function MergeView() {
  const { id = "" } = useParams();
  const [params, setParams] = useSearchParams();
  const voi = params.get("voi");
  const byStaff = params.get("theo") === "staff";
  const perms = usePerms();
  const q = useCustomer(id);
  const canMerge = perms.can("customer.merge");
  if (q.isError) return <div className="box box-err">{errorMessage(q.error)}</div>;
  const a = q.data;
  if (a === undefined) return <p className="muted">Đang tải…</p>;

  return (
    <div>
      <p className="crumb">
        <Link to="/khach">Khách hàng</Link> / <Link to={`/khach/${a.rootId}`}>{a.code}</Link> / Gộp
      </p>
      <div className="page-h">
        <div>
          <h1>Gộp hai khách</h1>
          <p className="sub">
            Gộp không dời đơn hay hội thoại: hai hồ sơ thành một nhóm, xem chung một chỗ. Hoàn tác được trong 30 ngày.
          </p>
        </div>
      </div>
      {canMerge && (
        <div className="stack" style={{ marginBottom: 24 }}>
          {voi === null ? (
            <PickOther
              c={a}
              onPick={(other, staff) => {
                setParams(staff ? { voi: other, theo: "staff" } : { voi: other });
              }}
            />
          ) : (
            <Compare key={voi} a={a} otherId={voi} byStaff={byStaff} />
          )}
          <p className="hint">Hệ thống không bao giờ tự gộp, kể cả khi trùng SĐT.</p>
        </div>
      )}
      <MergeHistory customer={a} canMerge={canMerge} />
    </div>
  );
}
