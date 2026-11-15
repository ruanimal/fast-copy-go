package main

import (
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"github.com/oneo-me/onevra/source/shells/plain"
	"github.com/oneo-me/onevra/source/theme"
)

const windowTitle = "Fast Copy"

type windowActions struct {
	minimize, toggleMaximize, close func()
	isMaximized                     func() bool
}

func newMainWindow(a *app) *mygo.Window {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		shell := plain.NewShell(plain.ShellOptions{Title: windowTitle, Width: 760, Height: 800, MinWidth: 680, MinHeight: minimumWindowHeight, Theme: theme.Onevra, Content: a.view})
		return shell.Window()
	}
	opts := mygo.WindowOptions{
		Title: windowTitle, Width: 760, Height: 800, MinWidth: 680, MinHeight: minimumWindowHeight,
		Content: ui.View(func(c *ui.Context) {
			var actions *windowActions
			if runtime.GOOS == "linux" {
				actions = &windowActions{
					minimize: func() { a.win.Minimize() }, toggleMaximize: func() { a.win.ToggleMaximize() },
					close: func() { a.win.Close() }, isMaximized: func() bool { return a.win != nil && a.win.IsMaximized() },
				}
			}
			a.windowView(c, actions)
		}),
	}
	if runtime.GOOS == "linux" {
		// Frameless avoids a second server-side title bar on Wayland.
		opts.Frameless = true
	} else {
		opts.TitleBarStyle = mygo.TitleBarHidden
		opts.TrafficLightPosition = &mygo.Point{X: 16, Y: 16}
	}
	return mygo.NewWindow(opts)
}

// windowView draws the header; a nil actions value leaves window controls to macOS.
func (a *app) windowView(c *ui.Context, actions *windowActions) {
	c.SetTheme(theme.Onevra(c))
	t := c.Theme()
	headerHeight := max(float32(48), t.Rem(3), c.TitleBar().Height)
	components.WithDialogWindowDrag(c, headerHeight, func() {
		ui.Column(c).Fill().Background(t.Background).Clip().Children(func() {
			ui.Row(c).FillWidth().Height(headerHeight).Shrink(0).Padding(4, 12).Gap(4).AlignItems(ui.Center).DragWindow().Children(func() {
				ui.Row(c).Absolute().Left(0).Right(0).Top(0).Bottom(0).Center().PassThrough().Children(func() {
					components.Text(c, windowTitle).Bold().SingleLine().PassThrough()
				})
				if actions == nil {
					return
				}
				ui.Spacer(c)
				if components.IconButton(c, lucide.Minus(), a.tr("最小化"), components.IconButtonPlain).Clicked() {
					actions.minimize()
				}
				maximizeIcon, maximizeLabel := lucide.Square(), "最大化"
				if actions.isMaximized() {
					maximizeIcon, maximizeLabel = lucide.Copy(), "还原窗口"
				}
				if components.IconButton(c, maximizeIcon, a.tr(maximizeLabel), components.IconButtonPlain).Clicked() {
					actions.toggleMaximize()
				}
				if components.IconButton(c, lucide.X(), a.tr("关闭窗口"), components.IconButtonPlain).Clicked() {
					actions.close()
				}
			})
			ui.Column(c).FillWidth().Grow(1).MinHeight(0).Children(func() { a.view(c) })
		})
	})
}
