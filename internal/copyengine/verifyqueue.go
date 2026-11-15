package copyengine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

// stagedFile belongs to exactly one pipeline stage at a time. Only the caller
// commits verified files; a canceled or failed queue can therefore discard them.
type stagedFile struct {
	en             entry
	source, output *os.File
	current        os.FileInfo
	tempInfo       os.FileInfo
	verified       os.FileInfo
	digest         []byte
}

func (s *stagedFile) cleanup() {
	name := s.output.Name()
	_ = s.output.Close()
	_ = s.source.Close()
	if info, err := os.Lstat(name); err == nil && s.tempInfo != nil && os.SameFile(s.tempInfo, info) {
		_ = os.Remove(name)
	}
}
func verifyStaged(ctx context.Context, s *stagedFile, opt Options, buf []byte) error {
	before, err := s.output.Stat()
	if err != nil {
		return err
	}
	if before.Size() != s.current.Size() {
		return errors.New("temporary file size differs before verification")
	}
	if _, err := s.output.Seek(0, io.SeekStart); err != nil {
		return err
	}
	actual, err := newHash(opt.HashMode)
	if err != nil {
		return err
	}
	for {
		if err = waitForTask(ctx, opt); err != nil {
			return err
		}
		n, e := s.output.Read(buf)
		if n > 0 {
			_, _ = actual.Write(buf[:n])
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
	}
	releaseCache(s.output)
	if !bytes.Equal(s.digest, actual.Sum(nil)) {
		return fmt.Errorf("destination verification failed: expected %x, got %x", s.digest, actual.Sum(nil))
	}
	if opt.VerifySourceAgain {
		if _, err = s.source.Seek(0, io.SeekStart); err != nil {
			return err
		}
		sourceHash, _ := newHash(opt.HashMode)
		for {
			if err = waitForTask(ctx, opt); err != nil {
				return err
			}
			n, e := s.source.Read(buf)
			if n > 0 {
				_, _ = sourceHash.Write(buf[:n])
			}
			if e == io.EOF {
				break
			}
			if e != nil {
				return e
			}
		}
		releaseCache(s.source)
		if !bytes.Equal(s.digest, sourceHash.Sum(nil)) {
			return fmt.Errorf("source re-verification failed: expected %x, got %x", s.digest, sourceHash.Sum(nil))
		}
		after, err := s.source.Stat()
		if err != nil {
			return err
		}
		if after.Size() != s.current.Size() || !after.ModTime().Equal(s.current.ModTime()) {
			return errors.New("source changed during verification")
		}
	}
	after, err := s.output.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return errors.New("temporary file changed during verification")
	}
	s.verified = after
	return nil
}
func commitStaged(ctx context.Context, s *stagedFile, opt Options) error {
	if err := waitForTask(ctx, opt); err != nil {
		return err
	}
	currentFile, err := s.output.Stat()
	if err != nil {
		return err
	}
	pathFile, err := os.Lstat(s.output.Name())
	if err != nil {
		return err
	}
	if s.tempInfo == nil || !os.SameFile(s.tempInfo, currentFile) || !os.SameFile(s.tempInfo, pathFile) {
		return errors.New("temporary file was replaced")
	}
	if opt.Verify {
		current, err := s.output.Stat()
		if err != nil {
			return err
		}
		if s.verified == nil || !os.SameFile(s.verified, current) || s.verified.Size() != current.Size() || !s.verified.ModTime().Equal(current.ModTime()) {
			return errors.New("temporary file changed after verification")
		}
	}
	if err := os.Chtimes(s.output.Name(), s.en.info.ModTime(), s.en.info.ModTime()); err != nil {
		return err
	}
	if opt.PreserveACL {
		if err := copyACL(s.source, s.output); err != nil {
			return err
		}
	}
	if err := s.output.Sync(); err != nil {
		return err
	}
	// The staging file is private and its data is now clean. On Linux, ask the
	// kernel to discard its cached pages even when verification was disabled.
	releaseCache(s.output)
	if err := s.output.Close(); err != nil {
		return err
	}
	if err := waitForTask(ctx, opt); err != nil {
		return err
	}
	pathFile, err = os.Lstat(s.output.Name())
	if err != nil {
		return err
	}
	if !os.SameFile(s.tempInfo, pathFile) {
		return errors.New("temporary file was replaced before commit")
	}
	return commitFile(s.output.Name(), s.en.dst, opt.Mode == Skip)
}

type verifiedResult struct {
	file *stagedFile
	err  error
}
type verifyQueue struct {
	ctx     context.Context
	cancel  context.CancelFunc
	jobs    chan *stagedFile
	results chan verifiedResult
	done    chan struct{}
	mu      sync.Mutex
	failure error
	nonStop bool
	pending int
}

func newVerifyQueue(parent context.Context, opt Options) *verifyQueue {
	ctx, cancel := context.WithCancel(parent)
	q := &verifyQueue{ctx: ctx, cancel: cancel, jobs: make(chan *stagedFile, 2), results: make(chan verifiedResult, 4), done: make(chan struct{}), nonStop: opt.NonStop}
	verify := verifyStaged
	if opt.verifyFn != nil {
		verify = opt.verifyFn
	}
	go func() {
		defer close(q.done)
		buf := make([]byte, opt.BufferSize)
		for file := range q.jobs {
			err := verify(ctx, file, opt, buf)
			if err != nil && !q.nonStop && !errors.Is(err, context.Canceled) {
				q.mu.Lock()
				if q.failure == nil {
					q.failure = fmt.Errorf("verify %s: %w", file.en.src, err)
				}
				q.mu.Unlock()
				q.cancel()
			}
			q.results <- verifiedResult{file, err}
		}
		close(q.results)
	}()
	return q
}
func (q *verifyQueue) firstError() error { q.mu.Lock(); defer q.mu.Unlock(); return q.failure }

// closeAndDiscard joins the worker and removes every staged but uncommitted file.
func (q *verifyQueue) closeAndDiscard() {
	q.cancel()
	close(q.jobs)
	for r := range q.results {
		r.file.cleanup()
	}
	<-q.done
}
