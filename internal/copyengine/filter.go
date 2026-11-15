package copyengine

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

type filterPattern struct {
	pattern             string
	anchored, directory bool
}
type fileFilter struct{ includeFiles, includeDirs, excludeFiles, excludeDirs []filterPattern }

// Patterns use '/' as the path separator on both supported platforms. A
// leading '/' anchors at the source parent, a trailing '/' selects directories,
// and otherwise patterns match a suffix of path components.
func makeFilter(includes, excludes []string) (fileFilter, error) {
	var f fileFilter
	parse := func(raw string) (filterPattern, error) {
		raw = strings.TrimSpace(raw)
		p := filterPattern{anchored: strings.HasPrefix(raw, "/"), directory: strings.HasSuffix(raw, "/")}
		raw = strings.TrimPrefix(raw, "/")
		raw = strings.TrimSuffix(raw, "/")
		raw = strings.ReplaceAll(raw, "[!", "[^")
		if raw == "" {
			return p, fmt.Errorf("empty filter pattern")
		}
		if _, err := path.Match(raw, ""); err != nil {
			return p, fmt.Errorf("invalid filter %q: %w", raw, err)
		}
		p.pattern = raw
		return p, nil
	}
	for _, set := range []struct {
		patterns []string
		include  bool
	}{{includes, true}, {excludes, false}} {
		for _, raw := range set.patterns {
			if strings.TrimSpace(raw) == "" {
				continue
			}
			p, err := parse(raw)
			if err != nil {
				return f, err
			}
			switch {
			case set.include && p.directory:
				f.includeDirs = append(f.includeDirs, p)
			case set.include:
				f.includeFiles = append(f.includeFiles, p)
			case p.directory:
				f.excludeDirs = append(f.excludeDirs, p)
			default:
				f.excludeFiles = append(f.excludeFiles, p)
			}
		}
	}
	return f, nil
}
func (p filterPattern) matches(rel string) bool {
	rel = filepath.ToSlash(rel)
	if p.anchored {
		ok, _ := path.Match(p.pattern, rel)
		return ok
	}
	for {
		if ok, _ := path.Match(p.pattern, rel); ok {
			return true
		}
		i := strings.IndexByte(rel, '/')
		if i < 0 {
			return false
		}
		rel = rel[i+1:]
	}
}
func anyMatch(patterns []filterPattern, rel string) bool {
	for _, p := range patterns {
		if p.matches(rel) {
			return true
		}
	}
	return false
}
func anyDirectoryMatch(patterns []filterPattern, rel string) bool {
	for rel != "." && rel != "/" && rel != "" {
		if anyMatch(patterns, rel) {
			return true
		}
		rel = path.Dir(rel)
	}
	return false
}
func (f fileFilter) excluded(rel string, isDir bool) bool {
	if isDir {
		return anyMatch(f.excludeDirs, rel)
	}
	return anyMatch(f.excludeFiles, rel)
}
func (f fileFilter) includeDir(rel string) bool {
	return len(f.includeDirs) == 0 || anyDirectoryMatch(f.includeDirs, rel)
}
func (f fileFilter) includeFile(rel string) bool {
	return (len(f.includeFiles) == 0 || anyMatch(f.includeFiles, rel)) && (len(f.includeDirs) == 0 || anyDirectoryMatch(f.includeDirs, path.Dir(filepath.ToSlash(rel))))
}
