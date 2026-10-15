package bootstrap

import (
	"os"
	"os/exec"
	"runtime"
)

type Check struct {
	Name     string `json:"name"`
	OK       bool   `json:"ok"`
	Required bool   `json:"required"`
	Detail   string `json:"detail"`
}

type Environment struct {
	OS     string  `json:"os"`
	Arch   string  `json:"arch"`
	Checks []Check `json:"checks"`
}

// Inspect uses local checks only; it never starts tools or makes model requests.
func Inspect(dataDir, keyDir string) Environment {
	e := Environment{OS: runtime.GOOS, Arch: runtime.GOARCH}
	for _, item := range []struct{ name, path string }{{"工作区写入", dataDir}, {"配置及密钥目录写入", keyDir}} {
		err := os.MkdirAll(item.path, 0700)
		if err == nil {
			var f *os.File
			f, err = os.CreateTemp(item.path, ".diting-environment-*")
			if err == nil {
				name := f.Name()
				err = f.Close()
				if removeErr := os.Remove(name); err == nil {
					err = removeErr
				}
			}
		}
		detail := "可写"
		if err != nil {
			detail = "不可写，请检查目录权限或磁盘状态"
		}
		e.Checks = append(e.Checks, Check{item.name, err == nil, true, detail})
	}
	for _, tool := range []string{"python", "git", "docker", "bash"} {
		_, err := exec.LookPath(tool)
		detail := "已在 PATH 中找到"
		if err != nil {
			detail = "未找到；仅相关工具或部署方式需要，可稍后安装"
		}
		e.Checks = append(e.Checks, Check{tool, err == nil, false, detail})
	}
	return e
}
