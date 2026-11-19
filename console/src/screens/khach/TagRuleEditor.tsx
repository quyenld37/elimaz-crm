// Hộp "Nhãn khách" (kế hoạch M2 Task M2-29; mockup docs/mockups/m2/khach-danh-sach.html trạng thái E; HV35): tạo nhãn,
// đổi tên và màu (version), quy tắc tự gắn theo chỉ số khách (điều kiện nối bằng VÀ, bật/tắt). Quy tắc tự gắn và tự gỡ,
// không đụng nhãn gắn tay (máy chủ làm, Task M2-25). Tỷ lệ hoàn nhập theo %, gửi phần vạn. Cần customer.tags.manage.
import { useQueries, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import {
  createCustomerTag,
  getCustomerTagRule,
  putCustomerTagRule,
  updateCustomerTag,
  type CreateCustomerTagRequest,
  type CustomerTag,
  type CustomerTagRule,
  type PutCustomerTagRuleRequest,
  type UpdateCustomerTagRequest,
} from "../../api";
import { ApiError, errorMessage, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { queryKey } from "../../app/queryClient";
import { Dialog } from "../staff/staffData";
import { CUSTOMER_TOPIC, headers, reloadCustomers, useTags } from "./customerData";
import {
  OP_TEXT,
  TAG_COLORS,
  TAG_FIELDS,
  tagChip,
  toDisplayValue,
  toServerValue,
  valueHint,
  type TagField,
  type TagOp,
} from "./customerRules";

const MAX_CONDITIONS = 10;
const MAX_TAG_NAME = 50;
const ruleKey = (tagId: string) => queryKey(CUSTOMER_TOPIC, "tag-rule", tagId);

/** Quy tắc của nhãn; null khi nhãn chưa có quy tắc (máy chủ trả 404). */
async function readRule(tagId: string): Promise<CustomerTagRule | null> {
  try {
    return await unwrap(getCustomerTagRule({ path: { tagId } }));
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) return null;
    throw e;
  }
}

interface Row {
  key: number;
  field: TagField;
  op: TagOp;
  value: string;
}

const isField = (v: string): v is TagField => TAG_FIELDS.some((f) => f.field === v);
const isOp = (v: string): v is TagOp => v === "gte" || v === "lte";

function TagForm({ tag, onDone }: { tag: CustomerTag | null; onDone: () => void }) {
  const qc = useQueryClient();
  const [name, setName] = useState(tag?.name ?? "");
  const [color, setColor] = useState(tag?.color ?? "gray");
  const create = useWriteAction((key, body: CreateCustomerTagRequest) =>
    unwrap(createCustomerTag({ body, headers: headers(key) })),
  );
  const update = useWriteAction((key, b: { tagId: string; body: UpdateCustomerTagRequest }) =>
    unwrap(updateCustomerTag({ path: { tagId: b.tagId }, body: b.body, headers: headers(key) })),
  );
  const message = create.message ?? update.message;
  const save = async () => {
    const n = name.trim();
    if (n === "") return;
    const r =
      tag === null
        ? await create.run({ name: n, color })
        : await update.run({ tagId: tag.id, body: { version: tag.version, name: n, color } });
    if (!r.ok) return;
    reloadCustomers(qc);
    onDone();
  };
  return (
    <div className="card">
      <h2>{tag === null ? "Tạo nhãn" : `Đổi tên, màu: ${tag.name}`}</h2>
      <div className="row" style={{ marginTop: 8 }}>
        <input
          className="input"
          aria-label="Tên nhãn"
          maxLength={MAX_TAG_NAME}
          value={name}
          onChange={(e) => {
            setName(e.target.value);
          }}
          style={{ flex: "1 1 180px" }}
        />
        <span className="select-wrap" style={{ flex: "0 1 160px" }}>
          <select
            aria-label="Màu"
            value={color}
            onChange={(e) => {
              setColor(e.target.value);
            }}
          >
            {TAG_COLORS.map((c) => (
              <option key={c.key} value={c.key}>
                {c.name}
              </option>
            ))}
          </select>
        </span>
        <span className={tagChip(color)}>{name.trim() === "" ? "Nhãn" : name.trim()}</span>
      </div>
      {message !== null && (
        <div className="box box-err" role="alert" style={{ marginTop: 8 }}>
          {message}
        </div>
      )}
      <div className="savebar" style={{ marginTop: 12 }}>
        <button type="button" className="btn btn-line" onClick={onDone}>
          Hủy
        </button>
        <button
          type="button"
          className="btn btn-dark"
          disabled={name.trim() === "" || create.pending || update.pending}
          onClick={() => {
            void save();
          }}
        >
          Lưu nhãn
        </button>
      </div>
    </div>
  );
}

export interface TagRuleEditorProps {
  tag: CustomerTag;
  /** Quy tắc hiện có; null khi nhãn chưa có quy tắc. */
  rule: CustomerTagRule | null;
  onDone: () => void;
}

/** Sửa quy tắc của một nhãn (mockup trạng thái E, thẻ "Quy tắc: …"). */
export function TagRuleEditor({ tag, rule, onDone }: TagRuleEditorProps) {
  const qc = useQueryClient();
  const [enabled, setEnabled] = useState(rule?.enabled ?? true);
  const [next, setNext] = useState(rule?.conditions.length ?? 1);
  const [rows, setRows] = useState<Row[]>(() =>
    rule === null
      ? [{ key: 0, field: "order_count", op: "gte", value: "" }]
      : rule.conditions.map((c, i) => ({ key: i, field: c.field, op: c.op, value: toDisplayValue(c.field, c.value) })),
  );
  const [invalid, setInvalid] = useState<string | null>(null);
  const put = useWriteAction((key, b: { tagId: string; body: PutCustomerTagRuleRequest }) =>
    unwrap(putCustomerTagRule({ path: { tagId: b.tagId }, body: b.body, headers: headers(key) })),
  );
  const set = (key: number, patch: Partial<Row>) => {
    setRows((rs) => rs.map((r) => (r.key === key ? { ...r, ...patch } : r)));
    setInvalid(null);
  };

  const save = async () => {
    const conditions = [];
    for (const [i, r] of rows.entries()) {
      const value = toServerValue(r.field, r.value);
      if (value === null) {
        setInvalid(`Điều kiện ${String(i + 1)}: ${valueHint(r.field)}`);
        return;
      }
      conditions.push({ field: r.field, op: r.op, value });
    }
    if (conditions.length === 0) {
      setInvalid("Thêm ít nhất một điều kiện");
      return;
    }
    const res = await put.run({
      tagId: tag.id,
      body: { ...(rule === null ? {} : { version: rule.version }), enabled, conditions },
    });
    if (!res.ok) return;
    qc.setQueryData(ruleKey(tag.id), res.data);
    reloadCustomers(qc);
    onDone();
  };
  const conflict = put.error instanceof ApiError && put.error.code === "version_conflict";

  return (
    <div className="card">
      <div className="row-between">
        <h2>{`Quy tắc: ${tag.name}`}</h2>
        <label className="check">
          <input
            type="checkbox"
            checked={enabled}
            onChange={(e) => {
              setEnabled(e.target.checked);
            }}
          />{" "}
          Đang bật
        </label>
      </div>
      <p className="small muted">
        Khách đạt <b>mọi</b> điều kiện thì tự gắn; không còn đạt thì tự gỡ. Tính lại khi đơn của khách đổi và mỗi đêm.
      </p>
      <div className="stack-sm" style={{ marginTop: 12 }}>
        {rows.map((r, i) => {
          const n = String(i + 1);
          return (
            <div className="row" key={r.key}>
              <span className="select-wrap" style={{ flex: "1 1 180px" }}>
                <select
                  aria-label={`Chỉ số ${n}`}
                  value={r.field}
                  onChange={(e) => {
                    if (isField(e.target.value)) set(r.key, { field: e.target.value });
                  }}
                >
                  {TAG_FIELDS.map((f) => (
                    <option key={f.field} value={f.field}>
                      {f.label}
                    </option>
                  ))}
                </select>
              </span>
              <span className="select-wrap" style={{ flex: "0 1 120px" }}>
                <select
                  aria-label={`Phép so ${n}`}
                  value={r.op}
                  onChange={(e) => {
                    if (isOp(e.target.value)) set(r.key, { op: e.target.value });
                  }}
                >
                  <option value="gte">{OP_TEXT.gte}</option>
                  <option value="lte">{OP_TEXT.lte}</option>
                </select>
              </span>
              <input
                className="input num"
                inputMode="decimal"
                aria-label={`Giá trị ${n}`}
                value={r.value}
                onChange={(e) => {
                  set(r.key, { value: e.target.value });
                }}
                style={{ flex: "0 1 140px" }}
              />
              <button
                type="button"
                className="ibtn"
                aria-label={`Bỏ điều kiện ${n}`}
                onClick={() => {
                  setRows((rs) => rs.filter((x) => x.key !== r.key));
                }}
              >
                ×
              </button>
            </div>
          );
        })}
        <button
          type="button"
          className="btn btn-quiet"
          style={{ justifySelf: "start" }}
          disabled={rows.length >= MAX_CONDITIONS}
          onClick={() => {
            setRows((rs) => [...rs, { key: next, field: "order_count", op: "gte", value: "" }]);
            setNext(next + 1);
          }}
        >
          + Thêm điều kiện (VÀ)
        </button>
      </div>
      {invalid !== null && <p className="field-err">{invalid}</p>}
      {put.message !== null && (
        <div className="box box-err" role="alert" style={{ marginTop: 8 }}>
          {conflict ? "Quy tắc vừa được người khác sửa, tải lại." : put.message}
          {conflict && (
            <div className="row" style={{ marginTop: 8 }}>
              <button
                type="button"
                className="btn btn-dark"
                onClick={() => {
                  void qc.invalidateQueries({ queryKey: ruleKey(tag.id) });
                  onDone();
                }}
              >
                Tải lại
              </button>
            </div>
          )}
        </div>
      )}
      <div className="savebar" style={{ marginTop: 16 }}>
        <button type="button" className="btn btn-line" onClick={onDone}>
          Hủy
        </button>
        <button
          type="button"
          className="btn btn-dark"
          disabled={put.pending}
          onClick={() => {
            void save();
          }}
        >
          Lưu quy tắc
        </button>
      </div>
    </div>
  );
}

type Editing = { kind: "tag"; tag: CustomerTag | null } | { kind: "rule"; tag: CustomerTag } | null;

/** Hộp quản lý nhãn khách mở từ nút "Nhãn" của danh sách khách. */
export function TagsDialog({ onClose }: { onClose: () => void }) {
  const tags = useTags();
  const items = tags.data?.items ?? [];
  const rules = useQueries({
    queries: items.map((t) => ({ queryKey: ruleKey(t.id), queryFn: () => readRule(t.id) })),
  });
  const [editing, setEditing] = useState<Editing>(null);
  const done = () => {
    setEditing(null);
  };
  const ruleOf = (i: number) => rules[i]?.data;

  return (
    <Dialog
      title="Nhãn khách"
      className="dlg-wide"
      onClose={onClose}
      footer={
        <button type="button" className="btn btn-dark" onClick={onClose}>
          Xong
        </button>
      }
    >
      <div className="stack">
        <div className="row-between">
          <p className="small muted">{`${String(items.length)} nhãn`}</p>
          <button
            type="button"
            className="btn btn-line"
            onClick={() => {
              setEditing({ kind: "tag", tag: null });
            }}
          >
            + Tạo nhãn
          </button>
        </div>
        {tags.isError && (
          <div className="box box-err" role="alert">
            {`Chưa đọc được danh sách nhãn. ${errorMessage(tags.error)}`}
          </div>
        )}
        {editing?.kind === "tag" && <TagForm key={editing.tag?.id ?? "new"} tag={editing.tag} onDone={done} />}
        {items.length > 0 && (
          <div className="olist">
            {items.map((t, i) => {
              const rule = ruleOf(i);
              return (
                <div className="oitem" key={t.id}>
                  <span className="grow">
                    <span className={tagChip(t.color)}>{t.name}</span>{" "}
                    {rules[i]?.isError === true && <span className="small err-text">chưa đọc được quy tắc</span>}
                    {rule !== undefined && (
                      <span className="small muted">
                        {rule === null ? "gắn tay" : rule.enabled ? "theo quy tắc" : "quy tắc đang tắt"}
                      </span>
                    )}
                  </span>
                  <button
                    type="button"
                    className="btn btn-quiet"
                    onClick={() => {
                      setEditing({ kind: "tag", tag: t });
                    }}
                  >
                    Đổi tên, màu
                  </button>
                  {rule !== undefined && (
                    <button
                      type="button"
                      className={rule === null ? "btn btn-quiet" : "btn btn-line"}
                      onClick={() => {
                        setEditing({ kind: "rule", tag: t });
                      }}
                    >
                      {rule === null ? "Thêm quy tắc" : "Sửa quy tắc"}
                    </button>
                  )}
                </div>
              );
            })}
          </div>
        )}
        <p className="hint">
          Nhãn có quy tắc vẫn gắn tay được; dòng gắn tay và dòng do quy tắc gắn tách nhau, quy tắc không gỡ nhãn gắn
          tay.
        </p>
        {editing?.kind === "rule" && (
          <TagRuleEditor
            key={editing.tag.id}
            tag={editing.tag}
            rule={ruleOf(items.findIndex((t) => t.id === editing.tag.id)) ?? null}
            onDone={done}
          />
        )}
      </div>
    </Dialog>
  );
}
