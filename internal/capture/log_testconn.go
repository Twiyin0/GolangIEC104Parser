package capture

import (
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// TestLogConn 测试日志连接：本机检查文件，SSH/SCP 检查远端可读，SFTP/FTP 取文件大小。
func TestLogConn(cfg Config) (string, error) {
	proto := strings.ToLower(strings.TrimSpace(cfg.LogProtocol))
	if proto == "" {
		proto = "local"
	}
	if proto == "local" {
		st, err := os.Stat(cfg.LogPath)
		if err != nil {
			return "", fmt.Errorf("无法读取日志文件: %v", err)
		}
		if st.IsDir() {
			return "", fmt.Errorf("路径是目录，不是日志文件")
		}
		return fmt.Sprintf("本机文件可读：%s（%d 字节）", cfg.LogPath, st.Size()), nil
	}
	switch proto {
	case "ssh", "scp":
		var client *ssh.Client
		var err error
		for i := 0; i < 3; i++ {
			client, err = sshClient(cfg)
			if err == nil {
				break
			}
			if i < 2 {
				time.Sleep(400 * time.Millisecond)
			}
		}
		if err != nil {
			return "", fmt.Errorf("SSH 连接失败（已重试 3 次，请确认 sshd 已启动、端口开放）: %v", err)
		}
		defer client.Close()
		sess, err := client.NewSession()
		if err != nil {
			return "", fmt.Errorf("SSH 会话失败: %v", err)
		}
		defer sess.Close()
		out, err := sess.Output("wc -c < " + shellQuote(cfg.LogPath))
		if err != nil {
			return "", fmt.Errorf("远端文件不可读或 wc 不可用: %v", err)
		}
		return fmt.Sprintf("SSH 连接成功，远端文件可读：%s（%s 字节）", cfg.LogPath, strings.TrimSpace(string(out))), nil
	case "sftp":
		c, err := newSFTPClient(cfg)
		if err != nil {
			return "", err
		}
		defer c.Close()
		size, err := c.Stat(cfg.LogPath)
		if err != nil {
			return "", fmt.Errorf("SFTP 文件不可读: %v", err)
		}
		return fmt.Sprintf("SFTP 连接成功，远端文件可读：%s（%d 字节）", cfg.LogPath, size), nil
	case "ftp":
		c, err := newFTPClient(cfg)
		if err != nil {
			return "", err
		}
		defer c.Close()
		size, err := c.Size(cfg.LogPath)
		if err != nil {
			return "", fmt.Errorf("FTP 文件不可读: %v", err)
		}
		return fmt.Sprintf("FTP 连接成功，远端文件可读：%s（%d 字节）", cfg.LogPath, size), nil
	}
	return "", fmt.Errorf("不支持的日志协议: %s", proto)
}
