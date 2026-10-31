package httpapi

import (
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/authn"
	"github.com/askuksov/yalkos/internal/domain/attachment"
	"github.com/askuksov/yalkos/internal/domain/kit"
)

// multipartOverhead is allowed on top of the file limit for the multipart framing.
const multipartOverhead = 1 << 20

func writeUnauthenticated(w http.ResponseWriter) {
	writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
}

// writeFileError answers like INVALID_INPUT in GraphQL: error, field, reason, params.
func writeFileError(w http.ResponseWriter, logger *slog.Logger, err error) {
	var ie *kit.InputError
	switch {
	case errors.As(err, &ie):
		status := http.StatusBadRequest
		if ie.Reason == kit.ReasonTooLarge {
			status = http.StatusRequestEntityTooLarge
		}
		body := map[string]any{"error": ie.Msg, "field": ie.Field, "reason": ie.Reason}
		if ie.Params != nil {
			body["params"] = ie.Params
		}
		writeJSON(w, status, body)
	case errors.Is(err, kit.ErrForbidden):
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
	case errors.Is(err, kit.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	default:
		logger.Error("attachment request failed", slog.String("error", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
	}
}

func tooLargeErr(svc *attachment.Service) error {
	mb := int(svc.MaxBytes() >> 20)
	return kit.Input("file", kit.ReasonTooLarge, "file is too large").With("maxMb", mb)
}

// upload handles POST /api/attachments: multipart with an optional issueId
// field (sent before the file) and the file field, streamed to storage.
func upload(svc *attachment.Service, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authn.ActorFrom(r.Context())
		if !ok {
			writeUnauthenticated(w)
			return
		}
		limit := svc.MaxBytes() + multipartOverhead
		if r.ContentLength > limit {
			writeFileError(w, logger, tooLargeErr(svc))
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		mr, err := r.MultipartReader()
		if err != nil {
			writeFileError(w, logger, kit.Input("file", kit.ReasonInvalidFormat, "send the file as multipart/form-data"))
			return
		}
		var issueID *uuid.UUID
		for {
			part, err := mr.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				writeFileError(w, logger, malformed(svc, err))
				return
			}
			switch part.FormName() {
			case "issueId":
				v, err := io.ReadAll(io.LimitReader(part, 100))
				if err != nil {
					writeFileError(w, logger, partErr(svc, err))
					return
				}
				id, err := uuid.Parse(strings.TrimSpace(string(v)))
				if err != nil {
					writeFileError(w, logger, kit.Input("issueId", kit.ReasonInvalidFormat, "invalid issue id"))
					return
				}
				issueID = &id
			case "file":
				a, err := svc.Upload(r.Context(), actor.User, part.FileName(), part, issueID)
				if err != nil {
					writeFileError(w, logger, partErr(svc, err))
					return
				}
				writeJSON(w, http.StatusCreated, map[string]any{
					"id": a.ID, "url": attachment.URL(a), "name": a.Name, "size": a.Size,
					"contentType": a.ContentType, "inline": attachment.Inline(a.ContentType),
				})
				return
			}
			_ = part.Close()
		}
		writeFileError(w, logger, kit.Input("file", kit.ReasonRequired, "choose a file"))
	})
}

// partErr turns a body over the size cap into too_large.
func partErr(svc *attachment.Service, err error) error {
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) || errors.Is(err, multipart.ErrMessageTooLarge) {
		return tooLargeErr(svc)
	}
	return err
}

// malformed reports a multipart body that cannot be parsed as a client error.
func malformed(svc *attachment.Service, err error) error {
	if err := partErr(svc, err); err != nil && errors.As(err, new(*kit.InputError)) {
		return err
	}
	return kit.Input("file", kit.ReasonInvalidFormat, "malformed multipart body")
}

// download handles GET /api/attachments/{id}/{name}; the name is decoration.
// Only raster images and PDF render in the browser; nothing runs script in the
// app's origin (nosniff, sandbox for all but PDF, attachment for the rest).
func download(svc *attachment.Service, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := authn.ActorFrom(r.Context()); !ok {
			writeUnauthenticated(w)
			return
		}
		id, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			writeFileError(w, logger, kit.ErrNotFound)
			return
		}
		a, content, err := svc.Open(r.Context(), id)
		if err != nil {
			writeFileError(w, logger, err)
			return
		}
		defer func() { _ = content.Close() }()
		h := w.Header()
		h.Set("Content-Type", a.ContentType)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "private, max-age=31536000, immutable")
		disposition := "attachment"
		if attachment.Inline(a.ContentType) {
			disposition = "inline"
		}
		h.Set("Content-Disposition", disposition+"; filename*=UTF-8''"+encodeRFC5987(a.Name))
		// Chrome's PDF viewer refuses sandboxed documents; it runs no script in our origin.
		if a.ContentType != "application/pdf" {
			h.Set("Content-Security-Policy", "sandbox")
		}
		http.ServeContent(w, r, "", a.CreatedAt, content)
	})
}

// encodeRFC5987 percent-encodes every byte outside attr-char (RFC 5987).
func encodeRFC5987(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("!#$&+-.^_`|~", c) >= 0 {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&15])
	}
	return b.String()
}
