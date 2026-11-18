// @vitest-environment jsdom
// Trình soạn bài viết (kế hoạch M1 Task 326; mockup docs/mockups/m1/san-pham-sua.html, trạng thái A, B, C): danh sách
// khối, thêm/xóa/đổi thứ tự, khối ảnh chọn từ ảnh của mẫu, thu gọn bài dài, "Xem trước" hiện chữ nguyên văn (không
// chạy mã), lỗi hiện tại đúng khối.
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ArticleBlock, ProductImage } from "../../api";
import { ArticleEditor } from "./ArticleEditor";
import { articleBody, editBlocks, type EditBlock } from "./articleRules";

const F1 = "0195f3b2-0000-7000-8000-00000000f001";
const F3 = "0195f3b2-0000-7000-8000-00000000f003";
const XSS = "Mẹo phối: đi cùng giày mũi nhọn và túi hộp nhỏ. <img src=x onerror=alert(1)>";

const IMAGES: ProductImage[] = [
  {
    id: "i1",
    fileId: F1,
    position: 1,
    status: "ready",
    color: "Đen",
    variants: [{ width: 400, format: "jpeg", url: "/a1.jpg" }],
  },
  {
    id: "i3",
    fileId: F3,
    position: 2,
    status: "ready",
    color: "Be",
    variants: [{ width: 400, format: "jpeg", url: "/a3.jpg" }],
  },
];

/** Bài 30 khối như mockup trạng thái A (khối 4 là ảnh, khối 29 có chuỗi HTML). */
function thirty(): ArticleBlock[] {
  return Array.from({ length: 30 }, (_, i): ArticleBlock => {
    if (i === 0) return { type: "heading", text: "Chất liệu và form dáng", level: 2 };
    if (i === 2) return { type: "list", items: ["Giặt tay nước lạnh", "Không vắt mạnh"] };
    if (i === 3) return { type: "image", fileId: F3 };
    if (i === 28) return { type: "paragraph", text: XSS };
    return { type: "paragraph", text: `Đoạn ${String(i + 1)}` };
  });
}

let latest: EditBlock[] = [];

function Harness(props: {
  initial: ArticleBlock[];
  errors?: Record<string, string>;
  readOnly?: boolean;
  images?: ProductImage[];
}) {
  const [blocks, setBlocks] = useState(() => editBlocks(props.initial));
  return (
    <ArticleEditor
      blocks={blocks}
      images={props.images ?? IMAGES}
      errors={props.errors ?? {}}
      readOnly={props.readOnly ?? false}
      onChange={(b) => {
        latest = b;
        setBlocks(b);
      }}
    />
  );
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("ArticleEditor", () => {
  it("30 khối: đếm, khối 1–4 và 29–30 hiện, 5–28 thu gọn; bấm thì mở", () => {
    render(<Harness initial={thirty()} />);
    expect(screen.getByText("30 khối")).toBeTruthy();
    expect(screen.getByText("1 · Tiêu đề")).toBeTruthy();
    expect(screen.getByText("4 · Ảnh")).toBeTruthy();
    expect(screen.getByText("29 · Đoạn văn")).toBeTruthy();
    expect(screen.queryByText("5 · Đoạn văn")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Khối 5–28 (24 khối) đang thu gọn — bấm để mở" }));
    expect(screen.getByText("5 · Đoạn văn")).toBeTruthy();
    expect(screen.getAllByTestId("article-block").length).toBe(30);
  });

  it("khối đầu không lên được, khối cuối không xuống được; đổi thứ tự, xóa", () => {
    render(<Harness initial={thirty().slice(0, 3)} />);
    expect(screen.getByRole("button", { name: "Đưa khối 1 lên" }).matches(":disabled")).toBe(true);
    expect(screen.getByRole("button", { name: "Đưa khối 3 xuống" }).matches(":disabled")).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Đưa khối 1 xuống" }));
    expect(articleBody(latest).map((b) => b.type)).toEqual(["paragraph", "heading", "list"]);
    fireEvent.click(screen.getByRole("button", { name: "Đưa khối 3 lên" }));
    expect(articleBody(latest).map((b) => b.type)).toEqual(["paragraph", "list", "heading"]);
    fireEvent.click(screen.getByRole("button", { name: "Xóa khối 1" }));
    expect(articleBody(latest).map((b) => b.type)).toEqual(["list", "heading"]);
  });

  it("0 khối: câu trống; thêm bốn loại khối, gõ chữ, chọn ảnh → khối đúng", () => {
    render(<Harness initial={[]} />);
    expect(screen.getByText("0 khối")).toBeTruthy();
    expect(screen.getByText("Bài viết chưa có khối nào. Website chỉ hiện mô tả ngắn.")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "+ Tiêu đề" }));
    fireEvent.click(screen.getByRole("button", { name: "+ Đoạn văn" }));
    fireEvent.click(screen.getByRole("button", { name: "+ Danh sách" }));
    fireEvent.click(screen.getByRole("button", { name: "+ Ảnh" }));
    fireEvent.change(screen.getByRole("textbox", { name: "Khối 1 · Tiêu đề" }), { target: { value: "Form dáng" } });
    fireEvent.change(screen.getByRole("textbox", { name: "Khối 2 · Đoạn văn" }), { target: { value: XSS } });
    fireEvent.change(screen.getByRole("textbox", { name: "Khối 3 · Danh sách" }), { target: { value: "Một\nHai" } });
    const pick = screen.getByRole("combobox", { name: "Chọn từ ảnh của mẫu (khối 4)" });
    expect(
      within(pick)
        .getAllByRole("option")
        .map((o) => o.textContent),
    ).toEqual(["Chọn ảnh", "Ảnh 1 · Đen", "Ảnh 2 · Be"]);
    fireEvent.change(pick, { target: { value: F3 } });
    expect(articleBody(latest)).toEqual([
      { type: "heading", text: "Form dáng", level: 2 },
      { type: "paragraph", text: XSS },
      { type: "list", items: ["Một", "Hai"] },
      { type: "image", fileId: F3 },
    ]);
    expect(screen.getByText("Chữ được giữ nguyên văn, kể cả ký tự < >; không có định dạng HTML.")).toBeTruthy();
    expect(screen.getByText("Mỗi dòng là một ý")).toBeTruthy();
  });

  it("mẫu chưa có ảnh sẵn sàng → nút + Ảnh tắt, có câu giải thích", () => {
    render(<Harness initial={[]} images={[]} />);
    expect(screen.getByRole("button", { name: "+ Ảnh" }).matches(":disabled")).toBe(true);
    expect(screen.getByText("Tải ảnh lên khối Ảnh trước, rồi thêm khối ảnh.")).toBeTruthy();
  });

  it("Xem trước: chữ chứa <img onerror> hiện nguyên văn, không tạo thẻ img lạ, không chạy", () => {
    const alert = vi.spyOn(window, "alert").mockImplementation(() => undefined);
    render(
      <Harness
        initial={thirty()
          .slice(0, 4)
          .concat([{ type: "paragraph", text: XSS }])}
      />,
    );
    fireEvent.click(screen.getByRole("tab", { name: "Xem trước" }));
    const pv = screen.getByTestId("article-preview");
    expect(within(pv).getByText(XSS)).toBeTruthy();
    expect(within(pv).getByRole("heading", { name: "Chất liệu và form dáng" }).tagName).toBe("H3");
    expect(
      within(pv)
        .getAllByRole("listitem")
        .map((x) => x.textContent),
    ).toEqual(["Giặt tay nước lạnh", "Không vắt mạnh"]);
    const imgs = pv.querySelectorAll("img");
    expect(imgs.length).toBe(1);
    expect(imgs[0]?.getAttribute("src")).toBe("/a3.jpg");
    expect(pv.querySelector("img[onerror]")).toBeNull();
    expect(alert).not.toHaveBeenCalled();
    expect(
      screen.getByText("Xem trước dựng giống trang sản phẩm trên web: chữ hiện nguyên văn, không chạy mã."),
    ).toBeTruthy();
  });

  it("lỗi hiện tại đúng khối (khối bị tô đỏ, câu dưới ô); khối sai trong đoạn thu gọn thì không thu gọn", () => {
    const art = thirty();
    const keyed = editBlocks(art);
    const bad = keyed[9];
    function WithErr() {
      const [blocks, setBlocks] = useState(keyed);
      return (
        <ArticleEditor
          blocks={blocks}
          images={IMAGES}
          errors={{ [bad?.key ?? ""]: "Khối chưa có chữ." }}
          readOnly={false}
          onChange={setBlocks}
        />
      );
    }
    render(<WithErr />);
    expect(screen.queryByText(/đang thu gọn/)).toBeNull();
    const blocks = screen.getAllByTestId("article-block");
    const tenth = blocks[9];
    if (tenth === undefined) throw new Error("thiếu khối 10");
    expect(tenth.className).toContain("is-err");
    expect(within(tenth).getByText("Khối chưa có chữ.")).toBeTruthy();
    expect(within(tenth).getByRole("textbox").getAttribute("aria-invalid")).toBe("true");
    expect(blocks.filter((b) => b.className.includes("is-err")).length).toBe(1);
  });

  it("chỉ xem (thiếu catalog.manage): chỉ có Xem trước, không có nút sửa", () => {
    render(<Harness initial={thirty().slice(0, 3)} readOnly />);
    expect(screen.queryByRole("tab")).toBeNull();
    expect(screen.queryByRole("button", { name: "+ Đoạn văn" })).toBeNull();
    expect(screen.getByTestId("article-preview")).toBeTruthy();
  });
});
