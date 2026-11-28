// Form nhập một kiện hàng hoàn của đơn cũ (Task 273; mockup docs/mockups/m7/hang-hoan-don-cu.html, trạng thái A, B; HV10,
// HV11). Mã tham chiếu gõ tay (chuẩn hóa: bỏ khoảng trắng, viết hoa), quét từng món, mỗi dòng một tình trạng (Bán lại
// được / Hỏng), ghi chú ≤ 500 chỉ nằm trong phiếu. Nhập kho = tồn tăng ngay; mỗi kiện một lần (409 → lần nhập cũ).
import { useQuery } from "@tanstack/react-query";
import { useId, useMemo, useRef, useState, type KeyboardEvent } from "react";
import { Link } from "react-router";
import {
  createLegacyReturn,
  getLegacyReturn,
  getMe,
  listWarehouses,
  searchVariants,
  type CreateLegacyReturnRequest,
  type LegacyReturn,
} from "../../../api";
import { ApiError, errorMessage, unwrap } from "../../../app/errors";
import { useWriteAction } from "../../../app/idempotency";
import { queryKey } from "../../../app/queryClient";
import { shortTime } from "../../../app/time";
import { canAt, matchHit } from "../stocktakeRules";
import { WarehouseSelect } from "../WarehouseSelect";
import {
  NOTE_MAX,
  QTY_MAX,
  TEXT_INCOMPLETE,
  addScan,
  blockedReason,
  dayKey,
  normalizeReference,
  qtyError,
  referenceError,
  returnCounts,
  summary,
  type FormLine,
} from "./legacyRules";

export const PERM_LEGACY = "inventory.legacy_return.create";
export const TEXT_NO_LEGACY_WAREHOUSE = "Bạn chưa được nhập hàng hoàn ở kho nào";

const meKey = queryKey("identity.me");
const warehousesKey = queryKey("inventory.warehouses");

export function LegacyReturnForm(props: { onCreated: () => void; onJump: (day: string, id: string) => void }) {
  const { onCreated, onJump } = props;
  const me = useQuery({ queryKey: meKey, queryFn: () => unwrap(getMe()) });
  const warehouses = useQuery({ queryKey: warehousesKey, queryFn: () => unwrap(listWarehouses()) });
  const [picked, setPicked] = useState<string | null>(null);
  const [rawRef, setRawRef] = useState("");
  const [lines, setLines] = useState<FormLine[]>([]);
  const [note, setNote] = useState("");
  const [scanText, setScanText] = useState("");
  const [notFound, setNotFound] = useState<string | null>(null);
  const [lookupError, setLookupError] = useState<string | null>(null);
  const [result, setResult] = useState<LegacyReturn | null>(null);
  const [existingId, setExistingId] = useState<string | null>(null);
  const seq = useRef(0);
  const scanId = useId();

  const grants = me.data?.grants;
  const options = useMemo(
    () => (warehouses.data ?? []).filter((w) => w.status === "active" && canAt(grants ?? [], PERM_LEGACY, w.id)),
    [warehouses.data, grants],
  );
  const wh = options.find((w) => w.id === picked) ?? options[0];
  const ref = normalizeReference(rawRef);
  const refErr = referenceError(ref);
  const reason = blockedReason(ref, lines);

  const create = useWriteAction((key: string, body: CreateLegacyReturnRequest) =>
    unwrap(createLegacyReturn({ body, headers: { "Idempotency-Key": key } })),
  );
  const existing = useQuery({
    queryKey: queryKey("inventory.legacy_return", existingId ?? ""),
    enabled: existingId !== null && existingId !== "",
    queryFn: () => unwrap(getLegacyReturn({ path: { id: existingId ?? "" } })),
  });

  const scan = async () => {
    const code = scanText.trim();
    if (code === "") return;
    setScanText("");
    setNotFound(null);
    setLookupError(null);
    try {
      const hit = matchHit(code, await unwrap(searchVariants({ query: { q: code, limit: 20 } })));
      if (hit === null) {
        setNotFound(code);
        return;
      }
      setLines((cur) => addScan(cur, hit, () => `n${String(++seq.current)}`));
      setResult(null);
    } catch (e) {
      setLookupError(errorMessage(e));
    }
  };
  const onScanKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key !== "Enter") return;
    e.preventDefault();
    void scan();
  };
  const patch = (id: string, p: Partial<FormLine>) => {
    setLines((cur) => cur.map((l) => (l.id === id ? { ...l, ...p } : l)));
  };

  const submit = async () => {
    if (wh === undefined || reason !== null) return;
    setExistingId(null);
    setResult(null);
    const body: CreateLegacyReturnRequest = {
      warehouseId: wh.id,
      reference: ref,
      ...(note.trim() === "" ? {} : { note: note.trim() }),
      lines: lines.map((l) => ({ variantId: l.variantId, qty: l.qty, condition: l.condition })),
    };
    const r = await create.run(body);
    if (r.ok) {
      setResult(r.data);
      setLines([]);
      setRawRef("");
      setNote("");
      onCreated();
    } else if (r.error instanceof ApiError && r.error.code === "legacy_return_exists") {
      const id = r.error.details.existingId;
      setExistingId(typeof id === "string" ? id : "");
    }
  };

  const failed = me.error ?? warehouses.error;
  const exists = create.error instanceof ApiError && create.error.code === "legacy_return_exists";
  const found = existing.data;

  if (!me.isPending && !warehouses.isPending && failed === null && wh === undefined) {
    return (
      <div className="empty">
        <h2>{TEXT_NO_LEGACY_WAREHOUSE}</h2>
        <p className="small muted">Ô Kho nhận chỉ có kho bạn được giao. Báo Chủ nếu cần thêm kho.</p>
      </div>
    );
  }

  return (
    <div className="stack">
      {failed !== null && (
        <div className="box box-err" role="alert">
          {errorMessage(failed)}
        </div>
      )}
      <div className="card">
        <h2>Nhập kiện hàng hoàn</h2>
        <div className="grid-f">
          <label className="field">
            <span>Kho nhận</span>
            <span className="select-wrap">
              <WarehouseSelect ariaLabel="Kho nhận" warehouses={options} value={wh?.id ?? ""} onChange={setPicked} />
            </span>
          </label>
          <label className="field">
            <span>Mã tham chiếu</span>
            <input
              className={refErr === null ? "input" : "input is-err"}
              aria-label="Mã tham chiếu"
              placeholder="Mã vận đơn hoặc mã đơn cũ"
              value={rawRef}
              onChange={(e) => {
                setRawRef(e.target.value);
              }}
            />
            {refErr !== null && <span className="field-err">{refErr}</span>}
            <span className="hint">
              Mã vận đơn in trên kiện hoặc mã đơn cũ. Không phân biệt chữ hoa và khoảng trắng.
              {ref !== "" && ref !== rawRef && ` Sẽ lưu là ${ref}.`}
            </span>
          </label>
        </div>

        <div className="scan field" style={{ marginTop: 16 }}>
          <label htmlFor={scanId}>Quét hoặc gõ SKU để thêm món</label>
          <input
            id={scanId}
            className="input"
            placeholder="Quét barcode hoặc gõ SKU rồi Enter"
            autoComplete="off"
            value={scanText}
            onChange={(e) => {
              setScanText(e.target.value);
            }}
            onKeyDown={onScanKey}
          />
        </div>
        {notFound !== null && (
          <div className="box box-err" role="alert" style={{ marginTop: 8 }}>
            <b>{`Không thấy mã ${notFound}.`}</b> <span>Kiểm lại tem, hoặc gõ SKU.</span>
          </div>
        )}
        {lookupError !== null && (
          <div className="box box-err" role="alert" style={{ marginTop: 8 }}>
            {lookupError}
          </div>
        )}

        {lines.length === 0 ? (
          <div className="empty" style={{ marginTop: 8 }}>
            <p className="small muted">Chưa có món nào. Quét hoặc gõ SKU để thêm.</p>
          </div>
        ) : (
          <div style={{ marginTop: 8 }}>
            {lines.map((l) => {
              const qe = qtyError(l.qty);
              return (
                <div className={qe === null ? "lrow" : "lrow is-err"} key={l.id}>
                  <div>
                    <span className="sku">{l.sku}</span>
                    <br />
                    <span className="nm clamp2">{l.name}</span>
                  </div>
                  <span className="stepper">
                    <button
                      type="button"
                      className="ibtn"
                      aria-label={`Bớt 1 ${l.sku}`}
                      disabled={l.qty <= 1}
                      onClick={() => {
                        patch(l.id, { qty: l.qty - 1 });
                      }}
                    >
                      −
                    </button>
                    <input
                      className={qe === null ? "input qty" : "input qty is-err"}
                      inputMode="numeric"
                      aria-label={`Số lượng ${l.sku}`}
                      value={String(l.qty)}
                      onChange={(e) => {
                        const n = Number.parseInt(e.target.value.replace(/\D/g, ""), 10);
                        patch(l.id, { qty: Number.isNaN(n) ? 0 : n });
                      }}
                    />
                    <button
                      type="button"
                      className="ibtn"
                      aria-label={`Thêm 1 ${l.sku}`}
                      disabled={l.qty >= QTY_MAX}
                      onClick={() => {
                        patch(l.id, { qty: l.qty + 1 });
                      }}
                    >
                      +
                    </button>
                  </span>
                  <span className="cond" role="radiogroup" aria-label={`Tình trạng ${l.sku}`}>
                    <label className={l.condition === "sellable" ? "is-on" : ""}>
                      <input
                        type="radio"
                        name={`cond-${l.id}`}
                        checked={l.condition === "sellable"}
                        onChange={() => {
                          patch(l.id, { condition: "sellable" });
                        }}
                      />
                      Bán lại được
                    </label>
                    <label className={l.condition === "damaged" ? "is-bad is-on" : "is-bad"}>
                      <input
                        type="radio"
                        name={`cond-${l.id}`}
                        checked={l.condition === "damaged"}
                        onChange={() => {
                          patch(l.id, { condition: "damaged" });
                        }}
                      />
                      Hỏng
                    </label>
                  </span>
                  <button
                    type="button"
                    className="ibtn"
                    aria-label={`Bỏ ${l.sku}`}
                    onClick={() => {
                      setLines((cur) => cur.filter((x) => x.id !== l.id));
                    }}
                  >
                    ✕
                  </button>
                  {qe !== null && <p className="field-err full">{qe}</p>}
                </div>
              );
            })}
            <p className="hint" style={{ marginTop: 8 }}>
              Quét một mã nhiều lần để cộng số lượng. Cùng một mã có cả cái tốt và cái hỏng: đổi dòng đã quét sang Hỏng
              rồi quét mã đó lần nữa để có dòng Bán lại được mới. Hàng hỏng vào ngăn hàng lỗi, không bán được.
            </p>
          </div>
        )}

        <label className="field" style={{ marginTop: 16 }}>
          <span>Ghi chú (không bắt buộc)</span>
          <textarea
            className="input"
            aria-label="Ghi chú (không bắt buộc)"
            maxLength={NOTE_MAX}
            placeholder="Ví dụ: khách trả vì sai size, hãng giao lại sau 2 tuần"
            value={note}
            onChange={(e) => {
              setNote(e.target.value);
            }}
          />
          <span className="hint">{`${String(note.length)} / ${String(NOTE_MAX)} · chỉ lưu trong phiếu nhập, không vào nhật ký.`}</span>
        </label>

        <div className="savebar is-sticky" style={{ marginTop: 16 }}>
          <span className="small muted grow">{create.pending ? "Đang nhập… không bấm lại" : summary(lines)}</span>
          <button
            type="button"
            className="btn btn-dark"
            disabled={reason !== null || create.pending}
            onClick={() => {
              void submit();
            }}
          >
            {create.pending ? "Đang nhập…" : "Nhập kho"}
          </button>
        </div>
        {reason === TEXT_INCOMPLETE && (
          <p className="reason" style={{ marginTop: 4 }}>
            {reason}
          </p>
        )}
      </div>

      {create.error !== null && !exists && (
        <div className="box box-err" role="alert">
          {create.message ?? errorMessage(create.error)}
        </div>
      )}
      {exists && (
        <div className="box box-err" role="alert">
          <b>{`Kiện ${found?.reference ?? ref} đã nhập rồi.`}</b>{" "}
          <span>
            {found === undefined
              ? "Không nhập lại, kẻo tồn bị cộng hai lần."
              : `Nhập lúc ${shortTime(found.createdAt)} vào ${warehouses.data?.find((w) => w.id === found.warehouseId)?.name ?? "kho"} (${found.createdBy.name}). Không nhập lại, kẻo tồn bị cộng hai lần.`}
          </span>
          {found !== undefined && (
            <div className="row" style={{ marginTop: 8 }}>
              <a
                className="btn btn-line"
                href={`#kien-${found.id}`}
                onClick={() => {
                  onJump(dayKey(found.createdAt), found.id);
                }}
              >
                Xem lần nhập cũ
              </a>
            </div>
          )}
        </div>
      )}
      {result !== null && (
        <>
          <div className="box box-ok">
            <b className="ok-text">{`Đã nhập kiện ${result.reference} vào ${warehouses.data?.find((w) => w.id === result.warehouseId)?.name ?? "kho"}.`}</b>{" "}
            {`${String(returnCounts(result).total)} cái, ${String(returnCounts(result).damaged)} hỏng.`}
            <div className="row" style={{ marginTop: 8 }}>
              <Link className="btn btn-line" to={`/kho/phieu/${result.stockDocumentId}`}>
                Xem phiếu nhập
              </Link>
              {result.damageDocumentId !== undefined && (
                <Link className="btn btn-line" to={`/kho/phieu/${result.damageDocumentId}`}>
                  Xem phiếu chuyển hàng lỗi
                </Link>
              )}
              <button
                type="button"
                className="btn btn-dark"
                onClick={() => {
                  setResult(null);
                }}
              >
                Nhập kiện khác
              </button>
            </div>
          </div>
          <div className="box box-info small">
            <b>Tồn tăng ngay khi bấm Nhập kho.</b> Món "Bán lại được" vào hàng bán được; món "Hỏng" vào ngăn hàng lỗi.
            Nhập nhầm thì làm phiếu xuất hoặc điều chỉnh có lý do ở Phiếu kho.
          </div>
        </>
      )}
    </div>
  );
}
