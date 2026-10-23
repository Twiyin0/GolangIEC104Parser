package logfile

import (
	"bytes"
	"fmt"
	"os"
	"sync"
)

// Reader 本机日志文件增量读取器（支持轮转/截断、末尾半行缓存）。
type Reader struct {
	mu             sync.Mutex
	path           string
	encoding       string
	offset         int64
	lastSize       int64
	pending        []byte
	discardPartial bool // 从文件尾部启动时丢弃第一段半行
}

// NewReader 创建 reader，从文件 0 开始读取。
func NewReader(path, encoding string) (*Reader, error) {
	if path == "" {
		return nil, fmt.Errorf("日志文件路径为空")
	}
	return &Reader{path: path, encoding: encoding}, nil
}

// StartFromTail 动态监听从“文件末尾 - tail 字节”开始，避免把历史日志全部回放。
func (r *Reader) StartFromTail(tail int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, err := os.Stat(r.path)
	if err != nil {
		return fmt.Errorf("打开日志文件失败: %v", err)
	}
	r.lastSize = st.Size()
	r.offset = st.Size() - tail
	if r.offset < 0 {
		r.offset = 0
	}
	r.pending = nil
	r.discardPartial = false
	if r.offset > 0 {
		// 只有 offset 前一个字节不是换行时，第一段才是半行。
		if f, err := os.Open(r.path); err == nil {
			var b [1]byte
			if _, err := f.ReadAt(b[:], r.offset-1); err == nil && b[0] != '\n' {
				r.discardPartial = true
			}
			_ = f.Close()
		}
	}
	return nil
}

// ReadNew 读取自上次 offset 以来的新增内容；flush=true 时把末尾无换行的半行也作为一行返回。
// 返回的 lines 已按 encoding 解码。
func (r *Reader) ReadNew(flush bool) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	st, err := os.Stat(r.path)
	if err != nil {
		return nil, fmt.Errorf("读取日志文件失败: %v", err)
	}
	size := st.Size()
	// 文件轮转/截断：size 回退，重新从 0 读取。
	if size < r.offset || size < r.lastSize {
		r.offset = 0
		r.pending = nil
		r.discardPartial = false
	}
	r.lastSize = size
	if size == r.offset && !flush {
		return nil, nil
	}

	f, err := os.Open(r.path)
	if err != nil {
		return nil, fmt.Errorf("打开日志文件失败: %v", err)
	}
	defer f.Close()
	if _, err := f.Seek(r.offset, 0); err != nil {
		return nil, fmt.Errorf("定位日志文件失败: %v", err)
	}
	buf := make([]byte, size-r.offset)
	if len(buf) > 0 {
		n, err := f.Read(buf)
		if err != nil && n == 0 {
			return nil, fmt.Errorf("读取日志文件失败: %v", err)
		}
		buf = buf[:n]
	}
	r.offset += int64(len(buf))

	data := append(r.pending, buf...)
	r.pending = nil
	parts := bytes.Split(data, []byte{'\n'})
	if len(parts) > 0 {
		r.pending = append([]byte(nil), parts[len(parts)-1]...)
		parts = parts[:len(parts)-1]
	}
	var lines []string
	for _, p := range parts {
		p = bytes.TrimSuffix(p, []byte{'\r'})
		if len(p) == 0 {
			continue
		}
		lines = append(lines, Decode(p, r.encoding))
	}
	if r.discardPartial {
		r.discardPartial = false
		if len(lines) > 0 {
			lines = lines[1:] // 从文件中间启动时，第一段通常是被截断的半行
		}
	}
	if flush && len(r.pending) > 0 {
		p := bytes.TrimSuffix(r.pending, []byte{'\r'})
		r.pending = nil
		if len(p) > 0 {
			lines = append(lines, Decode(p, r.encoding))
		}
	}
	// 防止异常超长行占满内存。
	if len(r.pending) > 1<<20 {
		r.pending = nil
	}
	return lines, nil
}

// Offset 当前读取位置（供状态展示）。
func (r *Reader) Offset() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.offset
}
