// Form chiến dịch giảm giá /khuyen-mai/chien-dich/moi và /khuyen-mai/chien-dich/:id (kế hoạch M5 Task 458, dữ liệu
// theo mockup Task 487: tên đích, số mẫu, trần, SKU từ máy chủ; mockup
// docs/mockups/m5/web-campaigns.html trạng thái B, D). Tên, phần trăm, phạm vi và chọn đích (≤ 30), giờ Việt Nam, dùng
// kèm mã, hàng cắt mã; xem trước giá (API chỉ xem trước bản đã lưu: form đã sửa thì lưu trước, tệp task Quyết định 4).
// Gửi revision đã đọc; 409 version_conflict giữ nguyên form và mời mở bản mới ở tab khác. Chiến dịch đang bật không
// sửa được (máy chủ trả campaign_running): form khóa, nút Tạm dừng và sửa. Không promotion.manage: form chỉ đọc.
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useId, useRef, useState, type ReactNode } from "react";
import { Link, useLocation, useNavigate, useParams } from "react-router";
import {
  createWebCampaign,
  getWebCampaign,
  listCampaignTargets,
  saveWebCampaign,
  type WebCampaign,
  type WebCampaignInput,
  type WebCampaignPreview,
  type WebCampaignScope,
} from "../../api";
import { ApiError, errorMessage, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { queryKey } from "../../app/queryClient";
import { SEARCH_DELAY_MS, useDebounced } from "../inventory/Balances";
import { formatMoney } from "../products/productRules";
import { StateDialog, usePreview } from "./CampaignDialogs";
import {
  campaignKey,
  headers,
  pickedTargets,
  reloadCampaigns,
  storeCampaign,
  targetKind,
  useCampaign,
  useMaxPercent,
  usePerms,
} from "./campaignData";
import {
  NAME_MAX,
  SCOPES,
  SCOPE_TEXT,
  TARGET_MAX,
  TEXT_RAISE_CAP,
  TEXT_READ_ONLY,
  draftOf,
  emptyDraft,
  percentHint,
  percentTooHighText,
  sameAsSaved,
  statusChip,
  targetSub,
  toInput,
  validateDraft,
  type CampaignDraft,
  type PickedTarget,
} from "./campaignRules";
import "../don/don.css";
import "../len-don/len-don.css";
import "./web-campaigns.css";

/** Số đích mỗi lần tìm (Limit 1–100). */
const SEARCH_PAGE = 50;
const LIST = "/khuyen-mai/chien-dich";

const isCode = (e: unknown, code: string) => e instanceof ApiError && e.code === code;
const asScope = (v: string): WebCampaignScope => SCOPES.find((s) => s === v) ?? "categories";

function Field(o: { label: string; error?: string; hint?: ReactNode; children: (id: string) => ReactNode }) {
  const id = useId();
  return (
    <div className="field">
      <label htmlFor={id}>{o.label}</label>
      {o.children(id)}
      {o.error !== undefined ? <span className="field-err">{o.error}</span> : o.hint}
    </div>
  );
}

function TargetPicker(o: {
  scope: Exclude<WebCampaignScope, "all">;
  picked: PickedTarget[];
  onChange: (t: PickedTarget[]) => void;
  error?: string;
}) {
  const [text, setText] = useState("");
  const q = useDebounced(text.trim(), SEARCH_DELAY_MS);
  const kind = targetKind(o.scope);
  const found = useQuery({
    queryKey: queryKey("promotion.web_campaign.targets", kind, q),
    queryFn: () => unwrap(listCampaignTargets({ query: { kind, limit: SEARCH_PAGE, ...(q === "" ? {} : { q }) } })),
  });
  const ids = new Set(o.picked.map((t) => t.id));
  const full = o.picked.length >= TARGET_MAX;
  const toggle = (t: PickedTarget) => {
    o.onChange(ids.has(t.id) ? o.picked.filter((x) => x.id !== t.id) : [...o.picked, t]);
  };
  const n = o.picked.length;
  return (
    <div className="stack-sm">
      <Field label={o.scope === "products" ? "Tìm sản phẩm theo mã hoặc tên" : "Tìm trong toàn bộ danh mục"}>
        {(id) => (
          <input
            id={id}
            className="input is-search"
            type="search"
            maxLength={120}
            value={text}
            onChange={(e) => {
              setText(e.target.value);
            }}
          />
        )}
      </Field>
      {found.isError && <div className="box box-err">{errorMessage(found.error)}</div>}
      {found.data !== undefined && found.data.items.length > 0 && (
        <div className="res">
          {found.data.items.map((t) => {
            const on = ids.has(t.id);
            return (
              <label key={t.id} className={!on && full ? "check is-off" : "check"}>
                <input
                  type="checkbox"
                  checked={on}
                  disabled={!on && full}
                  onChange={() => {
                    toggle(t);
                  }}
                />
                <span>
                  <b>{t.name}</b> · <span className="small muted">{targetSub(t)}</span>
                </span>
              </label>
            );
          })}
        </div>
      )}
      {found.data?.items.length === 0 && <p className="small muted">Không có mục nào khớp.</p>}
      {found.data?.nextCursor !== undefined && (
        <p className="small muted">Còn nhiều mục khác: gõ thêm chữ để thu hẹp.</p>
      )}
      <p className="small">
        {full
          ? `Đã chọn ${String(n)}/${String(TARGET_MAX)} — bỏ bớt một mục để chọn thêm.`
          : `Đã chọn ${String(n)}/${String(TARGET_MAX)}`}
      </p>
      {o.error !== undefined && <span className="field-err">{o.error}</span>}
      {n > 0 && (
        <div className="picks">
          {o.picked.map((t) => (
            <span key={t.id} className="tag">
              {t.name}{" "}
              <button
                type="button"
                aria-label={`Bỏ ${t.name}`}
                onClick={() => {
                  toggle(t);
                }}
              >
                ×
              </button>
            </span>
          ))}
        </div>
      )}
    </div>
  );
}

function PreviewCard(o: { preview: WebCampaignPreview | null; base: WebCampaign | null; stale: boolean }) {
  const p = o.preview;
  if (p === null) {
    const saved = o.base?.preview;
    return (
      <section className="card stack-sm">
        <h2>Xem trước giá</h2>
        <p className="small muted">
          {saved !== undefined && o.base !== null && saved.revision === o.base.revision
            ? `Đã xem trước phiên bản ${String(saved.revision)}: ${String(saved.modelCount)} mẫu · ${String(saved.variantCount)} biến thể.`
            : "Chưa xem trước. Bấm Xem trước giá để xem giá mẫu trước khi kích hoạt."}
        </p>
      </section>
    );
  }
  return (
    <section className="card stack-sm">
      <h2>{`Xem trước: ${String(p.modelCount)} mẫu · ${String(p.variantCount)} biến thể`}</h2>
      <p className="small muted">
        {`Theo phạm vi và phần trăm đã lưu, phiên bản ${String(p.revision)}. Giá chỉ áp khi thấp hơn giá niêm yết.`}
      </p>
      {o.stale && <p className="small">Form đã sửa sau lần xem này; bấm Xem trước giá để xem lại.</p>}
      {p.samples.length > 0 && (
        <div className="tbl-frame">
          <table className="tbl">
            <thead>
              <tr>
                <th>Sản phẩm</th>
                <th className="r">Giá gốc</th>
                <th className="r">Giá chiến dịch</th>
              </tr>
            </thead>
            <tbody>
              {p.samples.map((s) => (
                <tr key={s.variantId}>
                  <td className="lead">
                    <div className="sp">
                      <span className="nm clamp2">{s.productName}</span>
                      <span className="code">{s.sku}</span>
                    </div>
                  </td>
                  <td data-label="Giá gốc" className="r">
                    <span className="money">{formatMoney(s.listPrice)}</span>
                  </td>
                  <td data-label="Giá chiến dịch" className="r">
                    <span className="money">{formatMoney(s.salePrice)}</span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}

interface SaveBody {
  id: string;
  revision: number;
  campaign: WebCampaignInput;
}

function EditorForm(o: { base: WebCampaign | null; targets: PickedTarget[]; autoPreview: boolean }) {
  const perms = usePerms();
  const qc = useQueryClient();
  const navigate = useNavigate();
  // Bản đọc đầu tiên: props đổi sau đó (báo hiệu tải lại) không dựng lại form.
  const [initial] = useState(o.base);
  const [base, setBase] = useState(o.base);
  const [draft, setDraft] = useState<CampaignDraft>(() =>
    initial === null ? emptyDraft() : draftOf(initial, o.targets),
  );
  const [tried, setTried] = useState(false);
  const [preview, setPreview] = useState<WebCampaignPreview | null>(null);
  const [conflict, setConflict] = useState(false);
  const [saved, setSaved] = useState(false);
  const [pausing, setPausing] = useState(false);
  const create = useWriteAction((key: string, input: WebCampaignInput) =>
    unwrap(createWebCampaign({ body: input, headers: headers(key) })),
  );
  const save = useWriteAction((key: string, b: SaveBody) =>
    unwrap(
      saveWebCampaign({
        path: { campaignId: b.id },
        body: { revision: b.revision, campaign: b.campaign },
        headers: headers(key),
      }),
    ),
  );
  const previewAction = usePreview();
  const maxPercent = useMaxPercent();

  const canManage = perms.can("promotion.manage");
  const running = base?.state === "enabled";
  const locked = !canManage || running;
  const busy = create.pending || save.pending || previewAction.pending;
  const errors = tried ? validateDraft(draft) : {};
  const writeError = base === null ? create.error : save.error;
  const tooHigh = isCode(writeError, "percent_too_high");
  const otherError = !conflict && !tooHigh ? (base === null ? create.message : save.message) : null;

  const edit = (p: Partial<CampaignDraft>) => {
    setDraft((d) => ({ ...d, ...p }));
    setSaved(false);
    // Câu lỗi của lần lưu trước (vượt trần…) nói về chữ cũ: bỏ khi sửa.
    create.reset();
    save.reset();
  };

  const after = (c: WebCampaign) => {
    storeCampaign(qc, c);
    reloadCampaigns(qc);
  };

  /**
   * 409 version_conflict: giữ nguyên chữ đang gõ, đọc bản mới làm gốc (revision mới) để lần Lưu sau ghi đè có chủ ý;
   * bản mới đang bật thì form tự khóa. Đọc lỗi thì giữ gốc cũ (lần lưu sau lại báo 409).
   */
  const onConflict = (id: string) => {
    setConflict(true);
    setPreview(null);
    void qc
      .query({
        queryKey: campaignKey(id),
        staleTime: 0,
        queryFn: () => unwrap(getWebCampaign({ path: { campaignId: id } })),
      })
      .then(setBase, () => undefined);
  };

  /** Lưu form (tạo khi chưa có); trả bản đã lưu hay null khi form sai, lỗi. */
  const persist = async (): Promise<WebCampaign | null> => {
    setTried(true);
    setConflict(false);
    const input = toInput(draft);
    if (input === null) return null;
    if (base === null) {
      const r = await create.run(input);
      if (!r.ok) return null;
      after(r.data);
      return r.data;
    }
    const r = await save.run({ id: base.id, revision: base.revision, campaign: input });
    if (!r.ok) {
      if (isCode(r.error, "version_conflict")) onConflict(base.id);
      return null;
    }
    setBase(r.data);
    setPreview(null);
    after(r.data);
    return r.data;
  };

  const runPreview = async (c: WebCampaign) => {
    const r = await previewAction.run({ id: c.id, revision: c.revision });
    if (r.ok) setPreview(r.data);
    else if (isCode(r.error, "version_conflict")) onConflict(c.id);
  };
  const runPreviewRef = useRef(runPreview);
  useEffect(() => {
    runPreviewRef.current = runPreview;
  });
  // Vừa tạo từ nút Xem trước giá: xem trước ngay bản vừa tạo.
  useEffect(() => {
    if (o.autoPreview && initial !== null) void runPreviewRef.current(initial);
  }, [o.autoPreview, initial]);

  const onSave = async () => {
    const isNew = base === null;
    const c = await persist();
    if (c === null) return;
    if (isNew) void navigate(`${LIST}/${c.id}`, { replace: true });
    else setSaved(true);
  };
  const onPreview = async () => {
    if (base !== null && sameAsSaved(draft, base)) {
      await runPreview(base);
      return;
    }
    const isNew = base === null;
    const c = await persist();
    if (c === null) return;
    if (isNew) void navigate(`${LIST}/${c.id}`, { replace: true, state: { previewNow: true } });
    else await runPreview(c);
  };

  const chip = base === null ? null : statusChip(base.status);
  const stale = preview !== null && base !== null && !sameAsSaved(draft, base);

  return (
    <div className="wc">
      <div className="page-h">
        <div className="grow">
          <p className="crumb">
            <Link to={LIST}>Khuyến mãi / Chiến dịch giảm giá</Link>
          </p>
          <h1>{base === null ? "Tạo chiến dịch" : "Sửa chiến dịch"}</h1>
        </div>
        {chip !== null && <span className={chip.className}>{chip.label}</span>}
      </div>
      {perms.loaded && !canManage && <p className="reason">{TEXT_READ_ONLY}</p>}
      {canManage && running && (
        <div className="box box-soft stack-sm">
          <p>
            Chiến dịch đang chạy. <b>Tạm dừng để sửa</b>: trong lúc sửa, giá trên website trở về giá gốc. Sửa xong bấm
            Kích hoạt ở danh sách để chạy lại.
          </p>
          <div>
            <button
              type="button"
              className="btn btn-line"
              onClick={() => {
                setPausing(true);
              }}
            >
              Tạm dừng và sửa
            </button>
          </div>
        </div>
      )}
      {conflict && base !== null && (
        <div className="box box-err stack-sm">
          <b>Chiến dịch vừa được người khác đổi.</b>
          <p>Chưa lưu gì. Nội dung bạn đang nhập vẫn giữ trên form; mở bản mới ở tab khác để so rồi lưu lại.</p>
          <div className="row">
            <Link className="btn btn-line" to={`${LIST}/${base.id}`} target="_blank" rel="noopener">
              Mở bản mới
            </Link>
          </div>
        </div>
      )}

      <div className="cols">
        <section className="card stack">
          <h2>{base === null ? "Chiến dịch mới" : "Nội dung chiến dịch"}</h2>
          <fieldset className="wc-fs stack" disabled={locked || busy}>
            <Field
              label="Tên chiến dịch *"
              error={errors.name}
              hint={<span className="hint">1–120 ký tự; phần đầu tên thành liên kết /uu-dai/… trên website</span>}
            >
              {(id) => (
                <input
                  id={id}
                  className="input"
                  maxLength={NAME_MAX + 20}
                  value={draft.name}
                  onChange={(e) => {
                    edit({ name: e.target.value });
                  }}
                />
              )}
            </Field>
            <div className="grid-f">
              <Field label="Phạm vi">
                {(id) => (
                  <span className="select-wrap">
                    <select
                      id={id}
                      value={draft.scope}
                      onChange={(e) => {
                        edit({ scope: asScope(e.target.value), targets: [] });
                      }}
                    >
                      {SCOPES.map((s) => (
                        <option key={s} value={s}>
                          {SCOPE_TEXT[s]}
                        </option>
                      ))}
                    </select>
                  </span>
                )}
              </Field>
              <Field
                label="Phần trăm giảm *"
                error={
                  errors.percent ??
                  (tooHigh ? `${percentTooHighText(draft.percent, maxPercent)} ${TEXT_RAISE_CAP}` : undefined)
                }
              >
                {(id) => (
                  <span className="wc-unit">
                    <input
                      id={id}
                      className="input num"
                      inputMode="numeric"
                      maxLength={3}
                      value={draft.percent}
                      onChange={(e) => {
                        edit({ percent: e.target.value });
                      }}
                    />
                    <span>{percentHint(maxPercent)}</span>
                  </span>
                )}
              </Field>
            </div>
            {draft.scope !== "all" && !locked && (
              <TargetPicker
                key={draft.scope}
                scope={draft.scope}
                picked={draft.targets}
                error={errors.targets}
                onChange={(targets) => {
                  edit({ targets });
                }}
              />
            )}
            {draft.scope !== "all" && locked && (
              <div className="picks">
                {draft.targets.map((t) => (
                  <span key={t.id} className="tag">
                    {t.name}
                  </span>
                ))}
              </div>
            )}
            <div className="grid-f">
              <Field label="Bắt đầu (giờ Việt Nam) *" error={errors.start}>
                {(id) => (
                  <input
                    id={id}
                    className="input num"
                    type="datetime-local"
                    value={draft.start}
                    onChange={(e) => {
                      edit({ start: e.target.value });
                    }}
                  />
                )}
              </Field>
              <Field label="Kết thúc (giờ Việt Nam) *" error={errors.end}>
                {(id) => (
                  <input
                    id={id}
                    className="input num"
                    type="datetime-local"
                    value={draft.end}
                    onChange={(e) => {
                      edit({ end: e.target.value });
                    }}
                  />
                )}
              </Field>
            </div>
            <label className="check">
              <input
                type="checkbox"
                checked={draft.allowCoupon}
                onChange={(e) => {
                  edit({ allowCoupon: e.target.checked });
                }}
              />{" "}
              Cho dùng mã giảm giá cùng chiến dịch
            </label>
            <p className="hint">Tắt: món đang có giá chiến dịch không tính vào tiền được giảm của mã.</p>
            <label className="check">
              <input
                type="checkbox"
                checked={draft.clearance}
                onChange={(e) => {
                  edit({ clearance: e.target.checked });
                }}
              />{" "}
              Hàng cắt mã: hiện các mẫu này trong mục &quot;Siêu ưu đãi&quot; trên website
            </label>
          </fieldset>
          {otherError !== null && <div className="box box-err">{otherError}</div>}
          {previewAction.message !== null && !conflict && <div className="box box-err">{previewAction.message}</div>}
          <div className="savebar">
            {saved && (
              <span className="small" role="status">
                Đã lưu.
              </span>
            )}
            <Link className="btn btn-quiet" to={LIST}>
              {locked ? "Quay lại" : "Hủy"}
            </Link>
            {!locked && (
              <>
                <button type="button" className="btn btn-line" disabled={busy} onClick={() => void onPreview()}>
                  Xem trước giá
                </button>
                <button type="button" className="btn btn-dark" disabled={busy} onClick={() => void onSave()}>
                  {base === null || base.state === "draft" ? "Lưu bản nháp" : "Lưu"}
                </button>
              </>
            )}
          </div>
        </section>
        <aside className="stack">
          <PreviewCard preview={preview} base={base} stale={stale} />
        </aside>
      </div>

      {pausing && base !== null && (
        <StateDialog
          campaign={base}
          mode="pauseEdit"
          onClose={() => {
            setPausing(false);
          }}
          onDone={(c) => {
            setPausing(false);
            setBase(c);
            after(c);
          }}
        />
      )}
    </div>
  );
}

/** state của điều hướng sau khi tạo bằng nút Xem trước giá. */
function wantsPreview(state: unknown): boolean {
  return typeof state === "object" && state !== null && "previewNow" in state && state.previewNow === true;
}

function ExistingEditor({ id }: { id: string }) {
  const location = useLocation();
  const c = useCampaign(id);
  if (c.isError) return <div className="box box-err">{errorMessage(c.error)}</div>;
  if (c.data === undefined) return <p className="muted">Đang tải…</p>;
  // EditorForm dựng form một lần từ bản đọc đầu tiên; đọc lại sau đó không ghi đè chữ đang gõ.
  return <EditorForm base={c.data} targets={pickedTargets(c.data)} autoPreview={wantsPreview(location.state)} />;
}

export function CampaignEditor() {
  const { id } = useParams();
  // Chờ biết quyền: form khóa hay mở theo promotion.manage, không vẽ form khóa rồi mở ra.
  const perms = usePerms();
  if (!perms.loaded) return <p className="muted">Đang tải…</p>;
  return id === undefined ? (
    <EditorForm base={null} targets={[]} autoPreview={false} />
  ) : (
    <ExistingEditor key={id} id={id} />
  );
}
