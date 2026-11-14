package middleware

import (
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// RecordedResponse is an answer of a service that the gateway keeps, to replay it (idempotency
// and cache).
type RecordedResponse struct {
	StatusCode int                    `bson:"statusCode"`
	Headers    map[string]interface{} `bson:"headers"`
	Body       string                 `bson:"body"`
}

// replayRecorded writes a recorded answer. The hop-by-hop headers of the original answer are
// not replayed.
func replayRecorded(w http.ResponseWriter, resp *RecordedResponse) {
	for k, v := range resp.Headers {
		switch strings.ToLower(k) {
		case "transfer-encoding", "connection", "keep-alive":
			continue
		}
		switch t := v.(type) {
		case string:
			w.Header().Set(k, t)
		case []interface{}:
			for _, e := range t {
				w.Header().Add(k, fmt.Sprint(e))
			}
		case primitive.A:
			for _, e := range t {
				w.Header().Add(k, fmt.Sprint(e))
			}
		case []string:
			for _, e := range t {
				w.Header().Add(k, e)
			}
		default:
			w.Header().Set(k, fmt.Sprint(t))
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write([]byte(resp.Body))
}

// recordedHeaders are a service's answer headers as Node.js records them (IncomingMessage
// headers): lowercase names, set-cookie as a list, others joined with ", ".
func recordedHeaders(h http.Header) map[string]interface{} {
	out := make(map[string]interface{}, len(h))
	for k, v := range h {
		name := strings.ToLower(k)
		if name == "set-cookie" {
			list := make([]interface{}, len(v))
			for i, e := range v {
				list[i] = e
			}
			out[name] = list
			continue
		}
		out[name] = strings.Join(v, ", ")
	}
	return out
}

// utf8Body is a body as Buffer.toString("utf8") gives it: each invalid byte is U+FFFD.
func utf8Body(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	var s strings.Builder
	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		if r == utf8.RuneError && size == 1 {
			s.WriteRune(utf8.RuneError)
		} else {
			s.Write(b[:size])
		}
		b = b[size:]
	}
	return s.String()
}
