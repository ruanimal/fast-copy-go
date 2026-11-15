// Package copyengine implements a bounded, cancellable file-copy pipeline.
package copyengine

import (
	"context"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Mode string

const (
	Overwrite Mode = "overwrite"
	Skip      Mode = "skip"
	Update    Mode = "update"
	SizeDate  Mode = "size_date"
)

type Operation string

const (
	Copy       Operation = "copy"
	VerifyOnly Operation = "verify"
)

type DiskMode string

const (
	DiskAuto      DiskMode = "auto"
	DiskSame      DiskMode = "same"
	DiskDifferent DiskMode = "different"
)

type Options struct {
	Sources            []string
	IncludePatterns    []string
	ExcludePatterns    []string
	Operation          Operation
	Destination        string
	Mode               Mode
	Verify             bool
	PreserveXattrs     bool
	PreserveACL        bool
	PreserveHardlinks  bool
	Preallocate        bool
	TargetDirect       bool
	NonStop            bool
	Gate               *Gate
	HashMode           HashMode
	VerifySourceAgain  bool
	TimestampTolerance time.Duration
	BufferSize         int
	BatchSize          int
	DiskMode           DiskMode
	scratch            *buffers
	verifyFn           func(context.Context, *stagedFile, Options, []byte) error
	targetWriteFn      func(int, []byte, int64) (int, error)
}
type Progress struct {
	Phase, Path, Destination                          string
	Strategy                                          DiskMode
	IOBackend                                         string
	IOBackendReason                                   string
	TotalBytes, CopiedBytes, SkippedBytes             int64
	CommittedBytes                                    int64
	Operation                                         Operation
	TotalFiles, CopiedFiles, SkippedFiles, ErrorCount int
	PendingFiles                                      int
	BatchFiles                                        int
	Started, Finished                                 time.Time
}
type buffers struct {
	batch    []byte
	aio      aioPool
	temps    map[string]string
	direct   directPool
	same     []byte
	parallel [2][]byte
	verify   []byte
}
type Result struct{ Progress Progress }
type entry struct {
	src, dst string
	info     os.FileInfo
}

// Run copies each source into Destination under its base name. Existing directories
// are merged; regular files are replaced only after a complete temporary copy.
func Run(ctx context.Context, opt Options, report func(Progress)) (result Result, err error) {
	var p Progress
	p.Started = time.Now()
	p.Phase = "scanning"
	emit := func() {
		if report != nil {
			report(p)
		}
	}
	emit()
	if len(opt.Sources) == 0 || opt.Destination == "" {
		return Result{p}, errors.New("select at least one source and a destination")
	}
	filter, err := makeFilter(opt.IncludePatterns, opt.ExcludePatterns)
	if err != nil {
		return Result{p}, err
	}
	if opt.TimestampTolerance < 0 {
		return Result{p}, errors.New("timestamp tolerance must not be negative")
	}
	if opt.Operation == "" {
		opt.Operation = Copy
	}
	if opt.Operation != Copy && opt.Operation != VerifyOnly {
		return Result{p}, errors.New("invalid operation")
	}
	p.Operation = opt.Operation
	if opt.Operation == VerifyOnly {
		opt.Verify = true
	}
	if opt.Verify {
		if _, err := newHash(opt.HashMode); err != nil {
			return Result{p}, err
		}
	}
	if opt.Mode == "" {
		opt.Mode = SizeDate
	}
	if opt.Mode != Overwrite && opt.Mode != Skip && opt.Mode != Update && opt.Mode != SizeDate {
		return Result{p}, errors.New("invalid overwrite mode")
	}
	if opt.BufferSize == 0 {
		opt.BufferSize = 2 * 1024 * 1024
	}
	if opt.BufferSize < 4096 || opt.BufferSize > 16*1024*1024 {
		return Result{p}, errors.New("buffer size must be between 4 KiB and 16 MiB")
	}
	if opt.BatchSize == 0 {
		opt.BatchSize = 32 * 1024 * 1024
	}
	if opt.BatchSize < opt.BufferSize || opt.BatchSize > 256*1024*1024 {
		return Result{p}, errors.New("batch size must be at least the chunk size and at most 256 MiB")
	}
	if opt.DiskMode == "" {
		opt.DiskMode = DiskAuto
	}
	if opt.DiskMode != DiskAuto && opt.DiskMode != DiskSame && opt.DiskMode != DiskDifferent {
		return Result{p}, errors.New("invalid disk mode")
	}
	dest, err := filepath.Abs(opt.Destination)
	if err != nil {
		return Result{p}, err
	}
	dest, err = filepath.EvalSymlinks(dest)
	if err != nil {
		return Result{p}, fmt.Errorf("destination: %w", err)
	}
	di, err := os.Stat(dest)
	if err != nil || !di.IsDir() {
		return Result{p}, errors.New("destination must be an existing directory")
	}
	p.Destination = dest
	emit()
	opt.scratch = &buffers{}
	defer opt.scratch.direct.close()
	defer opt.scratch.aio.close()
	defer opt.scratch.cleanupTemps()
	var problems []error
	record := func(err error) bool {
		if !opt.NonStop || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			return false
		}
		problems = append(problems, err)
		p.ErrorCount++
		emit()
		return true
	}
	var entries []entry
	names := map[string]bool{}
	for _, source := range opt.Sources {
		if err = ctx.Err(); err != nil {
			return Result{p}, err
		}
		src, e := filepath.Abs(source)
		if e != nil {
			return Result{p}, e
		}
		src = filepath.Clean(src)
		info, e := os.Lstat(src)
		if e != nil {
			err := fmt.Errorf("source %s: %w", src, e)
			if record(err) {
				continue
			}
			return Result{p}, err
		}
		name := filepath.Base(src)
		if name == string(filepath.Separator) || name == "." {
			return Result{p}, errors.New("filesystem root cannot be a source")
		}
		if names[name] {
			return Result{p}, fmt.Errorf("duplicate source name: %s", name)
		}
		names[name] = true
		// Resolve parent symlinks, but preserve a source symlink as a link.
		parent, e := filepath.EvalSymlinks(filepath.Dir(src))
		if e != nil {
			return Result{p}, e
		}
		src = filepath.Join(parent, name)
		target := filepath.Join(dest, name)
		if src == target {
			return Result{p}, fmt.Errorf("source and destination are the same: %s", src)
		}
		if info.IsDir() {
			rel, e := filepath.Rel(src, dest)
			if e == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))) {
				return Result{p}, fmt.Errorf("destination is inside source: %s", src)
			}
		}
		type scanItem struct {
			en       entry
			selected bool
		}
		var scanned []scanItem
		e = filepath.Walk(src, func(path string, inf os.FileInfo, walkErr error) error {
			if walkErr != nil {
				if record(fmt.Errorf("scan %s: %w", path, walkErr)) {
					return nil
				}
				return walkErr
			}
			if e := waitForTask(ctx, opt); e != nil {
				return e
			}
			filterRel, err := filepath.Rel(filepath.Dir(src), path)
			if err != nil {
				return err
			}
			if filter.excluded(filterRel, inf.IsDir()) {
				if inf.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			selected := filter.includeFile(filterRel)
			if inf.IsDir() {
				selected = filter.includeDir(filterRel)
			}
			if !inf.IsDir() && !selected {
				return nil
			}
			if !inf.IsDir() && !inf.Mode().IsRegular() && inf.Mode()&os.ModeSymlink == 0 {
				err := fmt.Errorf("unsupported file type: %s", path)
				if record(err) {
					return nil
				}
				return err
			}
			rel, e := filepath.Rel(src, path)
			if e != nil {
				return e
			}
			dst := filepath.Join(target, rel)
			scanned = append(scanned, scanItem{entry{path, dst, inf}, selected})
			return nil
		})
		if e != nil {
			return Result{p}, e
		}
		// Keep ancestors of selected descendants so nested matches remain reachable.
		parents := map[string]bool{}
		for _, item := range scanned {
			if item.selected {
				for dir := filepath.Dir(item.en.src); dir != filepath.Dir(src) && dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
					parents[dir] = true
				}
			}
		}
		for _, item := range scanned {
			if !item.selected && !parents[item.en.src] {
				continue
			}
			en := item.en
			entries = append(entries, en)
			if en.info.Mode().IsRegular() {
				p.TotalFiles++
				p.TotalBytes += en.info.Size()
			} else if en.info.Mode()&os.ModeSymlink != 0 {
				p.TotalFiles++
			}
		}
	}
	p.Phase = "copying"
	if opt.Operation == VerifyOnly {
		p.Phase = "verifying"
	}
	emit()
	var dirs []entry
	hardlinks := make(map[hardlinkID]copiedHardlink)
	devices := &deviceCache{entries: make(map[uint64]deviceResult)}
	var queue *verifyQueue
	if opt.Operation == Copy && opt.Verify {
		queue = newVerifyQueue(ctx, opt)
		ctx = queue.ctx
		defer func() { queue.closeAndDiscard(); result.Progress.PendingFiles = 0 }()
	}
	handleVerified := func(r verifiedResult) error {
		queue.pending--
		p.PendingFiles = queue.pending
		defer r.file.cleanup()
		if r.err != nil {
			if e := queue.firstError(); e != nil {
				return e
			}
			err := fmt.Errorf("verify %s: %w", r.file.en.src, r.err)
			if record(err) {
				return nil
			}
			return err
		}
		if err := checkParents(dest, filepath.Dir(r.file.en.dst)); err != nil {
			if record(err) {
				return nil
			}
			return err
		}
		err := commitStaged(ctx, r.file, opt)
		if err != nil {
			if failure := queue.firstError(); failure != nil {
				return failure
			}
		}
		if err != nil && opt.Mode == Skip && errors.Is(err, os.ErrExist) {
			p.SkippedFiles++
			emit()
			return nil
		}
		if err != nil {
			err = fmt.Errorf("commit %s: %w", r.file.en.src, err)
			if record(err) {
				return nil
			}
			return err
		}
		if r.file.en.info.Mode().IsRegular() {
			p.CommittedBytes += r.file.en.info.Size()
		}
		p.CopiedFiles++
		if opt.PreserveHardlinks {
			if id, ok := hardlinkIdentity(r.file.en.info); ok {
				info, e := os.Lstat(r.file.en.dst)
				if e != nil {
					if record(e) {
						return nil
					}
					return e
				}
				hardlinks[id] = copiedHardlink{r.file.en.dst, r.file.en.info, info}
			}
		}
		emit()
		return nil
	}
	pollVerified := func() error {
		if queue == nil {
			return nil
		}
		for {
			select {
			case r := <-queue.results:
				if err := handleVerified(r); err != nil {
					return err
				}
			default:
				if err := queue.firstError(); err != nil {
					return err
				}
				return nil
			}
		}
	}
	flushVerified := func() error {
		if queue == nil {
			return nil
		}
		for queue.pending > 0 {
			select {
			case r := <-queue.results:
				if err := handleVerified(r); err != nil {
					return err
				}
			case <-ctx.Done():
				if err := queue.firstError(); err != nil {
					return err
				}
				return ctx.Err()
			}
		}
		return nil
	}
	// Bound staged files (and open descriptors / uncommitted disk usage).
	waitVerificationCapacity := func() error {
		if queue == nil {
			return nil
		}
		for queue.pending >= 3 {
			select {
			case r := <-queue.results:
				if err := handleVerified(r); err != nil {
					return err
				}
			case <-ctx.Done():
				if failure := queue.firstError(); failure != nil {
					return failure
				}
				return ctx.Err()
			}
		}
		return nil
	}
	submitVerified := func(file *stagedFile) error {
		for {
			select {
			case r := <-queue.results:
				if err := handleVerified(r); err != nil {
					file.cleanup()
					return err
				}
			case queue.jobs <- file:
				queue.pending++
				p.PendingFiles = queue.pending
				emit()
				return nil
			case <-ctx.Done():
				file.cleanup()
				if err := queue.firstError(); err != nil {
					return err
				}
				return ctx.Err()
			}
		}
	}
	var prefetched map[string]prefetchedFile
	for index, en := range entries {
		batchFile, fromBatch := prefetched[en.src]
		if fromBatch {
			delete(prefetched, en.src)
		}
		if err = pollVerified(); err != nil {
			return Result{p}, err
		}
		if err = waitForTask(ctx, opt); err != nil {
			if queue != nil {
				if failure := queue.firstError(); failure != nil {
					return Result{p}, failure
				}
			}
			return Result{p}, err
		}
		p.Path = en.src
		if err = checkParents(dest, filepath.Dir(en.dst)); err != nil {
			if record(err) {
				continue
			}
			return Result{p}, err
		}
		existing, e := os.Lstat(en.dst)
		if e != nil && !os.IsNotExist(e) {
			if record(e) {
				continue
			}
			return Result{p}, e
		}
		if en.info.IsDir() {
			if opt.Operation == VerifyOnly {
				if e != nil || !existing.IsDir() || existing.Mode()&os.ModeSymlink != 0 {
					err := fmt.Errorf("missing or unsafe destination directory: %s", en.dst)
					if record(err) {
						continue
					}
					return Result{p}, err
				}
				continue
			}
			if e == nil {
				if !existing.IsDir() || existing.Mode()&os.ModeSymlink != 0 {
					err := fmt.Errorf("destination is not a directory: %s", en.dst)
					if record(err) {
						continue
					}
					return Result{p}, err
				}
			} else {
				if err = os.Mkdir(en.dst, 0700); err != nil {
					if record(err) {
						continue
					}
					return Result{p}, err
				}
				dirs = append(dirs, en)
			}
			continue
		}
		// Recheck the scan snapshot before evaluating a skip or opening data.
		scannedNow, scanErr := os.Lstat(en.src)
		if scanErr == nil && (!os.SameFile(en.info, scannedNow) || en.info.Mode().Type() != scannedNow.Mode().Type() || en.info.Size() != scannedNow.Size() || !en.info.ModTime().Equal(scannedNow.ModTime())) {
			scanErr = errors.New("source changed during scan")
		}
		if scanErr != nil {
			scanErr = fmt.Errorf("source %s: %w", en.src, scanErr)
			if record(scanErr) {
				continue
			}
			return Result{p}, scanErr
		}
		if opt.Operation == VerifyOnly {
			if e != nil {
				err := fmt.Errorf("missing destination: %s", en.dst)
				if record(err) {
					continue
				}
				return Result{p}, err
			}
			if os.SameFile(en.info, existing) {
				err := fmt.Errorf("source and destination are the same: %s", en.src)
				if record(err) {
					continue
				}
				return Result{p}, err
			}
			p.Phase = "verifying"
			emit()
			if err = verifyExisting(ctx, en, existing, opt, &p, emit); err != nil {
				err = fmt.Errorf("verify %s: %w", en.src, err)
				if record(err) {
					continue
				}
				return Result{p}, err
			}
			p.CopiedFiles++
			emit()
			continue
		}
		if e == nil {
			if os.SameFile(en.info, existing) {
				err := fmt.Errorf("source and destination are the same: %s", en.src)
				if record(err) {
					continue
				}
				return Result{p}, err
			}
			if existing.IsDir() {
				err := fmt.Errorf("destination is a directory: %s", en.dst)
				if record(err) {
					continue
				}
				return Result{p}, err
			}
			if skipExisting(en.info, existing, opt) {
				p.SkippedFiles++
				if en.info.Mode().IsRegular() {
					p.SkippedBytes += en.info.Size()
				}
				emit()
				continue
			}
		}
		bytesBefore := p.CopiedBytes
		id, linkable := hardlinkIdentity(en.info)
		if opt.PreserveHardlinks && linkable {
			if err = flushVerified(); err != nil {
				return Result{p}, err
			}
		}
		previous, hasPrevious := hardlinks[id]
		if en.info.Mode()&os.ModeSymlink != 0 {
			if err = flushVerified(); err != nil {
				return Result{p}, err
			}
			err = copyLink(ctx, en, opt)
		} else if opt.PreserveHardlinks && linkable && hasPrevious {
			err = makeHardlink(ctx, en, previous, opt)
			if err == nil {
				p.CopiedBytes += en.info.Size()
			}
		} else {
			p.Strategy = chooseStrategy(en.src, dest, opt.DiskMode, devices)
			if !fromBatch && len(prefetched) == 0 && p.Strategy == DiskSame {
				prefetched, err = prefetchBatch(ctx, entries[index:], dest, opt, devices, &p, emit)
				if err != nil {
					if queue != nil {
						if failure := queue.firstError(); failure != nil {
							return Result{p}, failure
						}
					}
					if record(err) {
						continue
					}
					return Result{p}, err
				}
				if b, ok := prefetched[en.src]; ok {
					batchFile = b
					fromBatch = true
					delete(prefetched, en.src)
				}
			}
			if queue != nil {
				if err = waitVerificationCapacity(); err != nil {
					return Result{p}, err
				}
				var staged *stagedFile
				var pre *prefetchedFile
				if fromBatch {
					pre = &batchFile
				}
				staged, err = stageFile(ctx, en, opt, &p, emit, pre)
				if err == nil {
					p.Phase = "verifying"
					emit()
					p.Phase = "copying"
					if err = submitVerified(staged); err != nil {
						return Result{p}, err
					}
					continue
				}
				if failure := queue.firstError(); failure != nil {
					return Result{p}, failure
				}
			} else {
				if fromBatch {
					var staged *stagedFile
					staged, err = stageFile(ctx, en, opt, &p, emit, &batchFile)
					if err == nil {
						err = commitStaged(ctx, staged, opt)
						staged.cleanup()
					}
				} else {
					err = copyFile(ctx, en, opt, &p, emit)
				}
			}
		}
		if err != nil && opt.Mode == Skip && errors.Is(err, os.ErrExist) {
			p.SkippedFiles++
			if en.info.Mode().IsRegular() {
				p.SkippedBytes += max(int64(0), en.info.Size()-(p.CopiedBytes-bytesBefore))
			}
			emit()
			continue
		}
		if err != nil {
			err = fmt.Errorf("copy %s: %w", en.src, err)
			if record(err) {
				continue
			}
			return Result{p}, err
		}
		if en.info.Mode().IsRegular() {
			p.CommittedBytes += en.info.Size()
		}
		p.CopiedFiles++
		if opt.PreserveHardlinks && linkable && !hasPrevious {
			info, e := os.Lstat(en.dst)
			if e != nil {
				return Result{p}, e
			}
			hardlinks[id] = copiedHardlink{en.dst, en.info, info}
		}
		emit()
	}
	if queue != nil && queue.pending > 0 {
		p.Phase = "verifying"
		emit()
		if err = flushVerified(); err != nil {
			return Result{p}, err
		}
	}
	// Empty private staging directories before restoring restrictive directory modes.
	if err = opt.scratch.cleanupTemps(); err != nil {
		return Result{p}, fmt.Errorf("remove staging directory: %w", err)
	}
	// Restore directory timestamps after their children have been created.
	for i := len(dirs) - 1; i >= 0; i-- {
		if err = waitForTask(ctx, opt); err != nil {
			return Result{p}, err
		}
		d := dirs[i]
		if err = os.Chmod(d.dst, d.info.Mode().Perm()); err != nil {
			if record(err) {
				continue
			}
			return Result{p}, err
		}
		if opt.PreserveXattrs {
			if err = copyDirectoryXattrs(d.src, d.dst); err != nil {
				err = fmt.Errorf("directory %s: %w", d.src, err)
				if record(err) {
					continue
				}
				return Result{p}, err
			}
		}
		if err = os.Chtimes(d.dst, d.info.ModTime(), d.info.ModTime()); err != nil {
			if record(err) {
				continue
			}
			return Result{p}, err
		}
		if opt.PreserveACL {
			if err = copyDirectoryACL(d.src, d.dst); err != nil {
				err = fmt.Errorf("directory ACL %s: %w", d.src, err)
				if record(err) {
					continue
				}
				return Result{p}, err
			}
		}
	}
	if err = ctx.Err(); err != nil {
		return Result{p}, err
	}
	p.Phase = "done"
	p.Finished = time.Now()
	p.Path = ""
	if len(problems) > 0 {
		p.Phase = "error"
		emit()
		return Result{p}, errors.Join(problems...)
	}
	emit()
	return Result{p}, nil
}

func checkParents(root, dir string) error {
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("destination escapes root")
	}
	cur := root
	if rel == "." {
		return nil
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		cur = filepath.Join(cur, part)
		inf, e := os.Lstat(cur)
		if e != nil {
			return e
		}
		if !inf.IsDir() || inf.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe destination directory: %s", cur)
		}
	}
	return nil
}
func copyLink(ctx context.Context, en entry, opt Options) error {
	current, err := os.Lstat(en.src)
	if err != nil {
		return err
	}
	if !os.SameFile(en.info, current) || current.Mode()&os.ModeSymlink == 0 || !current.ModTime().Equal(en.info.ModTime()) || current.Size() != en.info.Size() {
		return errors.New("symbolic link changed during scan")
	}
	link, err := os.Readlink(en.src)
	if err != nil {
		return err
	}
	tempdir, err := os.MkdirTemp(filepath.Dir(en.dst), ".fastcopy-link-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempdir)
	tmp := filepath.Join(tempdir, "link")
	if err = os.Symlink(link, tmp); err != nil {
		return err
	}
	if err = copyLinkMetadata(en.src, tmp, en.info, opt.PreserveXattrs, opt.PreserveACL); err != nil {
		return err
	}
	after, err := os.Lstat(en.src)
	if err != nil {
		return err
	}
	if !os.SameFile(current, after) || !current.ModTime().Equal(after.ModTime()) {
		return errors.New("symbolic link changed while copying")
	}
	if err = waitForTask(ctx, opt); err != nil {
		return err
	}
	return commitFile(tmp, en.dst, opt.Mode == Skip)
}

type chunk struct {
	data []byte
	err  error
}

func stageFile(ctx context.Context, en entry, opt Options, p *Progress, emit func(), batch *prefetchedFile) (*stagedFile, error) {
	p.IOBackendReason = ""
	in, err := os.Open(en.src)
	if err != nil {
		return nil, err
	}
	var out *os.File
	var tempInfo os.FileInfo
	success := false
	defer func() {
		if !success {
			in.Close()
			if out != nil {
				name := out.Name()
				out.Close()
				if info, e := os.Lstat(name); e == nil && tempInfo != nil && os.SameFile(tempInfo, info) {
					_ = os.Remove(name)
				}
			}
		}
	}()
	current, err := in.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(en.info, current) || !current.Mode().IsRegular() || current.Size() != en.info.Size() || !current.ModTime().Equal(en.info.ModTime()) {
		return nil, errors.New("source changed during scan")
	}
	// The per-directory staging folder is private and on the target filesystem.
	out, err = opt.scratch.tempFile(tempParent(en.dst))
	if err != nil {
		return nil, err
	}
	tempInfo, err = out.Stat()
	if err != nil {
		return nil, err
	}

	if opt.Preallocate && current.Size() >= 1024*1024 && !maybeSparse(current) {
		if _, err = preallocate(out, current.Size()); err != nil {
			return nil, fmt.Errorf("preallocate destination: %w", err)
		}
	}
	// Same-device mode performs large sequential read and write batches. Different-device
	// mode overlaps the two operations with a bounded pool of reusable chunks.
	h, err := newHash(opt.HashMode)
	if err != nil {
		return nil, err
	}
	handled := false
	if batch != nil {
		p.IOBackend = "batch_buffered"
		if !batch.direct {
			p.IOBackendReason = "来源批量读取未启用直接 I/O"
		}
		emit()
		handled, err = batchWriteDirect(ctx, out, batch.data, batch.direct, opt, p, emit, h)
		if err != nil {
			return nil, err
		}
		if handled {
			if batch.direct {
				p.IOBackend = "batch_direct"
			} else {
				p.IOBackend = "batch_write_direct"
			}
			emit()
		} else {
			p.IOBackend = "batch_buffered"
			if p.IOBackendReason != "" {
				p.IOBackendReason += "；"
			}
			p.IOBackendReason += "目标批量写入未启用直接 I/O"
			emit()
			configureCache(in, out)
			for off := 0; off < len(batch.data); {
				if err = waitForTask(ctx, opt); err != nil {
					return nil, err
				}
				n := min(opt.BufferSize, len(batch.data)-off)
				written, e := out.Write(batch.data[off : off+n])
				if e != nil {
					return nil, e
				}
				if written != n {
					return nil, io.ErrShortWrite
				}
				if opt.Verify {
					_, _ = h.Write(batch.data[off : off+n])
				}
				off += n
				p.CopiedBytes += int64(n)
				emit()
			}
			handled = true
		}
	}
	if !handled && maybeSparse(current) {
		handled, err = sparseCopy(ctx, in, out, current.Size(), opt, p, emit, h)
		if err != nil {
			return nil, err
		}
	}
	if !handled && opt.TargetDirect {
		handled, err = targetDirectCopy(ctx, en, in, out, opt, p, emit, h)
		if err != nil {
			return nil, err
		}
	}
	if !handled && !opt.TargetDirect {
		handled, err = directCopy(ctx, en, in, out, opt, p, emit, h)
		if err != nil {
			return nil, err
		}
	}
	if !handled {
		p.IOBackend = configureCache(in, out)
		if p.IOBackend == "buffered" && p.IOBackendReason == "" {
			p.IOBackendReason = "操作系统未启用无缓存 I/O"
		}
		emit()
		if p.Strategy == DiskSame {
			err = copyBatches(ctx, in, out, h, opt, p, emit)
		} else {
			err = copyParallel(ctx, in, out, h, opt, p, emit)
		}
		if err == nil {
			releaseCache(in)
		}
	}
	if err != nil {
		return nil, err
	}

	if err = waitForTask(ctx, opt); err != nil {
		return nil, err
	}
	after, err := in.Stat()
	if err != nil {
		return nil, err
	}
	if after.Size() != current.Size() || !after.ModTime().Equal(current.ModTime()) {
		return nil, errors.New("source changed while copying")
	}
	if err = out.Chmod(en.info.Mode().Perm()); err != nil {
		return nil, err
	}
	if opt.PreserveXattrs {
		if err = copyXattrs(in, out); err != nil {
			return nil, err
		}
	}
	if err = out.Sync(); err != nil {
		return nil, err
	}
	success = true
	return &stagedFile{en: en, source: in, output: out, current: current, tempInfo: tempInfo, digest: h.Sum(nil)}, nil
}

func copyFile(ctx context.Context, en entry, opt Options, p *Progress, emit func()) error {
	staged, err := stageFile(ctx, en, opt, p, emit, nil)
	if err != nil {
		return err
	}
	defer staged.cleanup()
	if opt.Verify {
		p.Phase = "verifying"
		emit()
		if opt.scratch.verify == nil {
			opt.scratch.verify = make([]byte, opt.BufferSize)
		}
		if err = verifyStaged(ctx, staged, opt, opt.scratch.verify); err != nil {
			return err
		}
		p.Phase = "copying"
	}
	return commitStaged(ctx, staged, opt)
}

// copyBatches minimizes seeks by finishing each large read before its write.
func copyBatches(ctx context.Context, in, out *os.File, h hash.Hash, opt Options, p *Progress, emit func()) error {
	if opt.scratch.same == nil {
		opt.scratch.same = make([]byte, opt.BatchSize)
	}
	buf := opt.scratch.same
	for {
		if err := waitForTask(ctx, opt); err != nil {
			return err
		}
		n, err := io.ReadFull(in, buf)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return err
		}
		if n > 0 {
			if e := waitForTask(ctx, opt); e != nil {
				return e
			}
			if _, e := out.Write(buf[:n]); e != nil {
				return e
			}
			if opt.Verify {
				_, _ = h.Write(buf[:n])
			}
			p.CopiedBytes += int64(n)
			emit()
		}
		if err != nil {
			return nil
		}
	}
}

// copyParallel overlaps reads and writes without allowing the producer to run
// ahead by more than two chunks. The reader is joined before its file is closed.
func copyParallel(ctx context.Context, in, out *os.File, h hash.Hash, opt Options, p *Progress, emit func()) error {
	ctx, cancel := context.WithCancel(ctx)
	free := make(chan []byte, 2)
	chunks := make(chan chunk, 2)
	for i := 0; i < 2; i++ {
		if opt.scratch.parallel[i] == nil {
			opt.scratch.parallel[i] = make([]byte, opt.BufferSize)
		}
		free <- opt.scratch.parallel[i]
	}
	done := make(chan struct{})
	defer func() { cancel(); <-done }()
	go func() {
		defer close(done)
		defer close(chunks)
		for {
			var b []byte
			select {
			case <-ctx.Done():
				return
			case b = <-free:
			}
			if waitForTask(ctx, opt) != nil {
				return
			}
			n, e := in.Read(b)
			if n > 0 {
				select {
				case chunks <- chunk{data: b[:n]}:
				case <-ctx.Done():
					return
				}
			}
			if e != nil {
				if e != io.EOF {
					select {
					case chunks <- chunk{err: e}:
					case <-ctx.Done():
					}
				}
				return
			}
		}
	}()
	for c := range chunks {
		if c.err != nil {
			return c.err
		}
		if err := waitForTask(ctx, opt); err != nil {
			return err
		}
		if _, err := out.Write(c.data); err != nil {
			return err
		}
		if opt.Verify {
			_, _ = h.Write(c.data)
		}
		p.CopiedBytes += int64(len(c.data))
		emit()
		select {
		case free <- c.data[:cap(c.data)]:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return ctx.Err()
}

func timestampsEqual(a, b time.Time, tolerance time.Duration) bool {
	d := a.Sub(b)
	return d >= -tolerance && d <= tolerance
}
