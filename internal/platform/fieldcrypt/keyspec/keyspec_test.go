package keyspec_test

import (
	"bytes"
	"encoding/base64"
	"errors"
	"testing"

	"elimaz.com.vn/crm/internal/platform/fieldcrypt/keyspec"
)

func TestParse(t *testing.T) {
	a := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	b := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
	keys, err := keyspec.Parse("k2:" + b + ", k1:" + a)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 || keys[0].ID != "k2" || keys[1].ID != "k1" || !bytes.Equal(keys[0].Secret, bytes.Repeat([]byte{2}, 32)) {
		t.Fatalf("Parse: %+v", keys)
	}
	if _, err := keyspec.Parse("k1:" + a + ",k1:" + b); !errors.Is(err, keyspec.ErrInvalid) {
		t.Fatalf("trùng key_id: %v, muốn ErrInvalid", err)
	}
}
