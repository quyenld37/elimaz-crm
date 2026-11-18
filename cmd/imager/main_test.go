package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestLoad_Defaults(t *testing.T) {
	cfg, err := load(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.port != 4193 || cfg.maxParallel != 2 || cfg.lim.StepTimeout != 10*time.Second ||
		cfg.lim.StepMemory != 1<<30 || cfg.lim.VipsBin != "vips" || cfg.lim.VipsHeaderBin != "vipsheader" {
		t.Fatalf("mặc định sai: %+v", cfg)
	}
}

func TestLoad_Values(t *testing.T) {
	cfg, err := load(env(map[string]string{
		"PORT": "5000", "IMAGER_MAX_PARALLEL": "3", "IMAGER_STEP_TIMEOUT": "1s", "IMAGER_STEP_MEMORY": "256MiB",
		"VIPS_BIN": "/x/hang.sh", "TMPDIR": "/tmp/i",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.port != 5000 || cfg.maxParallel != 3 || cfg.lim.StepTimeout != time.Second ||
		cfg.lim.StepMemory != 256<<20 || cfg.lim.VipsBin != "/x/hang.sh" || cfg.lim.TmpDir != "/tmp/i" {
		t.Fatalf("đọc sai: %+v", cfg)
	}
	if cfg, err := load(env(map[string]string{"IMAGER_STEP_MEMORY": "1073741824"})); err != nil || cfg.lim.StepMemory != 1<<30 {
		t.Fatalf("số byte trần: %v %+v", err, cfg)
	}
}

func TestLoad_Invalid(t *testing.T) {
	for _, m := range []map[string]string{
		{"PORT": "0"},
		{"PORT": "x"},
		{"IMAGER_MAX_PARALLEL": "0"},
		{"IMAGER_STEP_TIMEOUT": "-1s"},
		{"IMAGER_STEP_TIMEOUT": "10"},
		{"IMAGER_STEP_MEMORY": "1MiB"},
		{"IMAGER_STEP_MEMORY": "nhiều"},
		{"IMAGER_STEP_MEMORY": "99999999999GiB"},
	} {
		if _, err := load(env(m)); err == nil {
			t.Fatalf("%v phải lỗi", m)
		}
	}
}

func TestRun_BadConfigExits2(t *testing.T) {
	var stderr bytes.Buffer
	if code := run(context.Background(), nil, env(map[string]string{"PORT": "x"}), &stderr); code != exitConfig {
		t.Fatalf("mã thoát %d", code)
	}
	if !strings.Contains(stderr.String(), "PORT") {
		t.Fatalf("stderr %q", stderr.String())
	}
}

func TestRun_HealthcheckFailsWhenNotRunning(t *testing.T) {
	var stderr bytes.Buffer
	if code := run(context.Background(), []string{"healthcheck"}, env(map[string]string{"PORT": "1"}), &stderr); code != exitFail {
		t.Fatalf("mã thoát %d", code)
	}
}
