// Bốn quy tắc tự động của một Page (kế hoạch M4 Task 178; mockup cai-dat.html trạng thái A, B; CH11, CH12, CH16;
// HV33, HV52 sửa, HV53): Tự ẩn comment, Tự trả lời comment, Tin chào, Nhắn lại khách chưa trả lời. Mỗi khối lưu riêng
// (updateAutomationRule, kiểm version của quy tắc); giới hạn báo tại ô trước khi gửi (≤ 5 câu, ≤ 13 nút, nút ≤ 20 ký
// tự); khối mà kênh không hỗ trợ bị ẩn hẳn (features). Quy tắc chưa cấu hình có version 0 (Task 165 quyết định 5).
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import {
  updateAutomationRule,
  type AutoHideConfig,
  type AutoReplyCommentConfig,
  type AutomationRule,
  type AutomationRuleConfig,
  type AutomationRuleKind,
  type AutomationRuleList,
  type ChannelFeatures,
  type FollowUpConfig,
  type GreetingConfig,
  type QuickReply,
  type ReplyTemplate,
} from "../../api";
import { unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { rulesKey } from "./kenhData";
import {
  GREETING_MAX_CHARS,
  PRIVATE_TEXT_MAX_CHARS,
  QR_MAX,
  QR_TITLE_MAX,
  REPLIES_MAX,
  charCount,
  counter,
  intInRange,
  quickReplyError,
  replyLineError,
} from "./kenhRules";
import { Block, Cnt, NumField, OnOff, Seg, useBlock, type BlockState } from "./settingsKit";

/** uuid rỗng: "chưa chọn mẫu" của nhắn lại chưa cấu hình (Task 165 quyết định 5). */
export const NIL_UUID = "00000000-0000-0000-0000-000000000000";

export interface RuleFormProps {
  pageId: string;
  rule: AutomationRule;
  canEdit: boolean;
  features: ChannelFeatures;
  /** Mẫu trả lời dùng được trên Page (chung + của Page). */
  templates: readonly ReplyTemplate[];
  /** Tải lại danh sách quy tắc (sau 409), trả danh sách mới. */
  reload: () => Promise<AutomationRuleList | undefined>;
}

interface RuleBody {
  pageId: string;
  kind: AutomationRuleKind;
  enabled: boolean;
  config: AutomationRuleConfig;
  expectedVersion: number;
}

/** Đường lưu chung của một quy tắc: lưu xong đặt bản mới vào danh sách và làm bản gốc của khối. */
function useRuleSave<I>(p: RuleFormProps, b: BlockState<AutomationRule, I>) {
  const reset = b.reset;
  const qc = useQueryClient();
  const write = useWriteAction((key, b: RuleBody) =>
    unwrap(
      updateAutomationRule({
        path: { id: b.pageId, kind: b.kind },
        body: { enabled: b.enabled, config: b.config, expectedVersion: b.expectedVersion },
        headers: { "Idempotency-Key": key },
      }),
    ),
  );
  const save = async (base: AutomationRule, enabled: boolean, config: AutomationRuleConfig) => {
    const r = await write.run({ pageId: p.pageId, kind: base.kind, enabled, config, expectedVersion: base.version });
    if (!r.ok) return;
    qc.setQueryData(rulesKey(p.pageId), (old: AutomationRuleList | undefined) =>
      old === undefined ? old : { items: old.items.map((x) => (x.kind === r.data.kind ? r.data : x)) },
    );
    reset(r.data);
  };
  const reload = async () => {
    const list = await p.reload();
    const fresh = list?.items.find((x) => x.kind === p.rule.kind);
    if (fresh !== undefined) reset(fresh);
    write.reset();
  };
  const blockProps = {
    dirty: b.dirty,
    canEdit: p.canEdit,
    pending: write.pending,
    error: write.error,
    message: write.message,
    onReload: () => void reload(),
  };
  return { write, save, blockProps };
}

/** Công tắc Bật/Tắt ở đầu khối cho quy tắc có ô enabled. */
function enabledToggle<B, I extends { enabled: boolean }>(b: BlockState<B, I>, label: string, canEdit: boolean) {
  return (
    <OnOff
      label={label}
      on={b.inputs.enabled}
      disabled={!canEdit}
      onChange={(on) => {
        b.setInputs((x) => ({ ...x, enabled: on }));
      }}
    />
  );
}

const fieldCls = (err: string | undefined, base = "input") => (err === undefined ? base : `${base} is-err`);
const invalid = (err: string | undefined) => (err === undefined ? {} : { "aria-invalid": true });

// --- Tự ẩn comment (CH11) ---

const hideOf = (r: AutomationRule): AutoHideConfig =>
  r.config.kind === "auto_hide" ? r.config : { kind: "auto_hide", mode: "off", exceptPostIds: [] };
type HideMode = AutoHideConfig["mode"];
const HIDE_MODES: readonly { value: HideMode; label: string }[] = [
  { value: "off", label: "Tắt" },
  { value: "phone_only", label: "Chỉ comment có SĐT" },
  { value: "all", label: "Tất cả" },
];

export function AutoHideForm(p: RuleFormProps) {
  const b = useBlock(p.rule, (r) => ({ mode: r.enabled ? hideOf(r).mode : ("off" satisfies HideMode) }));
  const s = useRuleSave(p, b);
  const except = hideOf(b.base).exceptPostIds;
  return (
    <Block
      title="Tự ẩn comment"
      aside={<span className="xs muted">Tính năng: Ẩn comment</span>}
      {...s.blockProps}
      onSave={() => {
        void s.save(b.base, b.inputs.mode !== "off", { ...hideOf(b.base), mode: b.inputs.mode });
      }}
    >
      <Seg
        label="Chế độ tự ẩn"
        value={b.inputs.mode}
        options={HIDE_MODES}
        disabled={!p.canEdit}
        onChange={(mode) => {
          b.setInputs(() => ({ mode }));
        }}
      />
      <p className="hint">
        Comment bị ẩn trên Page (khách khác không thấy SĐT) nhưng vẫn hiện đủ trong Hộp chat. Không bao giờ ẩn comment
        của chính Page.
      </p>
      {except.length > 0 && <p className="small muted">Trừ {String(except.length)} bài đã chọn.</p>}
    </Block>
  );
}

// --- Tự trả lời comment (CH12) ---

const replyOf = (r: AutomationRule): AutoReplyCommentConfig =>
  r.config.kind === "auto_reply_comment"
    ? r.config
    : {
        kind: "auto_reply_comment",
        replies: [],
        postIds: [],
        exceptPostIds: [],
        includeLive: false,
        perUserPerPostHours: 24,
      };

/** Giá trị ô "Tin riêng kèm theo": none, text, hoặc "t:<mã mẫu>". */
const pmChoice = (c: AutoReplyCommentConfig): string => {
  if (c.privateMessage?.templateId !== undefined) return `t:${c.privateMessage.templateId}`;
  if (c.privateMessage?.text !== undefined) return "text";
  return "none";
};

interface ReplyInputs {
  enabled: boolean;
  replies: string[];
  pm: string;
  pmText: string;
  hours: string;
  includeLive: boolean;
}
type ReplyErrors = Partial<Record<"replies" | "pm" | "hours", string>> & { lines?: (string | null)[] };

export function AutoReplyForm(p: RuleFormProps) {
  const b = useBlock(p.rule, (r): ReplyInputs => {
    const c = replyOf(r);
    return {
      enabled: r.enabled,
      replies: [...c.replies],
      pm: pmChoice(c),
      pmText: c.privateMessage?.text ?? "",
      hours: String(c.perUserPerPostHours),
      includeLive: c.includeLive,
    };
  });
  const s = useRuleSave(p, b);
  const [errs, setErrs] = useState<ReplyErrors>({});
  const v = b.inputs;
  const set = (f: (i: ReplyInputs) => ReplyInputs) => {
    b.setInputs(f);
  };
  const c = replyOf(b.base);

  const onSave = () => {
    const e: ReplyErrors = {};
    const lines = v.replies.map(replyLineError);
    if (lines.some((x) => x !== null)) e.lines = lines;
    if (v.replies.length === 0) e.replies = "Cần ít nhất một câu trả lời.";
    const hours = intInRange(v.hours, 1, 72);
    if (hours === null) e.hours = "Nhập số giờ từ 1 đến 72.";
    let privateMessage: AutoReplyCommentConfig["privateMessage"];
    if (v.pm === "text") {
      const n = charCount(v.pmText.trim());
      if (n === 0 || n > PRIVATE_TEXT_MAX_CHARS)
        e.pm = `Nhập nội dung tin riêng (tối đa ${String(PRIVATE_TEXT_MAX_CHARS)} ký tự).`;
      privateMessage = { text: v.pmText.trim() };
    } else if (v.pm.startsWith("t:")) {
      privateMessage = { templateId: v.pm.slice(2) };
    }
    setErrs(e);
    if (Object.keys(e).length > 0 || hours === null) return;
    const config: AutoReplyCommentConfig = {
      ...c,
      replies: v.replies.map((x) => x.trim()),
      perUserPerPostHours: hours,
      includeLive: v.includeLive,
    };
    if (privateMessage === undefined) delete config.privateMessage;
    else config.privateMessage = privateMessage;
    void s.save(b.base, v.enabled, config);
  };

  const full = v.replies.length >= REPLIES_MAX;
  return (
    <Block
      title="Tự trả lời comment"
      aside={enabledToggle(b, "Bật tự trả lời comment", p.canEdit)}
      {...s.blockProps}
      onSave={onSave}
    >
      <div className="field">
        <span>
          Câu trả lời công khai (CRM chọn xoay vòng){" "}
          <Cnt text={`${String(v.replies.length)}/${String(REPLIES_MAX)} câu`} />
        </span>
        <div className="lines-f">
          {v.replies.map((line, i) => {
            const err = errs.lines?.[i] ?? undefined;
            return (
              // Dòng không có mã riêng; thứ tự là danh tính của câu trong cấu hình.
              <div className="line-f" key={i}>
                <div className="stack-sm">
                  <input
                    className={fieldCls(err)}
                    aria-label={`Câu ${String(i + 1)}`}
                    value={line}
                    disabled={!p.canEdit}
                    {...invalid(err)}
                    onChange={(e) => {
                      const val = e.target.value;
                      set((x) => ({ ...x, replies: x.replies.map((r, k) => (k === i ? val : r)) }));
                    }}
                  />
                  {err !== undefined && <span className="field-err">{err}</span>}
                </div>
                <button
                  type="button"
                  className="ibtn"
                  aria-label={`Xóa câu ${String(i + 1)}`}
                  disabled={!p.canEdit}
                  onClick={() => {
                    set((x) => ({ ...x, replies: x.replies.filter((_, k) => k !== i) }));
                    setErrs((x) => ({ ...x, lines: undefined }));
                  }}
                >
                  ×
                </button>
              </div>
            );
          })}
        </div>
        {errs.replies !== undefined && <span className="field-err">{errs.replies}</span>}
        <div className="act">
          <button
            type="button"
            className="btn btn-quiet"
            disabled={!p.canEdit || full}
            onClick={() => {
              set((x) => ({ ...x, replies: [...x.replies, ""] }));
            }}
          >
            + Thêm câu
          </button>
          {full && <span className="reason">Tối đa {String(REPLIES_MAX)} câu.</span>}
        </div>
      </div>
      <label className="field">
        <span>Tin riêng kèm theo</span>
        <span className="select-wrap">
          <select
            value={v.pm}
            disabled={!p.canEdit || !p.features.privateReply}
            onChange={(e) => {
              const pm = e.target.value;
              set((x) => ({ ...x, pm }));
            }}
          >
            {p.templates.map((t) => (
              <option key={t.id} value={`t:${t.id}`}>
                Mẫu "{t.title}"
              </option>
            ))}
            <option value="none">Không gửi tin riêng</option>
            <option value="text">Nhập nội dung riêng</option>
          </select>
        </span>
      </label>
      {v.pm === "text" && (
        <label className="field">
          <span>
            Nội dung tin riêng <Cnt {...overCnt(v.pmText, PRIVATE_TEXT_MAX_CHARS)} />
          </span>
          <textarea
            className={fieldCls(errs.pm)}
            value={v.pmText}
            disabled={!p.canEdit}
            {...invalid(errs.pm)}
            onChange={(e) => {
              const pmText = e.target.value;
              set((x) => ({ ...x, pmText }));
            }}
          />
          {errs.pm !== undefined && <span className="field-err">{errs.pm}</span>}
        </label>
      )}
      <p className="small muted">
        {c.postIds.length > 0 ? `Áp cho ${String(c.postIds.length)} bài đã chọn` : "Áp cho mọi bài"}
        {c.exceptPostIds.length > 0 ? `, trừ ${String(c.exceptPostIds.length)} bài` : ""}.
      </p>
      <label className="field">
        <span>Mỗi người mỗi bài chỉ một lần trong (giờ, 1–72)</span>
        <input
          className={fieldCls(errs.hours, "input num")}
          inputMode="numeric"
          style={{ maxWidth: 120 }}
          value={v.hours}
          disabled={!p.canEdit}
          {...invalid(errs.hours)}
          onChange={(e) => {
            const hours = e.target.value;
            set((x) => ({ ...x, hours }));
          }}
        />
        {errs.hours !== undefined && <span className="field-err">{errs.hours}</span>}
      </label>
      {p.features.liveComments && (
        <label className="check">
          <input
            type="checkbox"
            checked={v.includeLive}
            disabled={!p.canEdit}
            onChange={(e) => {
              const includeLive = e.target.checked;
              set((x) => ({ ...x, includeLive }));
            }}
          />{" "}
          Áp cho comment trong live
        </label>
      )}
      <p className="hint">
        Mỗi comment nhận tối đa một trả lời công khai và một tin riêng tự động, kể cả khi hệ thống xử lý lại. Nhân viên
        đã nhắn riêng comment đó thì CRM không gửi thêm. Live mặc định không áp (đã có khung chốt).
      </p>
    </Block>
  );
}

const overCnt = (s: string, max: number) => {
  const c = counter(s, max);
  return { text: c.text, over: c.over };
};

// --- Tin chào (CH12): ≤ 13 nút trả lời nhanh, mỗi nút ≤ 20 ký tự ---

const greetOf = (r: AutomationRule): GreetingConfig =>
  r.config.kind === "greeting" ? r.config : { kind: "greeting", text: "", quickReplies: [] };

export function GreetingForm(p: RuleFormProps) {
  const b = useBlock(p.rule, (r) => {
    const c = greetOf(r);
    return { enabled: r.enabled, text: c.text, qrs: c.quickReplies.map((q): QuickReply => ({ ...q })) };
  });
  const s = useRuleSave(p, b);
  const [newQr, setNewQr] = useState("");
  const [newQrTried, setNewQrTried] = useState(false);
  const [textErr, setTextErr] = useState<string | undefined>();
  const v = b.inputs;
  const full = v.qrs.length >= QR_MAX;
  const newErr = newQr === "" && !newQrTried ? null : quickReplyError(newQr);
  const newCnt = counter(newQr.trim(), QR_TITLE_MAX);

  const addQr = () => {
    setNewQrTried(true);
    if (full || quickReplyError(newQr) !== null) return;
    const title = newQr.trim();
    b.setInputs((x) => ({ ...x, qrs: [...x.qrs, { title, payload: title }] }));
    setNewQr("");
    setNewQrTried(false);
  };
  const onSave = () => {
    const n = charCount(v.text.trim());
    const err =
      n === 0
        ? "Nhập nội dung tin chào."
        : n > GREETING_MAX_CHARS
          ? `Tối đa ${String(GREETING_MAX_CHARS)} ký tự.`
          : undefined;
    setTextErr(err);
    if (err !== undefined || v.qrs.some((q) => quickReplyError(q.title) !== null)) return;
    void s.save(b.base, v.enabled, { kind: "greeting", text: v.text.trim(), quickReplies: v.qrs });
  };
  const cnt = counter(v.text, GREETING_MAX_CHARS);
  return (
    <Block title="Tin chào" aside={enabledToggle(b, "Bật tin chào", p.canEdit)} {...s.blockProps} onSave={onSave}>
      <label className="field">
        <span>
          Nội dung <Cnt text={cnt.text} over={cnt.over} />
        </span>
        <textarea
          className={fieldCls(textErr)}
          style={{ minHeight: 72 }}
          value={v.text}
          disabled={!p.canEdit}
          {...invalid(textErr)}
          onChange={(e) => {
            const text = e.target.value;
            b.setInputs((x) => ({ ...x, text }));
          }}
        />
        {textErr !== undefined && <span className="field-err">{textErr}</span>}
      </label>
      {p.features.quickReplies && (
        <div className="field">
          <span>
            Nút trả lời nhanh{" "}
            <Cnt
              text={`${String(v.qrs.length)}/${String(QR_MAX)} nút · mỗi nút tối đa ${String(QR_TITLE_MAX)} ký tự`}
              over={full}
            />
          </span>
          <div className="qrs">
            {v.qrs.map((q, i) => (
              <span key={i} className={quickReplyError(q.title) === null ? "qr" : "qr is-err"}>
                {q.title}
                <button
                  type="button"
                  className="ibtn"
                  aria-label={`Xóa nút ${q.title}`}
                  disabled={!p.canEdit}
                  onClick={() => {
                    b.setInputs((x) => ({ ...x, qrs: x.qrs.filter((_, k) => k !== i) }));
                  }}
                >
                  ×
                </button>
              </span>
            ))}
          </div>
          {p.canEdit && (
            <div className="line-f">
              <label className="field">
                <span>
                  Nút mới <Cnt text={newCnt.text} over={newCnt.over} />
                </span>
                <input
                  className={fieldCls(newErr ?? undefined)}
                  value={newQr}
                  disabled={full}
                  {...invalid(newErr ?? undefined)}
                  onChange={(e) => {
                    setNewQr(e.target.value);
                  }}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") {
                      e.preventDefault();
                      addQr();
                    }
                  }}
                />
                {newErr !== null && <span className="field-err">{newErr}</span>}
              </label>
            </div>
          )}
          <div className="act">
            <button type="button" className="btn btn-quiet" disabled={!p.canEdit || full} onClick={addQr}>
              + Thêm nút
            </button>
            {full && <span className="reason">Đã đủ {String(QR_MAX)} nút.</span>}
          </div>
        </div>
      )}
      <p className="hint">
        Chỉ gửi một lần cho khách nhắn Page lần đầu. Khách bấm nút thì vào Hộp chat như tin thường.
      </p>
    </Block>
  );
}

// --- Nhắn lại khách chưa trả lời (CH16) ---

const followOf = (r: AutomationRule): FollowUpConfig =>
  r.config.kind === "follow_up" ? r.config : { kind: "follow_up", afterHours: 4, templateId: NIL_UUID };

export function FollowUpForm(p: RuleFormProps) {
  const b = useBlock(p.rule, (r) => {
    const c = followOf(r);
    return {
      enabled: r.enabled,
      hours: String(c.afterHours),
      templateId: c.templateId === NIL_UUID ? "" : c.templateId,
    };
  });
  const s = useRuleSave(p, b);
  const [errs, setErrs] = useState<{ hours?: string; template?: string }>({});
  const v = b.inputs;
  const onSave = () => {
    const hours = intInRange(v.hours, 1, 23);
    const e: { hours?: string; template?: string } = {};
    if (hours === null) e.hours = "Nhập số giờ từ 1 đến 23.";
    if (v.enabled && v.templateId === "") e.template = "Chọn mẫu để bật nhắn lại.";
    setErrs(e);
    if (hours === null || e.template !== undefined) return;
    const templateId = v.templateId === "" ? followOf(b.base).templateId : v.templateId;
    void s.save(b.base, v.enabled, { kind: "follow_up", afterHours: hours, templateId });
  };
  return (
    <Block
      title="Nhắn lại khách chưa trả lời"
      aside={enabledToggle(b, "Bật nhắn lại khách chưa trả lời", p.canEdit)}
      {...s.blockProps}
      onSave={onSave}
    >
      <NumField
        before="Tự gửi sau"
        after="giờ khách im lặng (1–23)"
        label="Số giờ khách im lặng"
        value={v.hours}
        error={errs.hours}
        disabled={!p.canEdit}
        onChange={(hours) => {
          b.setInputs((x) => ({ ...x, hours }));
        }}
      />
      <label className="field">
        <span>Mẫu</span>
        <span className="select-wrap">
          <select
            className={errs.template === undefined ? undefined : "is-err"}
            aria-label="Mẫu nhắn lại"
            value={v.templateId}
            disabled={!p.canEdit}
            {...invalid(errs.template)}
            onChange={(e) => {
              const templateId = e.target.value;
              b.setInputs((x) => ({ ...x, templateId }));
            }}
          >
            <option value="">Chọn mẫu</option>
            <TemplateOptions templates={p.templates} />
          </select>
        </span>
        {errs.template !== undefined && <span className="field-err">{errs.template}</span>}
      </label>
      <p className="hint">
        Mặc định tắt. Bật thì mỗi tin của nhân viên được nhắn lại tự động tối đa một lần, chỉ khi còn trong 24 giờ;
        không gửi cho khách không liên hệ hay đã chặn Page.
      </p>
    </Block>
  );
}

/** Các mẫu trả lời làm lựa chọn của một ô chọn mẫu (giá trị là mã mẫu). */
export function TemplateOptions({ templates }: { templates: readonly ReplyTemplate[] }) {
  return templates.map((t) => (
    <option key={t.id} value={t.id}>
      Mẫu "{t.title}"
    </option>
  ));
}

/** Bốn khối theo tính năng của kênh (HV33: không hỗ trợ thì ẩn hẳn). */
export function AutomationForms(p: Omit<RuleFormProps, "rule"> & { rules: readonly AutomationRule[] }) {
  const rule = (kind: AutomationRuleKind) => p.rules.find((r) => r.kind === kind);
  const hide = rule("auto_hide");
  const reply = rule("auto_reply_comment");
  const greet = rule("greeting");
  const follow = rule("follow_up");
  return (
    <>
      {p.features.hideComment && hide !== undefined && <AutoHideForm {...p} rule={hide} />}
      {p.features.comments && reply !== undefined && <AutoReplyForm {...p} rule={reply} />}
      {greet !== undefined && <GreetingForm {...p} rule={greet} />}
      {follow !== undefined && <FollowUpForm {...p} rule={follow} />}
    </>
  );
}
