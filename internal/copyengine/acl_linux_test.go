//go:build linux

package copyengine

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"testing"
)

func TestPreservePOSIXACL(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	write(t, src, "acl content")
	if err := os.Mkdir(dst, 0755); err != nil {
		t.Fatal(err)
	}
	var acl bytes.Buffer
	binary.Write(&acl, binary.LittleEndian, uint32(2))
	id := uint32(os.Getuid())
	for _, entry := range []struct {
		tag, perm uint16
		id        uint32
	}{{1, 6, ^uint32(0)}, {2, 4, id}, {4, 4, ^uint32(0)}, {16, 4, ^uint32(0)}, {32, 0, ^uint32(0)}} {
		binary.Write(&acl, binary.LittleEndian, entry.tag)
		binary.Write(&acl, binary.LittleEndian, entry.perm)
		binary.Write(&acl, binary.LittleEndian, entry.id)
	}
	if err := unix.Setxattr(src, "system.posix_acl_access", acl.Bytes(), 0); err != nil {
		if errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EPERM) {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	_, err := Run(context.Background(), Options{Sources: []string{src}, Destination: dst, PreserveACL: true, Verify: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 256)
	n, err := unix.Getxattr(filepath.Join(dst, "src"), "system.posix_acl_access", buf)
	if err != nil || !bytes.Equal(buf[:n], acl.Bytes()) {
		t.Fatalf("ACL not copied: %x %v", buf[:n], err)
	}
}

func TestPreserveACLRemovesInheritedACLWhenSourceHasNone(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "source")
	dst := filepath.Join(root, "dest")
	write(t, src, "file")
	if err := os.Mkdir(dst, 0755); err != nil {
		t.Fatal(err)
	}
	var acl bytes.Buffer
	_ = binary.Write(&acl, binary.LittleEndian, uint32(2))
	for _, item := range []struct {
		tag, perm uint16
		id        uint32
	}{{1, 7, ^uint32(0)}, {2, 4, uint32(os.Getuid())}, {4, 5, ^uint32(0)}, {16, 5, ^uint32(0)}, {32, 0, ^uint32(0)}} {
		_ = binary.Write(&acl, binary.LittleEndian, item.tag)
		_ = binary.Write(&acl, binary.LittleEndian, item.perm)
		_ = binary.Write(&acl, binary.LittleEndian, item.id)
	}
	if err := unix.Setxattr(dst, "system.posix_acl_default", acl.Bytes(), 0); err != nil {
		if errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EPERM) {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), Options{Sources: []string{src}, Destination: dst, PreserveACL: true}, nil); err != nil {
		t.Fatal(err)
	}
	_, err := unix.Getxattr(filepath.Join(dst, "source"), "system.posix_acl_access", nil)
	if !errors.Is(err, unix.ENODATA) {
		t.Fatalf("unexpected inherited ACL: %v", err)
	}
}

func TestPreserveDirectoryDefaultACL(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	os.Mkdir(src, 0755)
	os.Mkdir(dst, 0755)
	var acl bytes.Buffer
	_ = binary.Write(&acl, binary.LittleEndian, uint32(2))
	for _, item := range []struct {
		tag, perm uint16
		id        uint32
	}{{1, 7, ^uint32(0)}, {2, 4, uint32(os.Getuid())}, {4, 5, ^uint32(0)}, {16, 5, ^uint32(0)}, {32, 0, ^uint32(0)}} {
		_ = binary.Write(&acl, binary.LittleEndian, item.tag)
		_ = binary.Write(&acl, binary.LittleEndian, item.perm)
		_ = binary.Write(&acl, binary.LittleEndian, item.id)
	}
	if err := unix.Setxattr(src, "system.posix_acl_default", acl.Bytes(), 0); err != nil {
		if errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EPERM) {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	write(t, filepath.Join(src, "child"), "content")
	if _, err := Run(context.Background(), Options{Sources: []string{src}, Destination: dst, PreserveACL: true}, nil); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 256)
	n, err := unix.Getxattr(filepath.Join(dst, "src"), "system.posix_acl_default", buf)
	if err != nil || !bytes.Equal(buf[:n], acl.Bytes()) {
		t.Fatalf("directory default ACL not copied: %x %v", buf[:n], err)
	}
}
