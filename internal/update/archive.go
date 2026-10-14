package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"path"
	"runtime"
	"strings"
)

func unpack(data []byte) (map[string][]byte, error) {
	wanted := map[string]bool{"meshport": true, "meshport-helper": true, "meshport.service": true, "meshport-mihomo.service": true, "ARCH": true}
	files := map[string][]byte{}
	gz, e := gzip.NewReader(bytes.NewReader(data))
	if e != nil {
		return nil, errors.New("更新包压缩格式无效")
	}
	defer gz.Close()
	reader := tar.NewReader(io.LimitReader(gz, 512<<20))
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errors.New("更新包损坏或解压大小超限")
		}
		name := strings.TrimPrefix(header.Name, "./")
		if header.Typeflag == tar.TypeDir {
			name = strings.TrimSuffix(name, "/")
		}
		if path.IsAbs(name) || path.Clean(name) != name || strings.HasPrefix(name, "../") || strings.Contains(name, "\\") {
			if name == "" || name == "." {
				continue
			}
			return nil, errors.New("更新包包含无效路径")
		}
		if !wanted[name] {
			continue
		}
		if header.Typeflag != tar.TypeReg || header.Size > 160<<20 || header.Size < 0 || files[name] != nil {
			return nil, errors.New("更新包包含无效文件")
		}
		b, err := io.ReadAll(reader)
		if err != nil {
			return nil, err
		}
		files[name] = b
	}
	for name := range wanted {
		if len(files[name]) == 0 {
			return nil, fmt.Errorf("更新包缺少 %s", name)
		}
	}
	if strings.TrimSpace(string(files["ARCH"])) != runtime.GOARCH {
		return nil, errors.New("更新包架构不匹配")
	}
	return files, nil
}
