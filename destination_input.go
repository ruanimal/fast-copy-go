package main

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

// destinationInput keeps text editing, file drops and history in one field.
func (a *app) destinationInput(c *ui.Context) {
	t := c.Theme()
	box := ui.Box(c).Grow(1).MinWidth(0)
	box.Children(func() {
		combo := ui.ComboboxBase(c, &a.destination)
		field := combo.Input.FillWidth().MinWidth(0).Padding(t.Space(1.5), t.Space(9), t.Space(1.5), t.Space(2.5)).
			Radius(t.Radius).Background(t.Surface.Alpha(.5)).Border(1, t.Border).
			ReadOnly(a.running).Placeholder("选择或拖入已有文件夹").Label("目标文件夹路径").AutoComplete(ui.AutoCompleteList)
		if a.running {
			field.Background(t.Surface.Mix(t.Text, .04).Alpha(.5))
		} else {
			if field.Focused() {
				field.Border(1, t.Accent)
			} else if field.Hovered() {
				field.Border(1, t.Border.Mix(t.Text, .25))
			}
			if field.Changed() && a.targetDropPending {
				a.cancelTargetDrop()
			}
			if paths := box.DroppedFiles(); len(paths) > 0 {
				a.startTargetDrop(paths)
			}
			if box.FileDragOver() {
				field.Border(2, t.Accent)
			}
		}
		ui.Box(c).Absolute().Right(2).Top(0).Bottom(0).Width(t.Space(8)).Center().Children(func() {
			arrow := components.IconButton(c, lucide.ChevronDown(), "目标文件夹历史", components.IconButtonCompact).KeepFocus().Disabled(a.running || len(a.destinationHistory) == 0)
			if arrow.Clicked() {
				field.Focus()
				combo.SetOpen(!combo.Open())
			}
		})
		if a.running || len(a.destinationHistory) == 0 {
			combo.SetOpen(false)
		} else {
			combo.Popup(func(panel ui.Element) {
				width, _ := c.Size()
				panel.Column().Padding(4).Gap(2).MaxWidth(max(0, width-40)).Radius(t.Radius).
					Background(t.Surface).Border(1, t.Border).Shadow(0, 4, 16, 0, ui.RGBA(0, 0, 0, .12)).Label("最近使用的目标文件夹")
				for _, path := range a.destinationHistory {
					if combo.Filtering() && !strings.Contains(strings.ToLower(path), strings.ToLower(a.destination)) {
						continue
					}
					item := combo.Item(path).Label(path).FillWidth().MinWidth(0).Padding(6, 10).Radius(t.Radius)
					if item.Highlighted() {
						item.Background(t.Accent.Alpha(.12))
					}
					item.Children(func() { components.Text(c, path).SingleLine().Ellipsis("…").MinWidth(0).Tooltip(path) })
				}
			})
			if path, ok := combo.Chosen(); ok {
				a.cancelTargetDrop()
				a.destination = path
			}
		}
	})
}
