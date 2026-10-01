// Command prep-mcp serves a folder of markdown files with YAML frontmatter
// to an MCP client over stdio. stdout carries the protocol; all logs go to stderr.
package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Arkoes07/prep-mcp/internal/config"
	"github.com/Arkoes07/prep-mcp/internal/server"
	"github.com/Arkoes07/prep-mcp/internal/vault"
	"github.com/Arkoes07/prep-mcp/internal/write"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "prep-mcp:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string, stderr io.Writer) error {
	flags := flag.NewFlagSet("prep-mcp", flag.ContinueOnError)
	flags.SetOutput(stderr)
	rootFlag := flags.String("root", "", "vault directory (env PREP_ROOT; default: current directory)")
	configFlag := flags.String("config", "", "config file (env PREP_CONFIG; default: <root>/prep.config.yaml)")
	showVersion := flags.Bool("version", false, "print the version and exit")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *showVersion {
		fmt.Fprintln(stderr, "prep-mcp", version)
		return nil
	}
	// stdout carries the protocol, so every log line goes to stderr.
	logger := slog.New(slog.NewTextHandler(stderr, nil))

	root := cmp.Or(*rootFlag, getenv("PREP_ROOT"))
	if root == "" {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("cannot find the current directory: %w; pass --root", err)
		}
		root = wd
	}
	cfgPath := cmp.Or(*configFlag, getenv("PREP_CONFIG"))
	explicit := cfgPath != ""
	if !explicit {
		cfgPath = filepath.Join(root, "prep.config.yaml")
	}

	cfg, err := config.Load(cfgPath)
	switch {
	case errors.Is(err, fs.ErrNotExist) && !explicit:
		logger.Warn("no config file; starting with no document types (queries still work)",
			slog.String("config", cfgPath))
		cfg, err = config.Parse(nil)
		if err != nil {
			return err
		}
	case err != nil:
		return fmt.Errorf("%w\nfix the config, or point --config / PREP_CONFIG at a valid file", err)
	}

	v, err := vault.Open(root, cfg, time.Now)
	if err != nil {
		return err
	}
	logger.Info("serving vault", slog.String("root", v.Dir()), slog.Int("types", len(cfg.Types)))
	w := write.New(v, cfg, &write.Git{Bin: "git", Author: cfg.Author, Timeout: 10 * time.Second}, time.Now)
	err = server.New(v, w, cfg, version, server.WithLogger(logger)).Run(ctx, &mcp.StdioTransport{})
	if err != nil && ctx.Err() == nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}
