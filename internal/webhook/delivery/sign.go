// Package delivery sends webhook deliveries: it signs and posts the payload
// and runs the River worker that retries failed attempts.
package delivery

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Sign returns the Linear-Signature of a body: the lower-case hex HMAC-SHA256
// of the exact bytes sent, keyed with the secret.
func Sign(secret string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return strings.ToUpper(hex.EncodeToString(m.Sum(nil)))
}

// Body returns the request body of an attempt: the stored envelope as compact
// JSON with webhookTimestamp (Unix milliseconds of at) added. It is built
// once per attempt and the same bytes are signed and sent.
func Body(envelope json.RawMessage, at time.Time) ([]byte, error) {
	var buf bytes.Buffer
	if err := json.Compact(&buf, envelope); err != nil {
		return nil, fmt.Errorf("compact envelope: %w", err)
	}
	b := buf.Bytes()
	if len(b) < 2 || b[0] != '{' || b[len(b)-1] != '}' {
		return nil, errors.New("envelope is not a JSON object")
	}
	b = b[:len(b)-1]
	if len(b) > 1 {
		b = append(b, ',')
	}
	b = append(b, `"webhookTimestamp":`...)
	b = strconv.AppendInt(b, at.UnixMilli(), 10)
	return append(b, '}'), nil
}
