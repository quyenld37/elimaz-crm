package article_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"elimaz.com.vn/crm/internal/modules/catalog/internal/article"
)

func TestValidate_Valid(t *testing.T) {
	raw := `[
		{"type":"heading","text":"Chất liệu","level":2},
		{"type":"paragraph","text":"Lụa mềm\nmặc mát"},
		{"type":"list","items":["Đỏ","Đen"]},
		{"type":"image","fileId":"0193a4b2-7c3d-7e4f-8a9b-0c1d2e3f4a5b","caption":"Ảnh mặt trước"},
		{"type":"heading","text":"<script>alert(1)</script>","level":3}
	]`
	blocks, err := article.Validate(json.RawMessage(raw))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(blocks) != 5 {
		t.Fatalf("mong 5 khối, có %d", len(blocks))
	}
	if blocks[1].Text != "Lụa mềm\nmặc mát" || blocks[0].Level != 2 || len(blocks[2].Items) != 2 {
		t.Errorf("khối đọc sai: %+v", blocks)
	}
	if blocks[4].Text != "<script>alert(1)</script>" {
		t.Errorf("chuỗi phải giữ nguyên văn, có %q", blocks[4].Text)
	}
	if blocks[3].FileID == nil || blocks[3].FileID.String() != "0193a4b2-7c3d-7e4f-8a9b-0c1d2e3f4a5b" {
		t.Errorf("fileId đọc sai: %v", blocks[3].FileID)
	}
	for _, empty := range []string{"", "[]"} {
		b, err := article.Validate(json.RawMessage(empty))
		if err != nil || len(b) != 0 {
			t.Errorf("Validate(%q) = %v, %v; mong rỗng", empty, b, err)
		}
	}
	// Mã hóa lại rồi đọc lại → như cũ (dạng lưu chuẩn).
	enc, err := json.Marshal(blocks)
	if err != nil {
		t.Fatal(err)
	}
	again, err := article.Validate(enc)
	if err != nil || len(again) != len(blocks) {
		t.Fatalf("đọc lại dạng chuẩn: %v (%s)", err, enc)
	}
}

func TestValidate_Rejects(t *testing.T) {
	long := strings.Repeat("a", 5001)
	many := make([]string, 201)
	for i := range many {
		many[i] = `{"type":"paragraph","text":"x"}`
	}
	items := make([]string, 51)
	for i := range items {
		items[i] = `"x"`
	}
	big := make([]string, 45)
	for i := range big {
		big[i] = fmt.Sprintf(`{"type":"paragraph","text":%q}`, strings.Repeat("ạ", 1600))
	}
	cases := map[string]string{
		"không phải mảng":        `{"type":"paragraph","text":"x"}`,
		"null":                   `null`,
		"loại html":              `[{"type":"html","text":"<b>x</b>"}]`,
		"thiếu loại":             `[{"text":"x"}]`,
		"trường lạ":              `[{"type":"paragraph","text":"x","style":"color:red"}]`,
		"trường của loại khác":   `[{"type":"paragraph","text":"x","items":["a"]}]`,
		"heading cấp 4":          `[{"type":"heading","text":"x","level":4}]`,
		"heading thiếu cấp":      `[{"type":"heading","text":"x"}]`,
		"paragraph rỗng":         `[{"type":"paragraph","text":""}]`,
		"ký tự NUL":              `[{"type":"paragraph","text":"a\u0000b"}]`,
		"ký tự điều khiển":       `[{"type":"paragraph","text":"a\u001bb"}]`,
		"xuống dòng ở heading":   `[{"type":"heading","text":"a\nb","level":2}]`,
		"text quá 5000":          `[{"type":"paragraph","text":"` + long + `"}]`,
		"caption quá 5000":       `[{"type":"image","fileId":"0193a4b2-7c3d-7e4f-8a9b-0c1d2e3f4a5b","caption":"` + long + `"}]`,
		"201 khối":               "[" + strings.Join(many, ",") + "]",
		"51 mục":                 `[{"type":"list","items":[` + strings.Join(items, ",") + `]}]`,
		"list rỗng":              `[{"type":"list","items":[]}]`,
		"mục rỗng":               `[{"type":"list","items":[""]}]`,
		"image thiếu fileId":     `[{"type":"image"}]`,
		"image fileId sai dạng":  `[{"type":"image","fileId":"abc"}]`,
		"quá 200 KB":             "[" + strings.Join(big, ",") + "]",
		"UTF-8 hỏng":             "[{\"type\":\"paragraph\",\"text\":\"\xff\"}]",
		"hai mảng":               `[] []`,
		"khóa trùng":             `[{"type":"paragraph","text":"x","text":"y"}]`,
		"image có text":          `[{"type":"image","fileId":"0193a4b2-7c3d-7e4f-8a9b-0c1d2e3f4a5b","text":"x"}]`,
		"level ở paragraph":      `[{"type":"paragraph","text":"x","level":2}]`,
		"phần tử không phải obj": `["x"]`,
	}
	for name, raw := range cases {
		if _, err := article.Validate(json.RawMessage(raw)); !errors.Is(err, article.ErrInvalid) {
			t.Errorf("%s: mong ErrInvalid, có %v", name, err)
		}
	}
}
