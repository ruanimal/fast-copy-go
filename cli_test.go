package main

import (
	bytebuffer "bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/theme"
)

func TestParseCommand(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want command
		code int
	}{
		{"default GUI", nil, command{gui: true}, -1},
		{"GUI with paths", []string{"source one", "source two", "target"}, command{gui: true, sources: []string{"source one", "source two"}, destination: "target"}, -1},
		{"explicit GUI", []string{"--gui", "source", "target"}, command{gui: true, sources: []string{"source"}, destination: "target"}, -1},
		{"CLI", []string{"--cli", "--", "-source", "target"}, command{sources: []string{"-source"}, destination: "target"}, -1},
		{"help", []string{"--help"}, command{}, 0},
		{"missing destination", []string{"--cli", "source"}, command{}, 2},
		{"CLI without paths", []string{"--cli"}, command{}, 2},
		{"conflicting modes", []string{"--gui", "--cli", "source", "target"}, command{}, 2},
		{"unknown flag", []string{"--unknown"}, command{}, 2},
		{"empty path", []string{"", "target"}, command{}, 2},
		{"GUI newline", []string{"a\nb", "target"}, command{}, 2},
		{"CLI newline", []string{"--cli", "a\nb", "target"}, command{sources: []string{"a\nb"}, destination: "target"}, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytebuffer.Buffer
			got, code := parseCommand(tt.args, &stdout, &stderr)
			if code != tt.code || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseCommand() = %#v, %d; want %#v, %d; stderr=%s", got, code, tt.want, tt.code, stderr.String())
			}
			if code == 0 && !strings.Contains(stdout.String(), "Usage:") {
				t.Fatalf("missing help: %q", stdout.String())
			}
			if code == 2 && stderr.Len() == 0 {
				t.Fatal("missing error output")
			}
		})
	}
}

func TestGUICommandPrefillsWithoutStarting(t *testing.T) {
	cmd, code := parseCommand([]string{"first", "second", "destination"}, io.Discard, io.Discard)
	if code != -1 || !cmd.gui {
		t.Fatalf("expected GUI command: %#v, %d", cmd, code)
	}
	a := cmd.newApp()
	a.applyPreferences(defaultPreferences())
	tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.LightPalette()); a.view(c) }, 760, minimumWindowHeight)
	for i := 0; i < 3; i++ {
		tt.Frame()
		for label, want := range map[string]string{"来源路径": "first\nsecond", "目标文件夹路径": "destination"} {
			info, ok := tt.Access(label)
			if !ok || info.Value != want {
				t.Fatalf("%s: got %+v (found %v), want value %q", label, info, ok, want)
			}
		}
		if a.running {
			t.Fatal("task started without confirmation")
		}
		if _, ok := tt.Find("开始复制"); !ok {
			t.Fatal("start button is missing")
		}
	}
}

func TestCLICopiesMultipleSources(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source folder")
	dest := filepath.Join(root, "destination")
	for _, dir := range []string{source, dest} {
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	first := filepath.Join(source, "first")
	second := filepath.Join(root, "second")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("content"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	cmd, code := parseCommand([]string{"--cli", source, second, dest}, io.Discard, io.Discard)
	if code != -1 {
		t.Fatalf("parse code = %d", code)
	}
	var stdout, stderr bytebuffer.Buffer
	if code := runCLI(context.Background(), cmd, &stdout, &stderr); code != 0 {
		t.Fatalf("copy exit %d: %s", code, stderr.String())
	}
	for _, path := range []string{filepath.Join(dest, "source folder", "first"), filepath.Join(dest, "second")} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "content" {
			t.Fatalf("%s: %q, %v", path, data, err)
		}
	}
	if !strings.Contains(stdout.String(), "Copied 2") {
		t.Fatalf("summary: %s", stdout.String())
	}
}

func TestCLIErrorsAndCancellation(t *testing.T) {
	root := t.TempDir()
	dest := filepath.Join(root, "destination")
	if err := os.Mkdir(dest, 0755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "source")
	if err := os.WriteFile(source, []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dest, "source")
	if err := os.WriteFile(target, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range []struct {
		ctx context.Context
		cmd command
	}{
		{context.Background(), command{sources: []string{filepath.Join(root, "missing")}, destination: dest}},
		{ctx, command{sources: []string{source}, destination: dest}},
		{context.Background(), command{sources: []string{source}, destination: filepath.Join(root, "missing")}},
	} {
		var stdout, stderr bytebuffer.Buffer
		if code := runCLI(tc.ctx, tc.cmd, &stdout, &stderr); code == 0 || stderr.Len() == 0 {
			t.Fatalf("code=%d stderr=%q", code, stderr.String())
		}
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "original" {
		t.Fatalf("target changed: %q, %v", data, err)
	}
}
