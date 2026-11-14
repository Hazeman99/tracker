package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"tracker/api/internal/auth"
	"tracker/api/internal/store"
)

// apiError is a client error whose message is safe to return.
type apiError struct {
	status int
	msg    string
}

func (e *apiError) Error() string { return e.msg }

func badRequest(format string, args ...any) error {
	return &apiError{status: http.StatusBadRequest, msg: fmt.Sprintf(format, args...)}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(true)
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// internal logs err server-side and returns a generic 500.
func (s *Server) internal(w http.ResponseWriter, r *http.Request, err error) {
	s.log.Error("internal error", "request_id", requestID(r), "err", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

// fail maps an error to a response. Only apiError messages and fixed strings
// reach the client.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	var ae *apiError
	switch {
	case errors.As(err, &ae):
		writeError(w, ae.status, ae.msg)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "already exists")
	case errors.Is(err, store.ErrInvalidOrder):
		writeError(w, http.StatusBadRequest, "order must list every project exactly once")
	default:
		s.internal(w, r, err)
	}
}

// decode strictly decodes a single JSON object from the request body into
// dst: the Content-Type must be application/json, unknown fields are
// rejected and nothing may follow the object.
func decode(r *http.Request, dst any) error {
	mt, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "application/json" {
		return &apiError{status: http.StatusUnsupportedMediaType, msg: "Content-Type must be application/json"}
	}
	for k, v := range params {
		if k != "charset" || !strings.EqualFold(v, "utf-8") {
			return &apiError{status: http.StatusUnsupportedMediaType, msg: "Content-Type must be application/json"}
		}
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return &apiError{status: http.StatusRequestEntityTooLarge, msg: "request body too large"}
		}
		return badRequest("could not read request body")
	}
	trimmed := bytes.TrimLeft(body, " \t\r\n")
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return badRequest("request body must be a JSON object")
	}
	if !utf8.Valid(body) {
		return badRequest("request body must be valid UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var ute *json.UnmarshalTypeError
		switch {
		case errors.As(err, &ute):
			return badRequest("invalid type for field %q", ute.Field)
		case strings.HasPrefix(err.Error(), "json: unknown field "):
			return badRequest("unknown field %s", strings.TrimPrefix(err.Error(), "json: unknown field "))
		default:
			return badRequest("malformed JSON")
		}
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return badRequest("request body must contain a single JSON object")
	}
	return nil
}

// pathID returns the {id} path value after validating that it is a UUID, so
// malformed ids never reach the database.
func pathID(r *http.Request) (string, error) {
	id := r.PathValue("id")
	if !auth.ValidID(id) {
		return "", badRequest("invalid id")
	}
	return id, nil
}

// Field limits, in Unicode code points.
const (
	maxName        = 200
	maxTaskTitle   = 500
	maxLongText    = 100_000
	maxTagName     = 40
	maxTags        = 20
	maxDeviceName  = 100
	maxEmail       = 254
	maxOrderLength = 10_000
)

// singleLine validates a one-line field: trimmed, required, no control
// characters, at most max code points.
func singleLine(field, v string, max int) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", badRequest("%s must not be empty", field)
	}
	if utf8.RuneCountInString(v) > max {
		return "", badRequest("%s must be at most %d characters", field, max)
	}
	for _, c := range v {
		if unicode.IsControl(c) || c == ' ' || c == ' ' {
			return "", badRequest("%s must not contain control characters", field)
		}
	}
	return v, nil
}

// multiLine validates long plain text: line breaks are normalised to \n and
// preserved, tabs are allowed, other control characters are rejected.
func multiLine(field, v string, max int, required bool) (string, error) {
	v = strings.ReplaceAll(v, "\r\n", "\n")
	v = strings.ReplaceAll(v, "\r", "\n")
	v = strings.TrimSpace(v)
	if required && v == "" {
		return "", badRequest("%s must not be empty", field)
	}
	if utf8.RuneCountInString(v) > max {
		return "", badRequest("%s must be at most %d characters", field, max)
	}
	for _, c := range v {
		if c != '\n' && c != '\t' && unicode.IsControl(c) {
			return "", badRequest("%s must not contain control characters", field)
		}
	}
	return v, nil
}

// tagNames validates and de-duplicates (case-insensitively) a tag list.
func tagNames(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	for _, raw := range in {
		name, err := singleLine("tag name", raw, maxTagName)
		if err != nil {
			return nil, err
		}
		dup := false
		for _, have := range out {
			if strings.EqualFold(have, name) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, name)
		}
		if len(out) > maxTags {
			return nil, badRequest("a project can have at most %d tags", maxTags)
		}
	}
	return out, nil
}
