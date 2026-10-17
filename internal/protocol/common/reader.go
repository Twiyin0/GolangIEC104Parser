package common

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// Reader 带游标的字节读取器（对应参考实现中贯穿 offset 的用法）。
type Reader struct {
	B   []byte
	Off int
}

func NewReader(b []byte, off int) *Reader { return &Reader{B: b, Off: off} }

func (r *Reader) Left() int  { return len(r.B) - r.Off }
func (r *Reader) Need(n int) error {
	if r.Left() < n {
		return fmt.Errorf("buffer too short: need %d more bytes", n)
	}
	return nil
}

func (r *Reader) Skip(n int) *Reader { r.Off += n; return r }

func (r *Reader) U8() int { v := int(r.B[r.Off]); r.Off++; return v }

func (r *Reader) U16() int { v := int(binary.LittleEndian.Uint16(r.B[r.Off:])); r.Off += 2; return v }

// U24 读取 3 字节信息对象地址（104 IOA）。
func (r *Reader) U24() int {
	v := int(r.B[r.Off]) | int(r.B[r.Off+1])<<8 | int(r.B[r.Off+2])<<16
	r.Off += 3
	return v
}

func (r *Reader) U32() uint32 { v := binary.LittleEndian.Uint32(r.B[r.Off:]); r.Off += 4; return v }

func (r *Reader) I16() int { v := int(int16(binary.LittleEndian.Uint16(r.B[r.Off:]))); r.Off += 2; return v }

func (r *Reader) F32() float64 {
	v := float64(math.Float32frombits(binary.LittleEndian.Uint32(r.B[r.Off:])))
	r.Off += 4
	return v
}

func (r *Reader) F64() float64 {
	v := math.Float64frombits(binary.LittleEndian.Uint64(r.B[r.Off:]))
	r.Off += 8
	return v
}

// Slice 取 n 字节切片（越界时取剩余全部，容错与参考实现一致）。
func (r *Reader) Slice(n int) []byte {
	if r.Left() < n {
		n = r.Left()
	}
	v := r.B[r.Off : r.Off+n]
	r.Off += n
	return v
}

func (r *Reader) Ascii(n int) string { return asciiString(r.Slice(n)) }

func (r *Reader) Hex(n int) string { return hexEncode(r.Slice(n)) }

// CP56 读 7 字节时标。
func (r *Reader) CP56() (string, error) { return ParseCP56Time2a(r.B, r.Off) }

// ErrTooShort 统一长度不足错误。
var ErrTooShort = errors.New("message too short")
