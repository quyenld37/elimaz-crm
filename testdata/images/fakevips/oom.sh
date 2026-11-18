#!/bin/sh
# vips giả ăn 2 GiB (Task 42): dd xin một bộ đệm 2 GiB. Dưới prlimit --as=256MiB, xin không được và thoát lỗi
# ("memory exhausted"), như libvips gặp ảnh ngốn bộ nhớ.
exec dd if=/dev/zero of=/dev/null bs=2G count=1
