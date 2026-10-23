package capture

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
)

// RemoteFile 远端文件/目录条目（日志文件选择用）。
type RemoteFile struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Dir   bool   `json:"dir"`
	Size  int64  `json:"size"`
	Error string `json:"error,omitempty"`
}

// ListLogDir 连接目标机并列出目录（默认目录：配置文件所在目录）。
func ListLogDir(cfg Config, dir string) ([]RemoteFile, error) {
	proto := strings.ToLower(strings.TrimSpace(cfg.LogProtocol))
	if proto == "" {
		proto = "ssh"
	}
	if strings.TrimSpace(dir) == "" {
		dir = path.Dir(strings.TrimSpace(cfg.LogPath))
		if dir == "" || dir == "." {
			dir = "/"
		}
	}
	switch proto {
	case "ssh", "scp":
		return listDirSSH(cfg, dir)
	case "sftp":
		return listDirSFTP(cfg, dir)
	case "ftp":
		return listDirFTP(cfg, dir)
	}
	return nil, fmt.Errorf("本机文件请在“选择”中浏览；%s 不支持远程列目录", proto)
}

// listDirSSH 通过远端 ls -la 列目录；脚本在 sh/bash 下均可运行。
func listDirSSH(cfg Config, dir string) ([]RemoteFile, error) {
	client, err := sshClient(cfg)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	sess, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("SSH 会话失败: %v", err)
	}
	defer sess.Close()
	script := "ls -la " + shellQuote(dir)
	out, err := sess.Output(script)
	if err != nil {
		return nil, fmt.Errorf("列出目录失败（%s）: %v", dir, err)
	}
	files := parseLsOutput(string(out), dir)
	if len(files) == 0 {
		// BusyBox/精简系统 ls 不支持 -la 时退回 -l
		if sess2, err := client.NewSession(); err == nil {
			defer sess2.Close()
			if out2, err2 := sess2.Output("ls -l " + shellQuote(dir)); err2 == nil {
				files = parseLsOutput(string(out2), dir)
			}
		}
	}
	return files, nil
}

// parseLsOutput 解析 ls -la 输出：权限 链接 用户 组 大小 月 日 时间/年 名称。
func parseLsOutput(out, dir string) []RemoteFile {
	var files []RemoteFile
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		fields := strings.Fields(line)
		if len(fields) < 9 || !strings.HasPrefix(fields[0], "-") && !strings.HasPrefix(fields[0], "d") && !strings.HasPrefix(fields[0], "l") {
			continue
		}
		name := strings.Join(fields[8:], " ")
		if i := strings.Index(name, " -> "); i >= 0 {
			name = name[:i] // 符号链接去掉目标
		}
		if name == "." || name == ".." || strings.HasPrefix(name, "total ") {
			continue
		}
		isDir := fields[0][0] == 'd'
		var size int64
		if !isDir {
			size, _ = strconv.ParseInt(fields[4], 10, 64)
		}
		p := path.Join(dir, name)
		if name == "" || strings.ContainsAny(name, "\x00") {
			continue
		}
		files = append(files, RemoteFile{Name: name, Path: p, Dir: isDir, Size: size})
	}
	sortRemoteFiles(files)
	return files
}

func sortRemoteFiles(files []RemoteFile) {
	sort.Slice(files, func(i, j int) bool {
		if files[i].Dir != files[j].Dir {
			return files[i].Dir
		}
		return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name)
	})
}

func listDirSFTP(cfg Config, dir string) ([]RemoteFile, error) {
	c, err := newSFTPClient(cfg)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	files, err := c.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("SFTP 列目录失败（%s）: %v", dir, err)
	}
	for i := range files {
		files[i].Path = path.Join(dir, files[i].Name)
	}
	return files, nil
}

func listDirFTP(cfg Config, dir string) ([]RemoteFile, error) {
	c, err := newFTPClient(cfg)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	entries, err := c.List(dir)
	if err != nil {
		return nil, fmt.Errorf("FTP 列目录失败（%s）: %v", dir, err)
	}
	sortRemoteFiles(entries)
	return entries, nil
}
