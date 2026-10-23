package capture

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
)

// 最小 SFTP v3 客户端：只实现日志监听需要的 INIT/OPEN/STAT/READ/CLOSE。
const (
	sftpInit    = 1
	sftpVersion = 2
	sftpOpen    = 3
	sftpClose   = 4
	sftpRead    = 5
	sftpOpendir = 11
	sftpReaddir = 12
	sftpStat    = 17
	sftpName    = 104
	sftpStatus  = 101
	sftpHandle  = 102
	sftpData    = 103
	sftpAttrs   = 105
)

type sftpClient struct {
	ssh  *ssh.Client
	sess *ssh.Session
	in   io.WriteCloser
	out  *bufio.Reader
	mu   sync.Mutex
}

func newSFTPClient(cfg Config) (*sftpClient, error) {
	client, err := sshClient(cfg)
	if err != nil {
		return nil, err
	}
	sess, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("SFTP 会话失败: %v", err)
	}
	in, err := sess.StdinPipe()
	if err != nil {
		_ = sess.Close()
		_ = client.Close()
		return nil, err
	}
	out, err := sess.StdoutPipe()
	if err != nil {
		_ = sess.Close()
		_ = client.Close()
		return nil, err
	}
	if err := sess.RequestSubsystem("sftp"); err != nil {
		_ = sess.Close()
		_ = client.Close()
		return nil, fmt.Errorf("启动 SFTP 子系统失败: %v", err)
	}
	c := &sftpClient{ssh: client, sess: sess, in: in, out: bufio.NewReader(out)}
	if err := c.handshake(); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

func (c *sftpClient) handshake() error {
	if err := c.send(sftpInit, appendU32(nil, 3)); err != nil {
		return err
	}
	typ, payload, err := c.recv()
	if err != nil {
		return err
	}
	if typ != sftpVersion {
		return fmt.Errorf("SFTP 握手失败: 类型 %d", typ)
	}
	if len(payload) < 4 || binary.BigEndian.Uint32(payload) < 3 {
		return fmt.Errorf("SFTP 服务端版本不支持")
	}
	return nil
}

func (c *sftpClient) send(typ byte, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	buf := make([]byte, 5+len(payload))
	binary.BigEndian.PutUint32(buf, uint32(len(payload)+1))
	buf[4] = typ
	copy(buf[5:], payload)
	_, err := c.in.Write(buf)
	return err
}

func (c *sftpClient) recv() (byte, []byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var hdr [4]byte
	if _, err := io.ReadFull(c.out, hdr[:]); err != nil {
		return 0, nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n == 0 || n > 16<<20 {
		return 0, nil, fmt.Errorf("SFTP 包长度非法: %d", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(c.out, buf); err != nil {
		return 0, nil, err
	}
	return buf[0], buf[1:], nil
}

func (c *sftpClient) Open(path string) (string, error) {
	payload := appendString(nil, path)
	payload = appendU32(payload, 1) // SSH_FXF_READ
	payload = appendU32(payload, 0) // attrs flags
	if err := c.send(sftpOpen, payload); err != nil {
		return "", err
	}
	typ, resp, err := c.recv()
	if err != nil {
		return "", err
	}
	switch typ {
	case sftpHandle:
		h, _ := readString(resp)
		if h == "" {
			return "", fmt.Errorf("SFTP OPEN 未返回句柄")
		}
		return h, nil
	case sftpStatus:
		return "", sftpStatusError(resp)
	default:
		return "", fmt.Errorf("SFTP OPEN 响应错误: %d", typ)
	}
}

func (c *sftpClient) Stat(path string) (int64, error) {
	if err := c.send(sftpStat, appendString(nil, path)); err != nil {
		return 0, err
	}
	typ, resp, err := c.recv()
	if err != nil {
		return 0, err
	}
	if typ == sftpStatus {
		return 0, sftpStatusError(resp)
	}
	if typ != sftpAttrs {
		return 0, fmt.Errorf("SFTP STAT 响应错误: %d", typ)
	}
	return attrsSize(resp)
}

func (c *sftpClient) Read(handle string, offset int64, length uint32) ([]byte, bool, error) {
	payload := appendString(nil, handle)
	payload = appendU64(payload, uint64(offset))
	payload = appendU32(payload, length)
	if err := c.send(sftpRead, payload); err != nil {
		return nil, false, err
	}
	typ, resp, err := c.recv()
	if err != nil {
		return nil, false, err
	}
	switch typ {
	case sftpData:
		if len(resp) < 4 {
			return nil, false, fmt.Errorf("SFTP DATA 数据不完整")
		}
		n := int(binary.BigEndian.Uint32(resp))
		if n < 0 || 4+n > len(resp) {
			return nil, false, fmt.Errorf("SFTP DATA 长度非法")
		}
		return resp[4 : 4+n], true, nil
	case sftpStatus:
		return nil, false, sftpStatusError(resp)
	default:
		return nil, false, fmt.Errorf("SFTP READ 响应错误: %d", typ)
	}
}

func (c *sftpClient) CloseHandle(handle string) {
	_ = c.send(sftpClose, appendString(nil, handle))
	_, _, _ = c.recv()
}

// ReadDir 列出远端目录（SSH_FXP_OPENDIR/READDIR）。path 为空时用 "."。
func (c *sftpClient) ReadDir(path string) ([]RemoteFile, error) {
	if strings.TrimSpace(path) == "" {
		path = "."
	}
	payload := appendString(nil, path)
	if err := c.send(sftpOpendir, payload); err != nil {
		return nil, err
	}
	typ, resp, err := c.recv()
	if err != nil {
		return nil, err
	}
	if typ == sftpStatus {
		return nil, sftpStatusError(resp)
	}
	if typ != sftpHandle {
		return nil, fmt.Errorf("SFTP OPENDIR 响应错误: %d", typ)
	}
	handle, _ := readString(resp)
	if handle == "" {
		return nil, fmt.Errorf("SFTP OPENDIR 未返回句柄")
	}
	var out []RemoteFile
	for {
		if err := c.send(sftpReaddir, appendString(nil, handle)); err != nil {
			break
		}
		typ, resp, err = c.recv()
		if err != nil || typ == sftpStatus {
			break // SSH_FX_EOF 结束
		}
		if typ != sftpName {
			break
		}
		rest := resp
		for len(rest) > 0 {
			name, r2 := readString(rest)
			if r2 == nil {
				break
			}
			rest = r2
			a, r3, aerr := parseAttrs(rest)
			if aerr != nil {
				break
			}
			rest = r3
			if name == "." || name == ".." {
				continue
			}
			out = append(out, RemoteFile{Name: name, Dir: a.dir, Size: a.size})
		}
	}
	c.CloseHandle(handle)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func (c *sftpClient) Close() error {
	if c.in != nil {
		_ = c.in.Close()
	}
	if c.sess != nil {
		_ = c.sess.Close()
	}
	if c.ssh != nil {
		return c.ssh.Close()
	}
	return nil
}

func appendU32(b []byte, v uint32) []byte {
	var x [4]byte
	binary.BigEndian.PutUint32(x[:], v)
	return append(b, x[:]...)
}

func appendU64(b []byte, v uint64) []byte {
	var x [8]byte
	binary.BigEndian.PutUint64(x[:], v)
	return append(b, x[:]...)
}

func appendString(b []byte, s string) []byte {
	b = appendU32(b, uint32(len(s)))
	return append(b, s...)
}

func readString(b []byte) (string, []byte) {
	if len(b) < 4 {
		return "", nil
	}
	n := int(binary.BigEndian.Uint32(b))
	if n < 0 || 4+n > len(b) {
		return "", nil
	}
	return string(b[4 : 4+n]), b[4+n:]
}

func attrsSize(p []byte) (int64, error) {
	a, rest, err := parseAttrs(p)
	if err != nil {
		return 0, err
	}
	if !a.hasSize {
		return 0, fmt.Errorf("SFTP STAT 未返回文件大小")
	}
	_ = rest
	return a.size, nil
}

type remoteAttrs struct {
	dir     bool
	hasSize bool
	size    int64
}

// parseAttrs 解析 SFTP v3 ATTRS：flags + size/uid/gid/perm/times。
func parseAttrs(p []byte) (remoteAttrs, []byte, error) {
	var a remoteAttrs
	if len(p) < 4 {
		return a, nil, fmt.Errorf("SFTP ATTRS 数据不完整")
	}
	flags := binary.BigEndian.Uint32(p)
	p = p[4:]
	if flags&1 != 0 { // SIZE
		if len(p) < 8 {
			return a, nil, fmt.Errorf("SFTP ATTRS size 不完整")
		}
		a.size = int64(binary.BigEndian.Uint64(p))
		a.hasSize = true
		p = p[8:]
	}
	if flags&2 != 0 { // UIDGID
		if len(p) < 8 {
			return a, nil, fmt.Errorf("SFTP ATTRS uid/gid 不完整")
		}
		p = p[8:]
	}
	if flags&4 != 0 { // PERMISSIONS
		if len(p) < 4 {
			return a, nil, fmt.Errorf("SFTP ATTRS perms 不完整")
		}
		perm := binary.BigEndian.Uint32(p)
		a.dir = perm&0xF000 == 0x4000
		p = p[4:]
	}
	// EXTENDED(0x80000000) 不解析；ACMODTIME 只是时间，可忽略。
	return a, p, nil
}

func sftpStatusError(p []byte) error {
	if len(p) < 4 {
		return fmt.Errorf("SFTP 状态错误")
	}
	code := binary.BigEndian.Uint32(p)
	msg, _ := readString(p[4:])
	if msg == "" {
		msg = "未知错误"
	}
	if code == 1 {
		return io.EOF
	}
	return fmt.Errorf("SFTP 错误 %d: %s", code, msg)
}

// syncSFTP 拉取 SFTP 文件增量。
func (e *Engine) syncSFTP(flush bool) error {
	c, err := newSFTPClient(e.cfg)
	if err != nil {
		return err
	}
	defer c.Close()
	size, err := c.Stat(e.cfg.LogPath)
	if err != nil {
		return err
	}
	handle, err := c.Open(e.cfg.LogPath)
	if err != nil {
		return err
	}
	defer c.CloseHandle(handle)
	e.remoteMu.Lock()
	offset := e.remoteOffset
	if size < offset {
		offset = 0
		e.remoteOffset = 0
		e.remotePending = nil
	}
	e.remoteMu.Unlock()
	for offset < size {
		n := uint32(64 * 1024)
		if int64(n) > size-offset {
			n = uint32(size - offset)
		}
		data, ok, err := c.Read(handle, offset, n)
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		if !ok || len(data) == 0 {
			break
		}
		e.consumeRemoteChunk(data, false)
		offset += int64(len(data))
	}
	if flush {
		e.flushRemotePending()
	}
	return nil
}

// startSFTPLog 启动 SFTP 监听。
func (e *Engine) startSFTPLog() error {
	if strings.TrimSpace(e.cfg.LogHost) == "" || strings.TrimSpace(e.cfg.LogUser) == "" {
		return fmt.Errorf("SFTP 需要填写终端 IP 和用户名")
	}
	if e.cfg.LogPassword == "" && strings.TrimSpace(e.cfg.LogKeyPath) == "" {
		return fmt.Errorf("SFTP 需要密码或 SSH 私钥")
	}
	e.remoteKind = "sftp"
	if strings.ToLower(e.cfg.LogWatchMode) == "static" {
		if err := e.syncRemote(true); err != nil {
			return err
		}
		e.status("log", fmt.Sprintf("SFTP 静态监听已就绪：%s@%s:%s", e.cfg.LogUser, e.cfg.LogHost, e.cfg.LogPath))
		return nil
	}
	e.status("log", fmt.Sprintf("SFTP 动态监听已启动：%s@%s:%s", e.cfg.LogUser, e.cfg.LogHost, e.cfg.LogPath))
	e.wg.Add(1)
	go e.remotePollLoop()
	return nil
}
