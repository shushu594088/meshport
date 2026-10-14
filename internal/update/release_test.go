package update

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestCheckStableVersionAndFailure(t *testing.T) {
	for _, tc := range []struct {
		body               string
		code               int
		wantErr, available bool
	}{
		{`{"tag_name":"v0.2.0","body":"notes"}`, 200, false, true},
		{`{"tag_name":"v0.1.0"}`, 200, false, false},
		{`{"tag_name":"v0.0.9"}`, 200, false, false},
		{`{"tag_name":"v9.0.0","prerelease":true}`, 200, true, false},
		{`{"tag_name":"../evil"}`, 200, true, false},
		{`rate limit`, 403, true, false},
	} {
		client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			if r.URL.String() != "https://api.github.com/repos/shushu594088/meshport/releases/latest" {
				t.Fatal("wrong release source")
			}
			return &http.Response{StatusCode: tc.code, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
		})}
		release, err := Check(context.Background(), client, "0.1.0")
		if (err != nil) != tc.wantErr || release.Available != tc.available {
			t.Fatal(release, err)
		}
	}
	if !newer("v1.10.0", "1.9.99") || newer("1.2.0", "1.11.0") || newer("v1.1.0-beta", "1.0.0") {
		t.Fatal("invalid version comparison")
	}
}
func TestManifestAndChecksumsRejectAmbiguityAndTampering(t *testing.T) {
	manifest := "# meshport-install-targets-v1\nubuntu\t*\tamd64\tmeshport-{version}-linux-amd64.tar.gz\nubuntu\t24\tamd64\tmajor-{version}.tar.gz\nubuntu\t24.04\tamd64\texact-{version}.tar.gz\n"
	asset, e := selectAsset(manifest, "ubuntu", "24.04", "amd64", "0.2.0")
	if e != nil || asset != "exact-0.2.0.tar.gz" {
		t.Fatal(asset, e)
	}
	if _, e = selectAsset(manifest, "centos", "9", "amd64", "0.2.0"); e == nil {
		t.Fatal("unsupported OS accepted")
	}
	for _, template := range []string{"../../evil.tar.gz", "https://evil/pkg.tar.gz", "bad;rm.tar.gz"} {
		if _, e = selectAsset("# meshport-install-targets-v1\nubuntu\t*\tamd64\t"+template, "ubuntu", "24.04", "amd64", "0.2.0"); e == nil {
			t.Fatal(template)
		}
	}
	data := []byte("verified package")
	sum := sha256.Sum256(data)
	sums := fmt.Sprintf("%x  %s\r\n", sum, asset)
	if e = verify(data, sums, asset); e != nil {
		t.Fatal(e)
	}
	if verify([]byte("corrupt"), sums, asset) == nil || verify(data, sums+sums, asset) == nil || verify(data, sums, "another.tar.gz") == nil {
		t.Fatal("invalid checksum accepted")
	}
}
