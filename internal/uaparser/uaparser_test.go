package uaparser

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// The embedded ua-parser-js must stay 1.0.41 as published; a change must be deliberate.
func TestEmbeddedSourceIsPinned(t *testing.T) {
	sum := sha256.Sum256([]byte(source))
	if got := hex.EncodeToString(sum[:]); got != "9563363c1ea20cd7f5d751441b114278f9937e212dc84d29d5dd7a77c82cdbdb" {
		t.Fatalf("ua-parser.js changed: sha256 %s", got)
	}
	if !strings.Contains(source, "UAParser.js v1.0.41") {
		t.Fatal("ua-parser.js is not 1.0.41")
	}
}

// testdata/corpus.json was recorded from the Node.js gateway's own parseUserAgent
// (soajs.controller mw/mt/utils.js); every User-Agent must parse the same.
func TestCorpusParity(t *testing.T) {
	raw, err := os.ReadFile("testdata/corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus []struct {
		UA     string `json:"ua"`
		Parsed UA     `json:"parsed"`
	}
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	p := New(2, 16)
	p.timeout = Timeout * raceFactor
	for _, c := range corpus {
		got, err := p.Parse(c.UA)
		if err != nil {
			t.Errorf("%.60s: %v", c.UA, err)
			continue
		}
		if got != c.Parsed {
			t.Errorf("%.80s\n  got  %+v\n  want %+v", c.UA, got, c.Parsed)
		}
	}
}

// A 16 KB User-Agent is capped before the VM and parses quickly, as in the Node.js tests.
func TestLongUserAgentIsFast(t *testing.T) {
	p := New(1, 4)
	p.timeout = Timeout * raceFactor
	ua := "Mozilla/5.0 (" + strings.Repeat("X", 16*1024) + ") Gecko/20100101 Firefox/77.0"
	if _, err := p.Parse("curl/8.4.0"); err != nil { // creates and warms the VM
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := p.Parse(ua); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > Timeout*raceFactor {
		t.Errorf("a 16 KB User-Agent took %s", d)
	}
}

func TestCapUTF16(t *testing.T) {
	if got := capUTF16(strings.Repeat("a", 600), MaxLength); len(got) != MaxLength {
		t.Errorf("ASCII cap: %d", len(got))
	}
	// U+1F600 is two UTF-16 code units, as JavaScript counts them
	s := strings.Repeat("a", MaxLength-1) + "\U0001F600"
	if got := capUTF16(s, MaxLength); got != strings.Repeat("a", MaxLength-1) {
		t.Error("a code point that does not fit whole is cut")
	}
}

// A parse that runs past the timeout is an error, and the VM is usable afterwards.
func TestTimeout(t *testing.T) {
	p := New(1, 4)
	p.timeout = time.Nanosecond
	if _, err := p.Parse("Mozilla/5.0 (Windows NT 10.0) Chrome/120.0.0.0 Safari/537.36"); err != ErrTimeout {
		t.Fatalf("got %v, want ErrTimeout", err)
	}
	p.timeout = Timeout * raceFactor
	if ua, err := p.Parse("curl/8.4.0"); err != nil || ua.Family != "curl" {
		t.Fatalf("the VM must be usable after an interrupt: %+v %v", ua, err)
	}
}

func TestConcurrentParses(t *testing.T) {
	p := New(2, 8)
	p.timeout = Timeout * raceFactor
	uas := []string{"curl/8.4.0", "okhttp/4.12.0", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"}
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := p.Parse(uas[i%len(uas)] + strings.Repeat(" ", i%5)); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
}

func BenchmarkParse(b *testing.B) {
	p := New(1, 1)
	uas := []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.6099.130 Safari/537.36",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_2_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2 Mobile/15E148 Safari/604.1",
	}
	for i := 0; i < b.N; i++ {
		// a cache of 1 with two alternating User-Agents: every parse runs in the VM
		if _, err := p.Parse(uas[i%2]); err != nil {
			b.Fatal(err)
		}
	}
}
