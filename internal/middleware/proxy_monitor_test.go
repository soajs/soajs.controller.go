package middleware

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

// When the monitor stops a request (134, 133), its answer is the one sent, even when the
// cancelled call to the service returns at once and answers 135 (redirect's error path).
func TestRequestMonitor_AnswerBeforeCancel(t *testing.T) {
	for _, c := range []struct {
		name      string
		renewals  int
		heartbeat bool
		code      string
	}{
		{"renewals exhausted", 1, true, `"code":134`},
		{"heartbeat down", 3, false, `"code":133`},
	} {
		rec := httptest.NewRecorder()
		m := &requestMonitor{w: rec, done: make(chan struct{}), last: time.Now()}
		// the call to the service, cancelled, returns at once: redirect answers 135
		m.cancel = func() { m.respond(135) }
		m.run(zap.NewNop(), 10*time.Millisecond, c.renewals, func() bool { return c.heartbeat })
		if body := rec.Body.String(); !strings.Contains(body, c.code) {
			t.Errorf("%s: %s, want %s", c.name, body, c.code)
		}
	}
}
