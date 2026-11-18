package main

import (
	"runtime"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
)

func (a *app) advancedDialog(c *ui.Context) {
	t := c.Theme()
	verifyOnly := a.operation == "仅校验"
	components.Dialog(c, &a.advanced, components.DialogOptions{Label: a.tr("高级设置")}, func(panel ui.Element) {
		panel.Width(600).Height(460).Padding(22).Gap(12)
		components.Text(c, a.tr("高级设置")).FontSize(20).Bold()
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(10).Children(func() {
			components.Text(c, a.tr("语言")).FontSize(12).TextColor(t.TextMuted)
			components.NativeSelect(c, &a.language, []components.NativeSelectOption[string]{{Value: "", Label: "简体中文"}, {Value: "en", Label: "English"}}).Trigger.Width(160).Label(a.tr("界面语言"))
			components.Text(c, a.tr("主题")).FontSize(12).TextColor(t.TextMuted)
			components.NativeSelect(c, &a.themeMode, []components.NativeSelectOption[string]{{Value: "", Label: a.tr("自动")}, {Value: "light", Label: a.tr("亮色")}, {Value: "dark", Label: a.tr("暗色")}}).Trigger.Width(130).Label(a.tr("界面主题"))
		})
		components.Text(c, a.tr("文件属性与空间")).FontSize(12).TextColor(t.TextMuted)
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(18).Children(func() {
			components.Checkbox(c, &a.preserveXattrs, a.tr("扩展属性")).Disabled(verifyOnly)
			components.Checkbox(c, &a.preserveACL, "ACL").Disabled(verifyOnly)
			components.Checkbox(c, &a.preserveHardlinks, a.tr("硬链接")).Disabled(verifyOnly)
			components.Checkbox(c, &a.preallocate, a.tr("预分配空间")).Disabled(verifyOnly)
		})
		components.Text(c, a.tr("校验")).FontSize(12).TextColor(t.TextMuted)
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
			if verifyOnly {
				checked := true
				components.Checkbox(c, &checked, a.tr("校验文件")).Disabled(true)
			} else {
				components.Checkbox(c, &a.verify, a.tr("校验文件"))
			}
			components.NativeSelect(c, &a.hashMode, []components.NativeSelectOption[string]{{Value: "xxHash3", Label: "xxHash3"}, {Value: "SHA-256", Label: "SHA-256"}, {Value: "SHA-512", Label: "SHA-512"}, {Value: "SHA3-256", Label: "SHA3-256"}, {Value: "SHA3-512", Label: "SHA3-512"}, {Value: "SHA-1", Label: "SHA-1"}, {Value: "MD5", Label: "MD5"}}).Trigger.Width(140).Disabled(!a.verify && !verifyOnly).Label(a.tr("校验算法"))
			components.Checkbox(c, &a.verifySourceAgain, a.tr("重读来源")).Disabled(!a.verify || verifyOnly)
		})
		ui.Row(c).FillWidth().Gap(12).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(5).Children(func() {
				components.Text(c, a.tr("单次 I/O（MiB）")).FontSize(12).TextColor(t.TextMuted)
				components.TextBox(c, &a.chunkMB, components.TextBoxOptions{}).FillWidth().Placeholder("2").Label(a.tr("单次 I/O 大小"))
			})
			ui.Column(c).Grow(1).MinWidth(0).Gap(5).Children(func() {
				components.Text(c, a.tr("同盘缓冲（MiB）")).FontSize(12).TextColor(t.TextMuted)
				components.TextBox(c, &a.batchMB, components.TextBoxOptions{}).FillWidth().Placeholder("32").Label(a.tr("同盘缓冲大小"))
			})
			ui.Column(c).Grow(1).MinWidth(0).Gap(5).Children(func() {
				components.Text(c, a.tr("时间容差（毫秒）")).FontSize(12).TextColor(t.TextMuted)
				components.TextBox(c, &a.toleranceMS, components.TextBoxOptions{}).FillWidth().Placeholder("0").Label(a.tr("时间容差"))
			})
		})
		components.Checkbox(c, &a.targetDirect, a.tr("优先目标直接写入（Linux）")).Disabled(verifyOnly || runtime.GOOS != "linux").Tooltip(a.tr("来源使用程序缓冲区读取，目标尝试直接写入；不支持时在写入前回退。稀疏文件保留原有处理方式。"))
		if _, _, _, err := a.ioSettings(); err != nil {
			components.Text(c, a.localizeMessage(err.Error())).FontSize(12).SingleLine().Tooltip(a.localizeMessage(err.Error())).TextColor(t.Danger)
		}
		ui.Box(c).Grow(1).MinHeight(0)
		ui.Row(c).FillWidth().Children(func() {
			ui.Spacer(c)
			if components.Button(c, a.tr("关闭"), components.ButtonSecondary).Clicked() {
				a.advanced = false
			}
		})
	})
}
