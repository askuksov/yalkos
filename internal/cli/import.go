package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/askuksov/yalkos/internal/blob"
	"github.com/askuksov/yalkos/internal/config"
	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/importer/plane"
	"github.com/askuksov/yalkos/internal/store"
	"github.com/askuksov/yalkos/internal/store/q"
)

// The advisory lock lets one import run at a time.
const (
	importLockSQL   = "SELECT pg_try_advisory_lock(hashtext('yalkos:import'))"
	importUnlockSQL = "SELECT pg_advisory_unlock(hashtext('yalkos:import'))"
)

// stringList is a repeatable string flag.
type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

// importCmd dispatches "yalkos import <source>".
func importCmd(args []string, env Env) int {
	if len(args) < 1 || args[0] != "plane" {
		_, _ = fmt.Fprintf(env.Stderr, "import: expected \"plane\"\n\n%s", usage)
		return 2
	}
	return importPlane(args[1:], env)
}

// importPlane copies a Plane workspace into Yalkos. The Plane API key is read
// from PLANE_API_KEY only.
func importPlane(args []string, env Env) int {
	return importPlaneWith(args, env, plane.ClientOptions{})
}

// importPlaneWith is importPlane with the options of the Plane client, which
// tests use to skip the waits between retries.
func importPlaneWith(args []string, env Env, opts plane.ClientOptions) int {
	fs := flag.NewFlagSet("import plane", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	url := fs.String("url", "", "Plane base URL, for example https://plane.example.com")
	workspace := fs.String("workspace", "", "Plane workspace slug")
	var projects stringList
	fs.Var(&projects, "project", "Plane project identifier to import (repeatable; default all)")
	as := fs.String("as", "", "email of the Yalkos user recorded as the actor (default the Owner)")
	dryRun := fs.Bool("dry-run", false, "read Plane and report what would be created, write nothing")
	reportPath := fs.String("report", "", "also write the report as JSON to this file")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		_, _ = fmt.Fprintf(env.Stderr, "import plane: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if *url == "" || *workspace == "" {
		_, _ = fmt.Fprintln(env.Stderr, "import plane: --url and --workspace are required")
		return 2
	}
	// The address is stored in the import source and printed in the report, so
	// it must carry no credentials.
	if u, err := neturl.Parse(*url); err == nil && u.User != nil {
		_, _ = fmt.Fprintln(env.Stderr, "import plane: --url must not contain a user name or password; the key goes in PLANE_API_KEY")
		return 2
	}
	key := env.Getenv("PLANE_API_KEY")
	if key == "" {
		_, _ = fmt.Fprintln(env.Stderr, "import plane: set the Plane API key in the environment variable PLANE_API_KEY")
		return 2
	}
	client, err := plane.NewClient(*url, *workspace, key, opts)
	if err != nil {
		_, _ = fmt.Fprintln(env.Stderr, "import plane:", err)
		return 2
	}
	// The report file is prepared first: a path that cannot be written is a
	// usage error found before any work, not after a long run.
	var out *reportFile
	if *reportPath != "" {
		if out, err = openReportFile(*reportPath); err != nil {
			_, _ = fmt.Fprintln(env.Stderr, "import plane:", err)
			return 2
		}
		defer out.discard()
	}

	ctx := ctxOf(env)
	// fail reports an error before the run starts; Ctrl+C is not an error of
	// the setup but a stop.
	fail := func(err error) int {
		if ctx.Err() != nil {
			_, _ = fmt.Fprintln(env.Stderr, "import plane: stopped; run again to continue")
		} else {
			_, _ = fmt.Fprintln(env.Stderr, "import plane:", err)
		}
		return 1
	}
	cfg, err := config.Load(env.Getenv)
	if err != nil {
		return fail(err)
	}
	pool, err := store.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return fail(err)
	}
	defer pool.Close()

	actor, err := importActor(ctx, q.New(pool), *as)
	if err != nil {
		return fail(err)
	}
	blobs, err := blob.NewLocalFS(cfg.DataDir)
	if err != nil {
		return fail(err)
	}

	unlock, err := lockImport(ctx, pool)
	if err != nil {
		return fail(err)
	}
	defer unlock()

	report, runErr := plane.Run(ctx, plane.RunOptions{
		Client: client, Pool: pool, Store: blobs, MaxUpload: int64(cfg.MaxUploadMB) << 20,
		Actor: actor, Keys: projects, DryRun: *dryRun, URL: *url, Workspace: *workspace,
		Progress: env.Stderr, Now: time.Now,
	})

	var usageErr *plane.UsageError
	var conflict *plane.ConflictError
	isUsage, isConflict := errors.As(runErr, &usageErr), errors.As(runErr, &conflict)
	if report != nil {
		if runErr != nil && !isUsage && !isConflict {
			_, _ = fmt.Fprintln(env.Stdout, "Stopped before the end; counts show what was written.")
		}
		report.WriteText(env.Stdout)
		if out != nil {
			if err := out.write(report); err != nil {
				_, _ = fmt.Fprintln(env.Stderr, "import plane: report file:", err)
				if runErr == nil {
					return 1
				}
			}
		}
	}

	switch {
	case runErr == nil:
		return 0
	case isUsage:
		_, _ = fmt.Fprintln(env.Stderr, "import plane:", usageErr)
		return 2
	case isConflict:
		_, _ = fmt.Fprintf(env.Stderr, "import plane: nothing written; %v\n", conflict)
		return 1
	default:
		return fail(runErr)
	}
}

// importActor finds the user recorded as the actor of the import: the one
// with the email asEmail, else the workspace Owner. A workspace without an
// Owner is not set up yet.
func importActor(ctx context.Context, db *q.Queries, asEmail string) (account.User, error) {
	users, err := db.ListUsers(ctx)
	if err != nil {
		return account.User{}, fmt.Errorf("list users: %w", err)
	}
	var owner *q.User
	for i := range users {
		if users[i].Role == string(account.RoleOwner) {
			owner = &users[i]
			break
		}
	}
	if owner == nil {
		return account.User{}, errors.New("the workspace is not set up yet; set up the workspace first")
	}
	chosen := *owner
	if asEmail != "" {
		chosen, err = db.GetUserByEmail(ctx, asEmail)
		if errors.Is(err, pgx.ErrNoRows) {
			return account.User{}, fmt.Errorf("no user with email %s (--as)", asEmail)
		}
		if err != nil {
			return account.User{}, fmt.Errorf("find user: %w", err)
		}
	}
	if chosen.Disabled {
		return account.User{}, fmt.Errorf("user %s is disabled and cannot be the actor of the import", chosen.Email)
	}
	return account.User{
		ID: chosen.ID, Email: chosen.Email, Name: chosen.Name, Role: account.Role(chosen.Role),
		Disabled: chosen.Disabled, CreatedAt: chosen.CreatedAt, UpdatedAt: chosen.UpdatedAt,
	}, nil
}

// lockImport takes the import advisory lock on one pooled connection that it
// keeps until the returned function runs. A held lock is an error.
func lockImport(ctx context.Context, pool *pgxpool.Pool) (func(), error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection: %w", err)
	}
	var got bool
	if err := conn.QueryRow(ctx, importLockSQL).Scan(&got); err != nil {
		conn.Release()
		return nil, fmt.Errorf("take the import lock: %w", err)
	}
	if !got {
		conn.Release()
		return nil, errors.New("another import is running")
	}
	return func() {
		// The run may have ended because ctx was cancelled; unlock regardless.
		uctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var released bool
		if err := conn.QueryRow(uctx, importUnlockSQL).Scan(&released); err != nil || !released {
			// Closing the session drops the lock; never return it to the pool held.
			_ = conn.Hijack().Close(uctx)
			return
		}
		conn.Release()
	}, nil
}

// reportFile is the --report target. The JSON goes to a temporary file next
// to it that replaces the target only once a report is written, so a run that
// stops before it has a report leaves an earlier report as it was.
type reportFile struct {
	path string
	tmp  *os.File
}

// openReportFile creates the temporary file, private to the user (the report
// names people by email), in the directory of path.
func openReportFile(path string) (*reportFile, error) {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return nil, fmt.Errorf("report file: %s is a directory", path)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return nil, fmt.Errorf("report file: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return nil, fmt.Errorf("report file: %w", err)
	}
	return &reportFile{path: path, tmp: tmp}, nil
}

// write writes the report and puts it in place of the target.
func (f *reportFile) write(r *plane.Report) error {
	if err := r.WriteJSON(f.tmp); err != nil {
		return err
	}
	if err := f.tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.tmp.Name(), f.path); err != nil {
		return err
	}
	f.tmp = nil
	return nil
}

// discard removes the temporary file unless it became the report.
func (f *reportFile) discard() {
	if f.tmp == nil {
		return
	}
	_ = f.tmp.Close()
	_ = os.Remove(f.tmp.Name())
	f.tmp = nil
}
