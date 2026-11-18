// Màn Kho — soạn, ghi sổ, hủy phiếu kho (kế hoạch M1 Task 328; mockup đã duyệt docs/mockups/m1/kho-phieu.html, trạng
// thái B–F). "/kho/phieu/moi" là phiếu mới (?kind=import chọn sẵn loại); "/kho/phieu/<id>" là phiếu đã có.
//
// - API không có sửa đầu phiếu: loại, kho, nhà cung cấp, ghi chú chỉ chọn được trước lần lưu đầu (tệp task, Q2).
// - Lưu nháp phiếu mới: createStockDocument rồi replaceStockDocumentLines (hai yêu cầu, mỗi cái một khóa chống trùng).
// - Ghi sổ: hộp xác nhận lần hai; dòng chưa lưu thì lưu trước rồi ghi sổ với version mới. Thiếu hàng (insufficient_stock)
//   → câu "Không đủ hàng: còn N" ở hộp lỗi và ở đúng dòng, phiếu vẫn nháp.
// - Tên mã hàng của dòng: API phiếu chỉ trả variantId, nên lấy từ kết quả tìm khi thêm dòng và từ số dư của kho
//   (listBalances, cũng cho cột "Có thể bán").
import { useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router";
import {
  cancelStockDocument,
  createStockDocument,
  createSupplier,
  getMe,
  getStockDocument,
  listBalances,
  listSuppliers,
  listWarehouses,
  postStockDocument,
  replaceStockDocumentLines,
  searchVariants,
  type Balance,
  type CreateStockDocumentRequest,
  type ReplaceStockDocumentLinesRequest,
  type StockDocument,
  type StockDocumentKind,
  type StockDocumentVersion,
  type Supplier,
  type VariantSearchHit,
} from "../../api";
import { ApiError, errorMessage, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { hasPermission } from "../../app/nav";
import { TOPICS, queryKey } from "../../app/queryClient";
import { formatMoney } from "../products/productRules";
import { Dialog } from "../staff/staffData";
import { DOCS_PATH, NEW_DOC } from "./DocumentList";
import {
  KINDS,
  KIND_LABEL,
  KIND_TITLE,
  MAX_LINES,
  STATUS_CHIP,
  STATUS_LABEL,
  addScanned,
  checkLines,
  formatTime,
  formatUnits,
  fromApiLines,
  hasSupplier,
  lineCost,
  sameLines,
  stepQty,
  toApiLines,
  totals,
  type EditLine,
} from "./documentRules";
import { WarehouseSelect } from "./WarehouseSelect";
import "./inventory.css";

export const TEXT_NO_POST_RIGHT =
  "Cần quyền ghi sổ phiếu kho (vai trò Chủ hoặc Quản lý). Lưu nháp rồi báo người duyệt.";
export const TEXT_FIX_LINES = "Có dòng chưa đúng. Sửa các dòng được đánh dấu rồi lưu lại.";
export const TEXT_NO_MATCH = "Không có mã hàng nào khớp";
export const TEXT_TOO_MANY = `Một phiếu tối đa ${String(MAX_LINES)} dòng`;
const TEXT_SAVED = "Đã lưu nháp.";
/** Số trang số dư (mỗi trang 100) đọc để lấy tên mã hàng của dòng. */
const BALANCE_PAGES = 10;
const SEARCH_DELAY_MS = 300;

/** Tên một mã hàng để hiện ở dòng. */
interface VariantInfo {
  sku: string;
  productName: string;
  color: string;
  size: string;
}

const HITS_KEY = queryKey("inventory.documentHits");
const pendingKey = (id: string) => queryKey("inventory.documentPending", id);
const docKey = (id: string) => queryKey(TOPICS.balance, "document", id);

/** Nhớ tên mã hàng vừa tìm thấy (sống qua lần chuyển từ phiếu mới sang phiếu đã lưu). */
function rememberHit(qc: QueryClient, h: VariantSearchHit) {
  qc.setQueryData<Record<string, VariantInfo>>(HITS_KEY, (prev) => ({
    ...prev,
    [h.variantId]: { sku: h.sku, productName: h.productName, color: h.color, size: h.size },
  }));
}

/** Dòng chưa lưu được của phiếu vừa tạo: màn phiếu đã có nhận lại để người dùng lưu lại. */
interface Pending {
  lines: EditLine[];
  notice: string;
}

const isKind = (v: string | null): v is StockDocumentKind => KINDS.some((k) => k === v);

function useGrants() {
  const me = useQuery({ queryKey: queryKey("identity.me"), queryFn: () => unwrap(getMe()) });
  const g = me.data?.grants ?? [];
  return {
    canCreate: hasPermission(g, "inventory.documents.create"),
    canPost: hasPermission(g, "inventory.documents.post"),
    canSupplier: hasPermission(g, "inventory.suppliers.manage"),
  };
}

function useWarehouses() {
  return useQuery({ queryKey: queryKey("inventory.warehouses"), queryFn: () => unwrap(listWarehouses()) });
}

/** Số dư của kho (tối đa BALANCE_PAGES trang) để lấy tên mã hàng và "có thể bán" của dòng. */
function useBalances(warehouseId: string) {
  return useQuery({
    queryKey: queryKey(TOPICS.balance, "documentLines", warehouseId),
    enabled: warehouseId !== "",
    queryFn: async () => {
      const all: Balance[] = [];
      let cursor: string | undefined;
      for (let i = 0; i < BALANCE_PAGES; i++) {
        const page = await unwrap(
          listBalances({ query: { warehouseId, limit: 100, ...(cursor !== undefined && { cursor }) } }),
        );
        all.push(...page.items);
        cursor = page.nextCursor;
        if (cursor === undefined) break;
      }
      return all;
    },
  });
}

function useHits() {
  const qc = useQueryClient();
  return useQuery({
    queryKey: HITS_KEY,
    queryFn: () => qc.getQueryData<Record<string, VariantInfo>>(HITS_KEY) ?? {},
    staleTime: Infinity,
    gcTime: Infinity,
  }).data;
}

/** Thay toàn bộ dòng của phiếu nháp (khóa chống trùng; version nằm trong body). */
function useReplaceLines() {
  return useWriteAction((key, b: { id: string; body: ReplaceStockDocumentLinesRequest }) =>
    unwrap(replaceStockDocumentLines({ path: { id: b.id }, body: b.body, headers: { "Idempotency-Key": key } })),
  );
}

export function DocumentEdit() {
  const { id = "" } = useParams();
  const [sp] = useSearchParams();
  if (id === NEW_DOC) {
    const k = sp.get("kind");
    return <NewDocument key={NEW_DOC} initialKind={isKind(k) ? k : "import"} />;
  }
  return <ExistingDocument key={id} id={id} />;
}

// ---------------------------------------------------------------------------------------------------------------
// Phiếu mới

function NewDocument({ initialKind }: { initialKind: StockDocumentKind }) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const { canCreate, canSupplier } = useGrants();
  const warehouses = useWarehouses();
  const [kind, setKind] = useState<StockDocumentKind>(initialKind);
  const [picked, setPicked] = useState("");
  const [supplier, setSupplier] = useState<Supplier | null>(null);
  const [note, setNote] = useState("");
  const [lines, setLines] = useState<EditLine[]>([]);
  const [checked, setChecked] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  const list = (warehouses.data ?? []).filter((w) => w.status === "active");
  const wh = list.find((w) => w.id === picked) ?? list[0];
  const balances = useBalances(wh?.id ?? "");

  const create = useWriteAction((key, body: CreateStockDocumentRequest) =>
    unwrap(createStockDocument({ body, headers: { "Idempotency-Key": key } })),
  );
  const replace = useReplaceLines();

  const errs = checked ? checkLines(lines, kind) : {};

  const save = async () => {
    if (saving || wh === undefined) return;
    setChecked(true);
    if (Object.keys(checkLines(lines, kind)).length > 0) {
      setMessage(TEXT_FIX_LINES);
      return;
    }
    setMessage(null);
    setSaving(true);
    try {
      const body: CreateStockDocumentRequest = {
        warehouseId: wh.id,
        kind,
        ...(hasSupplier(kind) && supplier !== null && { supplierId: supplier.id }),
        ...(note.trim() !== "" && { note: note.trim() }),
      };
      const c = await create.run(body);
      if (!c.ok) {
        setMessage(errorMessage(c.error));
        return;
      }
      let saved = c.data;
      if (lines.length > 0) {
        const r = await replace.run({ id: saved.id, body: { version: saved.version, lines: toApiLines(lines, kind) } });
        if (r.ok) saved = r.data;
        else {
          const pending: Pending = {
            lines,
            notice: `Đã tạo phiếu nháp ${saved.code}, nhưng các dòng chưa lưu: ${errorMessage(r.error)} Bấm Lưu nháp lại.`,
          };
          qc.setQueryData(pendingKey(saved.id), pending);
        }
      }
      qc.setQueryData(docKey(saved.id), saved);
      void qc.invalidateQueries({ queryKey: [TOPICS.balance, "documents"] });
      void navigate(`${DOCS_PATH}/${encodeURIComponent(saved.id)}`, { replace: true });
    } finally {
      setSaving(false);
    }
  };

  if (!canCreate) {
    return (
      <>
        <BackLink />
        <div className="box box-err" role="alert">
          Bạn chưa được cấp quyền soạn phiếu kho.
        </div>
      </>
    );
  }

  return (
    <>
      <BackLink />
      <div className="card">
        <div className="row-between">
          <div>
            <h2>{`${KIND_TITLE[kind]} mới`}</h2>
            <p className="small muted">Nháp, chưa lưu. Loại, kho, nhà cung cấp chọn được tới lần lưu đầu.</p>
          </div>
          <span className="chip">Nháp</span>
        </div>
        {warehouses.isError && (
          <div className="box box-err" role="alert">
            {errorMessage(warehouses.error)}
          </div>
        )}
        <div className="grid-f">
          <label className="field">
            <span>Loại phiếu</span>
            <span className="select-wrap">
              <select
                value={kind}
                onChange={(e) => {
                  const v = e.target.value;
                  if (isKind(v)) setKind(v);
                }}
              >
                {KINDS.map((k) => (
                  <option key={k} value={k}>
                    {KIND_LABEL[k]}
                  </option>
                ))}
              </select>
            </span>
          </label>
          <label className="field">
            <span>Kho</span>
            <span className="select-wrap">
              <WarehouseSelect warehouses={list} value={wh?.id ?? ""} onChange={setPicked} />
            </span>
          </label>
          {hasSupplier(kind) && <SupplierField value={supplier} onChange={setSupplier} canCreate={canSupplier} />}
          <label className="field">
            <span>Ghi chú</span>
            <input
              className="input"
              maxLength={1000}
              value={note}
              onChange={(e) => {
                setNote(e.target.value);
              }}
            />
          </label>
        </div>

        <LinesEditor
          kind={kind}
          lines={lines}
          errors={errs}
          shortage={null}
          balances={balances.data}
          editable
          onChange={setLines}
        />
        <Totals kind={kind} lines={lines} />
        {message !== null && (
          <div className="box box-err" role="alert" style={{ marginTop: 8 }}>
            {message}
          </div>
        )}
        <div className="savebar is-sticky" style={{ marginTop: 8 }}>
          <button
            type="button"
            className="btn btn-line"
            disabled={saving || wh === undefined}
            onClick={() => void save()}
          >
            {saving ? "Đang lưu…" : "Lưu nháp"}
          </button>
        </div>
      </div>
    </>
  );
}

function BackLink() {
  return (
    <p className="crumb">
      <Link to={DOCS_PATH}>← Phiếu kho</Link>
    </p>
  );
}

// ---------------------------------------------------------------------------------------------------------------
// Phiếu đã có

function ExistingDocument({ id }: { id: string }) {
  const doc = useQuery({ queryKey: docKey(id), queryFn: () => unwrap(getStockDocument({ path: { id } })) });
  // Đã có phiếu thì giữ trình soạn kể cả khi lần tải lại sau (báo hiệu realtime) lỗi: không mất phần đang soạn.
  if (doc.data === undefined) {
    if (doc.isError) {
      return (
        <>
          <BackLink />
          <div className="box box-err" role="alert">
            {errorMessage(doc.error)}
          </div>
        </>
      );
    }
    return <p className="muted">Đang tải…</p>;
  }
  // Tải lại lỗi: trả lỗi để trình soạn báo, không lấy bản cũ trong bộ đệm.
  const reload = () =>
    doc
      .refetch()
      .then((r): StockDocument | Error =>
        r.data !== undefined && !r.isError ? r.data : (r.error ?? new Error("không tải được phiếu")),
      );
  return <Editor initial={doc.data} reload={reload} />;
}

interface Shortage {
  variantId: string;
  text: string;
}

function Editor({ initial, reload }: { initial: StockDocument; reload: () => Promise<StockDocument | Error> }) {
  const qc = useQueryClient();
  const { canCreate, canPost } = useGrants();
  const warehouses = useWarehouses();
  const balances = useBalances(initial.warehouseId);
  const [pending] = useState(() => qc.getQueryData<Pending>(pendingKey(initial.id)));
  const [doc, setDoc] = useState(initial);
  const [base, setBase] = useState(() => fromApiLines(initial.lines));
  const [lines, setLines] = useState(() => pending?.lines ?? fromApiLines(initial.lines));
  const [checked, setChecked] = useState(false);
  const [message, setMessage] = useState<string | null>(pending?.notice ?? null);
  const [info, setInfo] = useState<string | null>(null);
  const [conflict, setConflict] = useState(false);
  const [shortage, setShortage] = useState<Shortage | null>(null);
  const [dialog, setDialog] = useState<"post" | "cancel" | null>(null);
  const [busy, setBusy] = useState(false);
  const busyRef = useRef(false);

  useEffect(() => {
    if (pending !== undefined) qc.removeQueries({ queryKey: pendingKey(initial.id), exact: true });
  }, [pending, qc, initial.id]);

  const replace = useReplaceLines();
  const post = useWriteAction((key, b: { id: string; body: StockDocumentVersion }) =>
    unwrap(postStockDocument({ path: { id: b.id }, body: b.body, headers: { "Idempotency-Key": key } })),
  );
  const cancel = useWriteAction((key, b: { id: string; body: StockDocumentVersion }) =>
    unwrap(cancelStockDocument({ path: { id: b.id }, body: b.body, headers: { "Idempotency-Key": key } })),
  );

  const draft = doc.status === "draft";
  const editable = draft && canCreate;
  const dirty = !sameLines(lines, base);
  const errs = checked ? checkLines(lines, doc.kind) : {};
  const whName = (warehouses.data ?? []).find((w) => w.id === doc.warehouseId)?.name ?? "";

  const accept = (d: StockDocument) => {
    qc.setQueryData(docKey(d.id), d);
    setDoc(d);
    const ls = fromApiLines(d.lines);
    setBase(ls);
    setLines(ls);
  };

  const fail = (err: unknown) => {
    const code = err instanceof ApiError ? err.code : "";
    if (code === "version_conflict") {
      setConflict(true);
      return;
    }
    if (code === "insufficient_stock") {
      const v = err instanceof ApiError ? err.details.variantId : undefined;
      setShortage({ variantId: typeof v === "string" ? v : "", text: errorMessage(err) });
      void qc.invalidateQueries({ queryKey: [TOPICS.balance, "documentLines", doc.warehouseId] });
      return;
    }
    setMessage(errorMessage(err));
  };

  /** Lưu dòng nếu đã đổi; trả phiếu sau khi lưu, null khi lỗi (đã báo). */
  const saveLines = async (): Promise<StockDocument | null> => {
    setChecked(true);
    if (Object.keys(checkLines(lines, doc.kind)).length > 0) {
      setMessage(TEXT_FIX_LINES);
      return null;
    }
    if (!dirty) return doc;
    const r = await replace.run({ id: doc.id, body: { version: doc.version, lines: toApiLines(lines, doc.kind) } });
    if (!r.ok) {
      fail(r.error);
      return null;
    }
    accept(r.data);
    return r.data;
  };

  const begin = () => {
    if (busyRef.current) return false;
    busyRef.current = true;
    setBusy(true);
    setMessage(null);
    setInfo(null);
    setConflict(false);
    setShortage(null);
    return true;
  };
  const end = () => {
    busyRef.current = false;
    setBusy(false);
  };

  const onSave = async () => {
    if (!begin()) return;
    try {
      const d = await saveLines();
      if (d !== null) {
        setInfo(TEXT_SAVED);
        void qc.invalidateQueries({ queryKey: [TOPICS.balance, "documents"] });
      }
    } finally {
      end();
    }
  };

  const onPost = async () => {
    if (!begin()) return;
    try {
      const d = await saveLines();
      if (d === null) {
        setDialog(null);
        return;
      }
      const r = await post.run({ id: d.id, body: { version: d.version } });
      setDialog(null);
      if (!r.ok) {
        fail(r.error);
        return;
      }
      accept(r.data);
      void qc.invalidateQueries({ queryKey: [TOPICS.balance] });
    } finally {
      end();
    }
  };

  const onCancel = async () => {
    if (!begin()) return;
    try {
      const r = await cancel.run({ id: doc.id, body: { version: doc.version } });
      setDialog(null);
      if (!r.ok) {
        fail(r.error);
        return;
      }
      accept(r.data);
      void qc.invalidateQueries({ queryKey: [TOPICS.balance, "documents"] });
    } finally {
      end();
    }
  };

  const onReload = async () => {
    const d = await reload();
    if (d instanceof Error) {
      setMessage(errorMessage(d));
      return;
    }
    accept(d);
    setConflict(false);
    setMessage(null);
    setChecked(false);
  };

  const t = totals(lines);
  const sub = draft
    ? `Nháp · ${whName} · tạo lúc ${formatTime(doc.createdAt)}`
    : doc.status === "posted" && doc.postedAt !== undefined
      ? `Ghi sổ lúc ${formatTime(doc.postedAt)} · ${whName}`
      : `Đã hủy · ${whName} · tạo lúc ${formatTime(doc.createdAt)}`;

  return (
    <>
      <BackLink />
      <div className="card">
        <div className="row-between">
          <div>
            <h2>{`${KIND_TITLE[doc.kind]} ${doc.code}`}</h2>
            <p className="small muted">{sub}</p>
          </div>
          <span className={STATUS_CHIP[doc.status]}>{STATUS_LABEL[doc.status]}</span>
        </div>

        <dl className="grid-f" style={{ margin: 0 }}>
          {hasSupplier(doc.kind) && (
            <div className="field">
              <dt className="label">Nhà cung cấp</dt>
              <dd style={{ margin: 0 }} className="wrap-any">
                {doc.supplierName === "" ? "—" : doc.supplierName}
              </dd>
            </div>
          )}
          <div className="field">
            <dt className="label">Loại phiếu</dt>
            <dd style={{ margin: 0 }}>{KIND_LABEL[doc.kind]}</dd>
          </div>
          <div className="field">
            <dt className="label">Kho</dt>
            <dd style={{ margin: 0 }}>{whName}</dd>
          </div>
          {doc.note !== "" && (
            <div className="field">
              <dt className="label">Ghi chú</dt>
              <dd style={{ margin: 0 }} className="wrap-any">
                {doc.note}
              </dd>
            </div>
          )}
        </dl>

        {shortage !== null && <ShortageBox s={shortage} balances={balances.data} />}
        {conflict && (
          <div className="box box-err row-between" role="alert" style={{ marginTop: 16 }}>
            <span>{errorMessage(new ApiError({ status: 409, code: "version_conflict", details: {} }))}</span>
            <button type="button" className="btn btn-line" onClick={() => void onReload()}>
              Tải lại
            </button>
          </div>
        )}

        <LinesEditor
          kind={doc.kind}
          lines={lines}
          errors={errs}
          shortage={shortage}
          balances={balances.data}
          editable={editable}
          onChange={(ls) => {
            setLines(ls);
            setInfo(null);
          }}
        />
        <Totals kind={doc.kind} lines={lines} />

        {message !== null && (
          <div className="box box-err" role="alert" style={{ marginTop: 8 }}>
            {message}
          </div>
        )}
        {info !== null && (
          <p className="small muted" role="status" style={{ marginTop: 8 }}>
            {info}
          </p>
        )}
        {editable && (
          <>
            <div className="savebar is-sticky" style={{ marginTop: 8 }}>
              <button
                type="button"
                className="btn btn-danger"
                disabled={busy}
                onClick={() => {
                  setDialog("cancel");
                }}
              >
                Hủy phiếu
              </button>
              <button type="button" className="btn btn-line" disabled={busy} onClick={() => void onSave()}>
                Lưu nháp
              </button>
              <button
                type="button"
                className={canPost ? "btn btn-dark" : "btn"}
                disabled={!canPost || busy || lines.length === 0}
                onClick={() => {
                  setDialog("post");
                }}
              >
                Ghi sổ…
              </button>
            </div>
            {!canPost && (
              <p className="reason" style={{ textAlign: "right", marginTop: 4 }}>
                {TEXT_NO_POST_RIGHT}
              </p>
            )}
          </>
        )}
        {doc.status === "posted" && (
          <p className="small muted" style={{ marginTop: 16 }}>
            Phiếu đã ghi sổ không sửa được. Sửa sai bằng phiếu điều chỉnh.
          </p>
        )}
      </div>

      {dialog === "post" && (
        <ConfirmDialog
          title={`Ghi sổ phiếu ${doc.code}?`}
          confirm="Ghi sổ"
          working="Đang ghi sổ…"
          className="btn btn-dark"
          busy={busy}
          onConfirm={() => void onPost()}
          onClose={() => {
            setDialog(null);
          }}
        >
          <p>
            <PostEffect kind={doc.kind} warehouse={whName} units={t.units} skus={t.skus} />
            {hasSupplier(doc.kind) && doc.supplierName !== "" && ` Nhà cung cấp: ${doc.supplierName}.`}
          </p>
          <p className="small muted">
            Sau khi ghi sổ phiếu không sửa, không hủy được. Muốn sửa sai thì tạo phiếu điều chỉnh.
          </p>
        </ConfirmDialog>
      )}
      {dialog === "cancel" && (
        <ConfirmDialog
          title={`Hủy phiếu ${doc.code}?`}
          confirm="Hủy phiếu"
          working="Đang hủy…"
          className="btn btn-danger"
          busy={busy}
          onConfirm={() => void onCancel()}
          onClose={() => {
            setDialog(null);
          }}
        >
          <p>Phiếu nháp bị hủy, tồn không đổi. Phiếu đã hủy không mở lại được.</p>
        </ConfirmDialog>
      )}
    </>
  );
}

/** Hộp xác nhận lần hai: "Quay lại" và nút làm; khi đang chạy cả hai khóa, Esc không đóng. */
function ConfirmDialog(p: {
  title: string;
  confirm: string;
  working: string;
  className: string;
  busy: boolean;
  onConfirm: () => void;
  onClose: () => void;
  children: ReactNode;
}) {
  const close = () => {
    if (!p.busy) p.onClose();
  };
  return (
    <Dialog
      title={p.title}
      onClose={close}
      footer={
        <>
          <button type="button" className="btn btn-line" disabled={p.busy} onClick={close}>
            Quay lại
          </button>
          <button type="button" className={p.className} disabled={p.busy} onClick={p.onConfirm}>
            {p.busy ? p.working : p.confirm}
          </button>
        </>
      }
    >
      {p.children}
    </Dialog>
  );
}

function PostEffect(p: { kind: StockDocumentKind; warehouse: string; units: number; skus: number }) {
  const n = <b className="num">{formatUnits(Math.abs(p.units))}</b>;
  const w = <b>{p.warehouse}</b>;
  const m = ` cái (${String(p.skus)} mã).`;
  switch (p.kind) {
    case "import":
      return (
        <>
          Tồn ở {w} tăng {n}
          {m}
        </>
      );
    case "export":
      return (
        <>
          Tồn ở {w} giảm {n}
          {m}
        </>
      );
    case "damage":
      return (
        <>
          Ở {w}, {n}
          {` cái (${String(p.skus)} mã) chuyển sang hàng lỗi; có thể bán giảm theo.`}
        </>
      );
    case "damage_export":
      return (
        <>
          Hàng lỗi và tồn ở {w} cùng giảm {n}
          {m}
        </>
      );
    case "adjust":
      return (
        <>
          Tồn ở {w} {p.units < 0 ? "giảm" : "tăng"} tổng {n}
          {m}
        </>
      );
  }
}

function ShortageBox(p: { s: Shortage; balances: readonly Balance[] | undefined }) {
  const b = p.balances?.find((x) => x.variantId === p.s.variantId);
  return (
    <div className="box box-err" role="alert" style={{ marginTop: 16 }}>
      <b>{`${p.s.text}.`}</b>{" "}
      <span>
        {b !== undefined &&
          `Mã ${b.sku} chỉ còn ${String(Math.max(b.available, 0))} có thể bán${b.held > 0 ? ` (${String(b.held)} cái đang giữ cho đơn)` : ""}. `}
        Phiếu vẫn là nháp, tồn chưa đổi. Giảm số lượng rồi ghi sổ lại.
      </span>
    </div>
  );
}

// ---------------------------------------------------------------------------------------------------------------
// Bảng dòng và ô quét

function LinesEditor(p: {
  kind: StockDocumentKind;
  lines: EditLine[];
  errors: Record<string, string>;
  shortage: Shortage | null;
  balances: readonly Balance[] | undefined;
  editable: boolean;
  onChange: (ls: EditLine[]) => void;
}) {
  const hits = useHits();
  const isImport = p.kind === "import";
  const infoOf = (id: string): VariantInfo | undefined => hits?.[id] ?? p.balances?.find((b) => b.variantId === id);
  const availableOf = (id: string): string => {
    const b = p.balances?.find((x) => x.variantId === id);
    if (b !== undefined) return formatUnits(b.available);
    return p.balances === undefined ? "…" : "0";
  };
  const set = (variantId: string, patch: Partial<EditLine>) => {
    p.onChange(p.lines.map((l) => (l.variantId === variantId ? { ...l, ...patch } : l)));
  };

  return (
    <>
      {p.editable && (
        <ScanField
          full={p.lines.length >= MAX_LINES}
          onPick={(variantId) => {
            p.onChange(addScanned(p.lines, variantId));
          }}
        />
      )}
      {p.lines.length > 0 && (
        <div className="tbl-frame" style={{ marginTop: 16 }}>
          <table className="tbl ltbl">
            <thead>
              <tr>
                <th>Mã hàng</th>
                <th className="c-q">Số lượng</th>
                {isImport ? (
                  <>
                    <th className="c-c">Giá nhập (₫)</th>
                    <th className="r">Thành tiền</th>
                  </>
                ) : (
                  <th className="r">Có thể bán</th>
                )}
                {p.editable && (
                  <th className="c-x">
                    <span className="sr">Xóa</span>
                  </th>
                )}
              </tr>
            </thead>
            <tbody>
              {p.lines.map((l) => {
                const v = infoOf(l.variantId);
                const short = p.shortage !== null && p.shortage.variantId === l.variantId ? p.shortage.text : null;
                const err = p.errors[l.variantId] ?? short;
                const sku = v?.sku ?? "Mã hàng chưa tải được tên";
                const cost = lineCost(l);
                return (
                  <tr key={l.variantId} className={err !== null ? "is-err" : undefined}>
                    <td className="lead">
                      <span className="v-cell">
                        <span className="sku">{sku}</span>
                        {v !== undefined && (
                          <span className="clamp2">{`${v.productName} · ${v.color} · ${v.size}`}</span>
                        )}
                        {err !== null && <span className="field-err">{err}</span>}
                      </span>
                    </td>
                    <td data-label="Số lượng">
                      {p.editable ? (
                        <span className="stepper">
                          <button
                            type="button"
                            className="ibtn"
                            aria-label="Bớt 1"
                            onClick={() => {
                              p.onChange(stepQty(p.lines, l.variantId, -1, p.kind));
                            }}
                          >
                            −
                          </button>
                          <input
                            className={err !== null ? "input is-err" : "input"}
                            inputMode="numeric"
                            aria-label="Số lượng"
                            value={l.qty}
                            onChange={(e) => {
                              set(l.variantId, { qty: e.target.value });
                            }}
                          />
                          <button
                            type="button"
                            className="ibtn"
                            aria-label="Thêm 1"
                            onClick={() => {
                              p.onChange(stepQty(p.lines, l.variantId, 1, p.kind));
                            }}
                          >
                            +
                          </button>
                        </span>
                      ) : (
                        <span className="num">{l.qty}</span>
                      )}
                    </td>
                    {isImport ? (
                      <>
                        <td data-label="Giá nhập">
                          {p.editable ? (
                            <input
                              className="input num"
                              inputMode="numeric"
                              aria-label="Giá nhập"
                              placeholder="Không bắt buộc"
                              value={l.cost}
                              onChange={(e) => {
                                set(l.variantId, { cost: e.target.value });
                              }}
                            />
                          ) : (
                            <span className="money">{l.cost === "" ? "—" : `${l.cost} ₫`}</span>
                          )}
                        </td>
                        <td data-label="Thành tiền" className={cost === null ? "r muted" : "r money"}>
                          {cost === null ? "—" : formatMoney(cost)}
                        </td>
                      </>
                    ) : (
                      <td data-label="Có thể bán" className="r num">
                        {availableOf(l.variantId)}
                      </td>
                    )}
                    {p.editable && (
                      <td className="c-x">
                        <button
                          type="button"
                          className="ibtn"
                          aria-label={`Xóa dòng ${sku}`}
                          onClick={() => {
                            p.onChange(p.lines.filter((x) => x.variantId !== l.variantId));
                          }}
                        >
                          ×
                        </button>
                      </td>
                    )}
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
      {p.editable && !isImport && (
        <p className="hint" style={{ marginTop: 8 }}>
          Phiếu xuất, chuyển hàng lỗi, xuất hàng lỗi không có nhà cung cấp. Phiếu điều chỉnh nhận số âm (bớt) và số
          dương (thêm).
        </p>
      )}
    </>
  );
}

function Totals({ kind, lines }: { kind: StockDocumentKind; lines: readonly EditLine[] }) {
  if (lines.length === 0) return null;
  const t = totals(lines);
  return (
    <div className="totals">
      <span>
        {`${String(t.skus)} mã · `}
        <b className="num">{formatUnits(t.units)}</b> cái
      </span>
      {kind === "import" && (
        <span>
          Tổng giá nhập: <b className="money">{t.cost === null ? "—" : formatMoney(t.cost)}</b>
        </span>
      )}
    </div>
  );
}

/** Ô "Thêm dòng": máy quét dạng bàn phím gõ mã rồi Enter. Khớp đúng barcode hay SKU (hoặc chỉ một kết quả) → thêm;
 * nhiều kết quả → danh sách để chọn. */
function ScanField({ full, onPick }: { full: boolean; onPick: (variantId: string) => void }) {
  const qc = useQueryClient();
  const [q, setQ] = useState("");
  const [options, setOptions] = useState<VariantSearchHit[]>([]);
  const [hint, setHint] = useState<string | null>(null);
  const [searching, setSearching] = useState(false);

  const pick = (h: VariantSearchHit) => {
    rememberHit(qc, h);
    onPick(h.variantId);
    setQ("");
    setOptions([]);
    setHint(null);
  };

  const onKey = async (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key !== "Enter") return;
    e.preventDefault();
    const term = q.trim();
    if (term === "" || searching) return;
    if (full) {
      setHint(TEXT_TOO_MANY);
      return;
    }
    setSearching(true);
    try {
      const hits = await unwrap(searchVariants({ query: { q: term, limit: 20 } }));
      const up = term.toUpperCase();
      const exact = hits.find((h) => h.barcode === term || h.sku.toUpperCase() === up);
      const one = exact ?? (hits.length === 1 ? hits[0] : undefined);
      if (one !== undefined) pick(one);
      else if (hits.length === 0) {
        setOptions([]);
        setHint(TEXT_NO_MATCH);
      } else {
        setOptions(hits);
        setHint(null);
      }
    } catch (err) {
      setHint(errorMessage(err));
    } finally {
      setSearching(false);
    }
  };

  return (
    <div className="field combo" style={{ marginTop: 24 }}>
      <label htmlFor="scan">Thêm dòng</label>
      <input
        id="scan"
        className="input is-search"
        placeholder="Quét barcode hoặc gõ SKU, tên mẫu rồi Enter"
        style={{ fontVariantNumeric: "tabular-nums" }}
        maxLength={100}
        autoComplete="off"
        value={q}
        onChange={(e) => {
          setQ(e.target.value);
          setOptions([]);
          setHint(null);
        }}
        onKeyDown={(e) => void onKey(e)}
      />
      {options.length > 0 && (
        <div className="combo-list" role="listbox" aria-label="Mã hàng khớp">
          {options.map((h) => (
            <button
              key={h.variantId}
              type="button"
              className="combo-opt"
              role="option"
              aria-selected={false}
              onClick={() => {
                pick(h);
              }}
            >
              {`${h.sku} · ${h.productName} · ${h.color} · ${h.size}`}
            </button>
          ))}
        </div>
      )}
      <span className={hint === null ? "hint" : "field-err"}>
        {hint ?? "Máy quét dạng bàn phím: quét cùng mã nhiều lần thì số lượng cộng thêm 1."}
      </span>
    </div>
  );
}

// ---------------------------------------------------------------------------------------------------------------
// Ô chọn nhà cung cấp (thêm nhanh). Thay bằng SupplierPicker của Task 327 khi task đó gộp (tệp task, Q1).

function SupplierField(p: { value: Supplier | null; onChange: (s: Supplier | null) => void; canCreate: boolean }) {
  const qc = useQueryClient();
  const [text, setText] = useState(p.value?.name ?? "");
  const [open, setOpen] = useState(false);
  const [term, setTerm] = useState("");
  useEffect(() => {
    const t = setTimeout(() => {
      setTerm(text.trim());
    }, SEARCH_DELAY_MS);
    return () => {
      clearTimeout(t);
    };
  }, [text]);
  const suppliers = useQuery({
    queryKey: queryKey("inventory.suppliers", { q: term }),
    enabled: open,
    queryFn: () => unwrap(listSuppliers({ query: term === "" ? {} : { q: term } })),
  });
  const add = useWriteAction((key, body: { name: string }) =>
    unwrap(createSupplier({ body, headers: { "Idempotency-Key": key } })),
  );

  const typed = text.trim();
  const found = (suppliers.data ?? []).filter((s) => s.status === "active").slice(0, 8);
  const exists = found.some((s) => s.name.trim().toLocaleLowerCase("vi") === typed.toLocaleLowerCase("vi"));
  // Chỉ mời thêm khi đã có kết quả tìm của đúng chữ đang gõ (tránh thêm trùng tên đã có).
  const showAdd = p.canCreate && typed !== "" && !exists && term === typed && suppliers.isSuccess;

  const choose = (s: Supplier) => {
    p.onChange(s);
    setText(s.name);
    setOpen(false);
  };

  return (
    <div className="field combo">
      <label htmlFor="ncc">Nhà cung cấp</label>
      <input
        id="ncc"
        className="input"
        role="combobox"
        aria-expanded={open}
        aria-controls="ncc-list"
        autoComplete="off"
        maxLength={200}
        value={text}
        onFocus={() => {
          setOpen(true);
        }}
        onBlur={() => {
          // Chờ cú bấm vào lựa chọn chạy trước khi đóng danh sách.
          setTimeout(() => {
            setOpen(false);
          }, 150);
        }}
        onChange={(e) => {
          setText(e.target.value);
          setOpen(true);
          if (p.value !== null) p.onChange(null);
        }}
      />
      {open && (found.length > 0 || showAdd) && (
        <div className="combo-list" role="listbox" id="ncc-list" aria-label="Nhà cung cấp">
          {found.map((s) => (
            <button
              key={s.id}
              type="button"
              className="combo-opt"
              role="option"
              aria-selected={p.value?.id === s.id}
              onMouseDown={(e) => {
                e.preventDefault();
              }}
              onClick={() => {
                choose(s);
              }}
            >
              {s.name}
            </button>
          ))}
          {showAdd && (
            <button
              type="button"
              className="combo-opt is-add"
              role="option"
              aria-selected={false}
              disabled={add.pending}
              onMouseDown={(e) => {
                e.preventDefault();
              }}
              onClick={() => {
                void add.run({ name: typed }).then((r) => {
                  if (!r.ok) return;
                  void qc.invalidateQueries({ queryKey: ["inventory.suppliers"] });
                  choose(r.data);
                });
              }}
            >
              {`+ Thêm nhà cung cấp "${typed}"`}
            </button>
          )}
        </div>
      )}
      {add.message !== null && <span className="field-err">{add.message}</span>}
    </div>
  );
}
