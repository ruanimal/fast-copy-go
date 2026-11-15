package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"fastcopy-go/internal/copyengine"
)

type command struct {
	gui         bool
	sources     []string
	destination string
}

func (cmd command) newApp() *app {
	return &app{status: "选择来源与目标后开始复制", sources: strings.Join(cmd.sources, "\n"), destination: cmd.destination}
}

// A negative exit code means the command is ready to run.
func parseCommand(args []string, stdout, stderr io.Writer) (command, int) {
	cmd := command{gui: true}
	flags := flag.NewFlagSet("fast-copy", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	gui := flags.Bool("gui", false, "open the GUI and wait for confirmation (default)")
	cli := flags.Bool("cli", false, "copy directly without opening the GUI")
	usage := func(w io.Writer) {
		fmt.Fprintln(w, "Usage: fast-copy [--gui | --cli] [--] SOURCE... DESTINATION")
		fmt.Fprintln(w, "       fast-copy [--gui]")
		fmt.Fprintln(w, "Open the GUI by default; use --cli to copy directly.")
	}
	flags.Usage = func() {}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			usage(stdout)
			return command{}, 0
		}
		fmt.Fprintf(stderr, "fast-copy: %v\n", err)
		usage(stderr)
		return command{}, 2
	}
	if *gui && *cli {
		fmt.Fprintln(stderr, "fast-copy: --gui and --cli cannot be combined")
		return command{}, 2
	}
	cmd.gui = !*cli
	paths := flags.Args()
	if len(paths) == 0 && cmd.gui {
		return cmd, -1
	}
	if len(paths) < 2 {
		fmt.Fprintln(stderr, "fast-copy: provide at least one source and a destination")
		usage(stderr)
		return command{}, 2
	}
	for _, path := range paths {
		if path == "" || (cmd.gui && strings.Contains(path, "\n")) {
			fmt.Fprintln(stderr, "fast-copy: paths must not be empty; GUI paths must not contain newlines")
			return command{}, 2
		}
	}
	cmd.sources, cmd.destination = paths[:len(paths)-1], paths[len(paths)-1]
	return cmd, -1
}

func runCLI(ctx context.Context, cmd command, stdout, stderr io.Writer) int {
	result, err := copyengine.Run(ctx, copyengine.Options{
		Sources: cmd.sources, Destination: cmd.destination,
		Mode: copyengine.SizeDate, DiskMode: copyengine.DiskAuto,
		Verify: true, HashMode: copyengine.HashXXH3, Preallocate: true,
	}, nil)
	if err != nil {
		fmt.Fprintf(stderr, "fast-copy: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Copied %d file(s), skipped %d.\n", result.Progress.CopiedFiles, result.Progress.SkippedFiles)
	return 0
}
