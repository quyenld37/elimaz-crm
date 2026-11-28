// Màn Kho — kiểm kho trên điện thoại (kế hoạch M1 Task 329; mockup đã duyệt docs/mockups/m1/kho-kiem-kho.html, trạng
// thái A–F). Đếm hàng đang nằm trên kệ: quét barcode hoặc gõ SKU, số lưu sau 800 ms (useBarcodeScan); xem trước chênh
// lệch do máy chủ tính (trên kệ theo sổ = Tồn − Đang giao); áp dụng chỉ sửa Tồn, không đụng phần giữ (Q8, KH05, KH07).
// API không có danh sách phiên: máy này nhớ phiên đang mở của từng kho (localStorage) và mỗi phiên có đường dẫn riêng
// (docs/tasks/task-329.md, Quyết định 1).
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useId, useMemo, useRef, useState, type ReactNode } from "react";
import { Link, useNavigate, useParams } from "react-router";
import {
  applyStocktake,
  cancelStocktake,
  getMe,
  getStocktake,
  listBalances,
  listWarehouses,
  recordStocktakeCount,
  searchVariants,
  startStocktake,
  type ApplyStocktakeResult,
  type Grant,
  type Stocktake,
  type StocktakeLine,
  type Warehouse,
} from "../../api";
import { ApiError, errorMessage, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { TOPICS, queryKey } from "../../app/queryClient";
import { shortTime } from "../../app/time";
import { signed } from "./Balances";
import { canAt, confirmText, lineKind, matchHit, parseCount, preview, signedDelta } from "./stocktakeRules";
import { useBarcodeScan, type SaveStatus, type ScanEntry } from "./useBarcodeScan";
import { WarehouseSelect } from "./WarehouseSelect";
import "./stocktake.css";

export const TEXT_NO_COUNT_WAREHOUSE = "Bạn chưa được kiểm kho nào";
const PERM_COUNT = "inventory.stocktake";
const PERM_APPLY = "inventory.stocktake.apply";
const PERM_OPENING = "inventory.stocktake.opening";
/** Đọc tên mã của dòng đếm từ máy khác: tối đa bấy nhiêu trang Tồn kho (100 dòng một trang). */
const NAME_PAGES = 20;
const NAME_PAGE_SIZE = 100;

const meKey = queryKey("identity.me");
const warehousesKey = queryKey("inventory.warehouses");
const stocktakeKey = (id: string) => queryKey(TOPICS.balance, "stocktake", id);

// Phiên đang mở mà máy này đã mở hoặc đã vào, theo kho. Chỉ là gợi ý: luôn hỏi lại máy chủ trước khi hiện.
const rememberKey = (warehouseId: string) => `crm.stocktake.open.${warehouseId}`;
function remembered(warehouseId: string): string | null {
  try {
    return localStorage.getItem(rememberKey(warehouseId));
  } catch {
    return null;
  }
}
function remember(st: Stocktake) {
  try {
    if (st.status === "open") localStorage.setItem(rememberKey(st.warehouseId), st.id);
    else if (localStorage.getItem(rememberKey(st.warehouseId)) === st.id) {
      localStorage.removeItem(rememberKey(st.warehouseId));
    }
  } catch {
    // Trình duyệt chặn bộ nhớ: chỉ mất gợi ý "Vào phiên".
  }
}

function PageHead() {
  return (
    <div className="page-h">
      <div>
        <h1>Kiểm kho</h1>
        <p className="sub">
          Đếm hàng đang nằm trên kệ. Hàng đã giao cho hãng (đang giao) không có trên kệ nên không tính.
        </p>
      </div>
    </div>
  );
}

export function useMeAndWarehouses() {
  const me = useQuery({ queryKey: meKey, queryFn: () => unwrap(getMe()) });
  const warehouses = useQuery({ queryKey: warehousesKey, queryFn: () => unwrap(listWarehouses()) });
  return { me, warehouses };
}

/** Trạng thái A: chọn kho, bắt đầu phiên; phiên đang mở của kho (nếu máy này biết) → Vào phiên. */
export function StocktakeStart() {
  const { me, warehouses } = useMeAndWarehouses();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [picked, setPicked] = useState<string | null>(null);
  const [busyAt, setBusyAt] = useState<string | null>(null);
  const grants = me.data?.grants;
  const options = useMemo(
    () => (warehouses.data ?? []).filter((w) => w.status === "active" && canAt(grants ?? [], PERM_COUNT, w.id)),
    [warehouses.data, grants],
  );
  const wh: Warehouse | undefined = options.find((w) => w.id === picked) ?? options[0];
  const knownId = wh === undefined ? null : remembered(wh.id);
  const known = useQuery({
    queryKey: stocktakeKey(knownId ?? ""),
    enabled: knownId !== null,
    queryFn: () => unwrap(getStocktake({ path: { id: knownId ?? "" } })),
  });
  const open = known.data?.status === "open" && known.data.warehouseId === wh?.id ? known.data : undefined;
  useEffect(() => {
    if (known.data !== undefined) remember(known.data);
  }, [known.data]);

  const start = useWriteAction((key: string, body: { warehouseId: string }) =>
    unwrap(startStocktake({ body, headers: { "Idempotency-Key": key } })),
  );

  const loading = me.isPending || warehouses.isPending;
  const failed = me.error ?? warehouses.error;

  return (
    <div className="phone-col">
      <PageHead />
      {loading && <p className="muted">Đang tải…</p>}
      {failed !== null && (
        <div className="box box-err" role="alert">
          {errorMessage(failed)}
        </div>
      )}
      {!loading && failed === null && wh === undefined && (
        <div className="empty">
          <h2>{TEXT_NO_COUNT_WAREHOUSE}</h2>
          <p className="muted">Nhờ Chủ shop gán vai trò có quyền kiểm kho.</p>
        </div>
      )}
      {wh !== undefined && (
        <div className="stack">
          <div className="card">
            <h2>Bắt đầu phiên kiểm</h2>
            <label className="field">
              <span>Kho</span>
              <span className="select-wrap">
                <WarehouseSelect
                  ariaLabel="Kho"
                  warehouses={options}
                  value={wh.id}
                  onChange={(id) => {
                    setPicked(id);
                    setBusyAt(null);
                    start.reset();
                  }}
                />
              </span>
            </label>
            <button
              type="button"
              className="btn btn-dark btn-wide"
              style={{ marginTop: 16 }}
              disabled={start.pending}
              onClick={() => {
                const id = wh.id;
                setBusyAt(null);
                void start.run({ warehouseId: id }).then((r) => {
                  if (r.ok) {
                    remember(r.data);
                    qc.setQueryData(stocktakeKey(r.data.id), r.data);
                    void navigate(`/kho/kiem-kho/${r.data.id}`);
                  } else if (r.error instanceof ApiError && r.error.code === "stocktake_open") {
                    start.reset();
                    setBusyAt(id);
                  }
                });
              }}
            >
              {`Bắt đầu kiểm ${wh.name}`}
            </button>
            {/* Task 273 (HV01 của docs/mockups/m7): kiểm kho mở sổ là màn riêng, vào từ đây khi có quyền mở sổ kho này. */}
            {canAt(grants ?? [], PERM_OPENING, wh.id) && (
              <Link className="btn btn-line btn-wide" style={{ marginTop: 8 }} to={`/kho/mo-so?kho=${wh.id}`}>
                Mở sổ kho này
              </Link>
            )}
            <p className="hint" style={{ marginTop: 8 }}>
              Mỗi kho chỉ có một phiên mở. Trong lúc kiểm vẫn bán và giao hàng bình thường.
            </p>
          </div>
          {start.message !== null && (
            <div className="box box-err" role="alert">
              {start.message}
            </div>
          )}
          {open !== undefined && (
            <div className="box box-info">
              <b>{`${wh.name} đang có phiên ${open.code}`}</b>
              {` mở lúc ${shortTime(open.createdAt)}. Mọi người cùng đếm vào một phiên.`}
              <div className="row" style={{ marginTop: 8 }}>
                <button
                  type="button"
                  className="btn btn-dark"
                  onClick={() => void navigate(`/kho/kiem-kho/${open.id}`)}
                >
                  {`Vào phiên ${open.code}`}
                </button>
              </div>
            </div>
          )}
          {open === undefined && busyAt === wh.id && (
            <div className="box box-info" role="status">
              <b>{`${wh.name} đang có phiên kiểm chưa xong.`}</b> Mọi người cùng đếm vào một phiên: nhờ người mở gửi
              đường dẫn phiên.
            </div>
          )}
        </div>
      )}
    </div>
  );
}

interface VariantName {
  sku: string;
  /** "Tên mẫu · Màu · Size" */
  title: string;
  productName: string;
  color: string;
  size: string;
}

const TEXT_NO_NAME = "Mã chưa có trong Tồn kho";

/** Tên mã của các dòng: từ kết quả quét trên máy này, còn thiếu thì đọc Tồn kho của kho theo trang. */
function useNames(warehouseId: string | undefined, entries: readonly ScanEntry[]) {
  const fromScan = new Map<string, VariantName>();
  for (const e of entries) {
    if (e.hit !== null) {
      const h = e.hit;
      fromScan.set(e.variantId, {
        sku: h.sku,
        title: `${h.productName} · ${h.color} · ${h.size}`,
        productName: h.productName,
        color: h.color,
        size: h.size,
      });
    }
  }
  const missing = entries
    .map((e) => e.variantId)
    .filter((v) => !fromScan.has(v))
    .sort();
  const balances = useQuery({
    queryKey: queryKey(TOPICS.balance, "stocktake-names", warehouseId, missing),
    enabled: warehouseId !== undefined && missing.length > 0,
    staleTime: Infinity,
    queryFn: async () => {
      const want = new Set(missing);
      const found = new Map<string, VariantName>();
      let cursor: string | undefined;
      for (let i = 0; i < NAME_PAGES && found.size < want.size; i++) {
        const page = await unwrap(
          listBalances({
            query: {
              warehouseId: warehouseId ?? "",
              limit: NAME_PAGE_SIZE,
              ...(cursor === undefined ? {} : { cursor }),
            },
          }),
        );
        for (const b of page.items) {
          if (want.has(b.variantId)) {
            found.set(b.variantId, {
              sku: b.sku,
              title: `${b.productName} · ${b.color} · ${b.size}`,
              productName: b.productName,
              color: b.color,
              size: b.size,
            });
          }
        }
        cursor = page.nextCursor;
        if (cursor === undefined) break;
      }
      return found;
    },
  });
  return (variantId: string): VariantName | undefined => fromScan.get(variantId) ?? balances.data?.get(variantId);
}

const STATUS_TEXT: Record<Exclude<SaveStatus, "error">, { text: string; cls: string }> = {
  pending: { text: "Đang lưu…", cls: "muted" },
  saving: { text: "Đang lưu…", cls: "muted" },
  saved: { text: "Đã lưu", cls: "ok-text" },
  retrying: { text: "Chưa lưu được, đang thử lại", cls: "err-text" },
};

function SaveState({ e }: { e: ScanEntry }) {
  if (e.status === "error") return <span className="save-st err-text">{e.message}</span>;
  const s = STATUS_TEXT[e.status];
  return <span className={`save-st ${s.cls}`}>{s.text}</span>;
}

function Stepper(p: { label: string; value: number; onSet: (n: number) => void; onAdd: (d: number) => void }) {
  const [draft, setDraft] = useState<string | null>(null);
  return (
    <span className="stepper">
      <button
        type="button"
        className="ibtn"
        aria-label="Bớt 1"
        onClick={() => {
          p.onAdd(-1);
        }}
      >
        −
      </button>
      <input
        className="input"
        inputMode="numeric"
        aria-label={p.label}
        value={draft ?? String(p.value)}
        onChange={(e) => {
          setDraft(e.target.value);
          const n = parseCount(e.target.value);
          if (n !== null) p.onSet(n);
        }}
        onBlur={() => {
          setDraft(null);
        }}
      />
      <button
        type="button"
        className="ibtn"
        aria-label="Thêm 1"
        onClick={() => {
          p.onAdd(1);
        }}
      >
        +
      </button>
    </span>
  );
}

/** Hộp thoại theo mockup (.dlg): <dialog> mở bằng showModal, Esc đóng; trên điện thoại là tấm trượt từ đáy. */
function Confirm(p: { title: string; children: ReactNode; footer: ReactNode; onClose: () => void }) {
  const ref = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  const close = useRef(p.onClose);
  useEffect(() => {
    close.current = p.onClose;
  });
  useEffect(() => {
    const d = ref.current;
    if (d === null) return;
    if (!d.open) d.showModal();
    const onCancel = (e: Event) => {
      e.preventDefault();
      close.current();
    };
    d.addEventListener("cancel", onCancel);
    return () => {
      d.removeEventListener("cancel", onCancel);
      if (d.open) d.close();
    };
  }, []);
  return (
    <dialog ref={ref} className="dlg" aria-labelledby={titleId}>
      <div className="dlg-h">
        <h3 id={titleId}>{p.title}</h3>
      </div>
      <div className="dlg-b">{p.children}</div>
      <div className="dlg-f">{p.footer}</div>
    </dialog>
  );
}

type Mode = "count" | "preview";

/** Phiên kiểm: đếm (B), xem trước (C), xác nhận (D), đã áp dụng (E), không có quyền áp dụng (F), đã hủy. */
export function StocktakeSession() {
  const { id = "" } = useParams();
  const qc = useQueryClient();
  const { me, warehouses } = useMeAndWarehouses();
  const st = useQuery({
    queryKey: stocktakeKey(id),
    queryFn: () => unwrap(getStocktake({ path: { id } })),
  });
  useEffect(() => {
    if (st.data !== undefined) remember(st.data);
  }, [st.data]);

  return (
    <div className="phone-col">
      <PageHead />
      {st.isPending && <p className="muted">Đang tải…</p>}
      {st.isError && (
        <div className="stack">
          <div className="box box-err" role="alert">
            {errorMessage(st.error)}
          </div>
          <div className="row">
            <Link className="btn btn-line" to="/kho/kiem-kho">
              Về Kiểm kho
            </Link>
          </div>
        </div>
      )}
      {st.data !== undefined && (
        <Session
          key={st.data.id}
          st={st.data}
          grants={me.data?.grants ?? []}
          warehouse={warehouses.data?.find((w) => w.id === st.data.warehouseId)}
          reload={async () => {
            const r = await st.refetch();
            return r.data;
          }}
          setData={(s) => {
            qc.setQueryData(stocktakeKey(s.id), s);
          }}
        />
      )}
    </div>
  );
}

interface SessionProps {
  st: Stocktake;
  grants: readonly Grant[];
  warehouse: Warehouse | undefined;
  reload: () => Promise<Stocktake | undefined>;
  setData: (s: Stocktake) => void;
}

function Session({ st, grants, warehouse, reload, setData }: SessionProps) {
  const [mode, setMode] = useState<Mode>("count");
  const [dialog, setDialog] = useState<"apply" | "cancel" | null>(null);
  const [result, setResult] = useState<ApplyStocktakeResult | null>(null);
  const [unsaved, setUnsaved] = useState(false);
  const canCount = canAt(grants, PERM_COUNT, st.warehouseId);
  const canApply = canAt(grants, PERM_APPLY, st.warehouseId);
  const whName = warehouse?.name ?? "";

  const scan = useBarcodeScan({
    stocktakeId: st.id,
    lines: st.lines,
    lookup: async (code) => matchHit(code, await unwrap(searchVariants({ query: { q: code, limit: 20 } }))),
    save: (key, b) =>
      unwrap(
        recordStocktakeCount({
          path: { id: b.stocktakeId, variantId: b.variantId },
          body: { counted: b.counted, ...(b.lineVersion === undefined ? {} : { lineVersion: b.lineVersion }) },
          headers: { "Idempotency-Key": key },
        }),
      ),
    reload: async () => (await reload())?.lines ?? [],
  });
  const nameOf = useNames(st.warehouseId, scan.entries);

  const apply = useWriteAction((key: string, b: { id: string; version: number }) =>
    unwrap(applyStocktake({ path: { id: b.id }, body: { version: b.version }, headers: { "Idempotency-Key": key } })),
  );
  const cancel = useWriteAction((key: string, b: { id: string; version: number }) =>
    unwrap(cancelStocktake({ path: { id: b.id }, body: { version: b.version }, headers: { "Idempotency-Key": key } })),
  );

  if (st.status === "cancelled") {
    return (
      <div className="stack">
        <div className="box box-info">
          <b>{`Phiên ${st.code} đã hủy. Tồn không đổi.`}</b>
        </div>
        <div className="row">
          <Link className="btn btn-line" to="/kho/kiem-kho">
            Mở phiên mới
          </Link>
        </div>
      </div>
    );
  }

  if (st.status === "applied") {
    return <Applied st={st} result={result} nameOf={nameOf} />;
  }

  const head = (
    <div className="row-between">
      <div>
        <b>{whName === "" ? st.code : `${st.code} · ${whName}`}</b>
        <br />
        <span className="small muted">{`Đang mở · ${String(scan.entries.length)} mã đã đếm`}</span>
      </div>
      <span className="chip chip-ok">Đang mở</span>
    </div>
  );

  if (mode === "preview") {
    const p = preview(st.lines);
    return (
      <div className="stack">
        <p className="small muted">
          Trên kệ theo sổ = Tồn − Đang giao, chụp lúc đếm từng mã. Áp dụng chỉ sửa Tồn; hàng đang giữ cho đơn và các đơn
          không bị đổi.
        </p>
        {unsaved && (
          <div className="box box-err" role="alert">
            Còn mã chưa lưu được. Xem trước dùng số đã lưu trên máy chủ; quay lại Đếm tiếp để sửa.
          </div>
        )}
        {st.lines.length === 0 ? (
          <p className="muted">Chưa đếm mã nào.</p>
        ) : (
          <div className="lines">
            {p.rows.map((l) => (
              <PreviewRow key={l.variantId} l={l} name={nameOf(l.variantId)} />
            ))}
            {p.matched.length > 0 && (
              <div className="line">
                <div>
                  <span className="sku">{p.matched.map((l) => nameOf(l.variantId)?.sku ?? "").join(", ")}</span>
                  <br />
                  <span className="nm">{`${String(p.matched.length)} mã khớp sổ`}</span>
                </div>
                <span className="chip">Không chỉnh</span>
              </div>
            )}
          </div>
        )}
        {p.shortHeld.length > 0 && (
          <div className="box box-soft">
            <b>{`${String(p.shortHeld.length)} mã sẽ thiếu so với hàng đã giữ cho đơn.`}</b>{" "}
            {p.shortHeld
              .map(
                (l) =>
                  `${nameOf(l.variantId)?.sku ?? ""}: đang giữ ${String(l.held)}, đếm được ${String(l.counted)} → có thể bán ${signed(l.availableAfter)}.`,
              )
              .join(" ")}{" "}
            Các đơn vẫn giữ hàng; sau khi áp dụng hệ thống gửi một cảnh báo thường và mã này hiện ở lọc &quot;Thiếu so
            với phần giữ&quot;.
          </div>
        )}
        {apply.message !== null && (
          <div className="box box-err" role="alert">
            {apply.message}
          </div>
        )}
        <div className="savebar is-sticky">
          <button
            type="button"
            className="btn btn-line"
            onClick={() => {
              setMode("count");
            }}
          >
            Đếm tiếp
          </button>
          <button
            type="button"
            className={canApply ? "btn btn-dark" : "btn"}
            disabled={!canApply || st.lines.length === 0}
            onClick={() => {
              apply.reset();
              setDialog("apply");
            }}
          >
            Áp dụng…
          </button>
        </div>
        {!canApply && (
          <p className="reason" style={{ marginTop: 4 }}>
            {`Cần quyền áp dụng kiểm kho (Chủ hoặc Quản lý). Đếm xong thì báo người duyệt vào phiên ${st.code}.`}
          </p>
        )}
        {dialog === "apply" && (
          <Confirm
            title={`Áp dụng phiên ${st.code}?`}
            onClose={() => {
              setDialog(null);
            }}
            footer={
              <>
                <button
                  type="button"
                  className="btn btn-line"
                  onClick={() => {
                    setDialog(null);
                  }}
                >
                  Quay lại
                </button>
                <button
                  type="button"
                  className="btn btn-dark"
                  disabled={apply.pending}
                  onClick={() => {
                    void apply.run({ id: st.id, version: st.version }).then((r) => {
                      if (r.ok) {
                        setResult(r.data);
                        setData(r.data.stocktake);
                      } else {
                        setDialog(null);
                        void reload();
                      }
                    });
                  }}
                >
                  Áp dụng
                </button>
              </>
            }
          >
            <p>{confirmText(st.lines)}</p>
            <p className="small muted">Phiên đã áp dụng không mở lại được. Đếm sai thì mở phiên mới.</p>
          </Confirm>
        )}
      </div>
    );
  }

  const last = scan.last === null ? undefined : scan.entries.find((e) => e.variantId === scan.last?.variantId);
  const lastName = last === undefined ? undefined : nameOf(last.variantId);
  return (
    <div className="stack">
      {head}
      {canCount && (
        <div className="scan field">
          <label htmlFor="stocktake-scan">Quét barcode hoặc gõ mã</label>
          <input
            id="stocktake-scan"
            className="input"
            placeholder="Quét hoặc gõ SKU rồi Enter"
            autoComplete="off"
            autoCapitalize="characters"
            enterKeyHint="done"
            // Máy quét dạng bàn phím gõ vào ô đang có focus.
            autoFocus
            maxLength={100}
            {...scan.input}
          />
        </div>
      )}
      {scan.notFound !== null && (
        <div className="box box-err" role="alert">
          <b>{`Không thấy mã ${scan.notFound}.`}</b>{" "}
          <span>Mã này không có trong danh mục. Kiểm lại tem, hoặc gõ SKU.</span>
        </div>
      )}
      {scan.lookupError !== null && (
        <div className="box box-err" role="alert">
          {scan.lookupError}
        </div>
      )}
      {last !== undefined && scan.last !== null && (
        <div className="last-scan">
          <div className="row-between">
            <span className="small muted">{`Vừa quét · ${scan.last.code}`}</span>
            <SaveState e={last} />
          </div>
          <div>
            <span className="nm name bold">{lastName?.productName ?? TEXT_NO_NAME}</span>
            <br />
            <span className="small">
              {lastName === undefined ? "" : `${lastName.color} · ${lastName.size} · ${lastName.sku}`}
            </span>
          </div>
          <div className="row-between">
            <span className="count-big">{last.counted}</span>
            <Stepper
              label="Số đếm"
              value={last.counted}
              onSet={(n) => {
                scan.setCount(last.variantId, n);
              }}
              onAdd={(d) => {
                scan.add(last.variantId, d);
              }}
            />
          </div>
          <p className="hint">
            Quét cùng mã thêm lần nữa để cộng 1. Gõ số vào ô để sửa. Số được lưu khi ngừng quét chưa tới 1 giây.
          </p>
        </div>
      )}
      <div>
        <h3>{`Đã đếm (${String(scan.entries.length)})`}</h3>
        {scan.entries.length > 0 && (
          <div className="lines" style={{ marginTop: 8 }}>
            {scan.entries.map((e) => {
              const n = nameOf(e.variantId);
              const sku = n?.sku ?? "";
              return (
                <div className="line" key={e.variantId}>
                  <div>
                    <span className="sku">{sku}</span>
                    <br />
                    <span className="nm clamp2">{n?.title ?? TEXT_NO_NAME}</span>
                    <SaveState e={e} />
                  </div>
                  {canCount ? (
                    <Stepper
                      label={`Số đếm ${sku}`}
                      value={e.counted}
                      onSet={(c) => {
                        scan.setCount(e.variantId, c);
                      }}
                      onAdd={(d) => {
                        scan.add(e.variantId, d);
                      }}
                    />
                  ) : (
                    <span className="bold num">{e.counted}</span>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </div>
      {cancel.message !== null && (
        <div className="box box-err" role="alert">
          {cancel.message}
        </div>
      )}
      <div className="savebar is-sticky">
        {canCount && (
          <button
            type="button"
            className="btn btn-danger"
            onClick={() => {
              cancel.reset();
              setDialog("cancel");
            }}
          >
            Hủy phiên
          </button>
        )}
        <button
          type="button"
          className="btn btn-dark"
          onClick={() => {
            void scan.flush().then(async (ok) => {
              setUnsaved(!ok);
              await reload();
              setMode("preview");
            });
          }}
        >
          Xem chênh lệch
        </button>
      </div>
      {dialog === "cancel" && (
        <Confirm
          title={`Hủy phiên ${st.code}?`}
          onClose={() => {
            setDialog(null);
          }}
          footer={
            <>
              <button
                type="button"
                className="btn btn-line"
                onClick={() => {
                  setDialog(null);
                }}
              >
                Quay lại
              </button>
              <button
                type="button"
                className="btn btn-danger"
                disabled={cancel.pending}
                onClick={() => {
                  void cancel.run({ id: st.id, version: st.version }).then((r) => {
                    setDialog(null);
                    if (r.ok) setData(r.data);
                    else void reload();
                  });
                }}
              >
                Hủy phiên
              </button>
            </>
          }
        >
          <p>Số đã đếm của phiên này bỏ đi, Tồn không đổi.</p>
          <p className="small muted">Phiên đã hủy không mở lại được.</p>
        </Confirm>
      )}
    </div>
  );
}

const KIND_CHIP = {
  match: { text: "Không chỉnh", cls: "chip" },
  over: { text: "Thừa", cls: "chip chip-line" },
  under: { text: "Thiếu", cls: "chip chip-line" },
  short_held: { text: "Thiếu so với phần giữ", cls: "chip chip-err" },
} as const;

function PreviewRow({ l, name }: { l: StocktakeLine; name: VariantName | undefined }) {
  const chip = KIND_CHIP[lineKind(l)];
  return (
    <div className="line">
      <div>
        <span className="sku">{name?.sku ?? ""}</span>
        <br />
        <span className="nm clamp2">{name?.title ?? TEXT_NO_NAME}</span>
        {l.delta === 0 && l.inTransit > 0 && (
          <>
            <span className="xs muted">{`Tồn ${String(l.onHand)}, đang giao ${String(l.inTransit)}`}</span>
          </>
        )}
      </div>
      <span className={chip.cls}>{chip.text}</span>
      <div className="diff">
        <span>
          Trên kệ theo sổ<b>{l.expectedOnShelf}</b>
        </span>
        <span>
          Đếm<b>{l.counted}</b>
        </span>
        <span className={l.delta === 0 ? "is-zero" : l.delta < 0 ? "is-neg" : undefined}>
          Chênh<b>{signedDelta(l.delta)}</b>
        </span>
      </div>
    </div>
  );
}

function Applied(p: {
  st: Stocktake;
  result: ApplyStocktakeResult | null;
  nameOf: (variantId: string) => VariantName | undefined;
}) {
  const adjusted = p.result?.adjusted ?? p.st.lines.filter((l) => l.delta !== 0).length;
  const matched = p.st.lines.length - adjusted;
  const shortages = p.result?.shortages ?? [];
  return (
    <div className="stack">
      <div className="box box-ok">
        <b className="ok-text">{`Đã áp dụng ${p.st.code}.`}</b>
        {` ${String(adjusted)} mã đã chỉnh, ${String(matched)} mã khớp.`}
        {p.st.appliedAt !== undefined && ` Áp dụng lúc ${shortTime(p.st.appliedAt)}.`}
      </div>
      {shortages.length > 0 && (
        <div className="box box-soft">
          <b>{`Thiếu so với phần giữ (${String(shortages.length)})`}</b>
          <div className="lines" style={{ marginTop: 8, borderTop: 0 }}>
            {shortages.map((s) => {
              const n = p.nameOf(s.variantId);
              return (
                <div className="line" key={s.variantId}>
                  <div>
                    <span className="sku">{n?.sku ?? ""}</span>
                    <br />
                    <span className="nm">{n?.title ?? TEXT_NO_NAME}</span>
                    {s.orders.length > 0 && (
                      <>
                        <br />
                        <span className="small">
                          {`Đơn đang giữ: ${s.orders.map((o) => `${o.orderCode} (${String(o.quantity)})`).join(", ")}`}
                        </span>
                      </>
                    )}
                  </div>
                  <span className="bold err-text num">{signedDelta(s.available)}</span>
                </div>
              );
            })}
          </div>
          <p className="small" style={{ marginTop: 8 }}>
            Đã gửi một cảnh báo thường (chỉ ghi mã phiên và số mã thiếu). Hãng lấy hàng của các đơn đang giữ sẽ bị chặn
            tới khi điều chỉnh tồn, đổi kho hoặc hủy đơn.
          </p>
        </div>
      )}
      <div className="row">
        <Link className="btn btn-line" to="/kho/ton">
          Xem ở Tồn kho
        </Link>
      </div>
    </div>
  );
}
