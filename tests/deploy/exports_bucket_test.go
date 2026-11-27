package deploy_test

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

// Bucket tệp xuất crm-exports (Task 515; kế hoạch M6 G5 chốt (a)). Phần chạy thật trên SeaweedFS (không giữ phiên bản,
// luật hết hạn, ttlSec theo tiền tố, crm-app không xóa được) ở tests/probe/exports_test.go; ở đây kiểm tĩnh các tệp
// cấu hình mà máy chủ thật dùng lại.

// seaweedActions: danh sách việc của mỗi danh tính trong một tệp danh tính SeaweedFS của repo.
func seaweedActions(t *testing.T, file string) map[string][]string {
	t.Helper()
	raw, err := os.ReadFile("../../docker/seaweedfs/" + file)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Identities []struct {
			Name    string   `json:"name"`
			Actions []string `json:"actions"`
		} `json:"identities"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("giải %s: %v", file, err)
	}
	out := map[string][]string{}
	for _, id := range cfg.Identities {
		out[id.Name] = id.Actions
	}
	return out
}

// crm-app đọc, ghi, liệt kê crm-exports (files.S3Store.Put, Open, PresignGet); crm-filesync không có việc nào trên
// bucket này ở kho chính; kho ngoài giả không nhắc tới nó.
func TestSeaweedIdentities_ExportsBucket(t *testing.T) {
	main := seaweedActions(t, "s3.json")
	for _, a := range []string{"Read:crm-exports", "Write:crm-exports", "List:crm-exports"} {
		if !slices.Contains(main["crm-app"], a) {
			t.Errorf("s3.json: crm-app thiếu %s", a)
		}
	}
	for name, acts := range main {
		if name == "crm-app" || name == "crm-admin" {
			continue
		}
		for _, a := range acts {
			if strings.HasSuffix(a, ":crm-exports") {
				t.Errorf("s3.json: %s có %s (tệp xuất không chép ra kho ngoài)", name, a)
			}
		}
	}
	for name, acts := range seaweedActions(t, "s3-offsite.json") {
		for _, a := range acts {
			if strings.Contains(a, "exports") {
				t.Errorf("s3-offsite.json: %s có %s", name, a)
			}
		}
	}
}

// filesync chỉ chép ba bucket public, originals, private; bucket exports lạ với lib.sh (dừng, không chép nhầm chỗ).
func TestFilesyncLib_SkipsExports(t *testing.T) {
	out, err := sourceLib(t, nil, `printf '%s' "$FILESYNC_BUCKETS"`)
	if err != nil || slices.Contains(strings.Fields(out), "exports") {
		t.Fatalf("FILESYNC_BUCKETS = %q (%v), không được có exports", out, err)
	}
	for _, fn := range []string{"src_of", "dst_of"} {
		if got, err := sourceLib(t, nil, fn+` exports`); err == nil {
			t.Errorf("%s exports phải lỗi, có %q", fn, got)
		}
	}
}

// bootstrap.sh dựng <tiền tố>exports: không bật giữ phiên bản, hai luật hết hạn theo tiền tố, fast path, chính sách từ
// chối cả DeleteObject với crm-app; vai trò offsite không dựng bucket này.
func TestSeaweedBootstrap_ExportsBucket(t *testing.T) {
	raw, err := os.ReadFile("../../docker/seaweedfs/bootstrap.sh")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	_, rest, ok1 := strings.Cut(s, "  main)")
	main, offsite, ok2 := strings.Cut(rest, "  offsite)")
	if !ok1 || !ok2 {
		t.Fatal("bootstrap.sh không có nhánh main) và offsite)")
	}
	for _, want := range []string{
		`must "tạo ${p}exports" 200 409 -- PUT "/${p}exports"`,
		`<Rule><ID>reports-7-ngay</ID><Filter><Prefix>reports/</Prefix></Filter><Status>Enabled</Status><Expiration><Days>7</Days></Expiration></Rule>`,
		`<Rule><ID>customers-1-ngay</ID><Filter><Prefix>customers/</Prefix></Filter><Status>Enabled</Status><Expiration><Days>1</Days></Expiration></Rule>`,
		`fastpath "${p}exports"`,
		`policy "${p}exports" crm-app DeleteObject $loosen`,
	} {
		if !strings.Contains(main, want) {
			t.Errorf("nhánh main của bootstrap.sh thiếu %q", want)
		}
	}
	for _, line := range strings.Split(main, "\n") {
		if strings.Contains(line, "giữ phiên bản") && strings.Contains(line, "exports") {
			t.Errorf("bootstrap.sh bật giữ phiên bản cho exports: %q", line)
		}
	}
	if strings.Contains(offsite, "exports") {
		t.Error("nhánh offsite của bootstrap.sh nhắc tới exports: kho ngoài không có tệp xuất")
	}
}
