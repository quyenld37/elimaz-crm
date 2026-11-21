// Khối Vận chuyển trên chi tiết đơn (kế hoạch M3 Task 133; mockup docs/mockups/m3/don-van-chuyen.html trạng thái A, B,
// C, G, H; HV07–HV11, HV16 chủ shop OK 21/11). Đơn "Đã xác nhận" chưa có vận đơn: so phí các tài khoản của kho đơn
// (mở khối mà đơn chưa từng so phí thì tự hỏi một lần), chọn sẵn giá rẻ nhất, gửi hãng ngay không hộp xác nhận; báo phí
// quá hạn hay đổi cân nặng, tùy chọn sau khi so phí thì nút gửi thành "Báo phí lại"; 409 quote_stale tự báo phí lại và
// giữ tài khoản đã chọn. Tài khoản nhập mã tay dán mã khi gửi; tự giao chỉ một nút. Có vận đơn: đang tạo → kết quả
// lệnh tạo (CreateStateBanner, kèm "Hủy để đổi hãng" khi lệnh tạo đã kết thúc); đã gửi → ShipmentCard (Task 134). Nút
// theo quyền ở kho của đơn; máy chủ vẫn kiểm.
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useEffectEvent, useRef, useState } from "react";
import {
  createShipment,
  requestShippingQuotes,
  type CreateShipmentRequest,
  type Order,
  type Shipment,
} from "../../api";
import { ApiError, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { shortTime } from "../../app/time";
import { reloadOrder } from "../don/orderData";
import { canAt } from "../inventory/stocktakeRules";
import { parseMoney } from "../products/editRules";
import { formatMoney } from "../products/productRules";
import { CancelDialog } from "./CancelDialog";
import { Act, CreateStateBanner, RejectedNote, TEXT_NO_SHIP_PERM } from "./CreateStateBanner";
import { QuoteTable, buildRows, carrierName, isExpired, isPriced, resolvePick, type PickRow } from "./QuoteTable";
import {
  ManualFields,
  ParcelFields,
  initialForm,
  paramsKey,
  toParams,
  type ManualForm,
  type ParcelForm,
  type Params,
} from "./SendForm";
import { ShipmentCard, ShipmentExtras } from "./ShipmentCard";
import {
  SELF_CARRIER,
  shipmentKey,
  useCurrentShipment,
  useDeliveryStats,
  useFulfillmentSettings,
  useGrants,
  useLatestShipment,
  useManualAccounts,
  useNowUntil,
  useQuotes,
} from "./useShipment";
import "./shipping.css";

export const TEXT_QUOTE_STALE_REQUOTED = "Giá đã cũ hoặc đơn vừa đổi: đã báo phí lại. Kiểm lại giá rồi gửi.";
const TEXT_PARAMS_CHANGED =
  "Đã đổi cân nặng, khai giá hoặc tùy chọn sau khi so phí: so phí lại để có giá đúng (muốn tính theo kích thước kiện cũng phải so phí lại).";
const TEXT_EXPIRED = "Giá đã cũ. Báo phí lại trước khi gửi; tài khoản đang chọn được giữ.";

const percent = new Intl.NumberFormat("vi-VN", { maximumFractionDigits: 2 });

function Head({ chip }: { chip: string }) {
  return (
    <div className="row-between">
      <h2>Vận chuyển</h2>
      <span className="chip">{chip}</span>
    </div>
  );
}

interface Ship {
  orderId: string;
  body: CreateShipmentRequest;
}
interface Quote {
  orderId: string;
  params: Params;
}

/** Lỗi làm đơn đã khác bản đang xem (đã có vận đơn, không còn gửi được): đọc lại đơn. */
const ORDER_MOVED = new Set(["shipment_exists", "order_not_shippable", "create_in_progress", "invalid_transition"]);

function Compose({
  o,
  onCreated,
  onEdit,
}: {
  o: Order;
  onCreated: (s: Shipment) => void;
  onEdit?: (() => void) | undefined;
}) {
  const qc = useQueryClient();
  const { loaded, grants } = useGrants();
  const wh = o.warehouseId ?? "";
  const canQuote = canAt(grants, "fulfillment.quote", wh);
  const canShip = canAt(grants, "fulfillment.ship", wh);

  const [form, setForm] = useState<ParcelForm>(() => initialForm(o));
  const [initialKey] = useState(() => {
    const p = toParams(initialForm(o));
    return p === null ? "" : paramsKey(p);
  });
  const [manualForm, setManualForm] = useState<ManualForm>({ code: "", fee: "" });
  const [requestId, setRequestId] = useState<string | null>(null);
  const [quotedKey, setQuotedKey] = useState<string | null>(null);
  const [noApi, setNoApi] = useState(false);
  const [picked, setPicked] = useState<string | null>(null);
  const [info, setInfo] = useState<string | null>(null);

  const quotes = useQuotes(o.id, requestId, loaded && canQuote);
  const manual = useManualAccounts(o.id, loaded && canShip);
  const latest = useLatestShipment(o.id, true);
  const stats = useDeliveryStats(o.customerId);
  const settings = useFulfillmentSettings(true);

  const params = toParams(form);
  const pKey = params === null ? null : paramsKey(params);
  const rows = buildRows(quotes.data?.items ?? [], manual.data ?? []);
  const pick = resolvePick(rows, picked);
  const now = useNowUntil(pick?.kind === "quote" ? pick.q.validUntil : undefined);

  const quoteAct = useWriteAction((key, b: Quote) =>
    unwrap(requestShippingQuotes({ path: { id: b.orderId }, body: b.params, headers: { "Idempotency-Key": key } })),
  );
  const shipAct = useWriteAction((key, b: Ship) =>
    unwrap(createShipment({ path: { id: b.orderId }, body: b.body, headers: { "Idempotency-Key": key } })),
  );

  const requote = async (p: Params): Promise<boolean> => {
    const r = await quoteAct.run({ orderId: o.id, params: p });
    if (r.ok) {
      setRequestId(r.data.requestId);
      setQuotedKey(paramsKey(p));
      setNoApi(r.data.accounts.length === 0);
    } else if (r.error instanceof ApiError && ORDER_MOVED.has(r.error.code)) {
      reloadOrder(qc, o.id);
    }
    return r.ok;
  };

  // HV07: đơn chưa từng so phí → tự hỏi phí một lần khi mở khối.
  const autoAsked = useRef(false);
  const autoQuote = useEffectEvent(() => {
    if (params !== null) void requote(params);
  });
  const neverQuoted = quotes.isSuccess && requestId === null && quotes.data.items.length === 0;
  useEffect(() => {
    if (!neverQuoted || autoAsked.current) return;
    autoAsked.current = true;
    autoQuote();
  }, [neverQuoted]);

  const busy = quoteAct.pending || shipAct.pending;
  const loadingNew = quotes.isPlaceholderData;
  const expired = pick !== null && isExpired(pick, now);
  const changed = pick !== null && pick.kind === "quote" && pKey !== (quotedKey ?? initialKey);
  const stale = pick !== null && isPriced(pick) && (expired || changed);

  const send = async (row: PickRow) => {
    setInfo(null);
    let body: CreateShipmentRequest;
    if (row.kind === "quote") {
      body = {
        carrierAccountId: row.q.carrierAccountId,
        ...(row.q.quoteId === undefined ? {} : { quoteId: row.q.quoteId }),
        ...(row.q.service === undefined ? {} : { service: row.q.service }),
        ...params,
      };
    } else if (row.a.carrierKey === SELF_CARRIER) {
      body = { carrierAccountId: row.a.id };
    } else {
      const fee = manualForm.fee.trim() === "" ? null : parseMoney(manualForm.fee);
      body = {
        carrierAccountId: row.a.id,
        trackingCode: manualForm.code.trim(),
        ...(fee === null ? {} : { feeActual: fee }),
      };
    }
    const r = await shipAct.run({ orderId: o.id, body });
    if (r.ok) {
      onCreated(r.data);
      return;
    }
    if (!(r.error instanceof ApiError)) return;
    if (r.error.code === "quote_stale" && params !== null) {
      shipAct.reset();
      setPicked(row.key);
      if (await requote(params)) setInfo(TEXT_QUOTE_STALE_REQUOTED);
    } else if (ORDER_MOVED.has(r.error.code)) {
      reloadOrder(qc, o.id);
    }
  };

  // Nút chính: chữ, việc khi bấm, lý do mờ.
  let label = "Gửi hãng";
  let why: string | null = null;
  let onClick: (() => void) | null = null;
  if (!canShip) {
    why = loaded ? TEXT_NO_SHIP_PERM : null;
  } else if (pick === null) {
    why = rows.some((r) => r.kind === "quote" && r.q.status === "pending")
      ? "Đang hỏi phí các hãng"
      : quotes.isLoading || manual.isLoading
        ? "Đang tải tài khoản hãng"
        : "Chọn một tài khoản hãng";
  } else if (pick.kind === "quote") {
    if (params === null) {
      why = "Kiểm lại cân nặng, khai giá, phí khi từ chối";
    } else if (stale) {
      label = "Báo phí lại";
      why = canQuote ? null : "Bạn không có quyền so phí ở kho của đơn này";
      const p = params;
      onClick = () => {
        setInfo(null);
        void requote(p);
      };
    } else if (isPriced(pick)) {
      label = `Gửi hãng · ${carrierName(pick.q.carrierKey, pick.q.accountName)} · ${formatMoney(pick.q.feeTotal)}`;
      why = loadingNew ? "Đang đọc giá mới" : null;
      onClick = () => void send(pick);
    }
  } else if (pick.a.carrierKey === SELF_CARRIER) {
    label = "Tạo vận đơn tự giao";
    onClick = () => void send(pick);
  } else {
    label = "Lưu mã vận đơn";
    const fee = manualForm.fee.trim();
    why =
      manualForm.code.trim() === ""
        ? "Dán mã vận đơn của hãng"
        : fee !== "" && parseMoney(fee) === null
          ? "Phí hãng báo chưa đúng dạng"
          : null;
    onClick = () => void send(pick);
  }

  const st = stats.data;
  const priced = rows.find(isPriced);
  const items = o.items.reduce((s, i) => s + i.qty, 0);
  const perItem = settings.data?.defaultWeightGramsPerItem;
  const rejected =
    latest.data?.status === "cancelled" && latest.data.cancelKind === "create_rejected" ? latest.data : null;
  const showParcel = pick === null || pick.kind === "quote";
  const error = shipAct.message ?? quoteAct.message;

  return (
    <>
      <Head chip="Chưa gửi hãng" />
      {rejected !== null && <RejectedNote s={rejected} onEdit={onEdit} />}
      {canQuote && (
        <div className="row-between">
          <h3>So phí</h3>
          {!stale && (
            <button
              type="button"
              className="btn btn-quiet"
              disabled={busy || params === null}
              onClick={() => {
                if (params === null) return;
                setInfo(null);
                void requote(params);
              }}
            >
              Báo phí lại
            </button>
          )}
        </div>
      )}
      {rows.length > 0 && (
        <QuoteTable
          rows={rows}
          pick={pick}
          now={now}
          disabled={busy}
          onPick={(k) => {
            setPicked(k);
            shipAct.reset();
          }}
        />
      )}
      {quotes.isError && <div className="box box-err small">Chưa đọc được kết quả so phí. Bấm "Báo phí lại".</div>}
      {manual.isError && <div className="box box-err small">Chưa đọc được tài khoản nhập mã tay của kho.</div>}
      {loaded && !canQuote && <p className="hint">Bạn không có quyền so phí ở kho của đơn này.</p>}
      {noApi && <p className="hint">Kho này chưa có tài khoản hãng báo phí được (tài khoản API đang hoạt động).</p>}
      {priced?.q.validUntil !== undefined && !expired && (
        <p className="hint">{`Báo phí còn hiệu lực tới ${shortTime(priced.q.validUntil)}. Ô lỗi không bao giờ hiện "0 ₫".`}</p>
      )}
      {stale && <div className="box box-info small">{expired ? TEXT_EXPIRED : TEXT_PARAMS_CHANGED}</div>}
      {info !== null && <div className="box box-info small">{info}</div>}
      {showParcel && (
        <ParcelFields
          orderNote={o.carrierNote ?? ""}
          form={form}
          onChange={setForm}
          disabled={busy}
          weightHint={perItem === undefined ? null : { items, perItem }}
        />
      )}
      {pick?.kind === "manual" && pick.a.carrierKey !== SELF_CARRIER && (
        <ManualFields
          carrier={carrierName(pick.a.carrierKey, pick.a.name)}
          accountName={pick.a.name}
          form={manualForm}
          onChange={setManualForm}
          disabled={busy}
        />
      )}
      {st?.risk === "high" && (
        <div className="box box-err small">
          <b>Khách hay hoàn</b>
          {` (Nhận ${String(st.delivered)} · Hoàn ${String(st.returned)}, ${percent.format(st.returnRateBp / 100)}%). ${
            st.hint === undefined || st.hint === "" ? "Nên xin cọc hoặc gọi xác nhận trước khi gửi." : st.hint
          }`}
        </div>
      )}
      <Act wide reason={why}>
        <button
          type="button"
          className="btn btn-dark btn-wide"
          disabled={why !== null || onClick === null || busy}
          onClick={onClick ?? undefined}
        >
          {label}
        </button>
      </Act>
      {error !== null && <div className="box box-err small">{error}</div>}
      <p className="hint">
        {o.codAmount === 0 ? (
          <>
            Thu hộ <span className="money">{formatMoney(0)}</span>. Hãng chỉ giao, không thu tiền.
          </>
        ) : (
          <>
            Thu hộ <span className="money">{formatMoney(o.codAmount)}</span>. Sau khi gửi, dòng hàng, COD, địa chỉ khóa
            (đổi địa chỉ đi qua hãng).
          </>
        )}
      </p>
    </>
  );
}

function Current({ id, o, onEdit }: { id: string; o: Order; onEdit?: (() => void) | undefined }) {
  const { loaded, grants } = useGrants();
  const s = useCurrentShipment(id);
  const [cancel, setCancel] = useState(false);
  const sent = s.data !== undefined && s.data.status !== "creating" ? s.data : null;
  const wh = o.warehouseId ?? "";
  const canReplace = canAt(grants, "fulfillment.cancel", wh);
  // Lệnh tạo kết thúc mà chưa có vận đơn ở hãng (Task 105, HV12): hủy để đổi hãng; "Không rõ" đòi tick đã tra hãng.
  const state = s.data?.createState;
  const replaceable = state === "unknown" || state === "not_sent";
  // Nội dung của <section> chung (ShippingPanel): khối không dựng lại khi vận đơn tải xong hay đổi trạng thái.
  return (
    <>
      {sent !== null ? (
        <ShipmentCard s={sent} order={o} />
      ) : s.isError ? (
        <div className="box box-err small">Chưa đọc được vận đơn. Tải lại trang.</div>
      ) : s.data === undefined ? (
        <p className="small muted">Đang tải vận đơn…</p>
      ) : (
        <>
          <CreateStateBanner
            s={s.data}
            canShip={canAt(grants, "fulfillment.ship", wh)}
            orderId={o.id}
            onEdit={onEdit}
          />
          {replaceable && (
            <Act reason={loaded && !canReplace ? "Bạn không có quyền hủy vận đơn ở kho của đơn này" : null}>
              <button
                type="button"
                className="btn btn-quiet"
                disabled={!canReplace}
                onClick={() => {
                  setCancel(true);
                }}
              >
                Hủy để đổi hãng
              </button>
            </Act>
          )}
          {cancel && (
            <CancelDialog
              s={s.data}
              order={o}
              canReplace={canReplace}
              canCancelOrder={false}
              needsCheck={state === "unknown"}
              replaceOnly
              onClose={() => {
                setCancel(false);
              }}
            />
          )}
        </>
      )}
    </>
  );
}

/** Thẻ dưới khối Vận chuyển (đổi địa chỉ, COD) khi vận đơn id đã gửi hãng. */
function CurrentExtras({ id, o }: { id: string; o: Order }) {
  const s = useCurrentShipment(id);
  return s.data !== undefined && s.data.status !== "creating" ? <ShipmentExtras s={s.data} order={o} /> : null;
}

/** Khối Vận chuyển của đơn o: hiện khi đơn đã xác nhận hoặc đã có vận đơn hiện hành. */
export function ShippingPanel({ order: o, onEdit }: { order: Order; onEdit?: () => void }) {
  const qc = useQueryClient();
  // Vận đơn vừa tạo trên khối này, hiện ngay khi đơn chưa đọc lại (đơn đọc lại thì currentShipmentId là nguồn).
  const [created, setCreated] = useState<{ id: string; version: number | undefined } | null>(null);
  if (o.status !== "confirmed" && o.currentShipmentId === undefined) return null;
  const id = o.currentShipmentId ?? (created !== null && created.version === o.version ? created.id : undefined);
  // Một <section> cho cả hai nhánh: gửi xong (Compose → Current) không dựng lại khối.
  return (
    <>
      <section className="card ship" aria-label="Vận chuyển">
        {id !== undefined ? (
          <Current id={id} o={o} onEdit={onEdit} />
        ) : (
          <Compose
            o={o}
            onEdit={onEdit}
            onCreated={(s) => {
              qc.setQueryData(shipmentKey(s.id), s);
              setCreated({ id: s.id, version: o.version });
              reloadOrder(qc, o.id);
            }}
          />
        )}
      </section>
      {id !== undefined && <CurrentExtras id={id} o={o} />}
    </>
  );
}
