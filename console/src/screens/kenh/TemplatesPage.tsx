// Thẻ "Mẫu trả lời" của Cài đặt hộp chat /settings/templates (kế hoạch M4 Task 178; mockup cai-dat.html trạng thái C;
// HV55): phím tắt, tên, áp cho Page, nội dung có bảng biến ngay dưới ô; biến lạ báo tại ô Nội dung (máy chủ trả 422
// invalid_input, thân lỗi không nói ô nào: màn tìm biến lạ trong nội dung đã gửi — Quyết định 4). Mẫu chung cần
// conversation.settings cả shop, mẫu của một Page cần quyền đó trên Page (Task 161 quyết định 1). Sửa chỉ gửi trường đã
// đổi (PATCH) kèm version; nút trả lời nhanh, tệp đính kèm của mẫu giữ nguyên.
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import {
  createReplyTemplate,
  deleteReplyTemplate,
  updateReplyTemplate,
  type ChannelAccount,
  type Grant,
  type ReplyTemplate,
  type ReplyTemplateList,
  type UpdateReplyTemplateRequest,
} from "../../api";
import { ApiError, errorMessage, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { TEMPLATES_KEY, useChannels, useMe, useTemplates } from "./kenhData";
import {
  SHORTCUT_RE,
  TEMPLATE_BODY_MAX,
  TEMPLATE_TITLE_MAX,
  TEMPLATE_VARIABLES,
  charCount,
  counter,
  hasAll,
  hasOnPage,
  unknownVariables,
} from "./kenhRules";
import { SettingsShell } from "./PageSettingsPage";
import { Block, Cnt, DeleteDialog, useBlock } from "./settingsKit";

const EMPTY: ReplyTemplate = { id: "", shortcut: "", title: "", body: "", quickReplies: [], version: 0, createdAt: "" };

/** Được tạo, sửa mẫu ở phạm vi này ("" = mọi Page). */
const canManage = (grants: readonly Grant[], pageId: string) =>
  pageId === "" ? hasAll(grants, "conversation.settings") : hasOnPage(grants, "conversation.settings", pageId);

export const unknownVarText = (names: readonly string[]) =>
  `Biến ${names.map((n) => `{${n}}`).join(", ")} không có. Dùng các biến ở bảng dưới.`;

type Field = "shortcut" | "title" | "body";

interface EditorProps {
  tpl: ReplyTemplate | null;
  accounts: readonly ChannelAccount[];
  grants: readonly Grant[];
  onDone: () => void;
}

function TemplateEditor({ tpl, accounts, grants, onDone }: EditorProps) {
  const qc = useQueryClient();
  const scopes = [
    ...(hasAll(grants, "conversation.settings") ? [{ id: "", name: "Mọi Page" }] : []),
    ...accounts
      .filter((a) => hasOnPage(grants, "conversation.settings", a.id))
      .map((a) => ({ id: a.id, name: a.name })),
  ];
  // Mẫu mới: phạm vi đầu tiên người dùng được tạo (Mọi Page khi có quyền cả shop).
  const first = scopes[0]?.id ?? "";
  const b = useBlock(tpl ?? (first === "" ? EMPTY : { ...EMPTY, pageId: first }), (t) => ({
    shortcut: t.shortcut,
    title: t.title,
    body: t.body,
    pageId: t.pageId ?? "",
  }));
  const [errs, setErrs] = useState<Partial<Record<Field, string>>>({});
  const [askDelete, setAskDelete] = useState(false);
  const create = useWriteAction((key, body: { shortcut: string; title: string; body: string; pageId?: string }) =>
    unwrap(createReplyTemplate({ body, headers: { "Idempotency-Key": key } })),
  );
  const update = useWriteAction((key, body: UpdateReplyTemplateRequest & { id: string }) => {
    const { id, ...rest } = body;
    return unwrap(updateReplyTemplate({ path: { id }, body: rest, headers: { "Idempotency-Key": key } }));
  });
  const remove = useWriteAction((key, body: { id: string }) =>
    unwrap(deleteReplyTemplate({ path: { id: body.id }, headers: { "Idempotency-Key": key } })),
  );
  const isNew = b.base.id === "";
  const write = isNew ? create : update;
  const v = b.inputs;
  const canEdit = canManage(grants, b.base.pageId ?? "") || isNew;
  const scopeOk = canManage(grants, v.pageId);

  const onSave = async () => {
    const e: Partial<Record<Field, string>> = {};
    const shortcut = v.shortcut.trim();
    const title = v.title.trim();
    if (!SHORTCUT_RE.test(shortcut)) e.shortcut = "Chỉ a–z, 0–9, - và _; tối đa 32 ký tự.";
    const tn = charCount(title);
    if (tn === 0 || tn > TEMPLATE_TITLE_MAX) e.title = `Nhập tên mẫu (tối đa ${String(TEMPLATE_TITLE_MAX)} ký tự).`;
    const bn = charCount(v.body.trim());
    if (bn === 0 || bn > TEMPLATE_BODY_MAX) e.body = `Nhập nội dung (tối đa ${String(TEMPLATE_BODY_MAX)} ký tự).`;
    setErrs(e);
    if (Object.keys(e).length > 0 || !scopeOk) return;
    const body = v.body.trim();
    const r = isNew
      ? await create.run({ shortcut, title, body, ...(v.pageId === "" ? {} : { pageId: v.pageId }) })
      : await update.run({
          id: b.base.id,
          expectedVersion: b.base.version,
          ...(shortcut === b.base.shortcut ? {} : { shortcut }),
          ...(title === b.base.title ? {} : { title }),
          ...(body === b.base.body ? {} : { body }),
          ...(v.pageId === (b.base.pageId ?? "") ? {} : { pageId: v.pageId === "" ? null : v.pageId }),
        });
    if (!r.ok) {
      const err = r.error;
      if (err instanceof ApiError && err.code === "already_exists")
        setErrs({ shortcut: `Phím tắt /${shortcut} đã có.` });
      if (err instanceof ApiError && err.code === "invalid_input") {
        const unknown = unknownVariables(body);
        if (unknown.length > 0) setErrs({ body: unknownVarText(unknown) });
      }
      return;
    }
    void qc.invalidateQueries({ queryKey: TEMPLATES_KEY });
    onDone();
  };
  const reload = async () => {
    await qc.refetchQueries({ queryKey: TEMPLATES_KEY });
    const fresh = qc.getQueryData<ReplyTemplateList>(TEMPLATES_KEY)?.items.find((t) => t.id === b.base.id);
    if (fresh !== undefined) b.reset(fresh);
    update.reset();
  };
  const fieldShown = Object.keys(errs).length > 0;
  const cnt = counter(v.body, TEMPLATE_BODY_MAX);
  const input = (f: Field) => ({
    className: errs[f] === undefined ? "input" : "input is-err",
    value: v[f],
    disabled: !canEdit,
    ...(errs[f] === undefined ? {} : { "aria-invalid": true }),
  });
  const set = (f: Field, val: string) => {
    b.setInputs((x) => ({ ...x, [f]: val }));
  };

  return (
    <>
      <Block
        title={isNew ? "Mẫu mới" : `Sửa mẫu /${b.base.shortcut}`}
        dirty={b.dirty}
        canEdit={canEdit}
        pending={write.pending}
        error={write.error}
        message={write.message}
        fieldError={fieldShown}
        saveLabel="Lưu mẫu"
        onReload={() => void reload()}
        onSave={() => void onSave()}
        extra={
          <>
            {!isNew && canEdit && (
              <button
                type="button"
                className="btn btn-quiet btn-danger"
                onClick={() => {
                  setAskDelete(true);
                }}
              >
                Xóa mẫu
              </button>
            )}
            <button type="button" className="btn btn-quiet" onClick={onDone}>
              Hủy
            </button>
          </>
        }
      >
        <div className="grid-f" style={{ gap: 8 }}>
          <label className="field">
            <span>Phím tắt *</span>
            <input
              {...input("shortcut")}
              onChange={(e) => {
                set("shortcut", e.target.value);
              }}
            />
            {errs.shortcut === undefined ? (
              <span className="hint">a–z, 0–9, - và _; tối đa 32</span>
            ) : (
              <span className="field-err">{errs.shortcut}</span>
            )}
          </label>
          <label className="field">
            <span>Áp cho Page</span>
            <span className="select-wrap">
              <select
                value={v.pageId}
                disabled={!canEdit}
                onChange={(e) => {
                  const pageId = e.target.value;
                  b.setInputs((x) => ({ ...x, pageId }));
                }}
              >
                {!scopes.some((s) => s.id === v.pageId) && (
                  <option value={v.pageId}>
                    {v.pageId === "" ? "Mọi Page" : (accounts.find((a) => a.id === v.pageId)?.name ?? "Page khác")}
                  </option>
                )}
                {scopes.map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.name}
                  </option>
                ))}
              </select>
            </span>
            {!scopeOk && canEdit && <span className="field-err">Bạn chưa có quyền tạo mẫu ở phạm vi này.</span>}
          </label>
        </div>
        <label className="field">
          <span>Tên mẫu *</span>
          <input
            {...input("title")}
            onChange={(e) => {
              set("title", e.target.value);
            }}
          />
          {errs.title !== undefined && <span className="field-err">{errs.title}</span>}
        </label>
        <label className="field">
          <span>
            Nội dung * <Cnt text={cnt.text} over={cnt.over} />
          </span>
          <textarea
            {...input("body")}
            onChange={(e) => {
              set("body", e.target.value);
            }}
          />
          {errs.body !== undefined && <span className="field-err">{errs.body}</span>}
        </label>
        <div className="tbl-frame">
          <table className="tbl">
            <thead>
              <tr>
                <th>Biến</th>
                <th>Thay bằng</th>
              </tr>
            </thead>
            <tbody>
              {TEMPLATE_VARIABLES.map((x) => (
                <tr key={x.name}>
                  <td data-label="Biến">
                    <code>{`{${x.name}}`}</code>
                  </td>
                  <td data-label="Thay bằng">{x.desc}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Block>
      {askDelete && (
        <DeleteDialog
          title={`Xóa mẫu /${b.base.shortcut}?`}
          confirmLabel="Xóa mẫu"
          id={b.base.id}
          remove={remove}
          listKey={TEMPLATES_KEY}
          onCancel={() => {
            setAskDelete(false);
          }}
          onDeleted={onDone}
        >
          <p className="small">Nhân viên không chọn được mẫu này nữa. Tin đã gửi bằng mẫu vẫn giữ.</p>
        </DeleteDialog>
      )}
    </>
  );
}

export function TemplatesPage() {
  const me = useMe();
  const templates = useTemplates();
  const channels = useChannels();
  const grants = me.data?.grants ?? [];
  const accounts = channels.data?.items ?? [];
  const [editing, setEditing] = useState<ReplyTemplate | "new" | null>(null);
  const items = templates.data?.items ?? [];
  const canCreate =
    hasAll(grants, "conversation.settings") || accounts.some((a) => hasOnPage(grants, "conversation.settings", a.id));
  const scopeName = (t: ReplyTemplate) =>
    t.pageId === undefined ? "Mọi Page" : (accounts.find((a) => a.id === t.pageId)?.name ?? "Page khác");
  return (
    <SettingsShell>
      <div className="cfg">
        <section className="card">
          <div className="row-between">
            <h2>
              Mẫu trả lời <span className="small muted">{String(items.length)}</span>
            </h2>
            {canCreate && (
              <button
                type="button"
                className="btn btn-quiet"
                onClick={() => {
                  setEditing("new");
                }}
              >
                + Mẫu
              </button>
            )}
          </div>
          {templates.isError && <div className="box box-err small">{errorMessage(templates.error)}</div>}
          {templates.data !== undefined && items.length === 0 && <p className="muted">Chưa có mẫu nào.</p>}
          <ul className="olines">
            {items.map((t) => (
              <li key={t.id}>
                <button
                  type="button"
                  className="btn-text olines-b"
                  onClick={() => {
                    setEditing(t);
                  }}
                >
                  <b>/{t.shortcut}</b> · {t.title}
                </button>
                <span className="small muted">{scopeName(t)}</span>
              </li>
            ))}
          </ul>
        </section>
        {editing !== null && (
          <TemplateEditor
            key={editing === "new" ? "new" : editing.id}
            tpl={editing === "new" ? null : editing}
            accounts={accounts}
            grants={grants}
            onDone={() => {
              setEditing(null);
            }}
          />
        )}
      </div>
    </SettingsShell>
  );
}
