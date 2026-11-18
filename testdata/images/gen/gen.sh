#!/usr/bin/env bash
# Sinh lại ảnh thử của imager (Task 42). Chạy từ gốc apps/crm: testdata/images/gen/gen.sh
# Phần cần libvips chạy trong stage `test` của docker/imager/Dockerfile (cùng bản libvips với image imager).
set -euo pipefail
dir=testdata/images
go run ./testdata/images/gen "$dir"
docker build -q -f docker/imager/Dockerfile --target test -t crm-imager-test . >/dev/null
docker run --rm -v "$PWD/$dir:/img" crm-imager-test sh -euc '
  cd /img
  vips thumbnail source.jpg "tiny.webp[Q=80,keep=none]" 500
  vips thumbnail source.jpg "mid.jpg[Q=85,keep=none]" 1200
  head -c "$(( $(stat -c %s mid.jpg) * 6 / 10 ))" mid.jpg > truncated.jpg
  vips thumbnail source.jpg "srgb800.jpg[keep=none]" 800
  vips icc_export srgb800.jpg "cmyk.jpg[Q=85,keep=none]" --output-profile cmyk
  vips resize source.jpg "big-24mp.jpg[Q=85,keep=none]" 2.5
  rm -f mid.jpg srgb800.jpg source.jpg
  for f in *.jpg *.png *.webp; do printf "%s: " "$f"; vipsheader "$f" 2>&1 | head -1; done
'
