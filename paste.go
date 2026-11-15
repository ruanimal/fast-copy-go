package main

import (
	"errors"
	"strings"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

type filePaste struct {
	paths []string
	err   error
}

// queueFilePaste lets ordinary text pastes retain the input's usual editing
// behavior. File objects are handled after the editor has completed its update.
func queueFilePaste(svc ui.Services, pending **filePaste, text string) bool {
	data := svc.ReadClipboardData(transfer.FileList, transfer.URIList)
	if len(data.Items()) == 0 {
		return false
	}
	paths, err := data.Files()
	if err != nil {
		*pending = &filePaste{err: errors.New("无法读取剪贴板中的文件")}
		return true
	}
	// FileList and URIList can contain unsupported remote URLs alongside local
	// ones. Never silently accept only part of a copied selection.
	raw, rawErr := data.Read(transfer.FileList)
	if rawErr != nil {
		raw, rawErr = data.Read(transfer.URIList)
	}
	count := 0
	if rawErr == nil {
		for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				count++
			}
		}
	}
	if len(paths) == 0 && text != "" {
		// A browser URL or other plain text should still paste normally.
		return false
	}
	if rawErr != nil || len(paths) == 0 || len(paths) != count {
		*pending = &filePaste{err: errors.New("剪贴板包含无法识别的本地文件路径")}
	} else {
		*pending = &filePaste{paths: paths}
	}
	return true
}
