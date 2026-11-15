package copyengine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type copiedHardlink struct {
	path         string
	source, dest os.FileInfo
}

func makeHardlink(ctx context.Context, en entry, prev copiedHardlink, opt Options) error {
	source, err := os.Lstat(en.src)
	if err != nil {
		return err
	}
	if !os.SameFile(en.info, source) || !os.SameFile(source, prev.source) || source.Size() != prev.source.Size() || !source.ModTime().Equal(prev.source.ModTime()) {
		return errors.New("hardlink source changed")
	}
	target, err := os.Lstat(prev.path)
	if err != nil {
		return err
	}
	if !os.SameFile(prev.dest, target) || target.Size() != source.Size() || !target.ModTime().Equal(source.ModTime()) {
		return errors.New("previous hardlink destination changed")
	}
	tempdir, err := os.MkdirTemp(filepath.Dir(en.dst), ".fastcopy-link-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempdir)
	link := filepath.Join(tempdir, "link")
	if err = os.Link(prev.path, link); err != nil {
		return fmt.Errorf("preserve hardlink: %w", err)
	}
	if err = waitForTask(ctx, opt); err != nil {
		return err
	}
	return commitFile(link, en.dst, opt.Mode == Skip)
}
