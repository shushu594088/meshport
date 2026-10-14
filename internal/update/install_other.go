//go:build !linux

package update

import "errors"

func Run(string, int) error { return errors.New("自动更新仅支持 Linux systemd 安装") }
