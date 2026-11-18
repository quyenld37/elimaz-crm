package fieldcrypt_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/google/uuid"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/platform/fieldcrypt"
)

func genUUID(rt *rapid.T, label string) uuid.UUID {
	var u uuid.UUID
	copy(u[:], rapid.SliceOfN(rapid.Byte(), 16, 16).Draw(rt, label))
	return u
}

func genAAD(rt *rapid.T) fieldcrypt.AAD {
	name := rapid.StringMatching(`[a-z_][a-z0-9_.]{0,40}`)
	return fieldcrypt.AAD{
		TenantID: genUUID(rt, "tenant"), Table: name.Draw(rt, "table"), Column: name.Draw(rt, "column"),
		RowID: genUUID(rt, "row"),
	}
}

// Quy luật (vùng rủi ro cao): Open(Seal(x)) = x với x và AAD ngẫu nhiên; hai lần Seal cùng x khác bản mã (nonce ngẫu
// nhiên); đổi một byte bất kỳ của bản mã → ErrDecrypt.
func TestPropertySealOpen(t *testing.T) {
	k := ring(t, "k2:"+key(2)+",k1:"+key(1))
	rapid.Check(t, func(rt *rapid.T) {
		x := rapid.SliceOfN(rapid.Byte(), 0, 512).Draw(rt, "plain")
		aad := genAAD(rt)
		ct, kid, err := k.Seal(x, aad)
		if err != nil || kid != "k2" {
			rt.Fatalf("Seal: %q %v", kid, err)
		}
		got, err := k.Open(ct, kid, aad)
		if err != nil || !bytes.Equal(got, x) {
			rt.Fatalf("Open(Seal(x)) = %x, %v; muốn %x", got, err, x)
		}
		ct2, _, err := k.Seal(x, aad)
		if err != nil || bytes.Equal(ct, ct2) {
			rt.Fatalf("hai lần Seal cùng bản mã (err %v)", err)
		}
		i := rapid.IntRange(0, len(ct)-1).Draw(rt, "flip")
		bad := bytes.Clone(ct)
		bad[i] ^= rapid.ByteRange(1, 255).Draw(rt, "mask")
		if _, err := k.Open(bad, kid, aad); !errors.Is(err, fieldcrypt.ErrDecrypt) {
			rt.Fatalf("đổi byte %d: %v, muốn ErrDecrypt", i, err)
		}
	})
}
