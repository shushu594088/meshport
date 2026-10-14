package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"runtime"
	"testing"
)

func packageArchive(t *testing.T, extra *tar.Header, wrongArch bool) []byte {
	t.Helper()
	var data bytes.Buffer
	gz := gzip.NewWriter(&data)
	writer := tar.NewWriter(gz)
	files := map[string]string{"meshport": "program", "meshport-helper": "helper", "meshport.service": "unit", "meshport-mihomo.service": "unit", "ARCH": runtime.GOARCH}
	if wrongArch {
		files["ARCH"] = "unsupported"
	}
	if e := writer.WriteHeader(&tar.Header{Name: "./docs/", Typeflag: tar.TypeDir, Mode: 0755}); e != nil {
		t.Fatal(e)
	}
	for name, body := range files {
		if e := writer.WriteHeader(&tar.Header{Name: "./" + name, Typeflag: tar.TypeReg, Size: int64(len(body)), Mode: 0755}); e != nil {
			t.Fatal(e)
		}
		if _, e := writer.Write([]byte(body)); e != nil {
			t.Fatal(e)
		}
	}
	if extra != nil {
		if e := writer.WriteHeader(extra); e != nil {
			t.Fatal(e)
		}
	}
	if e := writer.Close(); e != nil {
		t.Fatal(e)
	}
	if e := gz.Close(); e != nil {
		t.Fatal(e)
	}
	return data.Bytes()
}
func TestPackageArchiveValidatesPathsTypesDuplicatesAndArchitecture(t *testing.T) {
	if _, e := unpack(packageArchive(t, nil, false)); e != nil {
		t.Fatal(e)
	}
	if _, e := unpack(packageArchive(t, nil, true)); e == nil {
		t.Fatal("wrong architecture accepted")
	}
	for _, header := range []*tar.Header{
		{Name: "../../usr/bin/evil", Typeflag: tar.TypeReg},
		{Name: "/etc/sudoers", Typeflag: tar.TypeReg},
		{Name: "meshport", Typeflag: tar.TypeReg},
		{Name: "meshport-helper", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"},
	} {
		if _, e := unpack(packageArchive(t, header, false)); e == nil {
			t.Fatalf("invalid header accepted: %+v", header)
		}
	}
}
