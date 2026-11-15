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
	if runtime.GOOS != "linux" {
		shell := plain.NewShell(plain.ShellOptions{Title: windowTitle, Width: 760, Height: 800, MinWidth: 680, MinHeight: minimumWindowHeight, Theme: theme.Onevra, Content: a.view})
		return shell.Window()
	}
	// KWin and other Wayland compositors may add a server-side title bar to
	// TitleBarHidden windows. Frameless asks MyGo for client-side decoration,
	// allowing this view to provide a single title bar and window controls.
	return mygo.NewWindow(mygo.WindowOptions{
		Title: windowTitle, Width: 760, Height: 800, MinWidth: 680, MinHeight: minimumWindowHeight,
		Frameless: true,
		Content: ui.View(func(c *ui.Context) {
			a.windowView(c, windowActions{
				minimize: func() { a.win.Minimize() }, toggleMaximize: func() { a.win.ToggleMaximize() },
				close: func() { a.win.Close() }, isMaximized: func() bool { return a.win != nil && a.win.IsMaximized() },
			})
		}),
	})
}

// windowView provides the Linux window chrome. DragWindow on the header keeps
// native move and double-click maximize behavior while its controls stay usable.
func (a *app) windowView(c *ui.Context, actions windowActions) {
	c.SetTheme(theme.Onevra(c))
	t := c.Theme()
	headerHeight := max(float32(48), t.Rem(3))
	components.WithDialogWindowDrag(c, headerHeight, func() {
		ui.Column(c).Fill().Background(t.Background).Clip().Children(func() {
			ui.Row(c).FillWidth().Height(headerHeight).Shrink(0).Padding(4, 12).Gap(4).AlignItems(ui.Center).DragWindow().Children(func() {
				components.Text(c, windowTitle).Bold().SingleLine()
				ui.Spacer(c)
				if components.IconButton(c, lucide.Minus(), "最小化", components.IconButtonPlain).Clicked() {
					actions.minimize()
				}
				maximizeIcon, maximizeLabel := lucide.Square(), "最大化"
				if actions.isMaximized() {
					maximizeIcon, maximizeLabel = lucide.Copy(), "还原窗口"
				}
				if components.IconButton(c, maximizeIcon, maximizeLabel, components.IconButtonPlain).Clicked() {
					actions.toggleMaximize()
				}
				if components.IconButton(c, lucide.X(), "关闭窗口", components.IconButtonPlain).Clicked() {
					actions.close()
				}
			})
			ui.Column(c).FillWidth().Grow(1).MinHeight(0).Children(func() { a.view(c) })
		})
	})
}
