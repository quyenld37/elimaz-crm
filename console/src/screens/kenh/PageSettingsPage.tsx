// Cài đặt hộp chat (kế hoạch M4 Task 178; mockup docs/mockups/m4/cai-dat.html; HV33, HV52 sửa, HV53, HV54). Một
// trang có thẻ: mỗi tài khoản kênh một thẻ (/settings/pages/:pageId), "Nhãn" (/settings/tags), "Mẫu trả lời"
// (/settings/templates); /settings/inbox chuyển sang thẻ đầu. Thẻ Page: bốn quy tắc tự động (AutomationForms), xoay vòng,
// Live (mẫu xin địa chỉ, khung giờ → nguồn đơn; hạn đơn nháp ở Cài đặt đơn của M2, Q2), nguồn đơn và nâng cao (đọc
// comment định kỳ, tuổi tối đa của sự kiện). Ba khối sau cùng ghi một bản cài đặt Page (updatePageSettings): mỗi khối
// chỉ đổi trường của mình trên bản mới nhất; trường của khối đã bị người khác đổi so với lúc mở → "Có người vừa sửa".
import { useQueryClient } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";
import { Link, Navigate, useLocation, useParams } from "react-router";
import {
  updatePageSettings,
  type ChannelAccount,
  type CommentPollMode,
  type PageSettings,
  type ReplyTemplate,
  type TrafficSource,
  type UpdatePageSettingsRequest,
} from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { hasPermission } from "../../app/nav";
import { useOrderSettings } from "../don/orderData";
import { AutomationForms, TemplateOptions } from "./AutomationForms";
import {
  pageSettingsKey,
  useAssignableUsers,
  useAutomationRules,
  useChannels,
  useMe,
  usePageSettings,
  useTemplates,
  useTrafficSources,
} from "./kenhData";
import {
  LIVE_SLOTS_MAX,
  channelIcon,
  featuresOf,
  hasOnPage,
  intInRange,
  slotErrors,
  type SlotInput,
} from "./kenhRules";
import { Block, DirtyGuard, NumField, OnOff, Seg, useBlock } from "./settingsKit";
import "./kenh.css";

/** Mẫu dùng được trên Page: mẫu chung và mẫu của chính Page. */
export const templatesForPage = (all: readonly ReplyTemplate[], pageId: string) =>
  all.filter((t) => t.pageId === undefined || t.pageId === pageId);

// --- Khung trang có thẻ ---

function Tabs({ accounts }: { accounts: readonly ChannelAccount[] }) {
  const { pathname } = useLocation();
  const tabs = [
    ...accounts.map((a) => ({ to: `/settings/pages/${a.id}`, label: `${channelIcon(a.channel)} · ${a.name}` })),
    { to: "/settings/tags", label: "Nhãn" },
    { to: "/settings/templates", label: "Mẫu trả lời" },
  ];
  return (
    <nav className="tabs" aria-label="Phần cài đặt">
      {tabs.map((t) => (
        <Link key={t.to} to={t.to} className="tab" {...(t.to === pathname ? { "aria-current": "page" } : {})}>
          {t.label}
        </Link>
      ))}
    </nav>
  );
}

/** Khung chung của các thẻ cài đặt: tên trang, thẻ, bảo vệ rời trang khi còn khối chưa lưu. */
export function SettingsShell({ children }: { children: ReactNode }) {
  const channels = useChannels();
  return (
    <DirtyGuard>
      <div className="chat-set">
        <div className="page-h">
          <div>
            <h1>Cài đặt hộp chat</h1>
            <p className="sub">
              Cài đặt theo từng tài khoản kênh (Page) và cài đặt chung cho cả shop (nhãn, mẫu trả lời). Mỗi khối lưu
              riêng.
            </p>
          </div>
        </div>
        {channels.isError && <div className="box box-err">{errorMessage(channels.error)}</div>}
        <Tabs accounts={channels.data?.items ?? []} />
        <div style={{ marginTop: 16 }}>{children}</div>
      </div>
    </DirtyGuard>
  );
}

/** /settings/inbox: thẻ Page đầu tiên, không có Page thì thẻ Nhãn. */
export function InboxSettingsHome() {
  const channels = useChannels();
  if (channels.isPending) return <p className="muted">Đang tải…</p>;
  const first = channels.data?.items[0];
  return <Navigate to={first === undefined ? "/settings/tags" : `/settings/pages/${first.id}`} replace />;
}

// --- Ba khối của bản cài đặt Page ---

interface PageBlockProps {
  account: ChannelAccount;
  /** Bản mới nhất trên máy chủ (đổi theo báo hiệu). */
  latest: PageSettings;
  canEdit: boolean;
  templates: readonly ReplyTemplate[];
  reload: () => Promise<PageSettings | undefined>;
}

const toRequest = (s: PageSettings): UpdatePageSettingsRequest => ({
  roundRobinEnabled: s.roundRobinEnabled,
  roundRobinUserIds: s.roundRobinUserIds,
  askAddressTemplateId: s.askAddressTemplateId ?? null,
  inboxTrafficSourceId: s.inboxTrafficSourceId ?? null,
  commentTrafficSourceId: s.commentTrafficSourceId ?? null,
  liveSlots: s.liveSlots,
  commentPollMode: s.commentPollMode,
  automationMaxAgeMinutes: s.automationMaxAgeMinutes,
  expectedVersion: s.version,
});

/**
 * Lưu một khối của bản cài đặt Page. pick lấy các trường của khối: trường đó trên bản mới nhất khác bản lúc mở khối →
 * không gửi, báo "Có người vừa sửa" (khối khác của chính trang lưu trước không làm khối này xung đột).
 */
function usePageSave<T>(p: PageBlockProps, pick: (s: PageSettings) => T) {
  const qc = useQueryClient();
  const b = useBlock(p.latest, pick);
  const [staleLocal, setStaleLocal] = useState(false);
  const write = useWriteAction((key, body: UpdatePageSettingsRequest & { pageId: string }) => {
    const { pageId, ...rest } = body;
    return unwrap(updatePageSettings({ path: { id: pageId }, body: rest, headers: { "Idempotency-Key": key } }));
  });
  const save = async (fields: Partial<UpdatePageSettingsRequest>) => {
    if (JSON.stringify(pick(p.latest)) !== JSON.stringify(pick(b.base))) {
      setStaleLocal(true);
      return;
    }
    const r = await write.run({
      ...toRequest(p.latest),
      ...fields,
      expectedVersion: p.latest.version,
      pageId: p.account.id,
    });
    if (!r.ok) return;
    qc.setQueryData(pageSettingsKey(p.account.id), r.data);
    b.reset(r.data);
  };
  const reload = async () => {
    const fresh = await p.reload();
    if (fresh !== undefined) b.reset(fresh);
    setStaleLocal(false);
    write.reset();
  };
  const blockProps = {
    dirty: b.dirty,
    canEdit: p.canEdit,
    pending: write.pending,
    error: write.error,
    message: write.message,
    staleLocal,
    onReload: () => void reload(),
  };
  return { b, save, blockProps };
}

function RoundRobinForm(p: PageBlockProps) {
  const users = useAssignableUsers(p.account.id);
  const { b, save, blockProps } = usePageSave(p, (s) => ({
    enabled: s.roundRobinEnabled,
    userIds: s.roundRobinUserIds,
  }));
  const v = b.inputs;
  const list = users.data?.items ?? [];
  const gone = v.userIds.filter((id) => !list.some((u) => u.id === id));
  const toggleUser = (id: string, on: boolean) => {
    b.setInputs((x) => ({ ...x, userIds: on ? [...x.userIds, id] : x.userIds.filter((u) => u !== id) }));
  };
  return (
    <Block
      title="Xoay vòng phân công"
      aside={
        <OnOff
          label="Bật xoay vòng phân công"
          on={v.enabled}
          disabled={!p.canEdit}
          onChange={(on) => {
            b.setInputs((x) => ({ ...x, enabled: on }));
          }}
        />
      }
      {...blockProps}
      onSave={() => {
        void save({ roundRobinEnabled: v.enabled, roundRobinUserIds: v.userIds });
      }}
    >
      {users.isError && <div className="box box-err small">{errorMessage(users.error)}</div>}
      <div className="radios">
        {list.map((u) => (
          <label key={u.id} className="check">
            <input
              type="checkbox"
              checked={v.userIds.includes(u.id)}
              disabled={!p.canEdit}
              onChange={(e) => {
                toggleUser(u.id, e.target.checked);
              }}
            />{" "}
            {u.name}
          </label>
        ))}
        {list.length === 0 && users.data !== undefined && (
          <p className="small muted">Chưa có nhân viên nào được xem hội thoại của Page này.</p>
        )}
      </div>
      {gone.length > 0 && (
        <p className="small muted">{String(gone.length)} người đã chọn không còn quyền trên Page, CRM tự bỏ qua.</p>
      )}
      <p className="hint">
        Mặc định tắt. Bật thì hội thoại mới lần lượt giao cho người được chọn; người bị khóa hoặc mất quyền trên Page tự
        bỏ qua. Hội thoại cũ không đổi người.
      </p>
    </Block>
  );
}

/** Ô chọn nguồn đơn: nguồn đang bật, cộng nguồn đang lưu dù đã tắt (ghi rõ). */
function SourceSelect(p: {
  label: string;
  value: string;
  sources: readonly TrafficSource[];
  disabled: boolean;
  allowEmpty: boolean;
  onChange: (v: string) => void;
}) {
  const shown = p.sources.filter((s) => s.active || s.id === p.value);
  return (
    <span className="select-wrap">
      <select
        aria-label={p.label}
        value={p.value}
        disabled={p.disabled}
        onChange={(e) => {
          p.onChange(e.target.value);
        }}
      >
        {p.allowEmpty && <option value="">Chưa chọn</option>}
        {!p.allowEmpty && p.value === "" && <option value="">Chọn nguồn đơn</option>}
        {shown.map((s) => (
          <option key={s.id} value={s.id}>
            {s.active ? s.name : `${s.name} (đã tắt)`}
          </option>
        ))}
      </select>
    </span>
  );
}

function DraftTtl() {
  const s = useOrderSettings();
  if (s.data === undefined) return null;
  const h = s.data.draftTtlMinutes / 60;
  const ttl = Number.isInteger(h) ? `${String(h)} giờ` : `${String(s.data.draftTtlMinutes)} phút`;
  return (
    <dl className="kv">
      <dt>Hạn đơn nháp</dt>
      <dd>{ttl} · đặt ở Cài đặt đơn, dùng chung mọi đơn nháp</dd>
    </dl>
  );
}

function LiveForm(p: PageBlockProps & { sources: readonly TrafficSource[]; canReadOrders: boolean }) {
  const { b, save, blockProps } = usePageSave(p, (s) => ({
    ask: s.askAddressTemplateId ?? "",
    slots: s.liveSlots.map((x): SlotInput => ({ ...x })),
  }));
  const [errs, setErrs] = useState<(string | undefined)[]>([]);
  const v = b.inputs;
  const setSlot = (i: number, f: Partial<SlotInput>) => {
    b.setInputs((x) => ({ ...x, slots: x.slots.map((s, k) => (k === i ? { ...s, ...f } : s)) }));
  };
  const full = v.slots.length >= LIVE_SLOTS_MAX;
  return (
    <Block
      title="Live"
      aside={<span className="xs muted">Tính năng: Live</span>}
      {...blockProps}
      extra={
        p.canReadOrders ? (
          <Link className="btn btn-text" to="/cai-dat/don">
            Mở Cài đặt đơn
          </Link>
        ) : null
      }
      onSave={() => {
        const e = slotErrors(v.slots);
        setErrs(e);
        if (e.some((x) => x !== undefined)) return;
        void save({ askAddressTemplateId: v.ask === "" ? null : v.ask, liveSlots: v.slots });
      }}
    >
      <label className="field">
        <span>Mẫu xin địa chỉ</span>
        <span className="select-wrap">
          <select
            value={v.ask}
            disabled={!p.canEdit}
            onChange={(e) => {
              const ask = e.target.value;
              b.setInputs((x) => ({ ...x, ask }));
            }}
          >
            <option value="">Chưa chọn</option>
            <TemplateOptions templates={p.templates} />
          </select>
        </span>
      </label>
      <div className="field">
        <span>Khung giờ → nguồn đơn</span>
        <div className="slots">
          {v.slots.map((s, i) => {
            const err = errs[i];
            return (
              <div key={i} className="stack-sm">
                <div className="slot">
                  <input
                    className={err === undefined ? "input num" : "input num is-err"}
                    aria-label={`Từ (khung ${String(i + 1)})`}
                    value={s.from}
                    disabled={!p.canEdit}
                    onChange={(e) => {
                      setSlot(i, { from: e.target.value });
                    }}
                  />
                  <input
                    className={err === undefined ? "input num" : "input num is-err"}
                    aria-label={`Đến (khung ${String(i + 1)})`}
                    value={s.to}
                    disabled={!p.canEdit}
                    onChange={(e) => {
                      setSlot(i, { to: e.target.value });
                    }}
                  />
                  <SourceSelect
                    label={`Nguồn đơn (khung ${String(i + 1)})`}
                    value={s.trafficSourceId}
                    sources={p.sources}
                    disabled={!p.canEdit || !p.canReadOrders}
                    allowEmpty={false}
                    onChange={(trafficSourceId) => {
                      setSlot(i, { trafficSourceId });
                    }}
                  />
                  <button
                    type="button"
                    className="ibtn"
                    aria-label={`Xóa khung ${s.from}`}
                    disabled={!p.canEdit}
                    onClick={() => {
                      b.setInputs((x) => ({ ...x, slots: x.slots.filter((_, k) => k !== i) }));
                      setErrs([]);
                    }}
                  >
                    ×
                  </button>
                </div>
                {err !== undefined && <span className="field-err">{err}</span>}
              </div>
            );
          })}
        </div>
        <div className="act">
          <button
            type="button"
            className="btn btn-quiet"
            disabled={!p.canEdit || full}
            onClick={() => {
              b.setInputs((x) => ({ ...x, slots: [...x.slots, { from: "", to: "", trafficSourceId: "" }] }));
            }}
          >
            + Thêm khung giờ
          </button>
          {full && <span className="reason">Tối đa {String(LIVE_SLOTS_MAX)} khung giờ.</span>}
        </div>
        {!p.canReadOrders && <span className="hint">Cần quyền xem đơn để chọn nguồn đơn.</span>}
      </div>
      {p.canReadOrders && <DraftTtl />}
    </Block>
  );
}

const POLL_MODES: readonly { value: CommentPollMode; label: string }[] = [
  { value: "auto", label: "Tự động" },
  { value: "on", label: "Luôn bật" },
  { value: "off", label: "Tắt" },
];

function SourcesForm(
  p: PageBlockProps & { sources: readonly TrafficSource[]; canReadOrders: boolean; comments: boolean },
) {
  const { b, save, blockProps } = usePageSave(p, (s) => ({
    inbox: s.inboxTrafficSourceId ?? "",
    comment: s.commentTrafficSourceId ?? "",
    poll: s.commentPollMode,
    maxAge: String(s.automationMaxAgeMinutes),
  }));
  const [ageErr, setAgeErr] = useState<string | undefined>();
  const v = b.inputs;
  return (
    <Block
      title="Nguồn đơn và nâng cao"
      {...blockProps}
      onSave={() => {
        const age = intInRange(v.maxAge, 5, 120);
        setAgeErr(age === null ? "Nhập số phút từ 5 đến 120." : undefined);
        if (age === null) return;
        void save({
          inboxTrafficSourceId: v.inbox === "" ? null : v.inbox,
          commentTrafficSourceId: v.comment === "" ? null : v.comment,
          commentPollMode: v.poll,
          automationMaxAgeMinutes: age,
        });
      }}
    >
      <div className="grid-f" style={{ gap: 8 }}>
        <label className="field">
          <span>Nguồn đơn cho tin nhắn</span>
          <SourceSelect
            label="Nguồn đơn cho tin nhắn"
            value={v.inbox}
            sources={p.sources}
            disabled={!p.canEdit || !p.canReadOrders}
            allowEmpty
            onChange={(inbox) => {
              b.setInputs((x) => ({ ...x, inbox }));
            }}
          />
        </label>
        {p.comments && (
          <label className="field">
            <span>Nguồn đơn cho comment</span>
            <SourceSelect
              label="Nguồn đơn cho comment"
              value={v.comment}
              sources={p.sources}
              disabled={!p.canEdit || !p.canReadOrders}
              allowEmpty
              onChange={(comment) => {
                b.setInputs((x) => ({ ...x, comment }));
              }}
            />
          </label>
        )}
      </div>
      {!p.canReadOrders && <span className="hint">Cần quyền xem đơn để chọn nguồn đơn.</span>}
      {p.comments && (
        <div className="field">
          <span>Đọc comment định kỳ</span>
          <Seg
            label="Đọc comment định kỳ"
            value={v.poll}
            options={POLL_MODES}
            disabled={!p.canEdit}
            onChange={(poll) => {
              b.setInputs((x) => ({ ...x, poll }));
            }}
          />
          <span className="hint">
            Tự động: khi 10 phút trong giờ bán không có comment mới qua webhook, CRM hỏi Facebook mỗi 30 giây.
          </span>
        </div>
      )}
      <NumField
        before="Bỏ qua tự động cho sự kiện cũ hơn"
        after="phút"
        label="Số phút"
        value={v.maxAge}
        error={ageErr}
        disabled={!p.canEdit}
        onChange={(maxAge) => {
          b.setInputs((x) => ({ ...x, maxAge }));
        }}
      />
      <p className="hint">Ví dụ sau sự cố CRM lấy bù comment cũ: không tự trả lời, không tự ẩn những comment đó.</p>
    </Block>
  );
}

// --- Thẻ của một Page ---

function PageTab({ account }: { account: ChannelAccount }) {
  const me = useMe();
  const grants = me.data?.grants ?? [];
  const canEdit = hasOnPage(grants, "conversation.settings", account.id);
  const canReadOrders = hasPermission(grants, "order.read");
  const settings = usePageSettings(account.id);
  const rules = useAutomationRules(account.id);
  const templates = useTemplates();
  const sources = useTrafficSources(canReadOrders);
  const f = featuresOf(account);
  const pageTemplates = templatesForPage(templates.data?.items ?? [], account.id);
  const srcs = sources.data?.items ?? [];

  const failed = [settings, rules, templates].find((q) => q.isError);
  if (failed !== undefined) return <div className="box box-err">{errorMessage(failed.error)}</div>;
  if (
    settings.data === undefined ||
    rules.data === undefined ||
    templates.data === undefined ||
    me.data === undefined
  ) {
    return <p className="muted">Đang tải…</p>;
  }
  const common = {
    account,
    latest: settings.data,
    canEdit,
    templates: pageTemplates,
    reload: async () => (await settings.refetch()).data,
  };
  return (
    <div className="stack-sm">
      {!canEdit && (
        <div className="box box-info small">
          <b>Bạn chỉ xem được cài đặt của Page này.</b> Sửa cần quyền "Cài đặt hộp chat" trên Page {account.name}.
        </div>
      )}
      {account.sendMode === "read_only" && account.status !== "disconnected" && (
        <div className="box box-soft small">
          <b>Page {account.name} đang ở chế độ chỉ xem.</b> CRM chưa gửi gì ra Facebook, kể cả tin tự động, cho tới khi
          tắt chỉ xem ở Kênh kết nối.
        </div>
      )}
      <div className="cfg">
        <AutomationForms
          pageId={account.id}
          rules={rules.data.items}
          canEdit={canEdit}
          features={f}
          templates={pageTemplates}
          reload={async () => (await rules.refetch()).data}
        />
        <RoundRobinForm {...common} />
        {f.liveComments && <LiveForm {...common} sources={srcs} canReadOrders={canReadOrders} />}
        <SourcesForm {...common} sources={srcs} canReadOrders={canReadOrders} comments={f.comments} />
      </div>
    </div>
  );
}

/** /settings/pages/:pageId */
export function PageSettingsPage() {
  const { pageId = "" } = useParams();
  const channels = useChannels();
  const account = channels.data?.items.find((a) => a.id === pageId);
  return (
    <SettingsShell>
      {channels.data !== undefined && account === undefined && (
        <p className="muted">Không tìm thấy Page này, hoặc bạn chưa được giao Page.</p>
      )}
      {account !== undefined && <PageTab key={account.id} account={account} />}
    </SettingsShell>
  );
}
