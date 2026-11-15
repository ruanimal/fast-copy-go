package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDestinationHistoryPersistsRecentUniqueDirectories(t *testing.T) {
	root := t.TempDir()
	store := &destinationHistoryStore{path: filepath.Join(root, "config", "history.json")}
	if got, err := store.load(); err != nil || len(got) != 0 {
		t.Fatalf("empty history: %v, %v", got, err)
	}
	paths := make([]string, 9)
	for i := range paths {
		paths[i] = filepath.Join(root, string(rune('a'+i)))
		if _, err := store.remember(paths[i]); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{paths[8], paths[7], paths[6], paths[5], paths[4], paths[3], paths[2]}
	// A new store simulates a restart; paths need not exist to remain in history.
	reopened := &destinationHistoryStore{path: store.path}
	if got, err := reopened.load(); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("history after restart: %v, %v; want %v", got, err, want)
	}
	if _, err := reopened.remember(filepath.Join(paths[5], "..", filepath.Base(paths[5]))); err != nil {
		t.Fatal(err)
	}
	want = []string{paths[5], paths[8], paths[7], paths[6], paths[4], paths[3], paths[2]}
	if got, err := store.load(); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("reused destination: %v, %v; want %v", got, err, want)
	}
	info, err := os.Stat(filepath.Dir(store.path))
	if err != nil || info.Mode().Perm()&0077 != 0 {
		t.Fatalf("config directory permissions: %v, %v", info, err)
	}
}

func TestDestinationHistoryDoesNotOverwriteInvalidFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	original := []byte("invalid json")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	store := &destinationHistoryStore{path: path}
	if _, err := store.remember(filepath.Join(t.TempDir(), "target")); err == nil {
		t.Fatal("expected invalid history to fail")
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(original) {
		t.Fatalf("history was changed: %q, %v", got, err)
	}
}

func TestSanitizeHistoryKeepsSpacesAndFiltersInvalidPaths(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, " folder ")
	got := sanitizeHistory([]string{"", "relative", root + "/bad\nname", path, path, root})
	if want := []string{path, root}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v; want %v", got, want)
	}
}
