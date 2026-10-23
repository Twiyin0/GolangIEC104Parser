package capture

import (
	"testing"
)

func TestParseLsOutput(t *testing.T) {
	out := `total 32
drwxr-xr-x    2 root     root          4096 Oct 22 09:01 .
drwxr-xr-x    9 root     root          4096 Oct 20 12:00 ..
-rw-r--r--    1 root     root       6367488 Oct 22 08:59 ftuMessage.log
drwxr-xr-x    2 root     root          4096 Oct 22 08:00 history
lrwxrwxrwx    1 root     root            14 Oct 22 08:00 latest -> ftuMessage.log
`
	files := parseLsOutput(out, "/data0/log")
	if len(files) != 3 {
		t.Fatalf("条目数错误: %+v", files)
	}
	// 目录排在前
	if !files[0].Dir || files[0].Name != "history" {
		t.Fatalf("目录未排前: %+v", files[0])
	}
	if files[1].Dir || files[1].Name != "ftuMessage.log" || files[1].Size != 6367488 {
		t.Fatalf("文件解析错误: %+v", files[1])
	}
	if files[1].Path != "/data0/log/ftuMessage.log" {
		t.Fatalf("绝对路径错误: %s", files[1].Path)
	}
	if files[2].Name != "latest" || files[2].Dir {
		t.Fatalf("符号链接解析错误: %+v", files[2])
	}
}

func TestParseFTPList(t *testing.T) {
	out := "drwxr-xr-x 2 root root 4096 Oct 22 09:01 history\r\n-rw-r--r-- 1 root root 2048 Oct 22 08:59 ftuMessage.log\r\n"
	files := parseFTPList(out, "/data0/log")
	if len(files) != 2 || files[0].Name != "history" || files[1].Path != "/data0/log/ftuMessage.log" {
		t.Fatalf("FTP LIST 解析错误: %+v", files)
	}
}

func TestParseAttrsDir(t *testing.T) {
	// flags=SIZE|PERMISSIONS, size=0, perm=040755
	p := []byte{0, 0, 0, 5, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x41, 0xED}
	a, _, err := parseAttrs(p)
	if err != nil || !a.dir {
		t.Fatalf("目录属性解析错误: %+v err=%v", a, err)
	}
}
