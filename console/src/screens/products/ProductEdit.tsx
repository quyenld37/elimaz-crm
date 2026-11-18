// Màn Sản phẩm — thêm và sửa mẫu (kế hoạch M1 Task 324; mockup docs/mockups/m1/san-pham-sua.html, trạng thái A, C).
// "/san-pham/moi": tạo mẫu bằng một createProduct kèm mọi biến thể. "/san-pham/<id>": bản nháp giữ version lúc mở; Lưu
// chia thành các bước theo editRules.savePlan, mỗi bước một khóa chống trùng (useWriteAction). Người khác vừa lưu →
// "Mẫu đã thay đổi, tải lại." Báo hiệu realtime tải lại dữ liệu nền nhưng không đè bản nháp.
// Khối Ảnh (Task 325, ImageManager) nằm ở cột phải trên thẻ Phân loại: ảnh tải lên (useUploads) sống ở ExistingProduct
// nên lưu mẫu hay tải lại mẫu không làm mất; thao tác ảnh tăng version mẫu, bản nháp nhận version mới; trình soạn bài viết (Task 326) vào cột trái dưới thẻ Thông tin.
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useId, useMemo, useState, type ReactNode } from "react";
import { Link, useLocation, useNavigate, useParams } from "react-router";
import {
  addVariant,
  createProduct,
  getMe,
  getProduct,
  getProductClassification,
  listAttributes,
  listCategories,
  listTags,
  setProductClassification,
  updateProduct,
  updateVariant,
  type AddVariantRequest,
  type Attribute,
  type Category,
  type CreateProductRequest,
  type Product,
  type ProductClassification,
  type ProductImage,
  type ProductStatus,
  type SetClassificationRequest,
  type Tag,
  type UpdateProductRequest,
  type UpdateVariantRequest,
} from "../../api";
import { ApiError, errorMessage, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { hasPermission } from "../../app/nav";
import { TOPICS, queryKey } from "../../app/queryClient";
import {
  applyClassification,
  applyProduct,
  applyVariant,
  charCount,
  classificationBody,
  classificationChanged,
  createBody,
  draftFromProduct,
  emptyDraft,
  MAX_DESCRIPTION,
  savePlan,
  setCode,
  validate,
  type Draft,
  type Pair,
  type RowErrors,
  type SaveStep,
  type Validation,
} from "./editRules";
import { ImageManager } from "./ImageManager";
import { categoryOptions, STATUS_CHIP, STATUSES } from "./productRules";
import { useUploads, type Uploads } from "./useUpload";
import { VariantGrid } from "./VariantGrid";
import "./products.css";

export const TEXT_CONFLICT = "Mẫu đã thay đổi, tải lại.";
export const TEXT_CONFLICT_MORE = "Có người vừa lưu mẫu này. Thay đổi của bạn chưa được lưu.";
export const TEXT_SKU_TAKEN = "Mã hàng đã có";
const TEXT_SKU_TAKEN_CREATE = "Mã hàng đã có: một SKU trong bảng đã dùng cho mẫu khác. Sửa SKU rồi lưu lại.";
const TEXT_FIX_FIELDS = "Chưa lưu: có ô chưa đúng, xem các ô được tô đỏ.";
const TEXT_BAD_ID = "Không có mẫu này. Quay lại danh sách sản phẩm.";
const NEW_ID = "moi";
const LIST = "/san-pham";

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/** Chữ của ô chọn trạng thái mẫu (mockup). */
const STATUS_OPTION: Readonly<Record<ProductStatus, string>> = {
  active: "Đang bán",
  draft: "Đang nhập · ẩn với khách",
  discontinued: "Ngừng bán",
};

const NUM = new Intl.NumberFormat("vi-VN");

/** Câu báo mang theo khi chuyển từ trang tạo sang trang sửa (phân loại chưa lưu được). */
function noticeOf(state: unknown): string | null {
  if (typeof state !== "object" || state === null || !("notice" in state)) return null;
  return typeof state.notice === "string" ? state.notice : null;
}

export function ProductEdit() {
  const { id = "" } = useParams();
  if (id === NEW_ID) return <EditForm key={NEW_ID} mode="create" initial={emptyDraft()} onReload={null} />;
  if (!UUID.test(id)) return <p className="box box-err">{TEXT_BAD_ID}</p>;
  return <ExistingProduct key={id} id={id} />;
}

function ExistingProduct({ id }: { id: string }) {
  const product = useQuery({
    queryKey: queryKey(TOPICS.product, "detail", id),
    queryFn: () => unwrap(getProduct({ path: { id } })),
  });
  const cls = useQuery({
    queryKey: queryKey(TOPICS.product, "classification", id),
    queryFn: () => unwrap(getProductClassification({ path: { id } })),
  });
  // Mỗi lần tải lại (bấm "Tải lại mẫu" hay lưu xong) dựng bản nháp mới từ dữ liệu vừa đọc.
  const [generation, setGeneration] = useState(0);
  const uploads = useUploads();
  const [snapshot, setSnapshot] = useState<{ p: Product; c: ProductClassification } | null>(null);
  if (snapshot === null && product.data !== undefined && cls.data !== undefined) {
    setSnapshot({ p: product.data, c: cls.data });
  }
  // Đọc lỗi thì refetch vẫn trả dữ liệu cũ: không dựng lại bản nháp từ đó, trả câu lỗi cho biểu mẫu báo.
  const reload = async (): Promise<string | null> => {
    const [p, c] = await Promise.all([product.refetch(), cls.refetch()]);
    const err = p.error ?? c.error;
    if (err !== null) return errorMessage(err);
    if (p.data === undefined || c.data === undefined) return errorMessage(null);
    setSnapshot({ p: p.data, c: c.data });
    setGeneration((g) => g + 1);
    return null;
  };
  if (snapshot === null) {
    const err = product.error ?? cls.error;
    if (err !== null) return <p className="box box-err">{errorMessage(err)}</p>;
    return <p className="muted">Đang tải mẫu…</p>;
  }
  return (
    <EditForm
      key={generation}
      mode="edit"
      productId={id}
      initialImages={snapshot.p.images}
      uploads={uploads}
      initial={draftFromProduct(snapshot.p, snapshot.c)}
      onReload={reload}
    />
  );
}

interface EditFormProps {
  mode: "create" | "edit";
  productId?: string;
  /** Ảnh đang hiện lúc đọc mẫu (trang sửa). */
  initialImages?: readonly ProductImage[];
  /** Ảnh đang tải lên của trang sửa (sống ngoài biểu mẫu). */
  uploads?: Uploads;
  initial: Draft;
  /** Đọc lại mẫu và dựng bản nháp mới (chỉ trang sửa); đọc lỗi → câu lỗi, bản nháp giữ nguyên. */
  onReload: (() => Promise<string | null>) | null;
}

type StepResult = { ok: true; pair: Pair } | { ok: false; error: unknown };

const NO_IMAGES: readonly ProductImage[] = [];

function EditForm({ mode, productId = "", initialImages = NO_IMAGES, uploads, initial, onReload }: EditFormProps) {
  const navigate = useNavigate();
  const location = useLocation();
  const qc = useQueryClient();
  const me = useQuery({ queryKey: queryKey("identity.me"), queryFn: () => unwrap(getMe()) });
  const canManage = hasPermission(me.data?.grants ?? [], "catalog.manage");
  const canUpload = hasPermission(me.data?.grants ?? [], "files.upload");
  const cats = useQuery({ queryKey: queryKey("catalog.categories"), queryFn: () => unwrap(listCategories()) });
  const tags = useQuery({ queryKey: queryKey("catalog.tags"), queryFn: () => unwrap(listTags()) });
  const attrs = useQuery({ queryKey: queryKey("catalog.attributes"), queryFn: () => unwrap(listAttributes()) });

  const [pair, setPair] = useState<Pair>({ base: initial, edit: initial });
  const { base, edit } = pair;
  const setEdit = (d: Draft) => {
    setPair((p) => ({ ...p, edit: d }));
  };
  const [checked, setChecked] = useState<Validation | null>(null);
  const [serverRows, setServerRows] = useState<Record<string, RowErrors>>({});
  const [codeError, setCodeError] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(noticeOf(location.state));
  const [conflict, setConflict] = useState(false);
  const [saving, setSaving] = useState(false);
  const [images, setImages] = useState(initialImages);
  const [imagePending, setImagePending] = useState(false);

  const headers = (key: string) => ({ "Idempotency-Key": key });
  const create = useWriteAction((key, body: CreateProductRequest) =>
    unwrap(createProduct({ body, headers: headers(key) })),
  );
  const upd = useWriteAction((key, b: { id: string; body: UpdateProductRequest }) =>
    unwrap(updateProduct({ path: { id: b.id }, body: b.body, headers: headers(key) })),
  );
  const classify = useWriteAction((key, b: { id: string; body: SetClassificationRequest }) =>
    unwrap(setProductClassification({ path: { id: b.id }, body: b.body, headers: headers(key) })),
  );
  const add = useWriteAction((key, b: { id: string; body: AddVariantRequest }) =>
    unwrap(addVariant({ path: { id: b.id }, body: b.body, headers: headers(key) })),
  );
  const updV = useWriteAction((key, b: { id: string; body: UpdateVariantRequest }) =>
    unwrap(updateVariant({ path: { id: b.id }, body: b.body, headers: headers(key) })),
  );

  const readOnly = !canManage;
  const plan = useMemo(() => (mode === "edit" ? savePlan(base, edit) : []), [mode, base, edit]);
  const dirty = mode === "create" || plan.length > 0;
  // Lỗi kiểm trước khi gửi chỉ hiện sau lần bấm Lưu đầu, và cập nhật theo bản nháp từ đó.
  const live = checked === null ? null : validate(edit, mode);
  const rowErrors: Record<string, RowErrors> = { ...(live?.rows ?? {}) };
  for (const [k, e] of Object.entries(serverRows)) rowErrors[k] = { ...e, ...rowErrors[k] };
  const fieldErr = live?.fields ?? {};

  const runStep = async (p: Pair, step: SaveStep): Promise<StepResult> => {
    const v = p.edit.version;
    switch (step.kind) {
      case "product": {
        const r = await upd.run({ id: productId, body: { version: v, ...step.body } });
        return r.ok ? { ok: true, pair: applyProduct(p, r.data) } : r;
      }
      case "classification": {
        const r = await classify.run({ id: productId, body: { version: v, ...step.body } });
        return r.ok ? { ok: true, pair: applyClassification(p, r.data) } : r;
      }
      case "add": {
        const r = await add.run({ id: productId, body: { productVersion: v, ...step.body } });
        return r.ok ? { ok: true, pair: applyVariant(p, step.key, r.data) } : r;
      }
      case "variant": {
        const r = await updV.run({ id: step.id, body: step.body });
        return r.ok ? { ok: true, pair: applyVariant(p, step.key, r.data) } : r;
      }
    }
  };

  const showError = (err: unknown, step: SaveStep | null) => {
    const code = err instanceof ApiError ? err.code : "";
    if (code === "version_conflict") {
      setConflict(true);
      return;
    }
    const rowKey = step !== null && (step.kind === "add" || step.kind === "variant") ? step.key : null;
    if (rowKey !== null && code === "sku_taken") {
      setServerRows({ [rowKey]: { sku: TEXT_SKU_TAKEN } });
      setMessage(`Chưa lưu xong: ${TEXT_SKU_TAKEN.toLowerCase()}, sửa dòng được tô đỏ.`);
      return;
    }
    if (rowKey !== null && code === "barcode_taken") {
      setServerRows({ [rowKey]: { barcode: errorMessage(err) } });
      setMessage("Chưa lưu xong: sửa dòng được tô đỏ.");
      return;
    }
    if (mode === "create" && code === "sku_taken") {
      setMessage(TEXT_SKU_TAKEN_CREATE);
      return;
    }
    if (mode === "create" && code === "code_taken") {
      setCodeError(errorMessage(err));
      return;
    }
    setMessage(errorMessage(err));
  };

  const save = async () => {
    if (saving) return;
    const v = validate(edit, mode);
    setChecked(v);
    setServerRows({});
    setCodeError(null);
    setConflict(false);
    if (!v.ok) {
      setMessage(TEXT_FIX_FIELDS);
      return;
    }
    setMessage(null);
    setSaving(true);
    try {
      if (mode === "create") {
        const r = await create.run(createBody(edit));
        if (!r.ok) {
          showError(r.error, null);
          return;
        }
        let notice: string | null = null;
        if (classificationChanged(base, edit)) {
          const c = await classify.run({
            id: r.data.id,
            body: { version: r.data.version, ...classificationBody(edit) },
          });
          if (!c.ok)
            notice = `Đã tạo mẫu, nhưng phân loại chưa lưu: ${errorMessage(c.error)} Chọn lại rồi bấm Lưu mẫu.`;
        }
        await qc.invalidateQueries({ queryKey: [TOPICS.product, "list"] });
        void navigate(`${LIST}/${encodeURIComponent(r.data.id)}`, { replace: true, state: { notice } });
        return;
      }
      let p: Pair = pair;
      for (const step of plan) {
        const r = await runStep(p, step);
        if (!r.ok) {
          setPair(p);
          showError(r.error, step);
          return;
        }
        p = r.pair;
      }
      setPair(p);
      await qc.invalidateQueries({ queryKey: [TOPICS.product, "list"] });
      if (onReload !== null) {
        const e = await onReload();
        if (e !== null) setMessage(`Đã lưu, nhưng chưa tải lại được mẫu: ${e}`);
      }
    } finally {
      setSaving(false);
    }
  };

  // Thao tác ảnh xong: ảnh mới; version chỉ tăng bởi thao tác đó → bản nháp nhận version (lần lưu sau không xung đột
  // oan); nhảy hơn (người khác vừa sửa) → giữ version cũ, báo tải lại (không đè thay đổi của người khác).
  const onImages = (p: Product, accepted: boolean) => {
    setImages(p.images);
    if (accepted)
      setPair((x) => ({ base: { ...x.base, version: p.version }, edit: { ...x.edit, version: p.version } }));
    else setConflict(true);
  };
  const savedColors = base.rows.filter((r) => r.id !== undefined).map((r) => r.color);

  const discard = () => {
    setPair((p) => ({ ...p, edit: p.base }));
    setChecked(null);
    setServerRows({});
    setCodeError(null);
    setMessage(null);
  };

  const title = mode === "create" ? "Thêm mẫu" : base.name;
  const saveBtn = (
    <button
      type="button"
      className="btn btn-dark"
      disabled={saving || imagePending || !dirty}
      onClick={() => {
        void save();
      }}
    >
      {saving ? "Đang lưu…" : "Lưu mẫu"}
    </button>
  );

  return (
    <div className="p-edit">
      <p className="crumb">
        <Link to={LIST}>Sản phẩm</Link> / {mode === "create" ? "Thêm mẫu" : base.code}
      </p>
      <div className="page-h">
        <div className="grow p-title">
          <h1>{title}</h1>
          <div className="row p-meta">
            <span className={STATUS_CHIP[base.status].className}>{STATUS_CHIP[base.status].label}</span>
            {mode === "edit" && (
              <span className="small muted">
                {NUM.format(base.rows.length)} biến thể · {NUM.format(images.length)} ảnh
              </span>
            )}
          </div>
        </div>
        {!readOnly && (
          <div className="savebar desk-only">
            {mode === "edit" && dirty && <span className="small muted">Có thay đổi chưa lưu</span>}
            {saveBtn}
          </div>
        )}
      </div>

      {conflict && (
        <div className="box box-err p-box" role="alert">
          <b>{TEXT_CONFLICT}</b> <span>{TEXT_CONFLICT_MORE}</span>
          <div className="row p-box-act">
            <button
              type="button"
              className="btn btn-dark"
              onClick={() => {
                if (onReload === null) return;
                void onReload().then((e) => {
                  if (e !== null) setMessage(`Chưa tải lại được mẫu: ${e}`);
                });
              }}
            >
              Tải lại mẫu
            </button>
          </div>
        </div>
      )}
      {message !== null && (
        <div className="box box-err p-box" role="alert">
          {message}
        </div>
      )}

      <fieldset className="fs-plain" disabled={readOnly || saving}>
        <div className="cols">
          <div className="stack">
            <InfoCard mode={mode} draft={edit} errors={fieldErr} codeError={codeError} onChange={setEdit} />
            {/* Task 326: trình soạn bài viết (ArticleEditor) gắn ở đây. */}
          </div>
          <div className="stack">
            {mode === "edit" && uploads !== undefined && (
              <ImageManager
                productId={productId}
                images={images}
                version={edit.version}
                colors={savedColors}
                canManage={canManage}
                canUpload={canUpload}
                busy={saving}
                uploads={uploads}
                onProduct={onImages}
                onPending={setImagePending}
                onConflict={() => {
                  setConflict(true);
                }}
              />
            )}
            <ClassificationCard
              draft={edit}
              categories={cats.data ?? []}
              tags={tags.data ?? []}
              attributes={attrs.data ?? []}
              onChange={setEdit}
            />
          </div>
        </div>
        <VariantGrid draft={edit} errors={rowErrors} readOnly={readOnly} onChange={setEdit} />
        {fieldErr.variants !== undefined && <p className="field-err">{fieldErr.variants}</p>}
      </fieldset>

      {!readOnly && (
        <div className="savebar is-sticky p-savebar">
          <button
            type="button"
            className="btn btn-line"
            disabled={saving || (mode === "edit" && !dirty)}
            onClick={discard}
          >
            Hủy thay đổi
          </button>
          {saveBtn}
        </div>
      )}
    </div>
  );
}

/** Ô của biểu mẫu: nhãn gắn bằng htmlFor (tên truy cập chỉ là nhãn), câu lỗi hoặc gợi ý bên dưới. */
function Field({
  label,
  error,
  hint,
  children,
}: {
  label: string;
  error?: string;
  hint?: ReactNode;
  children: (id: string, invalid: boolean) => ReactNode;
}) {
  const id = useId();
  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      {children(id, error !== undefined)}
      {error !== undefined && <span className="field-err">{error}</span>}
      {hint !== undefined && <span className="hint">{hint}</span>}
    </div>
  );
}

const inputClass = (invalid: boolean) => `input${invalid ? " is-err" : ""}`;

function InfoCard({
  mode,
  draft,
  errors,
  codeError,
  onChange,
}: {
  mode: "create" | "edit";
  draft: Draft;
  errors: Validation["fields"];
  codeError: string | null;
  onChange: (d: Draft) => void;
}) {
  const codeErr = errors.code ?? codeError ?? undefined;
  return (
    <div className="card">
      <h2>Thông tin</h2>
      <div className="grid-f">
        <Field label="Mã mẫu *" error={codeErr}>
          {(id, invalid) => (
            <input
              id={id}
              className={inputClass(invalid)}
              value={draft.code}
              maxLength={64}
              disabled={mode === "edit"}
              aria-invalid={invalid || undefined}
              onChange={(e) => {
                onChange(setCode(draft, e.target.value));
              }}
            />
          )}
        </Field>
        <Field label="Trạng thái">
          {(id) => (
            <span className="select-wrap">
              <select
                id={id}
                value={draft.status}
                onChange={(e) => {
                  const v = STATUSES.find((s) => s === e.target.value);
                  if (v !== undefined) onChange({ ...draft, status: v });
                }}
              >
                {STATUSES.map((s) => (
                  <option key={s} value={s}>
                    {STATUS_OPTION[s]}
                  </option>
                ))}
              </select>
            </span>
          )}
        </Field>
      </div>
      <div className="stack p-info-more">
        <Field
          label="Tên mẫu *"
          error={errors.name}
          hint={errors.name === undefined ? `${NUM.format(charCount(draft.name.trim()))} ký tự` : undefined}
        >
          {(id, invalid) => (
            <input
              id={id}
              className={inputClass(invalid)}
              value={draft.name}
              aria-invalid={invalid || undefined}
              onChange={(e) => {
                onChange({ ...draft, name: e.target.value });
              }}
            />
          )}
        </Field>
        <Field
          label="Mô tả ngắn"
          error={errors.description}
          hint={`${NUM.format(charCount(draft.description))} / ${NUM.format(MAX_DESCRIPTION)} ký tự · chữ thường, không định dạng`}
        >
          {(id, invalid) => (
            <textarea
              id={id}
              className={inputClass(invalid)}
              value={draft.description}
              aria-invalid={invalid || undefined}
              onChange={(e) => {
                onChange({ ...draft, description: e.target.value });
              }}
            />
          )}
        </Field>
      </div>
    </div>
  );
}

function ClassificationCard({
  draft,
  categories,
  tags,
  attributes,
  onChange,
}: {
  draft: Draft;
  categories: readonly Category[];
  tags: readonly Tag[];
  attributes: readonly Attribute[];
  onChange: (d: Draft) => void;
}) {
  const options = categoryOptions(categories);
  // Danh mục đang gán đã lưu trữ: vẫn hiện (không tự bỏ khi lưu phần khác).
  const current = draft.categoryId;
  const archived =
    current !== "" && !options.some((o) => o.id === current) ? categories.find((c) => c.id === current) : undefined;
  const tagName = new Map(tags.map((t) => [t.id, t.name]));
  const freeTags = tags.filter((t) => !draft.tagIds.includes(t.id));
  return (
    <div className="card">
      <h2>Phân loại</h2>
      <div className="stack">
        <Field label="Danh mục">
          {(id) => (
            <span className="select-wrap">
              <select
                id={id}
                value={current}
                onChange={(e) => {
                  onChange({ ...draft, categoryId: e.target.value });
                }}
              >
                <option value="">Chưa chọn</option>
                {current !== "" && !options.some((o) => o.id === current) && (
                  <option value={current}>
                    {archived === undefined ? "Danh mục đang gán" : `${archived.name} (đã lưu trữ)`}
                  </option>
                )}
                {options.map((o) => (
                  <option key={o.id} value={o.id}>
                    {o.label}
                  </option>
                ))}
              </select>
            </span>
          )}
        </Field>
        <div className="field">
          <span>Nhãn (điều khiển kệ hàng trên web)</span>
          <div className="opt-row">
            {draft.tagIds.map((id) => {
              const name = tagName.get(id) ?? "Nhãn";
              return (
                <span className="tag" key={id}>
                  {name}
                  <button
                    type="button"
                    aria-label={`Bỏ nhãn ${name}`}
                    onClick={() => {
                      onChange({ ...draft, tagIds: draft.tagIds.filter((x) => x !== id) });
                    }}
                  >
                    ×
                  </button>
                </span>
              );
            })}
            {freeTags.length > 0 && (
              <span className="select-wrap">
                <select
                  aria-label="Thêm nhãn"
                  value=""
                  onChange={(e) => {
                    const id = e.target.value;
                    if (id !== "" && !draft.tagIds.includes(id)) onChange({ ...draft, tagIds: [...draft.tagIds, id] });
                  }}
                >
                  <option value="">+ Thêm nhãn</option>
                  {freeTags.map((t) => (
                    <option key={t.id} value={t.id}>
                      {t.name}
                    </option>
                  ))}
                </select>
              </span>
            )}
          </div>
        </div>
        {attributes.length > 0 && (
          <div className="field">
            <span>Thuộc tính</span>
            <div className="stack-sm">
              {attributes.map((a) => {
                const ids = new Set(a.values.map((v) => v.id));
                const chosen = draft.attributeValueIds.find((v) => ids.has(v)) ?? "";
                return (
                  <label className="attr" key={a.id}>
                    <span className="small">{a.name}</span>
                    <span className="select-wrap">
                      <select
                        aria-label={a.name}
                        value={chosen}
                        onChange={(e) => {
                          const v = e.target.value;
                          const rest = draft.attributeValueIds.filter((x) => !ids.has(x));
                          onChange({ ...draft, attributeValueIds: v === "" ? rest : [...rest, v] });
                        }}
                      >
                        <option value="">Chưa chọn</option>
                        {a.values.map((v) => (
                          <option key={v.id} value={v.id}>
                            {v.value}
                          </option>
                        ))}
                      </select>
                    </span>
                  </label>
                );
              })}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
