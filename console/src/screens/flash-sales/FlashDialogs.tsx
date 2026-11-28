// Hộp thoại của màn Đợt flash sale (mockup docs/mockups/m5/flash-sales.html trạng thái B–F, H; HV27–HV31): tạo, sửa
// tên giờ, thêm, sửa, bỏ món, lên lịch, thêm suất, đóng sớm. Mọi lệnh ghi qua useWriteAction: cùng Idempotency-Key khi tự
// gửi lại sau 503/409/mạng; thân gửi mang version đã thấy; hộp không đóng và không nhận thao tác khi một lần ghi đang chạy.
import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import {
  addFlashItem,
  addFlashSlots,
  closeFlashSale,
  createFlashSale,
  removeFlashItem,
  scheduleFlashSale,
  searchVariants,
  updateFlashItem,
  updateFlashSale,
  type FlashItem,
  type FlashItemInput,
  type FlashSaleInput,
  type UpdateFlashItemRequest,
  type UpdateFlashSaleRequest,
  type FlashLiveItem,
  type FlashSale,
  type FlashSaleSummary,
  type VariantSearchHit,
} from "../../api";
import { ApiError, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { queryKey } from "../../app/queryClient";
import { SEARCH_DELAY_MS, useDebounced } from "../inventory/Balances";
import { formatMoney } from "../products/productRules";
import { Dialog } from "../staff/staffData";
import { Field } from "../web-campaigns/CampaignEditor";
import { FLASH_TOPIC, headers, reloadFlash } from "./flashData";
import {
  SLOT_MAX,
  emptyItemDraft,
  emptySaleDraft,
  itemDraftOf,
  liveTotals,
  longTime,
  saleDraftOf,
  scheduleSummary,
  shortagesOf,
  stockAfter,
  toItemChanges,
  toItemInput,
  toSaleInput,
  validateItem,
  validateSale,
  type ItemDraft,
  type SaleDraft,
} from "./flashRules";

const num = (n: number) => new Intl.NumberFormat("vi-VN").format(n);

/** Ô số có đơn vị/ghi chú bên phải (mockup .unit). */
function UnitField(o: {
  label: string;
  value: string;
  note: string;
  error?: string | undefined;
  onChange: (v: string) => void;
}) {
  return (
    <Field label={o.label} error={o.error}>
      {(id) => (
        <span className="fs-unit">
          <input
            id={id}
            className={o.error === undefined ? "input" : "input is-err"}
            inputMode="numeric"
            value={o.value}
            onChange={(e) => {
              o.onChange(e.target.value);
            }}
          />
          <span>{o.note}</span>
        </span>
      )}
    </Field>
  );
}

/** Lỗi của một đợt ghi: chỉ hiện sau khi người dùng đã nhập vào ô (không bật lỗi lên khi mới mở hộp). */
const shown = (error: string | undefined, value: string) => (value === "" ? undefined : error);

/**
 * Hộp lỗi của lệnh ghi. insufficient_stock kèm shortages → bảng mọi mã thiếu; version_conflict → câu tải lại; flash_overlap
 * → lý do; lỗi khác → câu tiếng Việt của errorMessage. `lead` là đầu câu ("Chưa lên lịch").
 */
function WriteError(o: { error: unknown; message: string | null; lead: string; onReload: () => void }) {
  const { error, message } = o;
  if (message === null) return null;
  const shortages = shortagesOf(error);
  if (shortages.length > 0) {
    return (
      <div className="box box-err stack-sm" role="alert">
        <b>{`${o.lead}: ${String(shortages.length)} món không đủ hàng để giữ.`}</b>
        <p>Không món nào bị giữ. Giảm số suất hoặc nhập thêm hàng rồi thử lại.</p>
        <div className="tbl-frame">
          <table className="tbl">
            <thead>
              <tr>
                <th>Mã hàng</th>
                <th className="r">Cần</th>
                <th className="r">Có thể bán</th>
                <th className="r">Thiếu</th>
              </tr>
            </thead>
            <tbody>
              {shortages.map((s) => (
                <tr key={s.variantId}>
                  <td className="lead">
                    <b>{s.sku}</b>
                  </td>
                  <td data-label="Cần" className="r num">
                    {num(s.need)}
                  </td>
                  <td data-label="Có thể bán" className="r num">
                    {num(s.sellable)}
                  </td>
                  <td data-label="Thiếu" className="r num err-text">
                    {num(s.missing)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    );
  }
  if (error instanceof ApiError && error.code === "version_conflict") {
    return (
      <div className="box box-err stack-sm" role="alert">
        <b>Đợt vừa được người khác sửa, tải lại.</b>
        <p>Chưa lưu gì. Tải lại để thấy món, số suất, trạng thái mới nhất rồi làm lại.</p>
        <div className="row">
          <button type="button" className="btn btn-line" onClick={o.onReload}>
            Tải lại
          </button>
        </div>
      </div>
    );
  }
  if (error instanceof ApiError && error.code === "flash_overlap") {
    return (
      <div className="box box-err stack-sm" role="alert">
        <b>{`${o.lead}: một món của đợt đã nằm trong đợt flash sale khác trùng giờ.`}</b>
        <p>Một biến thể không được bán flash ở hai đợt cùng lúc. Bỏ món khỏi một đợt hoặc dời giờ cho không chồng.</p>
      </div>
    );
  }
  return (
    <div className="box box-err" role="alert">
      {message}
    </div>
  );
}

/** Nút Hủy và nút chính của chân hộp; khóa cả hai khi đang ghi. */
function Footer(o: {
  pending: boolean;
  onCancel: () => void;
  go: string;
  disabled?: boolean;
  danger?: boolean;
  onGo: () => void;
}) {
  return (
    <>
      <button type="button" className="btn btn-quiet" disabled={o.pending} onClick={o.onCancel}>
        Hủy
      </button>
      <button
        type="button"
        className={o.danger === true ? "btn btn-danger" : "btn btn-dark"}
        disabled={o.pending || o.disabled === true}
        onClick={o.onGo}
      >
        {o.pending ? "Đang gửi…" : o.go}
      </button>
    </>
  );
}

interface DialogProps {
  onClose: () => void;
}

/** Đóng hộp và đọc lại dữ liệu (dùng chung cho "Tải lại" của hộp lỗi version_conflict). */
function useReloadClose(onClose: () => void) {
  const qc = useQueryClient();
  return () => {
    reloadFlash(qc);
    onClose();
  };
}

// ---- tạo, sửa tên và giờ ----

interface SaleRequest {
  id: string;
  version: number;
  /** Thân tạo đợt. */
  input: FlashSaleInput;
  /** Trường đổi khi sửa đợt. */
  changes: Omit<UpdateFlashSaleRequest, "version">;
}

export function SaleFormDialog(o: DialogProps & { sale?: FlashSaleSummary; onDone: (s: FlashSale) => void }) {
  const edit = o.sale;
  const [d, setD] = useState<SaleDraft>(edit === undefined ? emptySaleDraft() : saleDraftOf(edit));
  const reload = useReloadClose(o.onClose);
  const write = useWriteAction((key, r: SaleRequest) =>
    edit === undefined
      ? unwrap(createFlashSale({ body: r.input, headers: headers(key) }))
      : unwrap(
          updateFlashSale({
            path: { flashSaleId: r.id },
            body: { version: r.version, ...r.changes },
            headers: headers(key),
          }),
        ),
  );
  const errors = validateSale(d);
  const input = toSaleInput(d);
  // Sửa: chỉ gửi trường đã đổi.
  const changes: Omit<UpdateFlashSaleRequest, "version"> | null =
    input === null || edit === undefined
      ? input
      : {
          ...(input.name !== edit.name ? { name: input.name } : {}),
          ...(Date.parse(input.startsAt) !== Date.parse(edit.startsAt) ? { startsAt: input.startsAt } : {}),
          ...(Date.parse(input.endsAt) !== Date.parse(edit.endsAt) ? { endsAt: input.endsAt } : {}),
        };
  const ready = changes !== null && Object.keys(changes).length > 0;
  const submit = async () => {
    if (input === null || changes === null) return;
    const res = await write.run({ id: edit?.id ?? "", version: edit?.version ?? 0, input, changes });
    if (res.ok) o.onDone(res.data);
  };
  const set = (p: Partial<SaleDraft>) => {
    write.reset();
    setD((x) => ({ ...x, ...p }));
  };
  return (
    <Dialog
      title={edit === undefined ? "Tạo đợt flash sale" : "Sửa tên, giờ"}
      className="fs-dlg"
      closeButton
      onClose={write.pending ? () => undefined : o.onClose}
      footer={
        <Footer
          pending={write.pending}
          onCancel={o.onClose}
          go={edit === undefined ? "Tạo đợt nháp" : "Lưu"}
          disabled={!ready}
          onGo={() => void submit()}
        />
      }
    >
      <fieldset className="fs-fs" disabled={write.pending}>
        <Field
          label="Tên đợt *"
          error={shown(errors.name, d.name)}
          hint={<span className="hint">1–120 ký tự; khách thấy tên này trên website</span>}
        >
          {(id) => (
            <input
              id={id}
              className={errors.name !== undefined && d.name !== "" ? "input is-err" : "input"}
              value={d.name}
              onChange={(e) => {
                set({ name: e.target.value });
              }}
            />
          )}
        </Field>
        <div className="grid-f">
          <Field label="Giờ mở (giờ VN) *" error={shown(errors.start, d.start)}>
            {(id) => (
              <input
                id={id}
                className="input num"
                type="datetime-local"
                value={d.start}
                onChange={(e) => {
                  set({ start: e.target.value });
                }}
              />
            )}
          </Field>
          <Field label="Giờ đóng (giờ VN) *" error={shown(errors.end, d.end)}>
            {(id) => (
              <input
                id={id}
                className={errors.end !== undefined && d.end !== "" ? "input num is-err" : "input num"}
                type="datetime-local"
                value={d.end}
                onChange={(e) => {
                  set({ end: e.target.value });
                }}
              />
            )}
          </Field>
        </div>
        <p className="hint">
          Đợt dài tối đa 7 ngày. Đợt tạo ra ở trạng thái Nháp: chưa giữ suất, chưa hiện trên website.
        </p>
      </fieldset>
      <WriteError error={write.error} message={write.message} lead="Chưa lưu" onReload={reload} />
    </Dialog>
  );
}

// ---- thêm, sửa món ----

/** Danh sách biến thể tìm được (searchVariants); biến thể ngừng bán hay đã có trong đợt hiện mờ, không chọn được. */
function VariantSearch(o: {
  taken: ReadonlySet<string>;
  picked: VariantSearchHit | null;
  onPick: (h: VariantSearchHit) => void;
}) {
  const [text, setText] = useState("");
  const term = useDebounced(text.trim(), SEARCH_DELAY_MS);
  const found = useQuery({
    queryKey: queryKey(FLASH_TOPIC, "variant-search", term),
    enabled: term !== "",
    queryFn: () => unwrap(searchVariants({ query: { q: term, limit: 8 } })),
    placeholderData: keepPreviousData,
  });
  const hits = term === "" || text.trim() === "" ? [] : (found.data ?? []);
  return (
    <div className="stack-sm">
      <Field label="Tìm biến thể theo tên hoặc mã hàng">
        {(id) => (
          <input
            id={id}
            className="input is-search"
            type="search"
            value={text}
            onChange={(e) => {
              setText(e.target.value);
            }}
          />
        )}
      </Field>
      {found.isError && <div className="box box-err">Chưa tìm được biến thể. Thử lại sau ít giây.</div>}
      {hits.length > 0 && (
        <div className="res">
          {hits.map((h) => {
            const dup = o.taken.has(h.variantId);
            const off = !h.sellable;
            return (
              <label key={h.variantId} className={dup || off ? "check is-off" : "check"}>
                <input
                  type="radio"
                  name="flash-variant"
                  disabled={dup || off}
                  checked={o.picked?.variantId === h.variantId}
                  onChange={() => {
                    o.onPick(h);
                  }}
                />{" "}
                <span className="sp">
                  <span className="nm">{[h.productName, h.color, h.size].filter(Boolean).join(" · ")}</span>
                  <span className="code">
                    {dup
                      ? `${h.sku} · đã có trong đợt này`
                      : off
                        ? `${h.sku} · đang ngừng bán, không thêm được`
                        : `${h.sku} · đang bán · ${formatMoney(h.price)}`}
                  </span>
                </span>
              </label>
            );
          })}
        </div>
      )}
    </div>
  );
}

interface ItemRequest {
  flashSaleId: string;
  itemId: string;
  version: number;
  /** Thân thêm món; null khi sửa món. */
  input: FlashItemInput | null;
  changes: Omit<UpdateFlashItemRequest, "version">;
}

export function ItemFormDialog(
  o: DialogProps & {
    sale: FlashSaleSummary;
    /** Có thì là sửa món này; không có thì là thêm món. */
    item?: FlashItem;
    taken: ReadonlySet<string>;
    onDone: () => void;
  },
) {
  const edit = o.item;
  const [picked, setPicked] = useState<VariantSearchHit | null>(null);
  const [d, setD] = useState<ItemDraft>(edit === undefined ? emptyItemDraft() : itemDraftOf(edit));
  const reload = useReloadClose(o.onClose);
  const write = useWriteAction((key, r: ItemRequest) =>
    r.input !== null
      ? unwrap(addFlashItem({ path: { flashSaleId: r.flashSaleId }, body: r.input, headers: headers(key) }))
      : unwrap(
          updateFlashItem({
            path: { flashSaleId: r.flashSaleId, flashItemId: r.itemId },
            body: { version: r.version, ...r.changes },
            headers: headers(key),
          }),
        ),
  );
  const listPrice = edit?.listPrice ?? picked?.price ?? 0;
  const errors = edit === undefined && picked === null ? {} : validateItem(d, listPrice);
  const input = edit === undefined && picked !== null ? toItemInput(d, listPrice, picked.variantId) : null;
  const changes = edit === undefined ? null : toItemChanges(d, edit);
  const body = input ?? changes;
  const submit = async () => {
    if (body === null) return;
    const res = await write.run({
      flashSaleId: o.sale.id,
      itemId: edit?.id ?? "",
      version: edit?.version ?? 0,
      input,
      changes: changes ?? {},
    });
    if (res.ok) o.onDone();
  };
  const set = (p: Partial<ItemDraft>) => {
    write.reset();
    setD((x) => ({ ...x, ...p }));
  };
  return (
    <Dialog
      title={edit === undefined ? `Thêm món vào "${o.sale.name}"` : `Sửa món: ${edit.sku}`}
      className="fs-dlg"
      closeButton
      onClose={write.pending ? () => undefined : o.onClose}
      footer={
        <Footer
          pending={write.pending}
          onCancel={o.onClose}
          go={edit === undefined ? "Thêm món" : "Lưu món"}
          disabled={body === null}
          onGo={() => void submit()}
        />
      }
    >
      <fieldset className="fs-fs" disabled={write.pending}>
        {edit === undefined ? (
          <VariantSearch taken={o.taken} picked={picked} onPick={setPicked} />
        ) : (
          <p className="small">{`${edit.name} · giá thường ${formatMoney(edit.listPrice)}`}</p>
        )}
        <div className="grid-f">
          <UnitField
            label="Giá flash *"
            value={d.price}
            note="₫"
            error={shown(errors.price, d.price)}
            onChange={(v) => {
              set({ price: v });
            }}
          />
          <UnitField
            label="Số suất *"
            value={d.qty}
            note="1–20.000"
            error={shown(errors.qty, d.qty)}
            onChange={(v) => {
              set({ qty: v });
            }}
          />
          <UnitField
            label="Mỗi khách mua tối đa"
            value={d.limit}
            note="1–10, mặc định 1"
            error={errors.limit}
            onChange={(v) => {
              set({ limit: v });
            }}
          />
          <UnitField
            label="Hạn giữ suất"
            value={d.hold}
            note="phút · 5–30, mặc định 10"
            error={errors.hold}
            onChange={(v) => {
              set({ hold: v });
            }}
          />
        </div>
        <p className="hint">
          Hạn giữ: khách bấm mua được giữ suất trong thời gian này để điền địa chỉ; quá hạn thì đơn nháp tự hủy và suất
          mở lại cho người khác.
        </p>
      </fieldset>
      <WriteError error={write.error} message={write.message} lead="Chưa lưu" onReload={reload} />
    </Dialog>
  );
}

export function RemoveItemDialog(o: DialogProps & { sale: FlashSaleSummary; item: FlashItem; onDone: () => void }) {
  const reload = useReloadClose(o.onClose);
  const write = useWriteAction((key, r: { flashSaleId: string; itemId: string }) =>
    unwrap(removeFlashItem({ path: { flashSaleId: r.flashSaleId, flashItemId: r.itemId }, headers: headers(key) })),
  );
  const go = async () => {
    const res = await write.run({ flashSaleId: o.sale.id, itemId: o.item.id });
    if (res.ok) o.onDone();
  };
  return (
    <Dialog
      title={`Bỏ món ${o.item.sku} khỏi đợt?`}
      className="fs-dlg"
      closeButton
      onClose={write.pending ? () => undefined : o.onClose}
      footer={<Footer pending={write.pending} onCancel={o.onClose} go="Bỏ món" danger onGo={() => void go()} />}
    >
      <p>Đợt còn nháp nên chưa giữ suất nào; bỏ món không đổi tồn.</p>
      <WriteError error={write.error} message={write.message} lead="Chưa bỏ" onReload={reload} />
    </Dialog>
  );
}

// ---- lên lịch, thêm suất, đóng sớm ----

export function ScheduleDialog(
  o: DialogProps & { sale: FlashSaleSummary; items: readonly FlashItem[]; onDone: () => void },
) {
  const reload = useReloadClose(o.onClose);
  const write = useWriteAction((key, r: { flashSaleId: string; version: number }) =>
    unwrap(
      scheduleFlashSale({
        path: { flashSaleId: r.flashSaleId },
        body: { version: r.version },
        headers: headers(key),
      }),
    ),
  );
  const sum = scheduleSummary(o.items);
  const go = async () => {
    const res = await write.run({ flashSaleId: o.sale.id, version: o.sale.version });
    if (res.ok) o.onDone();
  };
  return (
    <Dialog
      title={`Lên lịch "${o.sale.name}"?`}
      className="fs-dlg"
      closeButton
      onClose={write.pending ? () => undefined : o.onClose}
      footer={
        <Footer
          pending={write.pending}
          onCancel={o.onClose}
          go={`Lên lịch, giữ ${num(sum.total)} suất`}
          onGo={() => void go()}
        />
      }
    >
      <div className="box box-info">
        <b>{`${num(sum.total)} suất của ${String(sum.count)} món sẽ bị giữ khỏi bán thường ngay khi bấm`}</b>
        {` (lên đơn, chat, website đều không bán vào phần này). Đợt tự mở lúc ${longTime(o.sale.startsAt).slice(0, 11)} và tự đóng lúc ${longTime(o.sale.endsAt).slice(0, 11)}.`}
      </div>
      <ul className="small num fs-list">
        {sum.top.map((l) => (
          <li key={l.sku}>
            <b>{l.sku}</b>
            {`: giữ ${num(l.quantity)} · `}
            <span className={stockAfter(l.sellable, l.quantity).short ? "err-text" : undefined}>
              {stockAfter(l.sellable, l.quantity).text}
            </span>
          </li>
        ))}
        {sum.rest !== null && <li>{`và ${String(sum.rest.count)} món khác: giữ ${num(sum.rest.quantity)}`}</li>}
      </ul>
      <p className="small">
        Sau khi lên lịch <b>không sửa được</b> món, giá, giờ; chỉ <b>Thêm suất</b> hoặc <b>Đóng sớm</b>. Đóng đợt thì
        suất chưa ai lấy trả về bán thường.
      </p>
      <WriteError error={write.error} message={write.message} lead="Chưa lên lịch" onReload={reload} />
    </Dialog>
  );
}

export function AddSlotsDialog(
  o: DialogProps & {
    sale: FlashSaleSummary;
    item: FlashItem;
    live: FlashLiveItem | undefined;
    onDone: () => void;
  },
) {
  const [text, setText] = useState("");
  const write = useWriteAction((key, r: { flashSaleId: string; itemId: string; add: number }) =>
    unwrap(
      addFlashSlots({
        path: { flashSaleId: r.flashSaleId, flashItemId: r.itemId },
        body: { add: r.add },
        headers: headers(key),
      }),
    ),
  );
  const n = /^\d{1,6}$/.test(text.trim()) ? Number(text.trim()) : null;
  const valid = n !== null && n >= 1 && n <= SLOT_MAX;
  const short = shortagesOf(write.error).find((s) => s.variantId === o.item.variantId) ?? shortagesOf(write.error)[0];
  const fieldError =
    n === null && text !== ""
      ? "Nhập số suất là số nguyên"
      : n !== null && !valid
        ? "Từ 1 đến 20.000 suất"
        : short !== undefined
          ? `Không đủ hàng: cần ${num(short.need)}, có thể bán ${num(short.sellable)}. Chưa thêm suất nào.`
          : (write.message ?? undefined);
  const go = async () => {
    if (!valid) return;
    const res = await write.run({ flashSaleId: o.sale.id, itemId: o.item.id, add: n });
    if (res.ok) o.onDone();
  };
  return (
    <Dialog
      title={`Thêm suất: ${o.item.sku}`}
      className="fs-dlg"
      closeButton
      onClose={write.pending ? () => undefined : o.onClose}
      footer={
        <Footer
          pending={write.pending}
          onCancel={o.onClose}
          go={n !== null ? `Thêm ${String(n)} suất` : "Thêm suất"}
          disabled={!valid}
          onGo={() => void go()}
        />
      }
    >
      <p className="small">{`${o.item.name} · đang có ${num(o.item.quantity)} suất${o.live === undefined ? "" : `, còn trống ${num(o.live.available)}`}`}</p>
      <fieldset className="fs-fs" disabled={write.pending}>
        <Field label="Thêm *" error={fieldError}>
          {(id) => (
            <span className="fs-unit">
              <input
                id={id}
                className={fieldError === undefined ? "input" : "input is-err"}
                inputMode="numeric"
                value={text}
                onChange={(e) => {
                  write.reset();
                  setText(e.target.value);
                }}
              />
              <span>suất · 1–20.000</span>
            </span>
          )}
        </Field>
      </fieldset>
      {valid && short === undefined && (
        <div className="box box-info">
          {`Sau khi thêm: `}
          <b>{`${num(o.item.quantity + n)} suất`}</b>
          {`${o.live === undefined ? "" : `, còn trống ${num(o.live.available + n)}`}. `}
          {n <= o.item.sellable ? (
            <>
              {"Có thể bán thường của mã này "}
              <b>{`${num(o.item.sellable)} → ${num(o.item.sellable - n)}`}</b>
              {" ngay khi bấm."}
            </>
          ) : (
            `Chỉ còn ${num(o.item.sellable)} có thể bán thường, máy chủ sẽ báo thiếu hàng.`
          )}
        </div>
      )}
    </Dialog>
  );
}

export function CloseDialog(
  o: DialogProps & { sale: FlashSaleSummary; live: readonly FlashLiveItem[] | undefined; onDone: () => void },
) {
  const reload = useReloadClose(o.onClose);
  const write = useWriteAction((key, r: { flashSaleId: string }) =>
    unwrap(closeFlashSale({ path: { flashSaleId: r.flashSaleId }, headers: headers(key) })),
  );
  const t = o.live === undefined ? null : liveTotals({ items: [...o.live] });
  const go = async () => {
    const res = await write.run({ flashSaleId: o.sale.id });
    if (res.ok) o.onDone();
  };
  return (
    <Dialog
      title={`Đóng sớm "${o.sale.name}"?`}
      className="fs-dlg"
      closeButton
      onClose={write.pending ? () => undefined : o.onClose}
      footer={<Footer pending={write.pending} onCancel={o.onClose} go="Đóng đợt" danger onGo={() => void go()} />}
    >
      <p>
        Website ngừng nhận lượt mua của đợt ngay. <b>Không mở lại được.</b>
      </p>
      {t === null ? (
        <p className="small">
          Chưa tải được số liệu của đợt. Đóng đợt vẫn trả các suất chưa ai lấy về bán thường ngay; khách đang giữ suất
          hoàn tất được trong hạn giữ.
        </p>
      ) : (
        <ul className="small fs-list">
          <li>
            <b>{`${num(t.available)} suất còn trống`}</b> trả về bán thường ngay.
          </li>
          <li>
            <b>{`${num(t.held)} suất khách đang giữ`}</b>: khách vẫn hoàn tất đơn được trong hạn giữ của món; hết hạn
            thì suất trả về bán thường, không mở lại trong đợt.
          </li>
          <li>{`${num(t.sold)} suất đã bán: các đơn đã hoàn tất đi tiếp như đơn thường.`}</li>
        </ul>
      )}
      <WriteError error={write.error} message={write.message} lead="Chưa đóng" onReload={reload} />
    </Dialog>
  );
}
