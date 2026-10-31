// Package app wires configuration, database, migrations and the HTTP server together.
package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/askuksov/yalkos/db"
	"github.com/askuksov/yalkos/internal/authn"
	"github.com/askuksov/yalkos/internal/blob"
	"github.com/askuksov/yalkos/internal/config"
	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/attachment"
	"github.com/askuksov/yalkos/internal/domain/audit"
	"github.com/askuksov/yalkos/internal/domain/comment"
	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/label"
	"github.com/askuksov/yalkos/internal/domain/milestone"
	"github.com/askuksov/yalkos/internal/domain/project"
	"github.com/askuksov/yalkos/internal/domain/timeline"
	"github.com/askuksov/yalkos/internal/graphql/linear"
	"github.com/askuksov/yalkos/internal/graphql/native"
	"github.com/askuksov/yalkos/internal/httpapi"
	"github.com/askuksov/yalkos/internal/store"
)

const shutdownTimeout = 10 * time.Second

// Run connects to the database, applies migrations and serves HTTP until ctx is done.
// onListen (optional) receives the actual listen address once the server accepts connections.
func Run(ctx context.Context, cfg config.Config, ui fs.FS, logger *slog.Logger, onListen func(addr string)) error {
	pool, err := store.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	migrations := db.Migrations()
	if err := store.Migrate(ctx, pool, migrations); err != nil {
		return err
	}
	current, _, err := store.MigrationStatus(ctx, pool, migrations)
	if err != nil {
		return err
	}
	logger.Info("migrations applied", slog.Int64("version", current))

	baseURL := cfg.BaseURL
	if baseURL == nil {
		baseURL = &url.URL{Scheme: "http", Host: "localhost:8080"}
	}
	accounts := account.New(pool, time.Now, baseURL.String())
	if err := accounts.RefreshSetupCode(ctx); err != nil {
		return err
	}
	if code := accounts.SetupCode(); code != "" {
		// One-time code: whoever opens this URL first claims the instance.
		logger.Warn("setup required", slog.String("url", baseURL.String()+"/setup?code="+code))
	}

	projects := project.New(pool, time.Now)
	issues := issue.New(pool, projects, time.Now)

	if cfg.DataDir == "" {
		return errors.New("DATA_DIR is required")
	}
	files, err := blob.NewLocalFS(cfg.DataDir)
	if err != nil {
		return err
	}
	maxUploadMB := cfg.MaxUploadMB
	if maxUploadMB == 0 {
		maxUploadMB = 25
	}
	attachments := attachment.New(pool, issues, files, &blob.Locks{}, int64(maxUploadMB)<<20, time.Now)
	issues.SetLinker(attachments)
	// Pending uploads and unreferenced files are removed hourly until shutdown.
	go attachments.RunCleanup(ctx, time.Hour, logger)

	router := httpapi.NewRouter(httpapi.Deps{
		Ready: func(c context.Context) error { return store.Ready(c, pool, migrations) },
		UI:    ui,
		NativeGraphQL: native.NewHandler(native.Deps{
			Accounts: accounts, Projects: projects, Issues: issues, Audit: audit.New(pool),
			Milestones: milestone.New(pool, projects, time.Now), Labels: label.New(pool, time.Now),
			Comments: comment.New(pool, issues, attachments, time.Now), Attachments: attachments,
			Timeline: timeline.New(pool), Logger: logger,
		}),
		LinearGraphQL: linear.NewHandler(accounts, logger),
		Logger:        logger,
		Auth:          authn.Middleware(accounts, baseURL, logger),
		Attachments:   attachments,
	})

	ln, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.ListenAddr, err)
	}
	srv := newServer(router)
	addr := ln.Addr().String()
	logger.Info("listening", slog.String("addr", addr))
	if onListen != nil {
		onListen(addr)
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("graceful shutdown: %w", err)
		}
		return nil
	}
}

// newServer builds the HTTP server with connection timeouts, so slow or idle
// clients cannot pile up connections.
func newServer(h http.Handler) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}
