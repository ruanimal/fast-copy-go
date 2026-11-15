package copyengine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
)

// verifyExisting is read-only; a mismatch never replaces or removes a file.
func verifyExisting(ctx context.Context, en entry, target os.FileInfo, opt Options, p *Progress, emit func()) error {
	if en.info.Mode()&os.ModeSymlink != 0 {
		if target.Mode()&os.ModeSymlink == 0 {
			return errors.New("destination is not a symbolic link")
		}
		a, err := os.Readlink(en.src)
		if err != nil {
			return err
		}
		b, err := os.Readlink(en.dst)
		if err != nil {
			return err
		}
		if a != b {
			return errors.New("symbolic link targets differ")
		}
		return nil
	}
	if !target.Mode().IsRegular() {
		return errors.New("destination is not a regular file")
	}
	if en.info.Size() != target.Size() || !timestampsEqual(en.info.ModTime(), target.ModTime(), opt.TimestampTolerance) {
		return errors.New("file size or modification time differs")
	}
	source, err := os.Open(en.src)
	if err != nil {
		return err
	}
	defer source.Close()
	srcStat, err := source.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(en.info, srcStat) || en.info.Size() != srcStat.Size() || !en.info.ModTime().Equal(srcStat.ModTime()) {
		return errors.New("source changed during scan")
	}
	dest, err := os.Open(en.dst)
	if err != nil {
		return err
	}
	defer dest.Close()
	dstStat, err := dest.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(target, dstStat) || target.Size() != dstStat.Size() || !target.ModTime().Equal(dstStat.ModTime()) {
		return errors.New("destination changed during scan")
	}
	srcHash, _ := newHash(opt.HashMode)
	dstHash, _ := newHash(opt.HashMode)
	if opt.scratch.verify == nil {
		opt.scratch.verify = make([]byte, opt.BufferSize)
	}
	buf := opt.scratch.verify
	for _, item := range []struct {
		f      *os.File
		source bool
	}{{source, true}, {dest, false}} {
		h := dstHash
		if item.source {
			h = srcHash
		}
		for {
			if err = waitForTask(ctx, opt); err != nil {
				return err
			}
			n, e := item.f.Read(buf)
			if n > 0 {
				_, _ = h.Write(buf[:n])
				if item.source {
					p.CopiedBytes += int64(n)
					emit()
				}
			}
			if e == io.EOF {
				break
			}
			if e != nil {
				return e
			}
		}
		releaseCache(item.f)
	}
	a, err := source.Stat()
	if err != nil {
		return err
	}
	b, err := dest.Stat()
	if err != nil {
		return err
	}
	if a.Size() != srcStat.Size() || !a.ModTime().Equal(srcStat.ModTime()) || b.Size() != dstStat.Size() || !b.ModTime().Equal(dstStat.ModTime()) {
		return errors.New("file changed during verification")
	}
	if !bytes.Equal(srcHash.Sum(nil), dstHash.Sum(nil)) {
		return fmt.Errorf("file contents differ: source %x, destination %x", srcHash.Sum(nil), dstHash.Sum(nil))
	}
	return nil
}
