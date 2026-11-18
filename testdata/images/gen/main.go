// Chương trình sinh ảnh thử của imager (Task 42). Nằm dưới testdata nên go build ./... bỏ qua.
// Chạy bằng testdata/images/gen/gen.sh (cần libvips cho phần ảnh còn lại). Tạo:
//
//	photo-gps.jpg  2400×1600, có EXIF với GPS (IFD0 → GPS IFD), viết tay segment APP1
//	pixel-bomb.png khai 30.000 × 30.000 điểm ảnh, tệp khoảng 60 KB (IDAT chỉ có vài nghìn hàng đầu)
//	html-as.jpg    HTML đổi đuôi .jpg
//	source.jpg     2400×1600 không EXIF, nguồn cho gen.sh dựng tiny.webp, truncated.jpg, cmyk.jpg
package main

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"log"
	"math"
	"os"
	"path/filepath"
)

func main() {
	dir := os.Args[1]
	img := photo(2400, 1600)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		log.Fatal(err)
	}
	plain := buf.Bytes()
	write(dir, "source.jpg", plain)
	// Chèn APP1 Exif ngay sau SOI (FF D8).
	withExif := append([]byte{0xFF, 0xD8}, app1Exif()...)
	withExif = append(withExif, plain[2:]...)
	write(dir, "photo-gps.jpg", withExif)
	write(dir, "pixel-bomb.png", pixelBomb(30000, 30000, 60*1024))
	write(dir, "html-as.jpg", []byte("<!DOCTYPE html>\n<html><head><title>anh</title></head>\n"+
		"<body><script>alert(document.cookie)</script><img src=x onerror=alert(1)></body></html>\n"))
}

func write(dir, name string, b []byte) {
	if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
		log.Fatal(err)
	}
}

// photo: ảnh có chi tiết (dải màu và sóng) để bộ mã hóa phải làm việc như với ảnh thật.
func photo(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			fx, fy := float64(x)/float64(w), float64(y)/float64(h)
			r := 128 + 127*math.Sin(fx*20+fy*3)
			g := 128 + 127*math.Sin(fy*17+fx*fx*9)
			b := 255 * fx * fy
			img.Set(x, y, color.RGBA{uint8(r), uint8(g), uint8(b), 255})
		}
	}
	return img
}

// app1Exif dựng segment APP1 "Exif\0\0" + TIFF little-endian: IFD0 có Make và con trỏ GPSInfo (0x8825);
// GPS IFD có GPSLatitudeRef/GPSLatitude/GPSLongitudeRef/GPSLongitude (21°01'N, 105°51'E, Hà Nội).
func app1Exif() []byte {
	le := binary.LittleEndian
	var t bytes.Buffer
	t.Write([]byte{'I', 'I', 42, 0})
	_ = binary.Write(&t, le, uint32(8)) // IFD0 ở offset 8
	make := []byte("ELIMAZ test camera\x00")
	// IFD0: 2 mục, 12 byte mỗi mục, + 2 byte đếm + 4 byte IFD kế = 30 byte → dữ liệu ngoài từ offset 38.
	ifd0End := uint32(8 + 2 + 2*12 + 4)
	makeOff := ifd0End
	gpsOff := makeOff + uint32(len(make))
	if gpsOff%2 == 1 {
		gpsOff++
	}
	entry := func(tag, typ uint16, count, value uint32) {
		_ = binary.Write(&t, le, tag)
		_ = binary.Write(&t, le, typ)
		_ = binary.Write(&t, le, count)
		_ = binary.Write(&t, le, value)
	}
	_ = binary.Write(&t, le, uint16(2))
	entry(0x010F, 2, uint32(len(make)), makeOff) // Make, ASCII
	entry(0x8825, 4, 1, gpsOff)                  // GPSInfo, LONG
	_ = binary.Write(&t, le, uint32(0))
	t.Write(make)
	for uint32(t.Len()) < gpsOff {
		t.WriteByte(0)
	}
	// GPS IFD: 4 mục → 2 + 48 + 4 = 54 byte; hai mảng RATIONAL×3 (24 byte mỗi mảng) theo sau.
	latOff := gpsOff + 2 + 4*12 + 4
	lonOff := latOff + 24
	ascii2 := func(s string) uint32 { return uint32(s[0]) } // "N\0" nằm gọn trong 4 byte giá trị
	_ = binary.Write(&t, le, uint16(4))
	entry(0x0001, 2, 2, ascii2("N"))
	entry(0x0002, 5, 3, latOff)
	entry(0x0003, 2, 2, ascii2("E"))
	entry(0x0004, 5, 3, lonOff)
	_ = binary.Write(&t, le, uint32(0))
	for _, v := range []uint32{21, 1, 1, 1, 30, 1, 105, 1, 51, 1, 12, 1} {
		_ = binary.Write(&t, le, v)
	}
	payload := append([]byte("Exif\x00\x00"), t.Bytes()...)
	seg := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(payload)+2))
	return append(seg, payload...)
}

// pixelBomb: PNG xám 8 bit khai w×h; IDAT nén các hàng toàn số 0 tới khi dữ liệu nén chạm target byte.
func pixelBomb(w, h uint32, target int) []byte {
	var out bytes.Buffer
	out.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'})
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = 8, 0 // 8 bit, xám
	chunk(&out, "IHDR", ihdr)
	var z bytes.Buffer
	zw, _ := zlib.NewWriterLevel(&z, zlib.BestCompression)
	row := make([]byte, w+1)
	for z.Len() < target {
		_, _ = zw.Write(row)
	}
	_ = zw.Close()
	chunk(&out, "IDAT", z.Bytes())
	chunk(&out, "IEND", nil)
	return out.Bytes()
}

func chunk(out *bytes.Buffer, typ string, data []byte) {
	_ = binary.Write(out, binary.BigEndian, uint32(len(data)))
	out.WriteString(typ)
	out.Write(data)
	c := crc32.NewIEEE()
	c.Write([]byte(typ))
	c.Write(data)
	_ = binary.Write(out, binary.BigEndian, c.Sum32())
}
