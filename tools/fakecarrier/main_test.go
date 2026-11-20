package main

import (
	"errors"
	"testing"
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
}
