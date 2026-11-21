# Nén ảnh chụp của mockup M5 (Task M5-22, số toàn cục 205): đổi mỗi PNG trong shots/ sang bảng 256 màu (Pillow,
# quantize kiểu FASTOCTREE), ghi đè tại chỗ. Ảnh chỉ để xem, không dùng so điểm ảnh. Chạy sau soat.mjs:
#   python3 docs/mockups/m5/nen-anh.py
import pathlib
from PIL import Image

here = pathlib.Path(__file__).parent / "shots"
before = after = 0
for p in sorted(here.glob("*.png")):
    before += p.stat().st_size
    im = Image.open(p)
    if im.mode != "P":
        im.convert("RGB").quantize(colors=256, method=Image.Quantize.FASTOCTREE).save(p, optimize=True)
    after += p.stat().st_size
print(f"shots/: {before / 1e6:.1f} MB -> {after / 1e6:.1f} MB")
