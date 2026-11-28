package main

import (
	"errors"
	"testing"
	"time"
)

// Cấu hình: bí mật ngắn bị từ chối; "-" chỉ trong bộ nhớ; mặc định cổng 4194.
func TestLoadConfig(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if _, err := loadConfig(env(map[string]string{"FAKECARRIER_WEBHOOK_SECRET": "ngan"})); !errors.Is(err, errShortSecret) {
		t.Fatalf("bí mật ngắn: %v", err)
	}
	c, err := loadConfig(env(map[string]string{"FAKECARRIER_DATA": "-"}))
	if err != nil || c.port != 4194 || c.dataPath != "" || len(c.secret) < 32 {
		t.Fatalf("mặc định = %+v %v", c, err)
	}
	if _, err := loadConfig(env(map[string]string{"PORT": "x"})); err == nil {
		t.Fatal("PORT sai mà không lỗi")
	}
	if c.quoteFee != 0 {
		t.Fatalf("mặc định phí cố định = %d, muốn 0 (công thức)", c.quoteFee)
	}
	if c, err := loadConfig(env(map[string]string{"FAKECARRIER_QUOTE_FEE": "30000"})); err != nil || c.quoteFee != 30000 {
		t.Fatalf("FAKECARRIER_QUOTE_FEE=30000 = %d %v", c.quoteFee, err)
	}
	for _, v := range []string{"0", "-1", "3e4", "x"} {
		if _, err := loadConfig(env(map[string]string{"FAKECARRIER_QUOTE_FEE": v})); err == nil {
			t.Errorf("FAKECARRIER_QUOTE_FEE=%q mà không lỗi", v)
		}
	}
	if c.trackDelay != 0 {
		t.Fatalf("mặc định chờ tra = %v, muốn 0", c.trackDelay)
	}
	if c, err := loadConfig(env(map[string]string{"FAKECARRIER_TRACK_DELAY": "200ms"})); err != nil || c.trackDelay != 200*time.Millisecond {
		t.Fatalf("FAKECARRIER_TRACK_DELAY=200ms = %v %v", c.trackDelay, err)
	}
	for _, v := range []string{"-1s", "x", "200"} {
		if _, err := loadConfig(env(map[string]string{"FAKECARRIER_TRACK_DELAY": v})); err == nil {
			t.Errorf("FAKECARRIER_TRACK_DELAY=%q mà không lỗi", v)
		}
	}
}
