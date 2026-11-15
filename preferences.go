package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// preferences contains only settings that should carry over between launches.
// Text fields are saved verbatim so even incomplete edits remain visible.
type preferences struct {
	Operation         string `json:"operation"`
	Mode              string `json:"mode"`
	DiskMode          string `json:"disk_mode"`
	HashMode          string `json:"hash_mode"`
	Verify            bool   `json:"verify"`
	VerifySourceAgain bool   `json:"verify_source_again"`
	PreserveXattrs    bool   `json:"preserve_xattrs"`
	PreserveACL       bool   `json:"preserve_acl"`
	PreserveHardlinks bool   `json:"preserve_hardlinks"`
	Preallocate       bool   `json:"preallocate"`
	TargetDirect      bool   `json:"target_direct"`
	ChunkMB           string `json:"chunk_mb"`
	BatchMB           string `json:"batch_mb"`
	ToleranceMS       string `json:"tolerance_ms"`
}

func defaultPreferences() preferences {
	return preferences{Operation: "复制", Mode: "大小或时间不同", DiskMode: "自动", HashMode: "xxHash3", Verify: true, Preallocate: true}
}

func (a *app) preferences() preferences {
	return preferences{a.operation, a.mode, a.diskMode, a.hashMode, a.verify, a.verifySourceAgain,
		a.preserveXattrs, a.preserveACL, a.preserveHardlinks, a.preallocate, a.targetDirect,
		a.chunkMB, a.batchMB, a.toleranceMS}
}

func (a *app) applyPreferences(p preferences) {
	a.operation, a.mode, a.diskMode, a.hashMode = p.Operation, p.Mode, p.DiskMode, p.HashMode
	a.verify, a.verifySourceAgain = p.Verify, p.VerifySourceAgain
	a.preserveXattrs, a.preserveACL, a.preserveHardlinks = p.PreserveXattrs, p.PreserveACL, p.PreserveHardlinks
	a.preallocate, a.targetDirect = p.Preallocate, p.TargetDirect
	a.chunkMB, a.batchMB, a.toleranceMS = p.ChunkMB, p.BatchMB, p.ToleranceMS
}

type preferencesStore struct{ path string }

func defaultPreferencesStore() (*preferencesStore, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	return &preferencesStore{path: filepath.Join(dir, "fastcopy-go", "preferences.json")}, nil
}

func (s *preferencesStore) load() (preferences, error) {
	defaults := defaultPreferences()
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return defaults, nil
	}
	if err != nil {
		return defaults, err
	}
	p := defaults
	if err = json.Unmarshal(data, &p); err != nil {
		return defaults, fmt.Errorf("解析设置：%w", err)
	}
	// Unknown choices can come from a newer version or a manually edited file.
	// Fall back separately so valid settings are still retained.
	if !oneOf(p.Operation, "复制", "仅校验") {
		p.Operation = defaults.Operation
	}
	if !oneOf(p.Mode, "大小或时间不同", "覆盖", "跳过已有文件", "仅覆盖较旧文件") {
		p.Mode = defaults.Mode
	}
	if !oneOf(p.DiskMode, "自动", "同一硬盘", "不同设备 / SSD") {
		p.DiskMode = defaults.DiskMode
	}
	if !oneOf(p.HashMode, "xxHash3", "SHA-256", "SHA-512", "SHA3-256", "SHA3-512", "SHA-1", "MD5") {
		p.HashMode = defaults.HashMode
	}
	return p, nil
}

func oneOf(value string, choices ...string) bool {
	for _, choice := range choices {
		if value == choice {
			return true
		}
	}
	return false
}

func (s *preferencesStore) save(p preferences) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".preferences-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(name, s.path)
}

// persistPreferences is called after rendering controls and again on close.
// Comparing snapshots avoids writes during progress updates or idle redraws.
func (a *app) persistPreferences() {
	a.savePreferences(false)
}

func (a *app) savePreferences(force bool) {
	if a.preferencesStore == nil {
		return
	}
	p := a.preferences()
	if !force && p == a.lastPreferences {
		return
	}
	a.lastPreferences = p
	previousWarning := a.preferencesWarning
	if err := a.preferencesStore.save(p); err != nil {
		a.preferencesSaveFailed = true
		a.preferencesWarning = "无法保存设置：" + err.Error()
	} else {
		a.preferencesSaveFailed = false
		a.preferencesWarning = ""
	}
	if a.preferencesWarning != previousWarning && a.win != nil {
		a.win.Invalidate()
	}
}

// Retry a failed save on shutdown, even when the settings have not changed.
func (a *app) flushPreferences() {
	a.savePreferences(a.preferencesSaveFailed)
}
