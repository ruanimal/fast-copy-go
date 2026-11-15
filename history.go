package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const maxDestinationHistory = 7

type destinationHistoryStore struct{ path string }

func defaultHistoryStore() (*destinationHistoryStore, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	return &destinationHistoryStore{path: filepath.Join(dir, "fastcopy-go", "destination-history.json")}, nil
}

// sanitizeHistory keeps the supplied most-recent-first order; missing folders
// remain available, since removable and network volumes can be disconnected.
func sanitizeHistory(paths []string) []string {
	result := make([]string, 0, min(len(paths), maxDestinationHistory))
	seen := map[string]bool{}
	for _, path := range paths {
		if path == "" || !filepath.IsAbs(path) || strings.Contains(path, "\n") {
			continue
		}
		path = filepath.Clean(path)
		if seen[path] {
			continue
		}
		seen[path] = true
		result = append(result, path)
		if len(result) == maxDestinationHistory {
			break
		}
	}
	return result
}
func recentDestination(paths []string, path string) []string {
	return sanitizeHistory(append([]string{path}, paths...))
}
func (s *destinationHistoryStore) load() ([]string, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var paths []string
	if err = json.Unmarshal(data, &paths); err != nil {
		return nil, fmt.Errorf("读取目标文件夹历史：%w", err)
	}
	return sanitizeHistory(paths), nil
}

// clear removes persisted history without touching the selected destination.
func (s *destinationHistoryStore) clear() error {
	err := os.Remove(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (a *app) clearDestinationHistory() {
	if a.historyStore != nil {
		if err := a.historyStore.clear(); err != nil {
			a.historyWarning = "无法清除历史记录：" + err.Error()
			return
		}
		a.historyWarning = ""
	}
	a.destinationHistory = nil
}

func (s *destinationHistoryStore) remember(path string) ([]string, error) {
	paths, err := s.load()
	if err != nil {
		return nil, err
	}
	paths = recentDestination(paths, path)
	data, err := json.Marshal(paths)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(s.path)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(dir, ".destination-history-*")
	if err != nil {
		return nil, err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return nil, err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	if err = os.Rename(tmp, s.path); err != nil {
		return nil, err
	}
	return paths, nil
}
