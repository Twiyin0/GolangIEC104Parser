package capture

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"path"
	"strconv"
	"strings"
	"time"
)

// 最小 FTP 客户端：PASV 被动模式 + SIZE/REST/RETR，满足日志增量读取。
type ftpClient struct {
	conn net.Conn
	r    *bufio.Reader
	w    *bufio.Writer
}

func newFTPClient(cfg Config) (*ftpClient, error) {
	port := cfg.LogPort
	if port <= 0 {
		port = 21
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(cfg.LogHost, strconv.Itoa(port)), 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("FTP 连接失败: %v", err)
	}
	c := &ftpClient{conn: conn, r: bufio.NewReader(conn), w: bufio.NewWriter(conn)}
	if code, msg, err := c.readResp(); err != nil || code/100 != 2 {
		_ = conn.Close()
		return nil, fmt.Errorf("FTP 欢迎信息异常: %d %s (%v)", code, msg, err)
	}
	if err := c.login(cfg.LogUser, cfg.LogPassword); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if _, _, err := c.cmd("TYPE I"); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return c, nil
}

func (c *ftpClient) login(user, pass string) error {
	code, _, err := c.cmd("USER " + sanitizeFTPArg(user))
	if err != nil {
		return err
	}
	if code == 331 {
		code, _, err = c.cmd("PASS " + sanitizeFTPArg(pass))
		if err != nil {
			return err
		}
	}
	if code/100 != 2 {
		return fmt.Errorf("FTP 登录失败: %d", code)
	}
	return nil
}

// cmd 发送命令并读取一条（可能多行）响应。
func (c *ftpClient) cmd(line string) (int, string, error) {
	if _, err := c.w.WriteString(line + "\r\n"); err != nil {
		return 0, "", err
	}
	if err := c.w.Flush(); err != nil {
		return 0, "", err
	}
	return c.readResp()
}

func (c *ftpClient) readResp() (int, string, error) {
	line, err := c.r.ReadString('\n')
	if err != nil {
		return 0, "", err
	}
	line = strings.TrimRight(line, "\r\n")
	if len(line) < 3 {
		return 0, line, fmt.Errorf("FTP 响应过短")
	}
	code, err := strconv.Atoi(line[:3])
	if err != nil {
		return 0, line, fmt.Errorf("FTP 响应码非法: %s", line)
	}
	// 多行响应：首行第 4 字符为 '-'，直到同码 + 空格。
	if len(line) > 3 && line[3] == '-' {
		prefix := line[:3] + " "
		var b strings.Builder
		b.WriteString(line)
		for {
			next, err := c.r.ReadString('\n')
			if err != nil {
				return code, b.String(), err
			}
			next = strings.TrimRight(next, "\r\n")
			b.WriteString("\n")
			b.WriteString(next)
			if strings.HasPrefix(next, prefix) {
				break
			}
		}
		return code, b.String(), nil
	}
	return code, line, nil
}

func (c *ftpClient) Size(path string) (int64, error) {
	code, msg, err := c.cmd("SIZE " + sanitizeFTPArg(path))
	if err != nil {
		return 0, err
	}
	if code != 213 {
		return 0, fmt.Errorf("FTP SIZE 失败: %s", msg)
	}
	fields := strings.Fields(msg)
	if len(fields) < 2 {
		return 0, fmt.Errorf("FTP SIZE 响应非法: %s", msg)
	}
	return strconv.ParseInt(fields[1], 10, 64)
}

// RetrFrom 返回从 offset 开始的数据；服务端不支持 REST 时自动回退全量并切片。
func (c *ftpClient) RetrFrom(path string, offset int64) ([]byte, error) {
	host, port, err := c.pasv()
	if err != nil {
		return nil, err
	}
	dataConn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("FTP 数据连接失败: %v", err)
	}
	defer dataConn.Close()

	restOK := false
	if offset > 0 {
		code, _, err := c.cmd("REST " + strconv.FormatInt(offset, 10))
		if err == nil && code == 350 {
			restOK = true
		}
	}
	code, msg, err := c.cmd("RETR " + sanitizeFTPArg(path))
	if err != nil {
		return nil, err
	}
	if code != 150 && code != 125 {
		return nil, fmt.Errorf("FTP RETR 失败: %d %s", code, msg)
	}
	data, err := io.ReadAll(dataConn)
	if err != nil {
		return nil, fmt.Errorf("FTP 读取数据失败: %v", err)
	}
	if code, msg, err = c.readResp(); err != nil || code/100 != 2 {
		return nil, fmt.Errorf("FTP 传输未完成: %d %s (%v)", code, msg, err)
	}
	if !restOK && offset > 0 {
		if int64(len(data)) <= offset {
			return nil, nil
		}
		data = data[offset:]
	}
	return data, nil
}

func (c *ftpClient) pasv() (string, int, error) {
	code, msg, err := c.cmd("PASV")
	if err != nil {
		return "", 0, err
	}
	if code != 227 {
		return "", 0, fmt.Errorf("FTP PASV 失败: %s", msg)
	}
	l := strings.Index(msg, "(")
	r := strings.Index(msg, ")")
	if l < 0 || r <= l {
		return "", 0, fmt.Errorf("FTP PASV 响应非法: %s", msg)
	}
	parts := strings.Split(msg[l+1:r], ",")
	if len(parts) != 6 {
		return "", 0, fmt.Errorf("FTP PASV 地址非法: %s", msg)
	}
	nums := make([]int, 6)
	for i, p := range parts {
		nums[i], err = strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return "", 0, fmt.Errorf("FTP PASV 端口非法: %s", msg)
		}
	}
	host := fmt.Sprintf("%d.%d.%d.%d", nums[0], nums[1], nums[2], nums[3])
	port := nums[4]<<8 | nums[5]
	return host, port, nil
}

func (c *ftpClient) Close() error {
	_, _, _ = c.cmd("QUIT")
	return c.conn.Close()
}

// List 列出 FTP 目录（PASV + LIST）。
func (c *ftpClient) List(dir string) ([]RemoteFile, error) {
	host, port, err := c.pasv()
	if err != nil {
		return nil, err
	}
	dataConn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("FTP 数据连接失败: %v", err)
	}
	defer dataConn.Close()
	cmd := "LIST"
	if strings.TrimSpace(dir) != "" {
		cmd = "LIST " + sanitizeFTPArg(dir)
	}
	code, msg, err := c.cmd(cmd)
	if err != nil {
		return nil, err
	}
	if code != 150 && code != 125 {
		return nil, fmt.Errorf("FTP LIST 失败: %d %s", code, msg)
	}
	data, err := io.ReadAll(dataConn)
	if err != nil {
		return nil, fmt.Errorf("FTP 读取目录失败: %v", err)
	}
	if code, msg, err = c.readResp(); err != nil || code/100 != 2 {
		return nil, fmt.Errorf("FTP 目录传输未完成: %d %s (%v)", code, msg, err)
	}
	return parseFTPList(string(data), dir), nil
}

// parseFTPList 解析 UNIX LIST 输出（忽略 total/其它格式行）。
func parseFTPList(out, dir string) []RemoteFile {
	var files []RemoteFile
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		fields := strings.Fields(line)
		if len(fields) < 9 || (!strings.HasPrefix(fields[0], "-") && !strings.HasPrefix(fields[0], "d") && !strings.HasPrefix(fields[0], "l")) {
			continue
		}
		name := strings.Join(fields[8:], " ")
		if i := strings.Index(name, " -> "); i >= 0 {
			name = name[:i]
		}
		if name == "." || name == ".." {
			continue
		}
		isDir := fields[0][0] == 'd'
		var size int64
		if !isDir {
			size, _ = strconv.ParseInt(fields[4], 10, 64)
		}
		files = append(files, RemoteFile{Name: name, Path: path.Join(dir, name), Dir: isDir, Size: size})
	}
	return files
}

func sanitizeFTPArg(s string) string {
	return strings.NewReplacer("\r", "", "\n", "").Replace(s)
}

// syncFTP 拉取 FTP 文件增量。
func (e *Engine) syncFTP(flush bool) error {
	c, err := newFTPClient(e.cfg)
	if err != nil {
		return err
	}
	defer c.Close()
	size, sizeErr := c.Size(e.cfg.LogPath)
	e.remoteMu.Lock()
	offset := e.remoteOffset
	if sizeErr == nil && size < offset {
		offset = 0
		e.remoteOffset = 0
		e.remotePending = nil
	}
	e.remoteMu.Unlock()
	data, err := c.RetrFrom(e.cfg.LogPath, offset)
	if err != nil {
		return err
	}
	e.consumeRemoteChunk(data, false)
	if flush {
		e.flushRemotePending()
	}
	return nil
}

// startFTPLog 启动 FTP 监听。
func (e *Engine) startFTPLog() error {
	if strings.TrimSpace(e.cfg.LogHost) == "" || strings.TrimSpace(e.cfg.LogUser) == "" {
		return fmt.Errorf("FTP 需要填写终端 IP 和用户名")
	}
	e.remoteKind = "ftp"
	if strings.ToLower(e.cfg.LogWatchMode) == "static" {
		if err := e.syncRemote(true); err != nil {
			return err
		}
		e.status("log", fmt.Sprintf("FTP 静态监听已就绪：%s@%s:%s", e.cfg.LogUser, e.cfg.LogHost, e.cfg.LogPath))
		return nil
	}
	e.status("log", fmt.Sprintf("FTP 动态监听已启动：%s@%s:%s", e.cfg.LogUser, e.cfg.LogHost, e.cfg.LogPath))
	e.wg.Add(1)
	go e.remotePollLoop()
	return nil
}
