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
	"github.com/Arkoes07/prep-mcp/internal/read"
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
		if errors.Is(err, flag.ErrHelp) {
			return nil // usage is already printed; asking for it is not a failure
		}
		return err
	}
	if *showVersion {
		fmt.Fprintln(stderr, "prep-mcp", version)
		return nil
	}
	// stdout carries the protocol, so every log line goes to stderr, the
	// server's included.
	logger := slog.New(slog.NewTextHandler(stderr, nil))
	slog.SetDefault(logger)

	root, err := vaultRoot(*rootFlag, getenv)
	if err != nil {
		return err
	}
	cfg, err := loadConfig(cmp.Or(*configFlag, getenv("PREP_CONFIG")), root, logger)
	if err != nil {
		return err
	}

	v, err := vault.Open(root)
	if err != nil {
		return err
	}
	defer func() {
		if err := v.Close(); err != nil {
			logger.Warn("closing the vault failed", slog.Any("error", err))
		}
	}()
	logger.Info("serving vault", slog.String("root", v.Dir()), slog.Int("types", len(cfg.Types)))
	r := read.New(v, cfg, time.Now)
	w := write.New(
		v,
		cfg,
		&write.Git{Author: cfg.Author},
		time.Now,
	)
	srv := server.New(
		r,
		w,
		cfg,
		version,
	)
	err = srv.Run(ctx, &mcp.StdioTransport{})
	if err != nil && ctx.Err() == nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// vaultRoot picks the vault directory: --root, else PREP_ROOT, else the
// current directory.
func vaultRoot(flagValue string, getenv func(string) string) (string, error) {
	if root := cmp.Or(flagValue, getenv("PREP_ROOT")); root != "" {
		return root, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cannot find the current directory: %w; pass --root", err)
	}
	return wd, nil
}

// loadConfig loads the config at path, or <root>/prep.config.yaml when path
// is empty. A missing default config means an empty one, so reads still
// work; a missing explicit config is an error.
func loadConfig(path, root string, logger *slog.Logger) (*config.Config, error) {
	explicit := path != ""
	if !explicit {
		path = filepath.Join(root, "prep.config.yaml")
	}
	cfg, err := config.Load(path)
	switch {
	case errors.Is(err, fs.ErrNotExist) && !explicit:
		logger.Warn("no config file; starting with no document types (queries still work)",
			slog.String("config", path))
		return config.Parse(nil)
	case err != nil:
		return nil, fmt.Errorf("%w\nfix the config, or point --config / PREP_CONFIG at a valid file", err)
	}
	return cfg, nil
}
