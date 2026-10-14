package update

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type replacement struct {
	target string
	data   []byte
	mode   os.FileMode
}

func writeFile(target string, data []byte, mode os.FileMode) error {
	f, e := os.CreateTemp(filepath.Dir(target), ".meshport-update-*")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(mode); e == nil {
		_, e = f.Write(data)
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, target)
}

// All backups precede stopping the service. Any installation/health failure restores each file.
func installFiles(items []replacement, stop, activate, restore func() error) (err error) {
	type original struct {
		data   []byte
		mode   os.FileMode
		exists bool
	}
	backups := make([]original, len(items))
	for i, item := range items {
		info, e := os.Lstat(item.target)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil || !info.Mode().IsRegular() {
			return errors.New("安装目标不是普通文件，已取消更新")
		}
		b, e := os.ReadFile(item.target)
		if e != nil {
			return errors.New("无法备份当前安装文件")
		}
		backups[i] = original{b, info.Mode().Perm(), true}
	}
	if err = stop(); err != nil {
		return err
	}
	defer func() {
		if err == nil {
			return
		}
		failed := stop() != nil
		for i, item := range items {
			old := backups[i]
			if old.exists {
				if writeFile(item.target, old.data, old.mode) != nil {
					failed = true
				}
			} else if e := os.Remove(item.target); e != nil && !os.IsNotExist(e) {
				failed = true
			}
		}
		if restore() != nil {
			failed = true
		}
		if failed {
			err = errors.New("更新失败且程序回退未完成，请检查 systemd 日志并手动恢复安装")
		} else {
			err = fmt.Errorf("更新失败，已恢复原程序：%w", err)
		}
	}()
	for _, item := range items {
		if writeFile(item.target, item.data, item.mode) != nil {
			return errors.New("替换安装文件失败")
		}
	}
	return activate()
}
