package capture

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"iec104rt/internal/logfile"
)

// startSSHLog 启动 SSH 日志监听。scp=true 时静态下载一次（动态仍用 tail -F）。
func (e *Engine) startSSHLog(isSCP bool) error {
	if strings.TrimSpace(e.cfg.LogHost) == "" || strings.TrimSpace(e.cfg.LogUser) == "" {
		return fmt.Errorf("SSH/SCP 需要填写终端 IP 和用户名")
	}
	if e.cfg.LogPassword == "" && strings.TrimSpace(e.cfg.LogKeyPath) == "" {
		return fmt.Errorf("SSH/SCP 需要密码或 SSH 私钥")
	}
	e.remoteKind = "ssh"
	if isSCP {
		e.remoteKind = "scp"
	}
	if strings.ToLower(e.cfg.LogWatchMode) == "static" {
		if err := e.syncRemote(true); err != nil {
			return err
		}
		e.status("log", fmt.Sprintf("%s 静态监听已就绪：%s@%s:%s（点击“更新日志”读取增量）",
			strings.ToUpper(e.remoteKind), e.cfg.LogUser, e.cfg.LogHost, e.cfg.LogPath))
		return nil
	}
	e.status("log", fmt.Sprintf("%s 动态监听已启动：%s@%s:%s（tail -F）",
		strings.ToUpper(e.remoteKind), e.cfg.LogUser, e.cfg.LogHost, e.cfg.LogPath))
	e.wg.Add(1)
	go e.sshTailLoop()
	return nil
}

// sshClient 建立 SSH 连接（现场工具：不校验 known_hosts，优先密码/私钥）。
func sshClient(cfg Config) (*ssh.Client, error) {
	var auth []ssh.AuthMethod
	if cfg.LogPassword != "" {
		auth = append(auth, ssh.Password(cfg.LogPassword))
	}
	if strings.TrimSpace(cfg.LogKeyPath) != "" {
		data, err := os.ReadFile(cfg.LogKeyPath)
		if err != nil {
			return nil, fmt.Errorf("读取 SSH 私钥失败: %v", err)
		}
		signer, err := ssh.ParsePrivateKey(data)
		if err != nil {
			return nil, fmt.Errorf("解析 SSH 私钥失败: %v", err)
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if len(auth) == 0 {
		return nil, fmt.Errorf("SSH 需要密码或私钥")
	}
	port := cfg.LogPort
	if port <= 0 {
		port = 22
	}
	client, err := ssh.Dial("tcp", net.JoinHostPort(cfg.LogHost, strconv.Itoa(port)), &ssh.ClientConfig{
		User:            cfg.LogUser,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("SSH 连接失败（请确认 sshd 已启动、端口开放）: %v", err)
	}
	return client, nil
}

// sshTailLoop 断线自动重连，使用 tail -F 持续读取远端日志。
func (e *Engine) sshTailLoop() {
	defer e.wg.Done()
	delay := time.Second
	for {
		select {
		case <-e.stopCh:
			return
		default:
		}
		err := e.sshTailOnce()
		select {
		case <-e.stopCh:
			// 用户主动停止：不要把关闭连接当成 SSH 失败。
			return
		default:
		}
		if err != nil {
			e.setLogErr(err.Error())
			e.status("log", "SSH 日志流中断: "+err.Error())
		}
		select {
		case <-e.stopCh:
			return
		case <-time.After(delay):
		}
		if delay < 10*time.Second {
			delay *= 2
		}
	}
}

func (e *Engine) sshTailOnce() error {
	client, err := sshClient(e.cfg)
	if err != nil {
		return err
	}
	e.setRemoteCloser(client)
	defer func() {
		e.setRemoteCloser(nil)
		_ = client.Close()
	}()
	sess, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("SSH 会话失败: %v", err)
	}
	defer sess.Close()
	stdout, err := sess.StdoutPipe()
	if err != nil {
		return err
	}
	if err := sess.Start("tail -F " + shellQuote(e.cfg.LogPath)); err != nil {
		return fmt.Errorf("启动 tail -F 失败: %v", err)
	}
	// 命令启动成功即视为链路恢复：清除上次错误，状态栏显示成功。
	e.clearLogErr()
	e.status("log", fmt.Sprintf("SSH 日志连接成功：%s@%s:%s（tail -F）",
		e.cfg.LogUser, e.cfg.LogHost, e.cfg.LogPath))
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for sc.Scan() {
		e.ingestLine(sc.Text(), time.Now().Format("2006-01-02 15:04:05.000"))
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("读取 tail 输出失败: %v", err)
	}
	return fmt.Errorf("tail -F 已结束")
}

func (e *Engine) setRemoteCloser(c io.Closer) {
	e.remoteMu.Lock()
	e.remoteCloser = c
	e.remoteMu.Unlock()
}

// syncRemote 静态/增量拉取远程文件；ssh/scp 走 cat，sftp/ftp 走各自客户端。
func (e *Engine) syncRemote(flush bool) error {
	switch e.remoteKind {
	case "ssh", "scp":
		return e.syncSSH(flush)
	case "sftp":
		return e.syncSFTP(flush)
	case "ftp":
		return e.syncFTP(flush)
	}
	return fmt.Errorf("未知远程日志类型: %s", e.remoteKind)
}

// syncSSH 读取远端文件全量，并按本地已读 offset 只投递新增部分。
func (e *Engine) syncSSH(flush bool) error {
	client, err := sshClient(e.cfg)
	if err != nil {
		return err
	}
	defer client.Close()
	sess, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("SSH 会话失败: %v", err)
	}
	defer sess.Close()
	out, err := sess.Output("cat " + shellQuote(e.cfg.LogPath))
	if err != nil {
		return fmt.Errorf("读取远端文件失败: %v", err)
	}
	e.consumeRemoteBytes(out, flush)
	return nil
}

// consumeRemoteBytes 把远程文件字节按 offset 切片、按行拆分并投递。
// offset 以远端文件字节数为准；pending 保存末尾无换行的半行。
func (e *Engine) consumeRemoteBytes(data []byte, flush bool) {
	e.remoteMu.Lock()
	offset := e.remoteOffset
	if int64(len(data)) < offset {
		offset = 0
		e.remotePending = nil
	}
	if offset > int64(len(data)) {
		offset = int64(len(data))
	}
	chunk := append([]byte(nil), data[offset:]...)
	e.remoteOffset = int64(len(data))
	combined := append(e.remotePending, chunk...)
	e.remotePending = nil
	lines, pending := splitLogBytes(combined, e.cfg.LogEncoding, flush)
	e.remotePending = pending
	e.remoteMu.Unlock()
	e.ingestLines(lines)
}

// consumeRemoteChunk 追加远程增量字节，按行投递；同时推进远端 offset。
func (e *Engine) consumeRemoteChunk(chunk []byte, flush bool) {
	if len(chunk) == 0 && !flush {
		return
	}
	e.remoteMu.Lock()
	combined := append(e.remotePending, chunk...)
	e.remotePending = nil
	lines, pending := splitLogBytes(combined, e.cfg.LogEncoding, flush)
	e.remotePending = pending
	e.remoteOffset += int64(len(chunk))
	e.remoteMu.Unlock()
	e.ingestLines(lines)
}

// flushRemotePending 把末尾无换行的半行作为完整日志行处理。
func (e *Engine) flushRemotePending() {
	e.remoteMu.Lock()
	if len(e.remotePending) == 0 {
		e.remoteMu.Unlock()
		return
	}
	pending := append([]byte(nil), e.remotePending...)
	e.remotePending = nil
	e.remoteMu.Unlock()
	lines, _ := splitLogBytes(pending, e.cfg.LogEncoding, true)
	e.ingestLines(lines)
}

// remotePollLoop SFTP/FTP 动态监听：定时拉取远端文件增量。
func (e *Engine) remotePollLoop() {
	defer e.wg.Done()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	lastStatus := time.Now()
	var lastLines, lastFrames int64
	hadErr := false
	for {
		select {
		case <-e.stopCh:
			return
		case <-ticker.C:
			if err := e.syncRemote(false); err != nil {
				hadErr = true
				e.setLogErr(err.Error())
				e.status("log", "远程日志读取失败: "+err.Error())
			} else if hadErr {
				hadErr = false
				e.clearLogErr()
				e.status("log", fmt.Sprintf("%s 日志连接已恢复：%s:%s",
					strings.ToUpper(e.remoteKind), e.cfg.LogHost, e.cfg.LogPath))
			}
			if time.Since(lastStatus) >= 2*time.Second {
				st := e.LogStats()
				if st.Lines != lastLines || st.Frames != lastFrames {
					lastLines, lastFrames = st.Lines, st.Frames
					e.status("log", fmt.Sprintf("远程日志: 已读 %d 行，解析 %d 帧，忽略 %d 行",
						st.Lines, st.Frames, st.Ignored))
				}
				lastStatus = time.Now()
			}
		}
	}
}

// splitLogBytes 拆分行；flush=false 时保留最后半行。
func splitLogBytes(data []byte, encoding string, flush bool) ([]string, []byte) {
	parts := bytes.Split(data, []byte{'\n'})
	var pending []byte
	if len(parts) > 0 {
		pending = append([]byte(nil), parts[len(parts)-1]...)
		parts = parts[:len(parts)-1]
	}
	lines := make([]string, 0, len(parts))
	for _, p := range parts {
		p = bytes.TrimSuffix(p, []byte{'\r'})
		if len(p) > 0 {
			lines = append(lines, logfile.Decode(p, encoding))
		}
	}
	if flush && len(pending) > 0 {
		pending = bytes.TrimSuffix(pending, []byte{'\r'})
		if len(pending) > 0 {
			lines = append(lines, logfile.Decode(pending, encoding))
		}
		pending = nil
	}
	return lines, pending
}

// shellQuote 安全引用远端 shell 参数。
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
