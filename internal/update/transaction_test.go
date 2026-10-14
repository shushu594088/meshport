package update

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallRestoresFilesAfterFailedHealthCheck(t *testing.T) {
	for _, healthOK := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "success"}[healthOK], func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "meshport")
			newFile := filepath.Join(dir, "new-helper")
			if e := os.WriteFile(target, []byte("old program"), 0600); e != nil {
				t.Fatal(e)
			}
			stops, restores := 0, 0
			err := installFiles([]replacement{{target, []byte("new program"), 0755}, {newFile, []byte("helper"), 0755}}, func() error { stops++; return nil }, func() error {
				if !healthOK {
					return errors.New("health check failed")
				}
				return nil
			}, func() error { restores++; return nil })
			data, _ := os.ReadFile(target)
			if healthOK {
				if err != nil || string(data) != "new program" || restores != 0 {
					t.Fatal(err, string(data))
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), "已恢复原程序") || string(data) != "old program" || stops != 2 || restores != 1 {
					t.Fatal(err, stops, restores, string(data))
				}
				if _, e := os.Stat(newFile); !os.IsNotExist(e) {
					t.Fatal("new file was not removed")
				}
			}
		})
	}
}
func TestInstallDoesNotStopServiceWhenBackupCannotBeMade(t *testing.T) {
	dir := t.TempDir()
	stopped := false
	err := installFiles([]replacement{{dir, []byte("invalid"), 0755}}, func() error { stopped = true; return nil }, func() error { return nil }, func() error { return nil })
	if err == nil || stopped {
		t.Fatal("service stopped before validating installation targets")
	}
}
