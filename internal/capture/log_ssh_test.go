package capture

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"iec104rt/internal/protocol"
)

// testSSHServer 在测试内提供最小 SSH 服务，支持 cat / tail -F / wc -c。
type testSSHServer struct {
	ln       net.Listener
	path     string
	password string
	user     string
	done     chan struct{}
}

func newTestSSHServer(t *testing.T, path string) *testSSHServer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	s := &testSSHServer{path: path, user: "tester", password: "secret", done: make(chan struct{})}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == s.user && string(pass) == s.password {
				return nil, nil
			}
			return nil, fmt.Errorf("denied")
		},
	}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.ln = ln
	go s.accept(cfg)
	t.Cleanup(func() {
		close(s.done)
		_ = ln.Close()
		time.Sleep(150 * time.Millisecond)
	})
	return s
}

func (s *testSSHServer) port() int {
	return s.ln.Addr().(*net.TCPAddr).Port
}

func (s *testSSHServer) accept(cfg *ssh.ServerConfig) {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn, cfg)
	}
}

func (s *testSSHServer) handleConn(conn net.Conn, cfg *ssh.ServerConfig) {
	sshConn, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		_ = conn.Close()
		return
	}
	defer sshConn.Close()
	go ssh.DiscardRequests(reqs)
	for ch := range chans {
		if ch.ChannelType() != "session" {
			_ = ch.Reject(ssh.UnknownChannelType, "session only")
			continue
		}
		channel, requests, err := ch.Accept()
		if err != nil {
			continue
		}
		go func() {
			defer channel.Close()
			for req := range requests {
				if req.Type != "exec" {
					_ = req.Reply(false, nil)
					continue
				}
				var p struct{ Command string }
				_ = ssh.Unmarshal(req.Payload, &p)
				_ = req.Reply(true, nil)
				s.run(channel, p.Command)
				_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
				return
			}
		}()
	}
}

func (s *testSSHServer) run(ch io.ReadWriter, command string) {
	switch {
	case strings.Contains(command, "wc -c"):
		st, _ := os.Stat(s.path)
		fmt.Fprintf(ch, "%d\n", st.Size())
	case strings.Contains(command, "tail -F"):
		s.tail(ch)
	case strings.Contains(command, "cat "):
		data, _ := os.ReadFile(s.path)
		_, _ = ch.Write(data)
	}
}

func (s *testSSHServer) tail(ch io.ReadWriter) {
	f, err := os.Open(s.path)
	if err != nil {
		return
	}
	defer f.Close()
	offset := int64(0)
	buf := make([]byte, 64*1024)
	for {
		select {
		case <-s.done:
			return
		default:
		}
		n, err := f.Read(buf)
		if n > 0 {
			if _, werr := ch.Write(buf[:n]); werr != nil {
				return
			}
			offset += int64(n)
		}
		if err == io.EOF {
			time.Sleep(40 * time.Millisecond)
			if _, serr := f.Seek(offset, io.SeekStart); serr != nil {
				return
			}
			continue
		}
		if err != nil {
			return
		}
	}
}

func TestStartLogSSHStatic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "remote.log")
	line := "protolog:2026/10/18-16:36:40: Rx(18) <--- " + hexString(test101VarFrame()) + "\n"
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := newTestSSHServer(t, path)
	ch := make(chan protocol.Frame, 8)
	cfg := Config{
		LogProtocol: "scp", LogHost: "127.0.0.1", LogPort: srv.port(),
		LogUser: srv.user, LogPassword: srv.password, LogPath: path,
		LogWatchMode: "static", LogEncoding: "auto", LogTailBytes: 65536,
	}
	e, err := StartLog(cfg, func(f protocol.Frame) { ch <- f }, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Stop()
	if got := waitFrame(t, ch); got.Proto != "101" || got.Ti != 13 {
		t.Fatalf("SCP/SSH 静态解析错误: %+v", got)
	}
}

func TestStartLogSSHTail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "remote.log")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	srv := newTestSSHServer(t, path)
	ch := make(chan protocol.Frame, 8)
	cfg := Config{
		LogProtocol: "ssh", LogHost: "127.0.0.1", LogPort: srv.port(),
		LogUser: srv.user, LogPassword: srv.password, LogPath: path,
		LogWatchMode: "dynamic", LogEncoding: "auto", LogTailBytes: 65536,
	}
	e, err := StartLog(cfg, func(f protocol.Frame) { ch <- f }, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		e.Stop()
		time.Sleep(250 * time.Millisecond)
	}()
	time.Sleep(300 * time.Millisecond)
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	line := "protolog:2026/10/18-16:36:40: Rx(18) <--- " + hexString(test101VarFrame()) + "\n"
	_, _ = f.WriteString(line)
	_ = f.Close()
	if got := waitFrame(t, ch); got.Proto != "101" || got.Ti != 13 {
		t.Fatalf("SSH tail 解析错误: %+v", got)
	}
	if msg, err := TestLogConn(cfg); err != nil || !strings.Contains(msg, "SSH 连接成功") {
		t.Fatalf("SSH 连接测试失败: %v %s", err, msg)
	}
}
