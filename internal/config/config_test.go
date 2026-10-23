package config

import "testing"

func TestLogRemoteDefaults(t *testing.T) {
	c := Config{Connection: Connection{Log: LogConn{Protocol: "ssh"}}}
	c.Normalize()
	l := c.Connection.Log
	if l.Host != DefaultLogHost || l.User != DefaultLogUser || l.FilePath != DefaultLogPath {
		t.Fatalf("远程 log 默认值未补全: %+v", l)
	}
	if l.Port != 22 || l.WatchMode != "dynamic" || l.Encoding != "auto" || l.Proto != "auto" || l.TailBytes != 65536 {
		t.Fatalf("远程 log 其它默认值错误: %+v", l)
	}
	if l.Password != "" || l.SSHKeyPath != "" {
		t.Fatal("密码/私钥不应被默认填充")
	}
}

func TestLogFTPPortAndLocalPath(t *testing.T) {
	c := Config{Connection: Connection{Log: LogConn{Protocol: "ftp"}}}
	c.Normalize()
	if c.Connection.Log.Port != 21 {
		t.Fatalf("FTP 默认端口错误: %d", c.Connection.Log.Port)
	}
	local := Config{Connection: Connection{Log: LogConn{Protocol: "local"}}}
	local.Normalize()
	if local.Connection.Log.FilePath != "" {
		t.Fatalf("本机模式不应强制默认 Linux 路径: %q", local.Connection.Log.FilePath)
	}
}

func TestLogEmptyProtocolDefaultsSSH(t *testing.T) {
	c := Config{Connection: Connection{Log: LogConn{}}}
	c.Normalize()
	if c.Connection.Log.Protocol != "ssh" {
		t.Fatalf("空协议应默认 ssh: %s", c.Connection.Log.Protocol)
	}
	if c.Connection.Log.Host != DefaultLogHost {
		t.Fatalf("空主机应默认 %s: %s", DefaultLogHost, c.Connection.Log.Host)
	}
}
