package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/egoist/mygo"
)

const checkingTargetStatus = "正在检查目标文件夹…"

// appendSources preserves each path exactly, with one source per line.
func (a *app) appendSources(paths []string) {
	if a.running || len(paths) == 0 {
		return
	}
	for _, path := range paths {
		if path == "" || strings.Contains(path, "\n") {
			a.status = "无法添加空路径或包含换行符的路径"
			return
		}
	}
	if a.sources != "" && !strings.HasSuffix(a.sources, "\n") {
		a.sources += "\n"
	}
	a.sources += strings.Join(paths, "\n")
}

func (a *app) cancelTargetDrop() {
	a.targetDropID++
	if a.targetDropPending && a.status == checkingTargetStatus {
		a.status = ""
	}
	a.targetDropPending = false
}

func checkDirectory(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("请选择文件夹")
	}
	return nil
}

// startTargetDrop validates off the UI thread, so a disconnected volume cannot
// freeze the window. A newer choice or task invalidates the previous result.
func (a *app) startTargetDrop(paths []string) {
	if a.running || len(paths) == 0 {
		return
	}
	a.cancelTargetDrop()
	if len(paths) != 1 {
		a.status = "失败：请只拖入一个目标文件夹"
		return
	}
	path := paths[0]
	if path == "" || strings.Contains(path, "\n") {
		a.status = "失败：目标路径为空或包含换行符"
		return
	}
	a.targetDropPending = true
	a.status = checkingTargetStatus
	id := a.targetDropID
	finish := func(err error) {
		if id != a.targetDropID || !a.targetDropPending || a.running {
			return
		}
		a.targetDropPending = false
		if err != nil {
			a.status = fmt.Sprintf("失败：无法使用目标文件夹：%v", err)
			return
		}
		a.destination = path
		a.status = "目标文件夹已设置"
	}
	if a.checkDroppedDirectory != nil {
		a.checkDroppedDirectory(path, finish)
		return
	}
	go func() {
		err := checkDirectory(path)
		mygo.RunOnMain(func() {
			finish(err)
			if a.win != nil {
				a.win.Invalidate()
			}
		})
	}()
}
