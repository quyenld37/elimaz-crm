# Nén ảnh chụp của mockup M6 (Task M6-01, số toàn cục 224), chép từ docs/mockups/m5/nen-anh.py: đổi mỗi PNG trong thư
# mục này sang bảng 256 màu (Pillow, quantize kiểu FASTOCTREE), ghi đè tại chỗ; ảnh đã ở bảng màu thì bỏ qua. Ảnh chỉ
# để xem, không dùng so điểm ảnh. Chạy sau soat.mjs:
#   python3 docs/m6/mockup/nen-anh.py
import pathlib
from PIL import Image

here = pathlib.Path(__file__).parent
before = after = 0
for p in sorted(here.glob("*.png")):
    before += p.stat().st_size
    im = Image.open(p)
    if im.mode != "P":
        im.convert("RGB").quantize(colors=256, method=Image.Quantize.FASTOCTREE).save(p, optimize=True)
    after += p.stat().st_size
print(f"docs/m6/mockup: {before / 1e6:.1f} MB -> {after / 1e6:.1f} MB")
