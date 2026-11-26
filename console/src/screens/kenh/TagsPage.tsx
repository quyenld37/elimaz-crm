// Thẻ "Nhãn" của Cài đặt hộp chat /settings/tags (kế hoạch M4 Task 178; mockup cai-dat.html trạng thái C; HV55): nhãn
// hội thoại dùng chung cả shop, màu từ bốn sắc có sẵn, tên trùng báo tại ô. Sửa cần conversation.settings phạm vi cả
// shop (Task 161 quyết định 1, tài nguyên chung); sửa chỉ gửi trường đã đổi (PATCH) kèm version.
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import {
  createConversationTag,
  deleteConversationTag,
  updateConversationTag,
  type ConversationTag,
  type ConversationTagList,
  type UpdateConversationTagRequest,
} from "../../api";
import { ApiError, errorMessage, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { TAGS_KEY, useMe, useTags } from "./kenhData";
import { TAG_COLORS, TAG_NAME_MAX, charCount, hasAll, sameName, tagClass } from "./kenhRules";
import { SettingsShell } from "./PageSettingsPage";
import { Block, DeleteDialog, useBlock } from "./settingsKit";

const EMPTY: ConversationTag = { id: "", name: "", color: "soft", sort: 0, version: 0 };

interface EditorProps {
  tag: ConversationTag | null;
  all: readonly ConversationTag[];
  canEdit: boolean;
  onDone: () => void;
}

function TagEditor({ tag, all, canEdit, onDone }: EditorProps) {
  const qc = useQueryClient();
  const b = useBlock(tag ?? EMPTY, (t) => ({ name: t.name, color: t.color }));
  const [nameErr, setNameErr] = useState<string | undefined>();
  const [askDelete, setAskDelete] = useState(false);
  const create = useWriteAction((key, body: { name: string; color: string }) =>
    unwrap(createConversationTag({ body, headers: { "Idempotency-Key": key } })),
  );
  const update = useWriteAction((key, body: UpdateConversationTagRequest & { id: string }) => {
    const { id, ...rest } = body;
    return unwrap(updateConversationTag({ path: { id }, body: rest, headers: { "Idempotency-Key": key } }));
  });
  const remove = useWriteAction((key, body: { id: string }) =>
    unwrap(deleteConversationTag({ path: { id: body.id }, headers: { "Idempotency-Key": key } })),
  );
  const write = b.base.id === "" ? create : update;
  const v = b.inputs;
  const dupText = (name: string) => `Nhãn "${name}" đã có.`;

  const onSave = async () => {
    const name = v.name.trim();
    const n = charCount(name);
    const dup = all.find((t) => t.id !== b.base.id && sameName(t.name, name));
    const err =
      n === 0
        ? "Nhập tên nhãn."
        : n > TAG_NAME_MAX
          ? `Tối đa ${String(TAG_NAME_MAX)} ký tự.`
          : dup === undefined
            ? undefined
            : dupText(dup.name);
    setNameErr(err);
    if (err !== undefined) return;
    const r =
      b.base.id === ""
        ? await create.run({ name, color: v.color })
        : await update.run({
            id: b.base.id,
            expectedVersion: b.base.version,
            ...(name === b.base.name ? {} : { name }),
            ...(v.color === b.base.color ? {} : { color: v.color }),
          });
    if (!r.ok) {
      if (r.error instanceof ApiError && r.error.code === "already_exists") setNameErr(dupText(name));
      return;
    }
    void qc.invalidateQueries({ queryKey: TAGS_KEY });
    onDone();
  };
  const reload = async () => {
    await qc.refetchQueries({ queryKey: TAGS_KEY });
    const fresh = qc.getQueryData<ConversationTagList>(TAGS_KEY)?.items.find((t) => t.id === b.base.id);
    if (fresh !== undefined) b.reset(fresh);
    update.reset();
  };

  return (
    <>
      <Block
        title={b.base.id === "" ? "Nhãn mới" : `Sửa nhãn ${b.base.name}`}
        dirty={b.dirty}
        canEdit={canEdit}
        pending={write.pending}
        error={write.error}
        message={write.message}
        fieldError={write.error instanceof ApiError && write.error.code === "already_exists"}
        saveLabel="Lưu nhãn"
        onReload={() => void reload()}
        onSave={() => void onSave()}
        extra={
          <>
            {b.base.id !== "" && canEdit && (
              <button
                type="button"
                className="btn btn-quiet btn-danger"
                onClick={() => {
                  setAskDelete(true);
                }}
              >
                Xóa nhãn
              </button>
            )}
            <button type="button" className="btn btn-quiet" onClick={onDone}>
              Hủy
            </button>
          </>
        }
      >
        <label className="field">
          <span>Tên nhãn</span>
          <input
            className={nameErr === undefined ? "input" : "input is-err"}
            value={v.name}
            disabled={!canEdit}
            {...(nameErr === undefined ? {} : { "aria-invalid": true })}
            onChange={(e) => {
              const name = e.target.value;
              b.setInputs((x) => ({ ...x, name }));
            }}
          />
          {nameErr !== undefined && <span className="field-err">{nameErr}</span>}
        </label>
        <div className="radios" role="radiogroup" aria-label="Màu">
          {TAG_COLORS.map((c) => (
            <label key={c.value} className="check">
              <input
                type="radio"
                name="tag-color"
                checked={v.color === c.value}
                disabled={!canEdit}
                onChange={() => {
                  b.setInputs((x) => ({ ...x, color: c.value }));
                }}
              />{" "}
              <span className={`lb ${c.cls}`}>{c.label}</span>
            </label>
          ))}
        </div>
        <p className="hint">Nhãn dùng chung cả shop. Xóa nhãn thì gỡ khỏi mọi hội thoại.</p>
      </Block>
      {askDelete && (
        <DeleteDialog
          title={`Xóa nhãn ${b.base.name}?`}
          confirmLabel="Xóa nhãn"
          id={b.base.id}
          remove={remove}
          listKey={TAGS_KEY}
          onCancel={() => {
            setAskDelete(false);
          }}
          onDeleted={onDone}
        >
          <p className="small">Nhãn được gỡ khỏi mọi hội thoại đang gắn.</p>
        </DeleteDialog>
      )}
    </>
  );
}

export function TagsPage() {
  const me = useMe();
  const tags = useTags();
  const canEdit = hasAll(me.data?.grants ?? [], "conversation.settings");
  const [editing, setEditing] = useState<ConversationTag | "new" | null>(null);
  const items = tags.data?.items ?? [];
  return (
    <SettingsShell>
      <div className="cfg">
        <section className="card">
          <div className="row-between">
            <h2>
              Nhãn <span className="small muted">{String(items.length)}</span>
            </h2>
            {canEdit && (
              <button
                type="button"
                className="btn btn-quiet"
                onClick={() => {
                  setEditing("new");
                }}
              >
                + Nhãn
              </button>
            )}
          </div>
          {!canEdit && me.data !== undefined && (
            <div className="box box-info small">
              <b>Bạn chỉ xem được nhãn.</b> Sửa cần quyền "Cài đặt hộp chat" cho cả shop.
            </div>
          )}
          {tags.isError && <div className="box box-err small">{errorMessage(tags.error)}</div>}
          {tags.data !== undefined && items.length === 0 && <p className="muted">Chưa có nhãn nào.</p>}
          <div className="lbs">
            {items.map((t) =>
              canEdit ? (
                <button
                  key={t.id}
                  type="button"
                  className={`lb lb-btn ${tagClass(t.color)}`}
                  aria-label={`Sửa nhãn ${t.name}`}
                  onClick={() => {
                    setEditing(t);
                  }}
                >
                  {t.name}
                </button>
              ) : (
                <span key={t.id} className={`lb ${tagClass(t.color)}`}>
                  {t.name}
                </span>
              ),
            )}
          </div>
        </section>
        {editing !== null && (
          <TagEditor
            key={editing === "new" ? "new" : editing.id}
            tag={editing === "new" ? null : editing}
            all={items}
            canEdit={canEdit}
            onDone={() => {
              setEditing(null);
            }}
          />
        )}
      </div>
    </SettingsShell>
  );
}
