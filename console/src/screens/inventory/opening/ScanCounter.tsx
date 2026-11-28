// Đếm bằng quét mã của phiên mở sổ (Task 273; mockup docs/mockups/m7/kiem-kho-mo-so.html, trạng thái B; HV03–HV05).
// Mỗi lần quét gửi ngay một lần cộng +1 (useScanQueue: khóa riêng, mất mạng thì chờ và gửi lại cùng khóa); nút −1 sửa
// quét thừa; không có ô gõ số. Số to là số máy chủ trả sau lần cộng (tổng của mọi người đếm).
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useRef, useState, type KeyboardEvent } from "react";
import { incrementStocktakeLine, searchVariants, type VariantSearchHit } from "../../../api";
import { unwrap } from "../../../app/errors";
import { matchHit } from "../stocktakeRules";
import { scanLabel } from "./openingRules";
import { useScanQueue, type ScanItem } from "./useScanQueue";

/** Số dòng "Vừa quét" hiện. */
const RECENT = 20;

function variantLabel(h: VariantSearchHit | undefined): { sku: string; name: string; opt: string } | null {
  if (h === undefined) return null;
  return { sku: h.sku, name: h.productName, opt: [h.color, h.size].filter((x) => x !== "").join(" · ") };
}

export function ScanCounter(props: { stocktakeId: string; onSaved: () => void; onClosed: () => void }) {
  const { stocktakeId, onSaved, onClosed } = props;
  const qc = useQueryClient();
  const [text, setText] = useState("");
  // Tên mã đã quét (tra một lần mỗi mã): mã → biến thể, null khi không có.
  const [hits, setHits] = useState<ReadonlyMap<string, VariantSearchHit | null>>(new Map());
  const asked = useRef(new Set<string>());

  const q = useScanQueue({
    stocktakeId,
    send: (key, body) =>
      unwrap(
        incrementStocktakeLine({
          path: { id: body.stocktakeId },
          body: { code: body.code, by: body.by },
          headers: { "Idempotency-Key": key },
        }),
      ),
    onSaved,
    onClosed,
  });

  const lookup = useCallback(
    (code: string) => {
      if (asked.current.has(code)) return;
      asked.current.add(code);
      void qc
        .query({
          queryKey: ["catalog.variant_search", code],
          queryFn: async () => matchHit(code, await unwrap(searchVariants({ query: { q: code, limit: 20 } }))),
          staleTime: 60_000,
        })
        .then(
          (h) => {
            setHits((m) => new Map(m).set(code, h));
          },
          () => {
            asked.current.delete(code);
          },
        );
    },
    [qc],
  );

  // Lần quét còn lại từ lần tải trước (khôi phục từ localStorage) cũng cần tên.
  useEffect(() => {
    for (const i of q.items) lookup(i.code);
  }, [q.items, lookup]);

  const submit = () => {
    const code = text.trim();
    if (code === "") return;
    setText("");
    lookup(code);
    q.enqueue(code, 1);
  };
  const onKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key !== "Enter") return;
    e.preventDefault();
    submit();
  };

  const last = q.items.find((i) => i.status === "saved");
  const lastHit = last === undefined ? undefined : (hits.get(last.code) ?? undefined);
  const lastLabel = variantLabel(lastHit);
  const lastName = lastLabel?.sku ?? last?.code ?? "";
  const errors = q.items.filter((i) => i.status === "error");

  return (
    <div className="stack">
      <div className="scan field">
        <label htmlFor="mo-scan">Quét barcode hoặc gõ mã</label>
        <input
          id="mo-scan"
          className="input"
          placeholder="Quét hoặc gõ SKU rồi Enter"
          inputMode="text"
          autoComplete="off"
          value={text}
          onChange={(e) => {
            setText(e.target.value);
          }}
          onKeyDown={onKey}
        />
      </div>

      {q.offline && (
        <div className="box box-soft" role="status">
          <b>{`Mất mạng · ${String(q.waiting)} lần quét đang chờ gửi.`}</b> Cứ quét tiếp. Có mạng lại máy tự gửi đúng
          các lần quét đó, không đếm trùng. Đừng đóng trang khi còn lần chờ gửi.
        </div>
      )}

      {last?.result !== undefined && last.result !== null && (
        <div className="last-scan">
          <div className="row-between">
            <span className="small muted">{`Vừa quét · ${last.code}`}</span>
            <span className="xs ok-text">Đã lưu</span>
          </div>
          {lastLabel !== null && (
            <div>
              <span className="name bold">{lastLabel.name}</span>
              <br />
              <span className="small">{`${lastLabel.opt} · ${lastLabel.sku}`}</span>
            </div>
          )}
          <div className="row-between">
            <span className="count-big">{last.result.counted}</span>
            <span className="small muted">Tổng của mọi người đếm.</span>
          </div>
          <div className="big-acts">
            <button
              type="button"
              className="btn btn-line"
              aria-label={`Bớt 1 ${lastName}`}
              onClick={() => {
                q.enqueue(last.code, -1);
              }}
            >
              −1
            </button>
            <button
              type="button"
              className="btn btn-dark"
              aria-label={`Thêm 1 ${lastName}`}
              onClick={() => {
                q.enqueue(last.code, 1);
              }}
            >
              +1
            </button>
          </div>
          <p className="hint">
            Quét lại cùng mã để cộng 1. Quét thừa thì bấm −1. Mỗi lần quét gửi ngay một lần cộng; nhiều người đếm cùng
            mã thì số cộng dồn, không ghi đè nhau.
          </p>
        </div>
      )}

      {errors.map((e) => (
        <ScanError
          key={e.id}
          item={e}
          onPick={(sku) => {
            q.dismiss(e.id);
            lookup(sku);
            q.enqueue(sku, e.by);
          }}
          onClose={() => {
            q.dismiss(e.id);
          }}
        />
      ))}

      {q.items.length > 0 && (
        <div>
          <h3>Vừa quét</h3>
          <div style={{ marginTop: 8 }}>
            {q.items.slice(0, RECENT).map((i) => (
              <RecentRow key={i.id} item={i} hit={hits.get(i.code) ?? undefined} />
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

function ScanError(props: { item: ScanItem; onPick: (sku: string) => void; onClose: () => void }) {
  const { item, onPick, onClose } = props;
  const close = (
    <div className="row" style={{ marginTop: 8 }}>
      <button type="button" className="btn btn-line" onClick={onClose}>
        Đóng
      </button>
    </div>
  );
  if (item.errorCode === "unknown_code") {
    return (
      <div className="box box-err" role="alert">
        <b>{`Không thấy mã ${item.code}.`}</b>{" "}
        <span>Mã này không có trong danh mục. Kiểm lại tem, hoặc gõ SKU. Không cộng gì.</span>
        {close}
      </div>
    );
  }
  if (item.errorCode === "ambiguous_code" && item.candidates !== null && item.candidates.length > 0) {
    return (
      <div className="box box-soft" role="alert">
        <b>{`Mã ${item.code} có ở ${String(item.candidates.length)} người bán.`}</b> Chọn đúng món đang cầm để cộng{" "}
        {String(Math.abs(item.by))}:
        <div className="cands" style={{ marginTop: 8 }}>
          {item.candidates.map((c) => (
            <div className="cand" key={c.variantId}>
              <div>
                <span className="bold">{c.sku}</span>
                <br />
                <span className="small muted">{`Người bán ${c.sellerId.slice(-6)}`}</span>
              </div>
              <button
                type="button"
                className="btn btn-line"
                onClick={() => {
                  onPick(c.sku);
                }}
              >
                {`${item.by < 0 ? "Bớt" : "Cộng"} ${String(Math.abs(item.by))}`}
              </button>
            </div>
          ))}
        </div>
        {close}
      </div>
    );
  }
  return (
    <div className="box box-err" role="alert">
      <b>{item.by < 0 ? "Không bớt được." : "Không cộng được."}</b> <span>{item.message ?? ""}</span>
      {close}
    </div>
  );
}

function RecentRow({ item, hit }: { item: ScanItem; hit: VariantSearchHit | undefined }) {
  const label = variantLabel(hit);
  const state =
    item.status === "saved"
      ? `Đã lưu · ${String(item.result?.counted ?? "")}`
      : item.status === "sending"
        ? `Đang gửi · ${scanLabel(item.by)}`
        : item.status === "waiting"
          ? `Chờ gửi · ${scanLabel(item.by)}`
          : `Chưa cộng · ${scanLabel(item.by)}`;
  return (
    <div className={item.status === "saved" || item.status === "error" ? "qrow" : "qrow is-wait"}>
      <div>
        <span className="sku">{label?.sku ?? item.code}</span>
        <br />
        <span className="nm">{label === null ? "" : `${label.name} · ${label.opt}`}</span>
      </div>
      <span className={item.status === "saved" ? "xs ok-text" : item.status === "error" ? "xs err-text" : "xs muted"}>
        {state}
      </span>
    </div>
  );
}
