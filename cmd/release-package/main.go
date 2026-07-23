package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/k911mipt/agent-managed-bash/internal/release"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "release-package: %s\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("expected build or verify subcommand")
	}
	epoch, err := sourceDateEpoch()
	if err != nil {
		return err
	}
	switch args[0] {
	case "build":
		flags := flag.NewFlagSet("build", flag.ContinueOnError)
		root := flags.String("root", ".", "repository root")
		output := flags.String("output", "dist", "archive output directory")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		archives, err := release.Build(ctx, release.BuildConfig{RepositoryRoot: *root, OutputDirectory: *output, Epoch: epoch})
		if err != nil {
			return err
		}
		for _, archive := range archives {
			fmt.Println(archive)
		}
		return nil
	case "verify":
		flags := flag.NewFlagSet("verify", flag.ContinueOnError)
		archive := flags.String("archive", "", "archive path")
		version := flags.String("version", "", "release version")
		goos := flags.String("os", "", "target operating system")
		goarch := flags.String("arch", "", "target architecture")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		return release.VerifyFile(release.VerifyConfig{
			ArchivePath: *archive, Version: *version, OS: *goos, Architecture: *goarch, Epoch: epoch,
		})
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func sourceDateEpoch() (time.Time, error) {
	raw := os.Getenv("SOURCE_DATE_EPOCH")
	if raw == "" {
		return time.Time{}, fmt.Errorf("SOURCE_DATE_EPOCH is required")
	}
	seconds, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || seconds < 0 {
		return time.Time{}, fmt.Errorf("SOURCE_DATE_EPOCH must be a non-negative integer")
	}
	return time.Unix(seconds, 0).UTC(), nil
}
