//go:build darwin

package copyengine

import (
	"context"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

// This exercises the real system ACL implementation when tests run on macOS.
func TestDarwinNativeACL(t *testing.T) {
	u, err := user.Current()
	if err != nil {
		t.Skip(err)
	}
	root := t.TempDir()
	src := filepath.Join(root, "source")
	dst := filepath.Join(root, "dest")
	write(t, src, "content")
	if err = os.Mkdir(dst, 0755); err != nil {
		t.Fatal(err)
	}
	if output, e := exec.Command("/bin/chmod", "+a", "user:"+u.Username+" allow read", src).CombinedOutput(); e != nil {
		t.Skipf("ACL setup unavailable: %v: %s", e, output)
	}
	getACL := func(p string) string {
		output, e := exec.Command("/bin/ls", "-led", p).CombinedOutput()
		if e != nil {
			t.Fatalf("list ACL: %v: %s", e, output)
		}
		var lines []string
		for _, line := range strings.Split(string(output), "\n") {
			line = strings.TrimSpace(line)
			if len(line) > 2 && line[0] >= '0' && line[0] <= '9' {
				lines = append(lines, line)
			}
		}
		return strings.Join(lines, "\n")
	}
	expected := getACL(src)
	if expected == "" {
		t.Fatal("ACL setup produced no ACL")
	}
	if _, err = Run(context.Background(), Options{Sources: []string{src}, Destination: dst, PreserveACL: true, Verify: true}, nil); err != nil {
		t.Fatal(err)
	}
	if actual := getACL(filepath.Join(dst, "source")); actual != expected {
		t.Fatalf("ACL differs: source %q; destination %q", expected, actual)
	}
}
