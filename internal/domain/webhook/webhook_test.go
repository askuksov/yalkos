package webhook_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/event"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/project"
	"github.com/askuksov/yalkos/internal/domain/webhook"
	"github.com/askuksov/yalkos/internal/testfx"
)

var secretRe = regexp.MustCompile(`^yal_wh_[0-9A-Za-z]{40}$`)

func ptr[T any](v T) *T { return &v }

type env struct {
	pool     *pgxpool.Pool
	users    testfx.Users
	opts     webhook.Options
	svc      *webhook.Service
	projects *project.Service
}

func newEnv(t *testing.T) env {
	t.Helper()
	pool, users, clock := testfx.New(t)
	opts := webhook.Options{Now: clock, AllowPrivate: true, BaseURL: "http://yalkos.test"}
	return env{pool: pool, users: users, opts: opts, svc: webhook.New(pool, opts), projects: project.New(pool, clock)}
}

func (e env) create(t *testing.T, in webhook.CreateInput) webhook.Webhook {
	t.Helper()
	if in.URL == "" {
		in.URL = "https://hooks.example.com/yalkos"
	}
	if in.Resources == nil {
		in.Resources = []event.Resource{event.Issue}
	}
	w, err := e.svc.Create(context.Background(), e.users.Admin, in)
	if err != nil {
		t.Fatalf("Create %q: %v", in.URL, err)
	}
	return w
}

func (e env) project(t *testing.T, key string) project.Project {
	t.Helper()
	p, err := e.projects.Create(context.Background(), e.users.Admin, project.CreateInput{Key: key, Name: key})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// wantInput asserts err is an InputError with exactly this field, reason and message.
func wantInput(t *testing.T, err error, field string, reason kit.Reason, msg string) {
	t.Helper()
	var ie *kit.InputError
	if !errors.As(err, &ie) {
		t.Fatalf("err = %v, want InputError %s/%s", err, field, reason)
	}
	if ie.Field != field || ie.Reason != reason || ie.Msg != msg {
		t.Fatalf("InputError = {%s %s %q}, want {%s %s %q}", ie.Field, ie.Reason, ie.Msg, field, reason, msg)
	}
}

// inputParams returns the Params of the InputError in err.
func inputParams(err error) map[string]any {
	var ie *kit.InputError
	if errors.As(err, &ie) {
		return ie.Params
	}
	return nil
}

func TestCreateGeneratesSecret(t *testing.T) {
	e := newEnv(t)
	a := e.create(t, webhook.CreateInput{})
	b := e.create(t, webhook.CreateInput{})
	for _, w := range []webhook.Webhook{a, b} {
		if !secretRe.MatchString(w.Secret) {
			t.Errorf("secret %q does not match %s", w.Secret, secretRe)
		}
	}
	if a.Secret == b.Secret {
		t.Error("two webhooks share a secret")
	}
	got, err := e.svc.Get(context.Background(), e.users.Admin, a.ID)
	if err != nil || got.Secret != a.Secret {
		t.Errorf("stored secret = %q (err %v), want %q", got.Secret, err, a.Secret)
	}
}

func TestCreateKeepsGivenSecret(t *testing.T) {
	e := newEnv(t)
	w := e.create(t, webhook.CreateInput{Secret: ptr(" s3cr3t ")})
	if w.Secret != " s3cr3t " {
		t.Errorf("returned secret = %q, want %q", w.Secret, " s3cr3t ")
	}
	got, err := e.svc.Get(context.Background(), e.users.Admin, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Secret != " s3cr3t " {
		t.Errorf("stored secret = %q, want %q", got.Secret, " s3cr3t ")
	}
}

func TestCreateFields(t *testing.T) {
	e := newEnv(t)
	p := e.project(t, "YLK")
	w, err := e.svc.Create(context.Background(), e.users.Admin, webhook.CreateInput{
		URL: "http://hooks.example.com:8080/x?y=1", Name: "  CI  ", ProjectID: &p.ID, Enabled: true,
		Resources: []event.Resource{event.Milestone, event.Issue, event.Milestone},
	})
	if err != nil {
		t.Fatal(err)
	}
	if w.Name != "CI" || w.URL != "http://hooks.example.com:8080/x?y=1" || !w.Enabled {
		t.Errorf("webhook = %+v", w)
	}
	if w.ProjectID == nil || *w.ProjectID != p.ID {
		t.Errorf("ProjectID = %v, want %v", w.ProjectID, p.ID)
	}
	if want := []event.Resource{event.Issue, event.Milestone}; !reflect.DeepEqual(w.Resources, want) {
		t.Errorf("Resources = %v, want %v (canonical order, no duplicates)", w.Resources, want)
	}
	if w.CreatedBy == nil || *w.CreatedBy != e.users.Admin.ID {
		t.Errorf("CreatedBy = %v, want %v", w.CreatedBy, e.users.Admin.ID)
	}
	if w.DisabledReason != nil || w.DisabledAt != nil || w.CreatedAt.IsZero() || !w.UpdatedAt.Equal(w.CreatedAt) {
		t.Errorf("unexpected state: %+v", w)
	}
	// Enabled false is kept as given.
	off := e.create(t, webhook.CreateInput{Enabled: false})
	if off.Enabled {
		t.Error("Enabled = true, want false")
	}
}

func TestCreateValidation(t *testing.T) {
	e := newEnv(t)
	strict := webhook.New(e.pool, webhook.Options{Now: e.opts.Now, AllowPrivate: false, BaseURL: e.opts.BaseURL})
	longURL := "http://example.com/" + strings.Repeat("a", 2049-len("http://example.com/"))
	const badURL = "use an absolute http or https URL"

	cases := []struct {
		name   string
		svc    *webhook.Service
		in     webhook.CreateInput
		field  string
		reason kit.Reason
		msg    string
	}{
		{"ftp scheme", e.svc, webhook.CreateInput{URL: "ftp://x"}, "url", kit.ReasonInvalidFormat, badURL},
		{"no host", e.svc, webhook.CreateInput{URL: "http://"}, "url", kit.ReasonInvalidFormat, badURL},
		{"relative", e.svc, webhook.CreateInput{URL: "/hook"}, "url", kit.ReasonInvalidFormat, badURL},
		{"user info", e.svc, webhook.CreateInput{URL: "http://u:p@h"}, "url", kit.ReasonInvalidFormat, badURL},
		{"fragment", e.svc, webhook.CreateInput{URL: "http://h/#f"}, "url", kit.ReasonInvalidFormat, badURL},
		{"empty fragment", e.svc, webhook.CreateInput{URL: "http://h/#"}, "url", kit.ReasonInvalidFormat, badURL},
		{"empty url", e.svc, webhook.CreateInput{URL: " "}, "url", kit.ReasonInvalidFormat, badURL},
		{"url too long", e.svc, webhook.CreateInput{URL: longURL}, "url", kit.ReasonTooLong, "is too long"},
		{"metadata ip", e.svc, webhook.CreateInput{URL: "http://169.254.169.254/"}, "url", kit.ReasonNotAllowed, "this address is not allowed"},
		{"metadata ip, private allowed", e.svc, webhook.CreateInput{URL: "http://169.254.169.254/latest"}, "url", kit.ReasonNotAllowed, "this address is not allowed"},
		{"nat64 metadata ip", e.svc, webhook.CreateInput{URL: "http://[64:ff9b::a9fe:a9fe]/"}, "url", kit.ReasonNotAllowed, "this address is not allowed"},
		{"mapped metadata ip", e.svc, webhook.CreateInput{URL: "http://[::ffff:169.254.169.254]/"}, "url", kit.ReasonNotAllowed, "this address is not allowed"},
		{"link-local v6", e.svc, webhook.CreateInput{URL: "http://[fe80::1]:8080/"}, "url", kit.ReasonNotAllowed, "this address is not allowed"},
		{"private ip, private denied", strict, webhook.CreateInput{URL: "http://10.0.0.1/"}, "url", kit.ReasonNotAllowed, "this address is not allowed"},
		{"nat64 private ip, private denied", strict, webhook.CreateInput{URL: "http://[64:ff9b::a00:5]/"}, "url", kit.ReasonNotAllowed, "this address is not allowed"},
		{"loopback, private denied", strict, webhook.CreateInput{URL: "http://127.0.0.1:3000/"}, "url", kit.ReasonNotAllowed, "this address is not allowed"},
		{"no resources", e.svc, webhook.CreateInput{Resources: []event.Resource{}}, "resources", kit.ReasonRequired, "choose at least one event type"},
		{"unknown resource", e.svc, webhook.CreateInput{Resources: []event.Resource{"project"}}, "resources", kit.ReasonInvalidFormat, "unknown event type"},
		{"empty secret", e.svc, webhook.CreateInput{Secret: ptr("")}, "secret", kit.ReasonTooShort, "is too short"},
		{"secret 256", e.svc, webhook.CreateInput{Secret: ptr(strings.Repeat("s", 256))}, "secret", kit.ReasonTooLong, "is too long"},
		{"name 101", e.svc, webhook.CreateInput{Name: strings.Repeat("n", 101)}, "name", kit.ReasonTooLong, "is too long"},
		{"unknown project", e.svc, webhook.CreateInput{ProjectID: ptr(uuid.New())}, "projectId", kit.ReasonNotFound, "no such project"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := c.in
			if in.URL == "" {
				in.URL = "https://hooks.example.com/ok"
			}
			if in.Resources == nil {
				in.Resources = []event.Resource{event.Issue}
			}
			_, err := c.svc.Create(context.Background(), e.users.Admin, in)
			wantInput(t, err, c.field, c.reason, c.msg)
			if c.reason == kit.ReasonTooLong {
				want := map[string]int{"url": 2048, "secret": 255, "name": 100}[c.field]
				if got := inputParams(err)["max"]; got != want {
					t.Errorf("Params[max] = %v, want %d", got, want)
				}
			}
		})
	}

	// Positive controls: the boundary values and allowed destinations pass.
	ok := []struct {
		name string
		svc  *webhook.Service
		in   webhook.CreateInput
	}{
		{"url 2048", e.svc, webhook.CreateInput{URL: longURL[:2048]}},
		{"private ip, private allowed", e.svc, webhook.CreateInput{URL: "http://10.0.0.1/"}},
		{"public ip, private denied", strict, webhook.CreateInput{URL: "http://93.184.216.34/"}},
		{"host name, private denied", strict, webhook.CreateInput{URL: "https://hooks.example.com/"}},
		{"secret 255", e.svc, webhook.CreateInput{Secret: ptr(strings.Repeat("s", 255))}},
		{"secret 1", e.svc, webhook.CreateInput{Secret: ptr("s")}},
		{"name 100", e.svc, webhook.CreateInput{Name: strings.Repeat("n", 100)}},
	}
	for _, c := range ok {
		t.Run(c.name, func(t *testing.T) {
			in := c.in
			if in.URL == "" {
				in.URL = "https://hooks.example.com/ok"
			}
			in.Resources = []event.Resource{event.Issue}
			if _, err := c.svc.Create(context.Background(), e.users.Admin, in); err != nil {
				t.Fatalf("Create: %v", err)
			}
		})
	}
}

func TestCreateLimit(t *testing.T) {
	e := newEnv(t)
	for i := 1; i <= 50; i++ {
		e.create(t, webhook.CreateInput{URL: fmt.Sprintf("https://hooks.example.com/%d", i)})
	}
	_, err := e.svc.Create(context.Background(), e.users.Admin, webhook.CreateInput{
		URL: "https://hooks.example.com/51", Resources: []event.Resource{event.Issue},
	})
	wantInput(t, err, "url", kit.ReasonTooMany, "a workspace can have at most 50 webhooks")
	if got := inputParams(err)["max"]; got != 50 {
		t.Errorf("Params[max] = %v, want 50", got)
	}
	list, err := e.svc.List(context.Background(), e.users.Admin)
	if err != nil || len(list) != 50 {
		t.Fatalf("List = %d webhooks (err %v), want 50", len(list), err)
	}
	// Deleting one frees a slot.
	if err := e.svc.Delete(context.Background(), e.users.Admin, list[0].ID); err != nil {
		t.Fatal(err)
	}
	e.create(t, webhook.CreateInput{URL: "https://hooks.example.com/again"})
}

func TestListNewestFirst(t *testing.T) {
	e := newEnv(t)
	a := e.create(t, webhook.CreateInput{URL: "https://a.example.com/"})
	b := e.create(t, webhook.CreateInput{URL: "https://b.example.com/"})
	c := e.create(t, webhook.CreateInput{URL: "https://c.example.com/"})
	list, err := e.svc.List(context.Background(), e.users.Owner)
	if err != nil {
		t.Fatal(err)
	}
	var got []uuid.UUID
	for _, w := range list {
		got = append(got, w.ID)
	}
	if want := []uuid.UUID{c.ID, b.ID, a.ID}; !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestNotFound(t *testing.T) {
	e := newEnv(t)
	ctx, admin, id := context.Background(), e.users.Admin, uuid.New()
	calls := map[string]func() error{
		"Get":          func() error { _, err := e.svc.Get(ctx, admin, id); return err },
		"Update":       func() error { _, err := e.svc.Update(ctx, admin, id, webhook.Patch{Name: ptr("x")}); return err },
		"Delete":       func() error { return e.svc.Delete(ctx, admin, id) },
		"RotateSecret": func() error { _, err := e.svc.RotateSecret(ctx, admin, id); return err },
	}
	for name, call := range calls {
		err := call()
		var nf kit.NotFoundError
		if !errors.As(err, &nf) || nf.Entity != "Webhook" || !errors.Is(err, kit.ErrNotFound) {
			t.Errorf("%s: err = %v, want NotFoundError{Webhook}", name, err)
		}
	}
	// Positive control: an existing id is found.
	w := e.create(t, webhook.CreateInput{})
	if _, err := e.svc.Get(ctx, admin, w.ID); err != nil {
		t.Errorf("Get existing: %v", err)
	}
}

func TestUpdateFields(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	w := e.create(t, webhook.CreateInput{Name: "old", Enabled: true})
	got, err := e.svc.Update(ctx, e.users.Admin, w.ID, webhook.Patch{
		URL: ptr("https://new.example.com/"), Name: ptr(" new "), Secret: ptr("  keep  "),
		Resources: &[]event.Resource{event.Label, event.Comment},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != "https://new.example.com/" || got.Name != "new" || got.Secret != "  keep  " {
		t.Errorf("updated = %+v", got)
	}
	if want := []event.Resource{event.Comment, event.Label}; !reflect.DeepEqual(got.Resources, want) {
		t.Errorf("Resources = %v, want %v", got.Resources, want)
	}
	if !got.Enabled || !got.UpdatedAt.After(w.UpdatedAt) || !got.CreatedAt.Equal(w.CreatedAt) {
		t.Errorf("Enabled/UpdatedAt/CreatedAt wrong: %+v (was %+v)", got, w)
	}

	// An empty patch keeps everything.
	same, err := e.svc.Update(ctx, e.users.Admin, w.ID, webhook.Patch{})
	if err != nil || same.URL != got.URL || same.Secret != got.Secret || !reflect.DeepEqual(same.Resources, got.Resources) {
		t.Errorf("empty patch changed the webhook: %+v (err %v)", same, err)
	}

	// Validation applies to patches; the row stays as it was.
	_, err = e.svc.Update(ctx, e.users.Admin, w.ID, webhook.Patch{URL: ptr("http://169.254.169.254/")})
	wantInput(t, err, "url", kit.ReasonNotAllowed, "this address is not allowed")
	_, err = e.svc.Update(ctx, e.users.Admin, w.ID, webhook.Patch{Resources: &[]event.Resource{}})
	wantInput(t, err, "resources", kit.ReasonRequired, "choose at least one event type")
	_, err = e.svc.Update(ctx, e.users.Admin, w.ID, webhook.Patch{Secret: ptr("")})
	wantInput(t, err, "secret", kit.ReasonTooShort, "is too short")
	_, err = e.svc.Update(ctx, e.users.Admin, w.ID, webhook.Patch{Name: ptr(strings.Repeat("n", 101))})
	wantInput(t, err, "name", kit.ReasonTooLong, "is too long")
	_, err = e.svc.Update(ctx, e.users.Admin, w.ID, webhook.Patch{ProjectID: kit.Opt[uuid.UUID]{Set: true, Value: ptr(uuid.New())}})
	wantInput(t, err, "projectId", kit.ReasonNotFound, "no such project")
	after, _ := e.svc.Get(ctx, e.users.Admin, w.ID)
	if after.URL != got.URL || after.Name != got.Name {
		t.Errorf("failed updates changed the row: %+v", after)
	}
}

func TestUpdateEnableClearsReason(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	w := e.create(t, webhook.CreateInput{Enabled: true})
	disable := func() {
		t.Helper()
		_, err := e.pool.Exec(ctx, `UPDATE webhooks SET enabled = false, disabled_reason = 'failures', disabled_at = now() WHERE id = $1`, w.ID)
		if err != nil {
			t.Fatal(err)
		}
	}

	disable()
	got, err := e.svc.Get(ctx, e.users.Admin, w.ID)
	if err != nil || got.DisabledReason == nil || *got.DisabledReason != webhook.ReasonFailures || got.DisabledAt == nil || got.Enabled {
		t.Fatalf("setup: %+v (err %v)", got, err)
	}

	// Positive control: an unrelated change keeps the automatic disable.
	got, err = e.svc.Update(ctx, e.users.Admin, w.ID, webhook.Patch{Name: ptr("renamed")})
	if err != nil || got.DisabledReason == nil || got.DisabledAt == nil || got.Enabled {
		t.Fatalf("rename changed the disable state: %+v (err %v)", got, err)
	}

	got, err = e.svc.Update(ctx, e.users.Admin, w.ID, webhook.Patch{Enabled: ptr(true)})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || got.DisabledReason != nil || got.DisabledAt != nil {
		t.Errorf("after enabling: %+v, want enabled with no reason", got)
	}
	stored, _ := e.svc.Get(ctx, e.users.Admin, w.ID)
	if !stored.Enabled || stored.DisabledReason != nil || stored.DisabledAt != nil {
		t.Errorf("stored after enabling: %+v", stored)
	}

	// Disabling by hand sets no reason.
	got, err = e.svc.Update(ctx, e.users.Admin, w.ID, webhook.Patch{Enabled: ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled || got.DisabledReason != nil || got.DisabledAt != nil {
		t.Errorf("after hand disable: %+v, want disabled with no reason", got)
	}

	// Disabling by hand a webhook that was disabled automatically drops the reason and the time.
	disable()
	got, err = e.svc.Update(ctx, e.users.Admin, w.ID, webhook.Patch{Enabled: ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled || got.DisabledReason != nil || got.DisabledAt != nil {
		t.Errorf("hand disable of auto-disabled: %+v, want disabled with no reason and no time", got)
	}
	stored, _ = e.svc.Get(ctx, e.users.Admin, w.ID)
	if stored.DisabledReason != nil || stored.DisabledAt != nil {
		t.Errorf("stored after hand disable of auto-disabled: %+v", stored)
	}
}

func TestCreateConcurrentLimit(t *testing.T) {
	e := newEnv(t)
	const total = 55
	errs := make([]error, total)
	var wg sync.WaitGroup
	for i := range total {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = e.svc.Create(context.Background(), e.users.Admin, webhook.CreateInput{
				URL: fmt.Sprintf("https://hooks.example.com/%d", i), Resources: []event.Resource{event.Issue},
			})
		}()
	}
	wg.Wait()
	ok, tooMany := 0, 0
	for _, err := range errs {
		var ie *kit.InputError
		switch {
		case err == nil:
			ok++
		case errors.As(err, &ie) && ie.Field == "url" && ie.Reason == kit.ReasonTooMany:
			tooMany++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if ok != 50 || tooMany != 5 {
		t.Errorf("succeeded %d, too_many %d; want 50 and 5", ok, tooMany)
	}
	list, err := e.svc.List(context.Background(), e.users.Admin)
	if err != nil || len(list) != 50 {
		t.Errorf("stored %d webhooks (err %v), want 50", len(list), err)
	}
}

func TestCreateTrimsURL(t *testing.T) {
	e := newEnv(t)
	w := e.create(t, webhook.CreateInput{URL: "  https://x.example.com/  "})
	if w.URL != "https://x.example.com/" {
		t.Errorf("returned URL = %q", w.URL)
	}
	got, err := e.svc.Get(context.Background(), e.users.Admin, w.ID)
	if err != nil || got.URL != "https://x.example.com/" {
		t.Errorf("stored URL = %q (err %v)", got.URL, err)
	}
	upd, err := e.svc.Update(context.Background(), e.users.Admin, w.ID, webhook.Patch{URL: ptr("\t https://y.example.com/ \n")})
	if err != nil || upd.URL != "https://y.example.com/" {
		t.Errorf("updated URL = %q (err %v)", upd.URL, err)
	}
}

func TestMultibyteLengthsCountRunes(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	name100 := strings.Repeat("я", 100)                                                                  // 200 bytes
	w := e.create(t, webhook.CreateInput{Name: name100, Secret: ptr(strings.Repeat("ключ", 63) + "кл")}) // 254 runes
	if w.Name != name100 {
		t.Errorf("name = %q", w.Name)
	}
	_, err := e.svc.Create(ctx, e.users.Admin, webhook.CreateInput{
		URL: "https://hooks.example.com/", Name: strings.Repeat("я", 101), Resources: []event.Resource{event.Issue},
	})
	wantInput(t, err, "name", kit.ReasonTooLong, "is too long")
	_, err = e.svc.Create(ctx, e.users.Admin, webhook.CreateInput{
		URL: "https://hooks.example.com/", Secret: ptr(strings.Repeat("я", 256)), Resources: []event.Resource{event.Issue},
	})
	wantInput(t, err, "secret", kit.ReasonTooLong, "is too long")
	// 255 non-ASCII characters (510 bytes) are accepted.
	if _, err := e.svc.Create(ctx, e.users.Admin, webhook.CreateInput{
		URL: "https://hooks.example.com/", Secret: ptr(strings.Repeat("я", 255)), Resources: []event.Resource{event.Issue},
	}); err != nil {
		t.Errorf("255-rune secret: %v", err)
	}
}

func TestNULRejected(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	in := func() webhook.CreateInput {
		return webhook.CreateInput{URL: "https://hooks.example.com/", Resources: []event.Resource{event.Issue}}
	}
	// Positive control: the same input without NUL is accepted.
	ok := in()
	ok.Name, ok.Secret = "name", ptr("secret")
	w, err := e.svc.Create(ctx, e.users.Admin, ok)
	if err != nil {
		t.Fatalf("control: %v", err)
	}

	bad := in()
	bad.Name = "na\x00me"
	_, err = e.svc.Create(ctx, e.users.Admin, bad)
	wantInput(t, err, "name", kit.ReasonInvalidFormat, "contains an invalid character")
	bad = in()
	bad.Secret = ptr("se\x00cret")
	_, err = e.svc.Create(ctx, e.users.Admin, bad)
	wantInput(t, err, "secret", kit.ReasonInvalidFormat, "contains an invalid character")
	bad = in()
	bad.URL = "https://hooks.example.com/a\x00b"
	_, err = e.svc.Create(ctx, e.users.Admin, bad)
	wantInput(t, err, "url", kit.ReasonInvalidFormat, "use an absolute http or https URL")

	_, err = e.svc.Update(ctx, e.users.Admin, w.ID, webhook.Patch{Name: ptr("\x00")})
	wantInput(t, err, "name", kit.ReasonInvalidFormat, "contains an invalid character")
	_, err = e.svc.Update(ctx, e.users.Admin, w.ID, webhook.Patch{Secret: ptr("\x00")})
	wantInput(t, err, "secret", kit.ReasonInvalidFormat, "contains an invalid character")
	_, err = e.svc.Update(ctx, e.users.Admin, w.ID, webhook.Patch{URL: ptr("https://h.example.com/\x00")})
	wantInput(t, err, "url", kit.ReasonInvalidFormat, "use an absolute http or https URL")
}

func TestUpdateProjectScope(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p := e.project(t, "ONE")
	w := e.create(t, webhook.CreateInput{})
	if w.ProjectID != nil {
		t.Fatalf("new webhook ProjectID = %v, want nil", w.ProjectID)
	}

	got, err := e.svc.Update(ctx, e.users.Admin, w.ID, webhook.Patch{ProjectID: kit.Opt[uuid.UUID]{Set: true, Value: &p.ID}})
	if err != nil || got.ProjectID == nil || *got.ProjectID != p.ID {
		t.Fatalf("set project: %+v (err %v)", got, err)
	}

	// Absent keeps the scope.
	got, err = e.svc.Update(ctx, e.users.Admin, w.ID, webhook.Patch{Name: ptr("x")})
	if err != nil || got.ProjectID == nil || *got.ProjectID != p.ID {
		t.Fatalf("absent kept scope: %+v (err %v)", got, err)
	}

	// Set with nil means all projects.
	got, err = e.svc.Update(ctx, e.users.Admin, w.ID, webhook.Patch{ProjectID: kit.Opt[uuid.UUID]{Set: true}})
	if err != nil || got.ProjectID != nil {
		t.Fatalf("clear project: %+v (err %v)", got, err)
	}

	// An archived project is accepted.
	if _, err := e.projects.SetArchived(ctx, e.users.Admin, p.ID, true); err != nil {
		t.Fatal(err)
	}
	got, err = e.svc.Update(ctx, e.users.Admin, w.ID, webhook.Patch{ProjectID: kit.Opt[uuid.UUID]{Set: true, Value: &p.ID}})
	if err != nil || got.ProjectID == nil || *got.ProjectID != p.ID {
		t.Fatalf("archived project: %+v (err %v)", got, err)
	}
	created, err := e.svc.Create(ctx, e.users.Admin, webhook.CreateInput{
		URL: "https://hooks.example.com/arch", ProjectID: &p.ID, Resources: []event.Resource{event.Issue},
	})
	if err != nil || created.ProjectID == nil {
		t.Fatalf("create on archived project: %+v (err %v)", created, err)
	}
}

func TestRotateSecret(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	w := e.create(t, webhook.CreateInput{Secret: ptr("given")})
	got, err := e.svc.RotateSecret(ctx, e.users.Admin, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Secret == w.Secret || !secretRe.MatchString(got.Secret) {
		t.Errorf("rotated secret = %q (old %q)", got.Secret, w.Secret)
	}
	if !got.UpdatedAt.After(w.UpdatedAt) {
		t.Errorf("UpdatedAt = %v, want after %v", got.UpdatedAt, w.UpdatedAt)
	}
	if got.URL != w.URL || got.Name != w.Name || !reflect.DeepEqual(got.Resources, w.Resources) {
		t.Errorf("rotation changed other fields: %+v vs %+v", got, w)
	}
	stored, _ := e.svc.Get(ctx, e.users.Admin, w.ID)
	if stored.Secret != got.Secret {
		t.Errorf("stored secret = %q, want %q", stored.Secret, got.Secret)
	}
}

func TestWebhookMemberForbidden(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	w := e.create(t, webhook.CreateInput{})
	member := e.users.Member

	calls := map[string]func(account.User) error{
		"List": func(u account.User) error { _, err := e.svc.List(ctx, u); return err },
		"Get":  func(u account.User) error { _, err := e.svc.Get(ctx, u, w.ID); return err },
		"Create": func(u account.User) error {
			_, err := e.svc.Create(ctx, u, webhook.CreateInput{URL: "https://m.example.com/", Resources: []event.Resource{event.Issue}})
			return err
		},
		"Update": func(u account.User) error {
			_, err := e.svc.Update(ctx, u, w.ID, webhook.Patch{Name: ptr("m")})
			return err
		},
		"RotateSecret": func(u account.User) error { _, err := e.svc.RotateSecret(ctx, u, w.ID); return err },
		"Delete":       func(u account.User) error { return e.svc.Delete(ctx, u, w.ID) },
	}
	for _, name := range []string{"List", "Get", "Create", "Update", "RotateSecret", "Delete"} {
		if err := calls[name](member); !errors.Is(err, kit.ErrForbidden) {
			t.Errorf("%s as Member: err = %v, want ErrForbidden", name, err)
		}
	}
	// The Member's failed Create and Delete changed nothing.
	list, err := e.svc.List(ctx, e.users.Admin)
	if err != nil || len(list) != 1 || list[0].Name != "" {
		t.Fatalf("webhooks after Member calls = %+v (err %v), want the untouched original", list, err)
	}
	// Positive control: Admin and Owner pass the same calls.
	for _, name := range []string{"List", "Get", "Create", "Update", "RotateSecret"} {
		for _, u := range []account.User{e.users.Admin, e.users.Owner} {
			if err := calls[name](u); err != nil {
				t.Errorf("%s as %s: %v", name, u.Role, err)
			}
		}
	}
	if err := calls["Delete"](e.users.Admin); err != nil {
		t.Errorf("Delete as Admin: %v", err)
	}
}

func TestDeleteProjectDeletesScopedWebhooks(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p := e.project(t, "DEL")
	scoped := e.create(t, webhook.CreateInput{URL: "https://scoped.example.com/", ProjectID: &p.ID})
	global := e.create(t, webhook.CreateInput{URL: "https://global.example.com/"})

	if err := e.projects.Delete(ctx, e.users.Owner, p.ID, "DEL"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Get(ctx, e.users.Admin, scoped.ID); !errors.Is(err, kit.ErrNotFound) {
		t.Errorf("scoped webhook after project delete: err = %v, want not found", err)
	}
	if _, err := e.svc.Get(ctx, e.users.Admin, global.ID); err != nil {
		t.Errorf("all-projects webhook: %v", err)
	}
}
