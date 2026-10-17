// Command mkico 将 PNG 转换为多尺寸 Windows 图标（.ico，PNG 压缩条目）。
// 用法：go run ./scripts/mkico -in logo.png -out build/windows/icon.ico
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
)

func main() {
	in := flag.String("in", "logo.png", "输入 PNG 文件")
	out := flag.String("out", "build/windows/icon.ico", "输出 ICO 文件")
	flag.Parse()

	f, err := os.Open(*in)
	if err != nil {
		fmt.Fprintln(os.Stderr, "打开输入失败:", err)
		os.Exit(1)
	}
	defer f.Close()
	src, err := png.Decode(f)
	if err != nil {
		fmt.Fprintln(os.Stderr, "解码 PNG 失败:", err)
		os.Exit(1)
	}

	sizes := []int{16, 24, 32, 48, 64, 128, 256}
	datas := make([][]byte, 0, len(sizes))
	for _, s := range sizes {
		var buf bytes.Buffer
		if err := png.Encode(&buf, resize(src, s)); err != nil {
			fmt.Fprintln(os.Stderr, "编码 PNG 失败:", err)
			os.Exit(1)
		}
		datas = append(datas, buf.Bytes())
	}

	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ico, err := buildICO(sizes, datas)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, ico, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("已生成图标: %s（尺寸 %v，共 %d 字节）\n", *out, sizes, len(ico))
}

// resize 面积平均（box filter）缩放到 size×size。
func resize(src image.Image, size int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	b := src.Bounds()
	for y := 0; y < size; y++ {
		y0 := b.Min.Y + y*b.Dy()/size
		y1 := b.Min.Y + (y+1)*b.Dy()/size
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < size; x++ {
			x0 := b.Min.X + x*b.Dx()/size
			x1 := b.Min.X + (x+1)*b.Dx()/size
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var r, g, bl, a, n uint64
			for yy := y0; yy < y1; yy++ {
				for xx := x0; xx < x1; xx++ {
					cr, cg, cb, ca := src.At(xx, yy).RGBA()
					r += uint64(cr)
					g += uint64(cg)
					bl += uint64(cb)
					a += uint64(ca)
					n++
				}
			}
			if n == 0 {
				n = 1
			}
			dst.Set(x, y, color.RGBA64{
				R: uint16(r / n), G: uint16(g / n), B: uint16(bl / n), A: uint16(a / n),
			})
		}
	}
	return dst
}

// buildICO 组装 ICO（ICONDIR + ICONDIRENTRY + PNG 数据）。
func buildICO(sizes []int, datas [][]byte) ([]byte, error) {
	if len(sizes) != len(datas) {
		return nil, fmt.Errorf("sizes/datas 长度不一致")
	}
	var buf bytes.Buffer
	// ICONDIR: reserved(2) type(2)=1 count(2)
	binary.Write(&buf, binary.LittleEndian, uint16(0))
	binary.Write(&buf, binary.LittleEndian, uint16(1))
	binary.Write(&buf, binary.LittleEndian, uint16(len(sizes)))
	offset := 6 + 16*len(sizes)
	for i, s := range sizes {
		w := byte(s)
		if s >= 256 {
			w = 0 // 256 用 0 表示
		}
		buf.WriteByte(w)  // width
		buf.WriteByte(w)  // height
		buf.WriteByte(0)  // color count
		buf.WriteByte(0)  // reserved
		binary.Write(&buf, binary.LittleEndian, uint16(1))  // planes
		binary.Write(&buf, binary.LittleEndian, uint16(32)) // bit count
		binary.Write(&buf, binary.LittleEndian, uint32(len(datas[i])))
		binary.Write(&buf, binary.LittleEndian, uint32(offset))
		offset += len(datas[i])
	}
	for _, d := range datas {
		buf.Write(d)
	}
	return buf.Bytes(), nil
}
