// Màn Kho — Kiểm kho mở sổ (kế hoạch M7 Task 273; mockup đã duyệt docs/mockups/m7/kiem-kho-mo-so.html, trạng thái A–G).
// Đếm toàn bộ hàng của một kho CHƯA có tồn để làm số tồn đầu tiên; đăng xong tồn mỗi mã bằng đúng số đếm. Máy chủ giữ
// mọi luật (kho trống, đủ mã, đăng một lần, khóa); màn chỉ hiện đúng điều máy chủ báo (HV01, HV03–HV09).
//   OpeningStart   /kho/mo-so       chọn kho, tạo phiên (A)
//   OpeningSession /kho/mo-so/:id   đếm (B), còn N mã chưa đếm và đăng (C, E), nhập Excel (D), kết quả kiểm (F), quyền, lỗi (G)
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { Link, useParams, useSearchParams } from "react-router";
import {
  cancelStocktake,
  checkOpeningStocktake,
  createOpeningStocktake,
  getOpeningStocktake,
  getStocktake,
  listOpeningStocktakes,
  postOpeningStocktake,
  recordStocktakeCount,
  searchVariants,
  type OpeningCheckResult,
  type OpeningStocktake,
  type Warehouse,
} from "../../../api";
import { ApiError, errorMessage, unwrap } from "../../../app/errors";
import { useWriteAction } from "../../../app/idempotency";
import { queryKey } from "../../../app/queryClient";
import { shortTime } from "../../../app/time";
import { fmtInt } from "../../imports/importRules";
import { Dialog } from "../../staff/staffData";
import { useMeAndWarehouses } from "../Stocktake";
import { matchHit, canAt } from "../stocktakeRules";
import { WarehouseSelect } from "../WarehouseSelect";
import { ConfirmFooter, ErrorBox, useLater } from "../m7Parts";
import "../stocktake.css";
import "../m7.css";
import { CountImport } from "./CountImport";
import { canPost, postBlockedReason, remaining } from "./openingRules";
import { ScanCounter } from "./ScanCounter";

export const PERM_OPENING = "inventory.stocktake.opening";
export const TEXT_NO_OPENING_WAREHOUSE = "Bạn chưa được mở sổ kho nào";

const openingKey = (id: string) => queryKey("inventory.opening", id);

function PageHead(props: { crumb?: boolean }) {
  return (
    <div className="page-h">
      <div>
        {props.crumb === true && (
          <p className="crumb">
            <Link to="/kho/kiem-kho">Kiểm kho</Link> / Mở sổ
          </p>
        )}
        <h1>Kiểm kho mở sổ</h1>
        <p className="sub">
          Đếm toàn bộ hàng của một kho chưa có tồn để làm số tồn đầu tiên. Đăng xong, tồn của mỗi mã bằng đúng số đếm.
        </p>
      </div>
    </div>
  );
}

const whName = (list: readonly Warehouse[] | undefined, id: string) =>
  list?.find((w) => w.id === id)?.name ?? "kho này";

/** Trạng thái A: chọn kho (chỉ kho được quyền mở sổ), bắt đầu phiên; báo đúng lỗi máy chủ trả. */
export function OpeningStart() {
  const { me, warehouses } = useMeAndWarehouses();
  const [params] = useSearchParams();
  const [picked, setPicked] = useState<string | null>(params.get("kho"));
  const [created, setCreated] = useState<OpeningStocktake | null>(null);
  const [refusal, setRefusal] = useState<{
    code: string;
    warehouseId: string;
    existing: OpeningStocktake | null;
  } | null>(null);
  const grants = me.data?.grants;
  const options = useMemo(
    () => (warehouses.data ?? []).filter((w) => w.status === "active" && canAt(grants ?? [], PERM_OPENING, w.id)),
    [warehouses.data, grants],
  );
  const wh = options.find((w) => w.id === picked) ?? options[0];

  const create = useWriteAction((key: string, body: { warehouseId: string }) =>
    unwrap(createOpeningStocktake({ body, headers: { "Idempotency-Key": key } })),
  );

  const begin = async () => {
    if (wh === undefined) return;
    setCreated(null);
    setRefusal(null);
    const r = await create.run({ warehouseId: wh.id });
    if (r.ok) {
      setCreated(r.data);
      return;
    }
    if (!(r.error instanceof ApiError)) return;
    let existing: OpeningStocktake | null = null;
    if (r.error.code === "opening_exists") {
      try {
        const list = await unwrap(listOpeningStocktakes({ query: { warehouseId: wh.id } }));
        existing = list.items.find((o) => o.status === "open") ?? null;
      } catch {
        existing = null;
      }
    }
    setRefusal({ code: r.error.code, warehouseId: wh.id, existing });
  };

  const loading = me.isPending || warehouses.isPending;
  const failed = me.error ?? warehouses.error;
  const refusedName = refusal === null ? "" : whName(warehouses.data, refusal.warehouseId);

  return (
    <div className="m7 stack">
      <PageHead crumb />
      {loading && <p className="muted">Đang tải…</p>}
      {failed !== null && <ErrorBox error={failed} />}
      {!loading && failed === null && wh === undefined && (
        <div className="empty">
          <h2>{TEXT_NO_OPENING_WAREHOUSE}</h2>
          <p className="muted">Nhờ Chủ shop gán vai trò có quyền mở sổ kho.</p>
        </div>
      )}
      {wh !== undefined && (
        <div className="card">
          <h2>Bắt đầu mở sổ</h2>
          <label className="field">
            <span>Kho</span>
            <span className="select-wrap">
              <WarehouseSelect ariaLabel="Kho" warehouses={options} value={wh.id} onChange={setPicked} />
            </span>
          </label>
          <p className="hint" style={{ marginTop: 8 }}>
            Chỉ hiện kho bạn có quyền mở sổ. Chỉ mở sổ được kho chưa có phiếu, tồn hay hàng giữ nào; mỗi kho mở sổ một
            lần.
          </p>
          <button
            type="button"
            className="btn btn-dark btn-wide"
            style={{ marginTop: 16 }}
            disabled={create.pending}
            onClick={() => {
              void begin();
            }}
          >
            {`Bắt đầu đếm ${wh.name}`}
          </button>
        </div>
      )}

      {created !== null && (
        <div className="toast">
          <span className="grow">
            {`Đã tạo phiên · ${whName(warehouses.data, created.warehouseId)} · `}
            <b>{`${fmtInt(created.linesExpected)} mã`}</b> phải đếm.
          </span>
          <Link className="btn" to={`/kho/mo-so/${created.stocktakeId}`}>
            Vào đếm
          </Link>
        </div>
      )}

      {refusal !== null && refusal.code === "opening_exists" && (
        <div className="box box-info" role="alert">
          <b>{`${refusedName} đã có phiên mở sổ.`}</b> Mọi người cùng đếm vào một phiên.
          {refusal.existing !== null && (
            <div className="row" style={{ marginTop: 8 }}>
              <Link className="btn btn-dark" to={`/kho/mo-so/${refusal.existing.stocktakeId}`}>
                Vào phiên đang đếm
              </Link>
            </div>
          )}
        </div>
      )}
      {refusal !== null && (refusal.code === "warehouse_not_empty" || refusal.code === "stocktake_open") && (
        <div className="box box-err" role="alert">
          <b>
            {refusal.code === "stocktake_open"
              ? `${refusedName} đang có phiên kiểm thường.`
              : `${refusedName} đã có hàng hoặc phiếu kho.`}
          </b>{" "}
          <span>{errorMessage(create.error)}</span>
          <div className="row" style={{ marginTop: 8 }}>
            <Link className="btn btn-line" to="/kho/kiem-kho">
              Sang Kiểm kho
            </Link>
          </div>
        </div>
      )}
      {refusal !== null &&
        refusal.code !== "opening_exists" &&
        refusal.code !== "warehouse_not_empty" &&
        refusal.code !== "stocktake_open" && (
          <div className="box box-err" role="alert">
            {errorMessage(create.error)}
          </div>
        )}
    </div>
  );
}

/** Màn một phiên mở sổ: đếm khi đang mở; kết quả và kiểm lại khi đã đăng. */
export function OpeningSession() {
  const { id = "" } = useParams();
  const qc = useQueryClient();
  const { warehouses } = useMeAndWarehouses();
  const op = useQuery({ queryKey: openingKey(id), queryFn: () => unwrap(getOpeningStocktake({ path: { id } })) });

  // Làm mới tiến độ sau các lần cộng, gộp lại để quét nhanh không bắn một GET mỗi lần.
  const refresh = () => {
    void qc.invalidateQueries({ queryKey: openingKey(id) });
  };
  const refreshSoon = useLater(refresh, 700);

  if (op.isPending) {
    return (
      <div className="m7 stack">
        <PageHead crumb />
        <p className="muted">Đang tải…</p>
      </div>
    );
  }
  if (op.data === undefined) {
    const forbidden = op.error instanceof ApiError && op.error.status === 403;
    return (
      <div className="m7 stack">
        <PageHead crumb />
        {forbidden && (
          <div className="empty">
            <h2>Bạn không có quyền ở kho này</h2>
            <p className="small muted">
              Cần quyền mở sổ kho của kho này (Chủ, Quản lý hoặc nhân viên kho được giao kho). Báo Chủ nếu cần.
            </p>
          </div>
        )}
        <div className="box box-err" role="alert">
          <b>{errorMessage(op.error)}</b> {!forbidden && <span>Các lần quét chờ gửi vẫn giữ trên máy.</span>}
          <div className="row" style={{ marginTop: 8 }}>
            <button
              type="button"
              className="btn btn-line"
              onClick={() => {
                void op.refetch();
              }}
            >
              Thử lại
            </button>
          </div>
        </div>
      </div>
    );
  }
  const o = op.data;
  const name = whName(warehouses.data, o.warehouseId);
  return (
    <div className="m7 stack">
      <PageHead crumb />
      <div className="row-between">
        <div>
          <b>{`Mở sổ · ${name}`}</b>
          {o.appliedAt !== undefined && (
            <>
              <br />
              <span className="small muted">{`Đăng lúc ${shortTime(o.appliedAt)}`}</span>
            </>
          )}
        </div>
        <span className={o.status === "open" ? "chip chip-ok" : "chip"}>
          {o.status === "open" ? "Đang đếm" : o.status === "applied" ? "Đã đăng" : "Đã hủy"}
        </span>
      </div>

      {o.status === "cancelled" && (
        <div className="box box-info">
          <b>Phiên này đã hủy.</b> Tồn không đổi. Kho vẫn mở sổ được bằng một phiên mới ở màn Mở sổ.
        </div>
      )}

      {o.status === "open" && <OpenBody o={o} name={name} onSaved={refreshSoon} onChanged={refresh} />}
      {o.status === "applied" && <AppliedBody o={o} name={name} />}
    </div>
  );
}

function OpenBody(props: { o: OpeningStocktake; name: string; onSaved: () => void; onChanged: () => void }) {
  const { o, name, onSaved, onChanged } = props;
  const left = remaining(o);
  const pct = o.linesExpected === 0 ? 0 : Math.min(100, Math.round((o.linesCounted / o.linesExpected) * 100));
  return (
    <>
      <div className="prog">
        <div className="row-between">
          <span className="v">{`${fmtInt(o.linesCounted)} / ${fmtInt(o.linesExpected)} mã`}</span>
          <span className="small muted">{left > 0 ? `Còn ${fmtInt(left)} mã chưa đếm` : "Đã đếm đủ mọi mã"}</span>
        </div>
        <div className="bar">
          <i style={{ width: `${String(pct)}%` }} />
        </div>
      </div>
      <ScanCounter stocktakeId={o.stocktakeId} onSaved={onSaved} onClosed={onChanged} />
      <ZeroCount o={o} onDone={onChanged} left={left} />
      <CountImport stocktakeId={o.stocktakeId} onDone={onChanged} />
      <PostCard o={o} name={name} onChanged={onChanged} />
    </>
  );
}

/** "Nhập 0": mã không có hàng trên kệ vẫn phải có số đếm 0 thì mới đăng được (máy chủ chưa có danh sách mã chưa đếm). */
function ZeroCount(props: { o: OpeningStocktake; left: number; onDone: () => void }) {
  const { o, left, onDone } = props;
  const [code, setCode] = useState("");
  const [note, setNote] = useState<{ ok: boolean; text: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const record = useWriteAction((key: string, b: { id: string; variantId: string }) =>
    unwrap(
      recordStocktakeCount({
        path: { id: b.id, variantId: b.variantId },
        body: { counted: 0 },
        headers: { "Idempotency-Key": key },
      }),
    ),
  );
  const run = async () => {
    const c = code.trim();
    if (c === "" || busy) return;
    setBusy(true);
    setNote(null);
    try {
      const hit = matchHit(c, await unwrap(searchVariants({ query: { q: c, limit: 20 } })));
      if (hit === null) {
        setNote({ ok: false, text: `Không thấy mã ${c}.` });
        return;
      }
      // Chỉ nhập 0 khi mã chưa có số đếm: đã có người quét thì ghi 0 sẽ xóa số của họ.
      const st = await unwrap(getStocktake({ path: { id: o.stocktakeId } }));
      const line = st.lines.find((l) => l.variantId === hit.variantId);
      if (line !== undefined) {
        setNote({ ok: false, text: `${hit.sku} đã có số đếm ${String(line.counted)}. Dùng −1 để bớt.` });
        return;
      }
      const r = await record.run({ id: o.stocktakeId, variantId: hit.variantId });
      if (r.ok) {
        setNote({ ok: true, text: `Đã nhập 0 cho ${hit.sku}.` });
        setCode("");
        onDone();
      } else setNote({ ok: false, text: errorMessage(r.error) });
    } catch (e) {
      setNote({ ok: false, text: errorMessage(e) });
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="card">
      <h2>{left > 0 ? `Còn ${fmtInt(left)} mã chưa đếm` : "Mã không có hàng"}</h2>
      <p className="small muted">
        Phải có số cho mọi mã đang bán mới đăng được. Mã thật sự không có trên kệ thì nhập mã đó rồi bấm "Nhập 0".
      </p>
      <div className="row" style={{ marginTop: 8 }}>
        <input
          className="input"
          aria-label="Mã không có hàng trên kệ"
          placeholder="Gõ SKU hoặc barcode"
          value={code}
          onChange={(e) => {
            setCode(e.target.value);
          }}
        />
        <button
          type="button"
          className="btn btn-line"
          disabled={busy || code.trim() === ""}
          onClick={() => {
            void run();
          }}
        >
          Nhập 0
        </button>
      </div>
      {note !== null && (
        <p className={note.ok ? "small ok-text" : "small err-text"} role="status" style={{ marginTop: 8 }}>
          {note.text}
        </p>
      )}
    </div>
  );
}

function PostCard(props: { o: OpeningStocktake; name: string; onChanged: () => void }) {
  const { o, name, onChanged } = props;
  const [confirm, setConfirm] = useState(false);
  const [cancelAsk, setCancelAsk] = useState(false);
  const [result, setResult] = useState<{ code: string; count: number | null } | null>(null);
  const post = useWriteAction((key: string, b: { id: string }) =>
    unwrap(postOpeningStocktake({ path: { id: b.id }, headers: { "Idempotency-Key": key } })),
  );
  const cancel = useWriteAction(async (key: string, b: { id: string }) => {
    const st = await unwrap(getStocktake({ path: { id: b.id } }));
    return unwrap(
      cancelStocktake({ path: { id: b.id }, body: { version: st.version }, headers: { "Idempotency-Key": key } }),
    );
  });
  const reason = postBlockedReason(o);

  const doPost = async () => {
    setResult(null);
    const r = await post.run({ id: o.stocktakeId });
    setConfirm(false);
    if (r.ok) {
      onChanged();
      return;
    }
    if (r.error instanceof ApiError) {
      const c = r.error.details.count;
      setResult({ code: r.error.code, count: typeof c === "number" ? c : null });
      if (r.error.code === "uncounted_lines" || r.error.code === "stocktake_not_open") onChanged();
    } else setResult({ code: "", count: null });
  };
  const doCancel = async () => {
    const r = await cancel.run({ id: o.stocktakeId });
    setCancelAsk(false);
    if (r.ok) onChanged();
  };

  return (
    <div className="stack">
      <div className="card">
        <div className="row-between">
          <h2>Đăng mở sổ</h2>
          <span className="chip">{`${fmtInt(o.linesCounted)} / ${fmtInt(o.linesExpected)}`}</span>
        </div>
        <div className="kpis" style={{ marginTop: 16 }}>
          <div className="kpi">
            <span className="small muted">Đã đếm</span>
            <div className="v">{`${fmtInt(o.linesCounted)} mã`}</div>
          </div>
          <div className="kpi">
            <span className="small muted">Tổng số cái</span>
            <div className="v">{fmtInt(o.unitsCounted)}</div>
          </div>
        </div>
        <div className="savebar" style={{ marginTop: 16 }}>
          <button
            type="button"
            className="btn btn-dark"
            disabled={!canPost(o) || post.pending}
            onClick={() => {
              setConfirm(true);
            }}
          >
            Đăng mở sổ…
          </button>
        </div>
        {reason !== null && (
          <p className="reason" style={{ marginTop: 4 }}>
            {reason}
          </p>
        )}
      </div>

      {result?.code === "uncounted_lines" && (
        <div className="box box-err" role="alert">
          <b>{`Chưa đăng: còn ${result.count === null ? "mã" : fmtInt(result.count)} ${result.count === null ? "chưa đếm" : "mã chưa đếm"}.`}</b>{" "}
          <span>Có mã mới được bật bán trong lúc đếm. Đếm các mã đó rồi đăng lại.</span>
        </div>
      )}
      {result?.code === "stocktake_not_open" && (
        <div className="box box-info" role="alert">
          <b>Phiên này đã được đăng hoặc hủy bởi người khác.</b> Không đăng lại. Màn chuyển sang kết quả.
        </div>
      )}
      {result?.code === "warehouse_not_empty" && (
        <div className="box box-err" role="alert">
          <b>{`Không đăng được: ${name} vừa có phiếu kho khác.`}</b>{" "}
          <span>
            Có người ghi phiếu vào kho này sau khi bắt đầu đếm, nên kho không còn trống. Không số nào được ghi. Kho này
            không mở sổ được nữa: hủy phiên mở sổ rồi dùng Kiểm kho thường.
          </span>
          <div className="row" style={{ marginTop: 8 }}>
            <button
              type="button"
              className="btn btn-danger"
              onClick={() => {
                setCancelAsk(true);
              }}
            >
              Hủy phiên…
            </button>
          </div>
        </div>
      )}
      {result !== null &&
        result.code !== "uncounted_lines" &&
        result.code !== "stocktake_not_open" &&
        result.code !== "warehouse_not_empty" && (
          <div className="box box-err" role="alert">
            {post.message ?? errorMessage(post.error)}
          </div>
        )}
      {cancel.message !== null && (
        <div className="box box-err" role="alert">
          {cancel.message}
        </div>
      )}

      {confirm && (
        <Dialog
          title={`Đăng mở sổ ${name}?`}
          onClose={() => {
            setConfirm(false);
          }}
          footer={
            <ConfirmFooter
              onBack={() => {
                setConfirm(false);
              }}
              onConfirm={() => {
                void doPost();
              }}
              pending={post.pending}
              label="Đăng"
            />
          }
        >
          <p>
            Tồn của <b>{`${fmtInt(o.linesExpected)} mã`}</b> sẽ bằng đúng số đếm, tổng{" "}
            <b>{`${fmtInt(o.unitsCounted)} cái`}</b>. Mã đếm 0 có tồn 0.
          </p>
          <p className="small muted">
            Đăng xong phiên không sửa được nữa. Sai số sau đó sửa bằng phiếu điều chỉnh có lý do ở Phiếu kho.
          </p>
        </Dialog>
      )}
      {cancelAsk && (
        <Dialog
          title="Hủy phiên mở sổ?"
          onClose={() => {
            setCancelAsk(false);
          }}
          footer={
            <ConfirmFooter
              onBack={() => {
                setCancelAsk(false);
              }}
              onConfirm={() => {
                void doCancel();
              }}
              pending={cancel.pending}
              label="Hủy phiên"
              danger
            />
          }
        >
          <p>Số đã đếm của phiên này bị bỏ. Tồn không đổi.</p>
        </Dialog>
      )}
    </div>
  );
}

function AppliedBody(props: { o: OpeningStocktake; name: string }) {
  const { o, name } = props;
  const [res, setRes] = useState<OpeningCheckResult | null>(null);
  const [err, setErr] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const [at, setAt] = useState<string | null>(null);
  const check = async () => {
    setBusy(true);
    setErr(null);
    try {
      setRes(await unwrap(checkOpeningStocktake({ path: { id: o.stocktakeId } })));
      setAt(new Date().toISOString());
    } catch (e) {
      setErr(e);
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="stack">
      <div className="box box-ok">
        <b className="ok-text">{`Đã mở sổ ${name}.`}</b>
        {` ${fmtInt(o.linesCounted)} mã, ${fmtInt(o.unitsCounted)} cái.`}
        {o.appliedAt !== undefined && ` Đăng lúc ${shortTime(o.appliedAt)}.`}
      </div>
      <div className="card">
        <div className="row-between">
          <h2>Kiểm lại</h2>
          {res !== null && <ResultChip r={res} />}
        </div>
        {res === null && <p className="small muted">So số đếm với sổ kho và tồn hiện tại. Đây là mốc A của đêm mở.</p>}
        {res !== null && <CheckBody o={o} r={res} at={at} />}
        {err !== null && (
          <div className="box box-err" role="alert" style={{ marginTop: 8 }}>
            {errorMessage(err)}
          </div>
        )}
        <div className="row" style={{ marginTop: 8 }}>
          <button
            type="button"
            className="btn btn-line"
            disabled={busy}
            onClick={() => {
              void check();
            }}
          >
            Kiểm lại
          </button>
        </div>
      </div>
      <div className="box box-info">
        <b>Phát hiện đếm sai sau khi đăng?</b> Tạo phiếu điều chỉnh có lý do ở Phiếu kho. Phiên mở sổ không mở lại.
        <div className="row" style={{ marginTop: 8 }}>
          <Link className="btn btn-line" to="/kho/phieu">
            Sang Phiếu kho
          </Link>
        </div>
      </div>
    </div>
  );
}

function ResultChip({ r }: { r: OpeningCheckResult }) {
  if (!r.ok) return <span className="chip chip-err">{`Lệch ${String(r.mismatches.length)} mã`}</span>;
  return <span className="chip chip-ok">{r.changedSince ? "Sổ khớp" : "Khớp"}</span>;
}

function CheckBody({ o, r, at }: { o: OpeningStocktake; r: OpeningCheckResult; at: string | null }) {
  if (!r.ok) {
    return (
      <>
        <div className="tbl-frame" style={{ marginTop: 8 }}>
          <table className="tbl">
            <thead>
              <tr>
                <th>Mã</th>
                <th className="r">Đếm</th>
                <th className="r">Sổ mở sổ</th>
                <th className="r">Tồn hiện tại</th>
              </tr>
            </thead>
            <tbody>
              {r.mismatches.map((m) => (
                <tr key={m.variantId} className="is-err">
                  <td className="lead">
                    <span className="bold nowrap">{m.sku}</span>
                  </td>
                  <td data-label="Đếm" className="r num">
                    {m.counted}
                  </td>
                  <td data-label="Sổ mở sổ" className="r num">
                    {m.ledgerOpening}
                  </td>
                  <td data-label="Tồn hiện tại" className="r num">
                    {m.onHand}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <p className="small" style={{ marginTop: 8 }}>
          Mốc A chưa đạt. Báo người quyết đêm mở trước khi mở bán.
        </p>
      </>
    );
  }
  return (
    <>
      <p>
        {r.changedSince
          ? `Sổ mở sổ khớp số đếm của ${fmtInt(o.linesCounted)} mã.`
          : `Sổ kho và tồn của cả ${fmtInt(o.linesCounted)} mã bằng đúng số đếm.`}
      </p>
      <p className="small muted">
        {r.changedSince
          ? "Kho đã có đơn và phiếu khác sau giờ mở bán, nên chỉ so sổ mở sổ với số đếm, không so tồn hiện tại."
          : `Kiểm lúc ${at === null ? "" : shortTime(at)}. Đây là mốc A của đêm mở.`}
      </p>
    </>
  );
}
