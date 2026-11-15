package copyengine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// A batch contains a bounded number of consecutive files. All reads complete
// before any of the batch is written, avoiding per-file read/write alternation
// on a shared rotating device. Sparse files and individual files whose hardlinks must be preserved retain
// their specialized paths.
const maxBatchFiles = 8192

type prefetchedFile struct {
	data   []byte
	direct bool
}

func prefetchBatch(ctx context.Context, entries []entry, root string, opt Options, devices *deviceCache, p *Progress, emit func()) (map[string]prefetchedFile, error) {
	if len(entries) < 2 {
		return nil, nil
	}
	var selected []entry
	total := int64(0)
	for _, en := range entries {
		if opt.PreserveHardlinks {
			if _, linked := hardlinkIdentity(en.info); linked {
				break
			}
		}
		if len(selected) >= maxBatchFiles || !en.info.Mode().IsRegular() || maybeSparse(en.info) || en.info.Size() < 0 || en.info.Size() > int64(opt.BatchSize)-total {
			break
		}
		if chooseStrategy(en.src, root, opt.DiskMode, devices) != DiskSame || checkParents(root, filepath.Dir(en.dst)) != nil {
			break
		}
		current, err := os.Lstat(en.src)
		if err != nil || !os.SameFile(en.info, current) || current.Size() != en.info.Size() || !current.ModTime().Equal(en.info.ModTime()) {
			break
		}
		existing, err := os.Lstat(en.dst)
		if err != nil && !os.IsNotExist(err) {
			break
		}
		if err == nil && (existing.IsDir() || os.SameFile(en.info, existing) || skipExisting(en.info, existing, opt)) {
			break
		}
		selected = append(selected, en)
		total += en.info.Size()
	}
	if len(selected) < 2 {
		return nil, nil
	}
	if cap(opt.scratch.batch) < int(total) {
		opt.scratch.batch = make([]byte, int(total))
	}
	storage := opt.scratch.batch[:int(total)]
	result := make(map[string]prefetchedFile, len(selected))
	offset := 0
	for _, en := range selected {
		if err := waitForTask(ctx, opt); err != nil {
			return nil, err
		}
		in, err := os.Open(en.src)
		if err != nil {
			if len(result) == 0 {
				return nil, fmt.Errorf("batch read %s: %w", en.src, err)
			}
			break
		}
		st, err := in.Stat()
		if err != nil || !os.SameFile(en.info, st) || st.Size() != en.info.Size() || !st.ModTime().Equal(en.info.ModTime()) {
			in.Close()
			if len(result) == 0 {
				if err == nil {
					err = errors.New("source changed during scan")
				}
				return nil, fmt.Errorf("batch read %s: %w", en.src, err)
			}
			break
		}
		data := storage[offset : offset+int(st.Size())]
		direct, err := batchReadDirect(ctx, en, in, data, opt)
		if err == nil && !direct {
			configureBatchInput(in)
			_, err = io.ReadFull(in, data)
			if len(data) == 0 && err == io.EOF {
				err = nil
			}
			releaseCache(in)
		}
		after, statErr := in.Stat()
		in.Close()
		if err != nil || statErr != nil || after.Size() != st.Size() || !after.ModTime().Equal(st.ModTime()) {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if len(result) == 0 {
				if err == nil {
					err = statErr
				}
				if err == nil {
					err = errors.New("source changed during batch read")
				}
				return nil, fmt.Errorf("batch read %s: %w", en.src, err)
			}
			break
		}
		result[en.src] = prefetchedFile{data: data, direct: direct}
		offset += len(data)
	}
	if len(result) < 2 {
		return nil, nil
	}
	p.BatchFiles = len(result)
	emit()
	return result, nil
}
func skipExisting(source, target os.FileInfo, opt Options) bool {
	return opt.Mode == Skip || (opt.Mode == Update && !source.ModTime().After(target.ModTime().Add(opt.TimestampTolerance))) || (opt.Mode == SizeDate && source.Mode().Type() == target.Mode().Type() && source.Size() == target.Size() && timestampsEqual(source.ModTime(), target.ModTime(), opt.TimestampTolerance))
}
