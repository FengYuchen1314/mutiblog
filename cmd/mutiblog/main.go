package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/FengYuchen1314/mutiblog/internal/auth"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
	"github.com/FengYuchen1314/mutiblog/internal/server"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) > 1 && os.Args[1] == "reset-password" {
		return resetPassword(os.Args[2:], os.Stdin)
	}
	var (
		address     = flag.String("address", envOr("MUTIBLOG_ADDRESS", ":8080"), "HTTP listen address")
		dataDir     = flag.String("data-dir", envOr("MUTIBLOG_DATA_DIR", "./data"), "persistent data directory")
		consoleDir  = flag.String("console-dir", envOr("MUTIBLOG_CONSOLE_DIR", "./ui/console/dist"), "console distribution directory")
		nodeBinary  = flag.String("node-binary", envOr("MUTIBLOG_NODE_BINARY", "node"), "Node.js executable used by the static renderer")
		rendererCLI = flag.String("renderer-cli", envOr("MUTIBLOG_RENDERER_CLI", "./apps/renderer/dist/cli.mjs"), "static renderer CLI path")
	)
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	repository, err := fsrepo.Open(*dataDir)
	if err != nil {
		return fmt.Errorf("open data repository: %w", err)
	}

	app, err := server.New(server.Options{
		Repository:  repository,
		ConsoleDir:  *consoleDir,
		Version:     version,
		Logger:      logger,
		NodeBinary:  *nodeBinary,
		RendererCLI: *rendererCLI,
	})
	if err != nil {
		return fmt.Errorf("create server: %w", err)
	}
	defer app.Close()

	httpServer := &http.Server{
		Addr:              *address,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// Backup uploads are explicitly capped at 512 MiB and theme/media
		// uploads have tighter handler limits. Give those guarded bodies enough
		// time on a low-bandwidth self-hosted connection.
		ReadTimeout: 10 * time.Minute,
		// Restore and static-render gates intentionally finish before replying;
		// their previous-release rollback semantics must not be cut off by a
		// one-minute socket deadline.
		WriteTimeout: 15 * time.Minute,
		IdleTimeout:  90 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("mutiblog started", "address", *address, "dataDir", repository.Root(), "version", version)
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func resetPassword(arguments []string, stdin io.Reader) error {
	flags := flag.NewFlagSet("reset-password", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dataDir := flags.String("data-dir", envOr("MUTIBLOG_DATA_DIR", "./data"), "persistent data directory")
	passwordFile := flags.String("password-file", "-", "file containing the new password, or - for standard input")
	if err := flags.Parse(arguments); err != nil {
		return fmt.Errorf("parse reset-password options: %w", err)
	}
	if flags.NArg() != 0 {
		return errors.New("reset-password does not accept positional arguments")
	}
	reader := stdin
	var file *os.File
	if *passwordFile != "-" {
		info, err := os.Stat(*passwordFile)
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("password file must be a regular file")
		}
		if info.Mode().Perm()&0o077 != 0 {
			return errors.New("password file must not be accessible by group or other users")
		}
		file, err = os.Open(*passwordFile)
		if err != nil {
			return err
		}
		defer file.Close()
		reader = file
	}
	data, err := io.ReadAll(io.LimitReader(reader, 258))
	if err != nil {
		return fmt.Errorf("read new password: %w", err)
	}
	password := strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r")
	if len(password) < 12 || len(password) > 256 {
		return errors.New("new password must contain 12 to 256 characters")
	}
	initializedInfo, err := os.Stat(filepath.Join(*dataDir, "config", "initialized"))
	if err != nil || !initializedInfo.Mode().IsRegular() {
		return errors.New("MutiBlog is not initialized in the selected data directory")
	}
	repository, err := fsrepo.Open(*dataDir)
	if err != nil {
		return fmt.Errorf("open data repository: %w", err)
	}
	var admin domain.AdminConfig
	if err := repository.ReadYAML("config/admin.yaml", &admin); err != nil {
		return fmt.Errorf("read administrator: %w", err)
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return fmt.Errorf("hash new password: %w", err)
	}
	admin.PasswordHash = hash
	admin.UpdatedAt = time.Now().UTC()
	if err := repository.WriteYAML("config/admin.yaml", admin, true); err != nil {
		return fmt.Errorf("write administrator: %w", err)
	}
	fmt.Fprintln(os.Stdout, "Administrator password reset. Start MutiBlog again and log in with the new password.")
	return nil
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
