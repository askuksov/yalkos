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
	"sync"
	"time"

	"github.com/google/uuid"

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
	"github.com/askuksov/yalkos/internal/domain/webhook"
	"github.com/askuksov/yalkos/internal/graphql/linear"
	"github.com/askuksov/yalkos/internal/graphql/native"
	"github.com/askuksov/yalkos/internal/httpapi"
	"github.com/askuksov/yalkos/internal/mcpserver"
	"github.com/askuksov/yalkos/internal/ratelimit"
	"github.com/askuksov/yalkos/internal/store"
	"github.com/askuksov/yalkos/internal/webhook/delivery"
	"github.com/askuksov/yalkos/internal/webhook/linearpayload"
)

const (
	shutdownTimeout = 10 * time.Second
	// queueSoftStop is how long running deliveries get to finish at shutdown;
	// a hard stop follows, which cancels them for up to queueHardStop. The sum
	// stays inside the container's stop grace period.
	queueSoftStop = 8 * time.Second
	queueHardStop = 2 * time.Second
)

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

	milestones := milestone.New(pool, projects, time.Now)
	labels := label.New(pool, time.Now)
	comments := comment.New(pool, issues, attachments, time.Now)

	// Webhook deliveries: changes are queued in the transaction that makes
	// them and worked by the River client below.
	sender := delivery.NewSender(delivery.SenderOptions{AllowPrivate: cfg.WebhookAllowPrivate})
	worker := &delivery.Worker{Sender: sender}
	queue, err := delivery.NewClient(pool, worker, false)
	if err != nil {
		return err
	}
	webhooks := webhook.New(pool, webhook.Options{
		AllowPrivate: cfg.WebhookAllowPrivate, BaseURL: baseURL.String(),
		Builder: linearpayload.New(baseURL.String()), Queue: queue,
	})
	worker.Service = webhooks
	webhooks.SetSender(sender)
	issues.SetEmitter(webhooks)
	projects.SetEmitter(webhooks)
	comments.SetEmitter(webhooks)
	labels.SetEmitter(webhooks)
	milestones.SetEmitter(webhooks)
	// Hourly, deliveries whose job is gone are failed, and finished deliveries
	// beyond the newest 50 per webhook are removed.
	go webhooks.RunCleanup(ctx, time.Hour, logger)
	var limiter *ratelimit.Limiter
	if cfg.APIRateLimit > 0 {
		limiter = ratelimit.New(cfg.APIRateLimit, time.Now)
	}
	authenticate := authn.Middleware(accounts, baseURL, logger)
	limit := ratelimit.Middleware(limiter, apiKeyUser)
	router := httpapi.NewRouter(httpapi.Deps{
		Ready: func(c context.Context) error { return store.Ready(c, pool, migrations) },
		UI:    ui,
		NativeGraphQL: native.NewHandler(native.Deps{
			Accounts: accounts, Projects: projects, Issues: issues, Audit: audit.New(pool),
			Milestones: milestones, Labels: labels, Comments: comments, Attachments: attachments,
			Timeline: timeline.New(pool), Webhooks: webhooks, Logger: logger,
		}),
		LinearGraphQL: linear.NewHandler(linear.Deps{
			Accounts: accounts, Projects: projects, Issues: issues, Milestones: milestones,
			Labels: labels, Comments: comments, Attachments: attachments,
			Webhooks: webhooks, BaseURL: baseURL, Logger: logger,
		}),
		MCP: mcpserver.NewHandler(mcpserver.Deps{
			Accounts: accounts, Projects: projects, Issues: issues, Milestones: milestones,
			Labels: labels, Comments: comments, Attachments: attachments,
			BaseURL: baseURL, Logger: logger,
		}),
		BaseURL:     baseURL,
		Logger:      logger,
		Auth:        func(h http.Handler) http.Handler { return authenticate(limit(h)) },
		Attachments: attachments,
	})

	// The queue runs on a context that a shutdown signal does not cancel:
	// running deliveries get their time to finish (see stopQueue). Whatever
	// path leaves Run, the queue is stopped before the pool closes.
	stopDeliveries := sync.OnceFunc(func() { stopQueue(queue, queueSoftStop, queueHardStop, logger) })
	if err := queue.Start(context.WithoutCancel(ctx)); err != nil {
		return fmt.Errorf("start delivery queue: %w", err)
	}
	defer stopDeliveries()

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
		// The queue stops in parallel with the server; the pool closes after both.
		queueDone := make(chan struct{})
		go func() {
			defer close(queueDone)
			stopDeliveries()
		}()
		err := srv.Shutdown(shutdownCtx)
		<-queueDone
		if err != nil {
			return fmt.Errorf("graceful shutdown: %w", err)
		}
		return nil
	}
}

// queueStopper is the part of the River client that stopQueue uses.
type queueStopper interface {
	Stop(ctx context.Context) error
	StopAndCancel(ctx context.Context) error
}

// stopQueue stops taking new jobs and gives running ones soft to finish. If
// they do not, they are canceled (a canceled attempt is put back and retried
// later) and waited for up to hard.
func stopQueue(q queueStopper, soft, hard time.Duration, logger *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), soft)
	defer cancel()
	err := q.Stop(ctx)
	if err == nil {
		return
	}
	logger.Warn("delivery queue did not stop in time, canceling running attempts", slog.String("error", err.Error()))
	ctx, cancelHard := context.WithTimeout(context.Background(), hard)
	defer cancelHard()
	if err := q.StopAndCancel(ctx); err != nil {
		logger.Error("delivery queue hard stop failed", slog.String("error", err.Error()))
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

// apiKeyUser returns the user of a request authenticated by an API key.
func apiKeyUser(r *http.Request) (uuid.UUID, bool) {
	a, ok := authn.ActorFrom(r.Context())
	if !ok || a.Via != "api_key" {
		return uuid.Nil, false
	}
	return a.User.ID, true
}
