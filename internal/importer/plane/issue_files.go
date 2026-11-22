package plane

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/blob"
	"github.com/askuksov/yalkos/internal/domain/attachment"
	"github.com/askuksov/yalkos/internal/netguard"
)

// storedFile is a Plane file (an attachment or an image of a description or
// comment) already in the blob store, waiting for its attachment row.
type storedFile struct {
	id       uuid.UUID
	planeID  string
	name     string
	uploader uuid.UUID
	created  time.Time
	sum      []byte
	size     int64
	ctype    string
	// mapped is set when another issue already has the Plane id in the import
	// map (an image pasted into two issues): the file gets no map row of its own.
	mapped bool
}

// issueFiles downloads the files of one work item into the blob store. Files
// are committed to the store before the issue's transaction; a file whose
// transaction then fails has no blob row and is removed by the attachment
// cleanup later.
type issueFiles struct {
	c     *Client
	store blob.Store
	limit int64
	pid   string
	iid   string
	key   string
	ex    *Existing
	out   *issueOutcome
	// byPlane holds every Plane id tried; nil when the file was not copied.
	byPlane map[string]*storedFile
	files   []*storedFile
}

// fetch copies the file with Plane id assetID. name is the Plane file name
// (empty: the name of the download); size is Plane's size, 0 when unknown.
// A file too large, refused by Plane or the storage (4xx) or at a blocked
// address is reported and left out. Server and network errors, which the
// client has already retried, stop the run like any other request: the issue
// is not written, and the next run tries the file again. So do a cancelled
// run and a failing blob store.
func (f *issueFiles) fetch(ctx context.Context, assetID, name string, size int64, uploader uuid.UUID, created time.Time) error {
	if _, seen := f.byPlane[assetID]; seen {
		return nil
	}
	f.byPlane[assetID] = nil
	label := func(n string) string {
		if n == "" {
			return assetID
		}
		return attachment.CleanName(n)
	}
	if f.limit > 0 && size > f.limit {
		f.out.tooLarge = append(f.out.tooLarge, f.key+": "+label(name))
		return nil
	}
	d, err := f.c.Download(ctx, f.pid, f.iid, assetID, f.limit)
	if err != nil {
		switch {
		case ctx.Err() != nil:
			return ctx.Err()
		case errors.Is(err, ErrTooLarge):
			f.out.tooLarge = append(f.out.tooLarge, f.key+": "+label(name))
		case skippable(err):
			f.skip(label(name), err)
		default:
			return fmt.Errorf("download %s: %w", label(name), err)
		}
		return nil
	}
	if name == "" {
		name = d.Name
	}
	name = label(name)
	body := &readRecorder{r: d.Body}
	limit := f.limit
	if limit <= 0 {
		limit = 1 << 62
	}
	st, err := f.store.Put(ctx, body, limit)
	_ = d.Body.Close()
	switch {
	case err == nil:
	case errors.Is(err, blob.ErrTooLarge) || errors.Is(err, ErrTooLarge):
		f.out.tooLarge = append(f.out.tooLarge, f.key+": "+name)
		return nil
	case ctx.Err() != nil:
		return ctx.Err()
	case body.err != nil:
		// The body broke off: a network error, retried by the next run.
		return fmt.Errorf("download %s: %w", name, body.err)
	default:
		return fmt.Errorf("store %s: %w", name, err)
	}
	if err := st.Commit(); err != nil {
		st.Discard()
		return fmt.Errorf("store %s: %w", name, err)
	}
	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("attachment id: %w", err)
	}
	_, mapped := f.ex.mapped(kindAttachment, assetID)
	file := &storedFile{
		id: id, planeID: assetID, name: name, uploader: uploader, created: created,
		sum: st.Sum, size: st.Size, ctype: http.DetectContentType(st.Head), mapped: mapped,
	}
	f.byPlane[assetID] = file
	f.files = append(f.files, file)
	return nil
}

// notUploaded reports a Plane file whose upload never finished; it is not
// downloaded, and a text image with the same id is not tried either.
func (f *issueFiles) notUploaded(assetID, name string) {
	if _, seen := f.byPlane[assetID]; seen {
		return
	}
	f.byPlane[assetID] = nil
	if name == "" {
		name = assetID
	}
	f.out.failed = append(f.out.failed, FailedAttachment{Key: f.key, Name: attachment.CleanName(name), Reason: "not uploaded"})
}

// skip reports a file whose download failed.
func (f *issueFiles) skip(name string, err error) {
	f.out.failed = append(f.out.failed, FailedAttachment{Key: f.key, Name: name, Reason: failReason(err)})
}

// skippable reports a download error that a later run would meet again: a
// 4xx answer of Plane or the storage, a blocked address or a redirect the
// client refuses to follow.
func skippable(err error) bool {
	var se *StatusError
	switch {
	case errors.As(err, &se):
		return se.Code >= http.StatusBadRequest && se.Code < http.StatusInternalServerError
	case errors.Is(err, netguard.ErrBlocked), errors.Is(err, errTooManyRedirects), errors.Is(err, errRedirectScheme):
		return true
	}
	return false
}

// failReason says why a download failed, without the request path.
func failReason(err error) string {
	var se *StatusError
	switch {
	case errors.Is(err, netguard.ErrBlocked):
		return "blocked"
	case errors.As(err, &se):
		return se.Error()
	}
	msg := err.Error()
	if strings.HasPrefix(msg, "plane ") {
		if _, rest, ok := strings.Cut(msg, ": "); ok {
			msg = rest
		}
	}
	return msg
}

// url is the Markdown link of a copied image; false when it was not copied.
func (f *issueFiles) url(assetID string) (string, bool) {
	file := f.byPlane[assetID]
	if file == nil {
		return "", false
	}
	return attachment.URL(attachment.Attachment{ID: file.id, Name: file.name}), true
}

// readRecorder keeps the first read error of a download, which tells a
// broken download from a failing blob store.
type readRecorder struct {
	r   io.Reader
	err error
}

func (r *readRecorder) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	if err != nil && !errors.Is(err, io.EOF) && r.err == nil {
		r.err = err
	}
	return n, err
}
