// Khối Ảnh của màn Sửa mẫu (kế hoạch M1 Task 325; mockup docs/mockups/m1/san-pham-sua.html, trạng thái A, D, E).
//
// Lưới ảnh theo thứ tự (ảnh đầu là ảnh bìa), ảnh mất là ô trống có ghi chú và nút "Tải lại"; sắp bằng kéo thả hoặc nút
// ← → (luôn hiện, dùng được trên điện thoại); xóa có thông báo Hoàn tác 10 giây (khôi phục được 30 ngày); đổi màu của
// ảnh (setProductImageColor); "Ảnh gốc" tải đường dẫn ký sẵn 5 phút. Ảnh tải lên (useUploads, ở trang sửa mẫu) xử lý
// xong thì khối này gắn vào cuối lưới.
//
// Mọi thao tác ghi đi lần lượt qua một hàng đợi và gửi version mới nhất của bản nháp (docs/tasks/task-325.md, Soát
// thiết kế 6.1). Câu trả lời là mẫu sau khi đổi: version = version gửi + 1 → bản nháp nhận (accepted); lớn hơn nữa là
// người khác vừa sửa → bản nháp giữ version cũ, trang báo "Mẫu đã thay đổi, tải lại."
import { useCallback, useEffect, useRef, useState, type DragEvent } from "react";
import {
  attachProductImage,
  deleteProductImage,
  getOriginalImageUrl,
  reorderProductImages,
  restoreProductImage,
  setProductImageColor,
  type AttachProductImageRequest,
  type Product,
  type ProductCover,
  type ProductImage,
  type ReorderProductImagesRequest,
  type SetProductImageColorRequest,
} from "../../api";
import { ApiError, errorMessage, unwrap } from "../../app/errors";
import { useWriteAction, type WriteResult } from "../../app/idempotency";
import { colorChoices, IMAGE_ACCEPT, moveTo, replaceOrder } from "./imageRules";
import { coverPicture } from "./productRules";
import type { Replace, UploadItem, Uploads } from "./useUpload";

export const TEXT_IMAGE_CONFLICT = "Mẫu đã thay đổi, tải lại.";
const TEXT_ALREADY_ATTACHED = "Ảnh này đã có trong mẫu. Tải lại trang để thấy.";
const TEXT_PROCESSING = "Đang xử lý ảnh, thường dưới 30 giây. Xong thì ảnh tự vào cuối lưới.";
const UNDO_SECONDS = 10;

export interface ImageManagerProps {
  productId: string;
  /** Ảnh đang hiện theo thứ tự (Product.images). */
  images: readonly ProductImage[];
  /** Version của bản nháp: mọi thao tác ghi gửi version này (hoặc version vừa nhận từ thao tác trước). */
  version: number;
  /** Màu của các biến thể đã lưu (API chỉ nhận màu này cho ảnh). */
  colors: readonly string[];
  /** catalog.manage: không có thì chỉ xem. */
  canManage: boolean;
  /** files.upload: không có thì không có vùng tải lên, nút Tải lại. */
  canUpload: boolean;
  /** Đang lưu mẫu: chưa gắn ảnh vừa xử lý xong (tránh lệch version). */
  busy: boolean;
  uploads: Uploads;
  /** Mẫu sau một thao tác ảnh; accepted: version chỉ tăng bởi thao tác này. */
  onProduct: (p: Product, accepted: boolean) => void;
  /** Có thao tác ảnh đang chạy (trang khóa nút Lưu mẫu). */
  onPending: (pending: boolean) => void;
  /** API trả version_conflict (người khác vừa sửa mẫu). Không có thì khối tự hiện câu báo. */
  onConflict?: () => void;
}

interface Toast {
  imageId: string;
  n: number;
  left: number;
}

const coverOf = (img: ProductImage): ProductCover =>
  img.variants === undefined
    ? { imageId: img.id, status: img.status }
    : { imageId: img.id, status: img.status, variants: img.variants };

export function ImageManager(props: ImageManagerProps) {
  const { productId, images, version, colors, canManage, canUpload, busy, uploads, onPending } = props;
  const [message, setMessage] = useState<string | null>(null);
  const [toast, setToast] = useState<Toast | null>(null);
  const [pending, setPending] = useState(0);
  const [dragging, setDragging] = useState<string | null>(null);
  const ver = useRef(version);
  const imgs = useRef(images);
  const propsRef = useRef(props);
  const chain = useRef<Promise<void>>(Promise.resolve());
  const started = useRef(new Set<string>());
  const pickAll = useRef<HTMLInputElement>(null);
  const pickOne = useRef<HTMLInputElement>(null);
  const [replaceTarget, setReplaceTarget] = useState<Replace | null>(null);

  useEffect(() => {
    ver.current = version;
  }, [version]);
  useEffect(() => {
    imgs.current = images;
  }, [images]);
  useEffect(() => {
    propsRef.current = props;
  });
  useEffect(() => {
    onPending(pending > 0);
  }, [pending, onPending]);
  // Thông báo Hoàn tác đếm lùi từng giây rồi tự tắt.
  const toastOn = toast !== null;
  useEffect(() => {
    if (!toastOn) return;
    const t = setInterval(() => {
      setToast((x) => (x === null || x.left <= 1 ? null : { ...x, left: x.left - 1 }));
    }, 1000);
    return () => {
      clearInterval(t);
    };
  }, [toastOn]);

  const h = (key: string) => ({ "Idempotency-Key": key });
  const attachW = useWriteAction((key, b: { id: string; body: AttachProductImageRequest }) =>
    unwrap(attachProductImage({ path: { id: b.id }, body: b.body, headers: h(key) })),
  );
  const reorderW = useWriteAction((key, b: { id: string; body: ReorderProductImagesRequest }) =>
    unwrap(reorderProductImages({ path: { id: b.id }, body: b.body, headers: h(key) })),
  );
  const deleteW = useWriteAction((key, b: { id: string; imageId: string }) =>
    unwrap(deleteProductImage({ path: { id: b.id, imageId: b.imageId }, headers: h(key) })),
  );
  const restoreW = useWriteAction((key, b: { id: string; imageId: string }) =>
    unwrap(restoreProductImage({ path: { id: b.id, imageId: b.imageId }, headers: h(key) })),
  );
  const colorW = useWriteAction((key, b: { id: string; imageId: string; body: SetProductImageColorRequest }) =>
    unwrap(setProductImageColor({ path: { id: b.id, imageId: b.imageId }, body: b.body, headers: h(key) })),
  );

  /**
   * Thao tác do người bấm (dời, xóa, hoàn tác, đổi màu): bỏ qua khi đang lưu mẫu, vì các bước lưu và thao tác ảnh cùng
   * khóa theo version mẫu (chạy song song thì bên sau nhận version_conflict oan). Nút đã bị khóa trong fieldset lúc
   * lưu; chặn ở đây cho cả kéo thả.
   */
  const userOp = (fn: () => Promise<void>) => {
    if (propsRef.current.busy) return;
    setMessage(null);
    enqueue(fn);
  };

  /** Xếp thao tác vào hàng đợi: chạy sau thao tác trước, không bao giờ làm hỏng hàng đợi. */
  const enqueue = useCallback((fn: () => Promise<void>) => {
    setPending((c) => c + 1);
    chain.current = chain.current
      .then(fn)
      .catch(() => undefined)
      .finally(() => {
        setPending((c) => c - 1);
      });
  }, []);

  const conflict = useCallback(() => {
    const on = propsRef.current.onConflict;
    if (on === undefined) setMessage(TEXT_IMAGE_CONFLICT);
    else on();
  }, []);

  /** Nhận kết quả một thao tác: mẫu mới (và version nếu chỉ thao tác này đổi), hoặc báo lỗi. */
  const done = useCallback(
    (r: WriteResult<Product>, sent: number): Product | null => {
      if (!r.ok) {
        if (r.error instanceof ApiError && r.error.code === "version_conflict") conflict();
        else setMessage(errorMessage(r.error));
        return null;
      }
      const p = r.data;
      const accepted = p.version === sent + 1 || p.version === sent;
      imgs.current = p.images;
      if (accepted) ver.current = p.version;
      propsRef.current.onProduct(p, accepted);
      return p;
    },
    [conflict],
  );

  const reorder = (imageId: string, to: number) => {
    userOp(async () => {
      const ids = imgs.current.map((i) => i.id);
      const from = ids.indexOf(imageId);
      if (from < 0 || from === to) return;
      const sent = ver.current;
      done(
        await reorderW.run({ id: productId, body: { productVersion: sent, imageIds: moveTo(ids, from, to) } }),
        sent,
      );
    });
  };

  const remove = (img: ProductImage, n: number) => {
    userOp(async () => {
      const sent = ver.current;
      if (done(await deleteW.run({ id: productId, imageId: img.id }), sent) !== null)
        setToast({ imageId: img.id, n, left: UNDO_SECONDS });
    });
  };

  const undo = (t: Toast) => {
    if (busy) return;
    setToast(null);
    userOp(async () => {
      const sent = ver.current;
      done(await restoreW.run({ id: productId, imageId: t.imageId }), sent);
    });
  };

  const setColor = (img: ProductImage, value: string) => {
    userOp(async () => {
      const sent = ver.current;
      const body: SetProductImageColorRequest =
        value === "" ? { productVersion: sent } : { productVersion: sent, color: value };
      done(await colorW.run({ id: productId, imageId: img.id, body }), sent);
    });
  };

  const original = async (img: ProductImage) => {
    setMessage(null);
    try {
      const s = await unwrap(getOriginalImageUrl({ path: { id: productId, imageId: img.id } }));
      // Thẻ <a> thay vì mở tab: trình duyệt chặn cửa sổ mở sau await; máy chủ đặt tên tải về (DAM0123-1.jpg).
      const a = document.createElement("a");
      a.href = s.url;
      a.rel = "noopener";
      a.download = "";
      document.body.append(a);
      a.click();
      a.remove();
    } catch (e) {
      setMessage(errorMessage(e));
    }
  };

  /** Gắn tệp đã xử lý xong; ảnh thay ảnh mất thì dời vào chỗ ảnh mất rồi xóa ảnh mất. */
  const attachOne = useCallback(
    async (item: UploadItem, fileId: string) => {
      const up = propsRef.current.uploads;
      const rep = item.replaces;
      const color = rep?.color !== undefined && propsRef.current.colors.includes(rep.color) ? { color: rep.color } : {};
      const sent = ver.current;
      const r = await attachW.run({ id: productId, body: { productVersion: sent, fileId, ...color } });
      if (!r.ok) {
        const code = r.error instanceof ApiError ? r.error.code : "";
        if (code === "already_exists") {
          // Tệp đã gắn (lần gắn trước xong mà mất câu trả lời): không gắn thêm, nhắc tải lại để thấy ảnh.
          up.dismiss(item.key);
          setMessage(TEXT_ALREADY_ATTACHED);
          return;
        }
        if (code === "version_conflict") propsRef.current.onConflict?.();
        up.update(item.key, {
          phase: "failed",
          message: code === "version_conflict" ? TEXT_IMAGE_CONFLICT : errorMessage(r.error),
          retryAttach: true,
        });
        return;
      }
      const p = done(r, sent);
      up.dismiss(item.key);
      if (p === null || rep === undefined) return;
      const added = p.images.find((i) => i.fileId === fileId);
      if (added === undefined || !p.images.some((i) => i.id === rep.imageId)) return;
      const s2 = ver.current;
      const o = await reorderW.run({
        id: productId,
        body: {
          productVersion: s2,
          imageIds: replaceOrder(
            p.images.map((i) => i.id),
            rep.imageId,
            added.id,
          ),
        },
      });
      if (done(o, s2) === null) return;
      const s3 = ver.current;
      done(await deleteW.run({ id: productId, imageId: rep.imageId }), s3);
    },
    [attachW, reorderW, deleteW, done, productId],
  );

  // Ảnh vừa xử lý xong → gắn (lần lượt trong hàng đợi). Đang lưu mẫu thì chờ: lưu xong trang dựng lại khối này.
  useEffect(() => {
    if (busy || !canManage) return;
    for (const it of uploads.items) {
      const fileId = it.fileId;
      if (it.phase !== "ready" || fileId === undefined || started.current.has(it.key)) continue;
      started.current.add(it.key);
      uploads.update(it.key, { phase: "attaching" });
      enqueue(() => attachOne(it, fileId));
    }
  }, [uploads, busy, canManage, enqueue, attachOne]);

  const retryAttach = (key: string) => {
    started.current.delete(key);
    uploads.update(key, { phase: "ready" });
  };

  const addFiles = (list: FileList | null, rep?: Replace) => {
    const files = list === null ? [] : Array.from(list);
    if (files.length === 0) return;
    if (rep === undefined) uploads.add(files);
    else uploads.add(files, rep);
  };

  const onDropFiles = (e: DragEvent) => {
    e.preventDefault();
    addFiles(e.dataTransfer.files);
  };

  const onDropTile = (e: DragEvent, to: number) => {
    if (dragging === null) return;
    e.preventDefault();
    reorder(dragging, to);
    setDragging(null);
  };

  const n = images.length;
  return (
    <div className="card p-images">
      <div className="row-between">
        <h2>Ảnh</h2>
        <span className="small muted">{n} ảnh</span>
      </div>
      {canManage && canUpload && (
        <div
          className="drop"
          data-testid="img-drop"
          onDragOver={(e) => {
            if (dragging === null) e.preventDefault();
          }}
          onDrop={onDropFiles}
        >
          <p>
            <b>Kéo ảnh vào đây</b> hoặc
          </p>
          <button type="button" className="btn btn-line" onClick={() => pickAll.current?.click()}>
            Chọn ảnh từ máy
          </button>
          <input
            ref={pickAll}
            type="file"
            multiple
            accept={IMAGE_ACCEPT}
            hidden
            aria-label="Chọn ảnh từ máy"
            onChange={(e) => {
              addFiles(e.target.files);
              e.target.value = "";
            }}
          />
          <input
            ref={pickOne}
            type="file"
            accept={IMAGE_ACCEPT}
            hidden
            aria-label="Chọn ảnh thay ảnh bị mất"
            onChange={(e) => {
              if (replaceTarget !== null) addFiles(e.target.files, replaceTarget);
              setReplaceTarget(null);
              e.target.value = "";
            }}
          />
          <p className="hint">JPEG, PNG, WebP · tối đa 20 MB và 40 triệu điểm ảnh mỗi ảnh · chọn nhiều ảnh một lần</p>
        </div>
      )}
      {message !== null && (
        <div className="box box-err p-img-msg" role="alert">
          {message}
        </div>
      )}
      {uploads.items.length > 0 && (
        <div className="stack-sm p-img-ups">
          <p className="label">Đang tải lên (chưa thuộc mẫu)</p>
          {uploads.items.map((it) => (
            <UploadRow
              key={it.key}
              item={it}
              onDismiss={() => {
                uploads.dismiss(it.key);
              }}
              onRetry={() => {
                retryAttach(it.key);
              }}
            />
          ))}
        </div>
      )}
      {n > 0 && (
        <div className="imgs p-imgs">
          {images.map((img, i) => (
            <div
              key={img.id}
              className={`img-tile${dragging === img.id ? " is-drag" : ""}`}
              data-testid="img-tile"
              draggable={canManage && !busy}
              onDragStart={(e) => {
                setDragging(img.id);
                e.dataTransfer.effectAllowed = "move";
              }}
              onDragEnd={() => {
                setDragging(null);
              }}
              onDragOver={(e) => {
                if (dragging !== null) e.preventDefault();
              }}
              onDrop={(e) => {
                onDropTile(e, i);
              }}
            >
              <Tile img={img} n={i + 1} />
              <div className="img-b">
                <span className="select-wrap">
                  <select
                    aria-label={`Màu của ảnh ${String(i + 1)}`}
                    value={img.color ?? ""}
                    disabled={!canManage}
                    onChange={(e) => {
                      setColor(img, e.target.value);
                    }}
                  >
                    {colorChoices(img.color, colors).map((c) => (
                      <option key={c.value} value={c.value}>
                        {c.label}
                      </option>
                    ))}
                  </select>
                </span>
                {canManage && (
                  <div className="img-act">
                    {img.status === "lost" ? (
                      canUpload && (
                        <button
                          type="button"
                          className="btn btn-line grow"
                          onClick={() => {
                            setReplaceTarget(
                              img.color === undefined ? { imageId: img.id } : { imageId: img.id, color: img.color },
                            );
                            pickOne.current?.click();
                          }}
                        >
                          Tải lại
                        </button>
                      )
                    ) : (
                      <>
                        <button
                          type="button"
                          className="ibtn"
                          aria-label={`Dời ảnh ${String(i + 1)} sang trái`}
                          disabled={i === 0}
                          onClick={() => {
                            reorder(img.id, i - 1);
                          }}
                        >
                          ←
                        </button>
                        <button
                          type="button"
                          className="ibtn"
                          aria-label={`Dời ảnh ${String(i + 1)} sang phải`}
                          disabled={i === n - 1}
                          onClick={() => {
                            reorder(img.id, i + 1);
                          }}
                        >
                          →
                        </button>
                        <button
                          type="button"
                          className="btn btn-quiet grow"
                          onClick={() => {
                            void original(img);
                          }}
                        >
                          Ảnh gốc
                        </button>
                      </>
                    )}
                    <button
                      type="button"
                      className="ibtn"
                      aria-label={`Xóa ảnh ${String(i + 1)}`}
                      onClick={() => {
                        remove(img, i + 1);
                      }}
                    >
                      ×
                    </button>
                  </div>
                )}
              </div>
            </div>
          ))}
        </div>
      )}
      {toast !== null && (
        <div className="toast p-img-toast" role="status">
          <span className="grow">Đã xóa ảnh {toast.n}. Khôi phục được trong 30 ngày.</span>
          <button
            type="button"
            className="btn"
            onClick={() => {
              undo(toast);
            }}
          >
            Hoàn tác ({toast.left})
          </button>
        </div>
      )}
    </div>
  );
}

function Tile({ img, n }: { img: ProductImage; n: number }) {
  const pos = (
    <span className={`chip img-pos${n === 1 ? " chip-dark" : ""}`}>{n === 1 ? "1 · Ảnh bìa" : String(n)}</span>
  );
  const c = coverPicture(coverOf(img));
  if (c.kind !== "ready")
    return (
      <div className="img-ph is-lost">
        {pos}
        <span>
          <b>Ảnh bị mất</b>
          <br />
          <span className="xs">Kho tệp không còn ảnh này. Tải lại ảnh.</span>
        </span>
      </div>
    );
  return (
    <div className="img-ph">
      <picture>
        {c.sources.map((s) => (
          <source key={s.type} type={s.type} srcSet={s.srcSet} sizes="(max-width: 759px) 50vw, 200px" />
        ))}
        <img
          src={c.src}
          srcSet={c.srcSet}
          sizes="(max-width: 759px) 50vw, 200px"
          alt={`Ảnh ${String(n)}`}
          loading="lazy"
        />
      </picture>
      {pos}
    </div>
  );
}

function UploadRow({ item, onDismiss, onRetry }: { item: UploadItem; onDismiss: () => void; onRetry: () => void }) {
  if (item.phase === "failed")
    return (
      <div className="up-row is-bad">
        <span className="thumb is-empty">×</span>
        <span className="grow">
          <b className="small err-text">{item.message}</b>
          <br />
          <span className="xs muted">{item.name} · không được thêm vào mẫu</span>
        </span>
        {item.retryAttach === true && (
          <button type="button" className="btn btn-quiet" onClick={onRetry}>
            Gắn lại
          </button>
        )}
        <button type="button" className="btn btn-quiet" onClick={onDismiss}>
          Bỏ
        </button>
      </div>
    );
  const text =
    item.phase === "uploading" ? "Đang tải lên…" : item.phase === "processing" ? TEXT_PROCESSING : "Đang gắn vào mẫu…";
  return (
    <div className="up-row">
      <span className="thumb is-proc">…</span>
      <span className="grow">
        <b className="small">{item.name}</b>
        <br />
        <span className="xs muted">{text}</span>
      </span>
    </div>
  );
}
