package fieldcrypt_test

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/platform/fieldcrypt"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// key trả khóa 32 byte cố định (mỗi byte = b) dạng base64.
func key(b byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32)) }

func ring(t *testing.T, spec string) *fieldcrypt.Keyring {
	t.Helper()
	k, err := fieldcrypt.ParseKeyring(logging.Secret(spec))
	if err != nil {
		t.Fatalf("ParseKeyring: %v", err)
	}
	return k
}

func sampleAAD() fieldcrypt.AAD {
	return fieldcrypt.AAD{
		TenantID: uuid.MustParse("0195a000-0000-7000-8000-000000000001"),
		Table:    "channel.channel_accounts", Column: "token_enc",
		RowID: uuid.MustParse("0195a000-0000-7000-8000-0000000000aa"),
	}
}

func TestSealOpenRoundTrip(t *testing.T) {
	k := ring(t, "k1:"+key(1))
	ct, kid, err := k.Seal([]byte("EAAB-token-1"), sampleAAD())
	if err != nil || kid != "k1" {
		t.Fatalf("Seal: kid %q err %v", kid, err)
	}
	if bytes.Contains(ct, []byte("EAAB-token-1")) {
		t.Fatal("bản mã chứa bản rõ")
	}
	// nonce 12 byte || bản mã cùng độ dài bản rõ || tag 16 byte.
	if len(ct) != 12+len("EAAB-token-1")+16 {
		t.Fatalf("độ dài bản mã %d", len(ct))
	}
	got, err := k.Open(ct, kid, sampleAAD())
	if err != nil || string(got) != "EAAB-token-1" {
		t.Fatalf("Open: %q %v", got, err)
	}
}

// Đổi bất kỳ byte nào của bản mã (nonce, thân, tag) → ErrDecrypt.
func TestOpenTamperedFails(t *testing.T) {
	k := ring(t, "k1:"+key(1))
	ct, kid, err := k.Seal([]byte("EAAB-token-1"), sampleAAD())
	if err != nil {
		t.Fatal(err)
	}
	for i := range ct {
		bad := bytes.Clone(ct)
		bad[i] ^= 0x01
		if _, err := k.Open(bad, kid, sampleAAD()); !errors.Is(err, fieldcrypt.ErrDecrypt) {
			t.Fatalf("đổi byte %d: %v, muốn ErrDecrypt", i, err)
		}
	}
	for _, short := range [][]byte{nil, ct[:11], ct[:12+15]} {
		if _, err := k.Open(short, kid, sampleAAD()); !errors.Is(err, fieldcrypt.ErrDecrypt) {
			t.Fatalf("bản mã ngắn %d byte: %v, muốn ErrDecrypt", len(short), err)
		}
	}
}

// AAD gắn bản mã vào đúng dòng: khác tenant, bảng, cột hay dòng → ErrDecrypt.
func TestOpenWrongAADFails(t *testing.T) {
	k := ring(t, "k1:"+key(1))
	aad := sampleAAD()
	ct, kid, err := k.Seal([]byte("EAAB-token-1"), aad)
	if err != nil {
		t.Fatal(err)
	}
	other := uuid.MustParse("0195a000-0000-7000-8000-0000000000bb")
	cases := map[string]func(*fieldcrypt.AAD){
		"RowID":    func(a *fieldcrypt.AAD) { a.RowID = other },
		"TenantID": func(a *fieldcrypt.AAD) { a.TenantID = other },
		"Table":    func(a *fieldcrypt.AAD) { a.Table = "fulfillment.carrier_accounts" },
		"Column":   func(a *fieldcrypt.AAD) { a.Column = "credential_enc" },
	}
	for name, mod := range cases {
		a := aad
		mod(&a)
		if _, err := k.Open(ct, kid, a); !errors.Is(err, fieldcrypt.ErrDecrypt) {
			t.Errorf("khác %s: %v, muốn ErrDecrypt", name, err)
		}
	}
}

// AAD có Table, Column rỗng hay chứa ký tự ngoài [a-z0-9_.] (ví dụ "|", làm mã hóa AAD mơ hồ) → ErrInvalidAAD.
func TestInvalidAAD(t *testing.T) {
	k := ring(t, "k1:"+key(1))
	for _, a := range []fieldcrypt.AAD{
		{TenantID: uuid.New(), Table: "", Column: "c", RowID: uuid.New()},
		{TenantID: uuid.New(), Table: "t", Column: "", RowID: uuid.New()},
		{TenantID: uuid.New(), Table: "a|b", Column: "c", RowID: uuid.New()},
		{TenantID: uuid.New(), Table: "t", Column: "C", RowID: uuid.New()},
	} {
		if _, _, err := k.Seal([]byte("x"), a); !errors.Is(err, fieldcrypt.ErrInvalidAAD) {
			t.Errorf("Seal với AAD %+v: %v, muốn ErrInvalidAAD", a, err)
		}
		if _, err := k.Open(make([]byte, 40), "k1", a); !errors.Is(err, fieldcrypt.ErrInvalidAAD) {
			t.Errorf("Open với AAD %+v: %v, muốn ErrInvalidAAD", a, err)
		}
	}
}

// "k2:<b>,k1:<a>": Seal dùng khóa đầu k2; bản mã cũ của k1 vẫn mở được; NeedsRewrap chỉ đúng với khóa khác khóa đầu.
func TestRotation(t *testing.T) {
	old := ring(t, "k1:"+key(1))
	ct1, kid1, err := old.Seal([]byte("EAAB-token-1"), sampleAAD())
	if err != nil || kid1 != "k1" {
		t.Fatalf("Seal cũ: %q %v", kid1, err)
	}
	k := ring(t, " k2:"+key(2)+" , k1:"+key(1)+" ")
	if k.CurrentKeyID() != "k2" {
		t.Fatalf("CurrentKeyID = %q, muốn k2", k.CurrentKeyID())
	}
	ct2, kid2, err := k.Seal([]byte("EAAB-token-2"), sampleAAD())
	if err != nil || kid2 != "k2" {
		t.Fatalf("Seal mới: %q %v", kid2, err)
	}
	if got, err := k.Open(ct1, kid1, sampleAAD()); err != nil || string(got) != "EAAB-token-1" {
		t.Fatalf("mở bản mã k1: %q %v", got, err)
	}
	if got, err := k.Open(ct2, kid2, sampleAAD()); err != nil || string(got) != "EAAB-token-2" {
		t.Fatalf("mở bản mã k2: %q %v", got, err)
	}
	if !k.NeedsRewrap("k1") || k.NeedsRewrap("k2") || !k.NeedsRewrap("k9") {
		t.Fatalf("NeedsRewrap k1=%v k2=%v k9=%v, muốn true false true", k.NeedsRewrap("k1"), k.NeedsRewrap("k2"), k.NeedsRewrap("k9"))
	}
	// Bản mã của k2 mà gắn nhãn k1 → khóa sai → ErrDecrypt.
	if _, err := k.Open(ct2, "k1", sampleAAD()); !errors.Is(err, fieldcrypt.ErrDecrypt) {
		t.Fatalf("mở bằng khóa sai: %v, muốn ErrDecrypt", err)
	}
	if _, err := old.Open(ct2, kid2, sampleAAD()); !errors.Is(err, fieldcrypt.ErrUnknownKey) {
		t.Fatalf("mở bằng key_id không có: %v, muốn ErrUnknownKey", err)
	}
}

// Chuỗi khóa sai → lỗi; lỗi không bao giờ chứa khóa (base64 hay byte thô).
func TestParseKeyringRejects(t *testing.T) {
	short := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 31))
	long := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{8}, 33))
	cases := map[string]string{
		"rỗng":               "",
		"chỉ khoảng trắng":   "  ",
		"mục rỗng":           "k1:" + key(1) + ",",
		"31 byte":            "k1:" + short,
		"33 byte":            "k1:" + long,
		"trùng key_id":       "k1:" + key(1) + ",k1:" + key(2),
		"thiếu dấu hai chấm": key(1),
		"key_id hoa":         "K1:" + key(1),
		"key_id dài 17":      strings.Repeat("a", 17) + ":" + key(1),
		"key_id rỗng":        ":" + key(1),
		"base64 hỏng":        "k1:" + key(1)[:43] + "!",
	}
	for name, spec := range cases {
		_, err := fieldcrypt.ParseKeyring(logging.Secret(spec))
		if err == nil {
			t.Errorf("%s: không lỗi", name)
			continue
		}
		for _, secret := range []string{key(1), key(2), short, long, key(1)[:43]} {
			if strings.Contains(err.Error(), secret) {
				t.Errorf("%s: lỗi chứa khóa: %v", name, err)
			}
		}
	}
}

// Keyring (con trỏ và giá trị) không lộ khóa qua slog, fmt (%v, %+v, %#v, %s).
func TestKeyringNeverPrintsKeys(t *testing.T) {
	k := ring(t, "k2:"+key(2)+",k1:"+key(1))
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("x", slog.Any("k", k))
	if !strings.Contains(buf.String(), `"k":"[Redacted]"`) {
		t.Fatalf("slog: %s", buf.String())
	}
	raw := [][]byte{bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32)}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		out := fmt.Sprintf(verb, k) + fmt.Sprintf(verb, *k)
		if strings.Contains(out, key(1)) || strings.Contains(out, key(2)) || bytes.Contains([]byte(out), raw[0]) ||
			bytes.Contains([]byte(out), raw[1]) || strings.Contains(out, "[1 1 1") || strings.Contains(out, "[2 2 2") {
			t.Fatalf("%s lộ khóa: %s", verb, out)
		}
	}
}
