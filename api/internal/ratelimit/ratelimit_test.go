package ratelimit

import (
	"sync"
	"testing"
	"time"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

func TestLimiter(t *testing.T) {
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	l := NewLimiter(60, 3, c.Now)
	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow("a"); !ok {
			t.Fatalf("request %d denied within burst", i)
		}
	}
	ok, retry := l.Allow("a")
	if ok || retry <= 0 || retry > time.Second {
		t.Fatalf("expected denial with ~1s retry, got ok=%v retry=%v", ok, retry)
	}
	if ok, _ := l.Allow("b"); !ok {
		t.Fatal("independent key denied")
	}
	c.Add(time.Second)
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("token not refilled")
	}
	c.Add(time.Hour)
	l.Sweep(time.Minute)
	if len(l.buckets) != 0 {
		t.Fatal("sweep left idle buckets")
	}
}

func TestGuardBackoff(t *testing.T) {
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	g := NewGuard(Rule{Max: 5, Window: time.Minute, Base: time.Minute, Cap: time.Hour}, c.Now)
	for i := 0; i < 5; i++ {
		if locked, _ := g.Record("ip"); locked {
			t.Fatalf("locked after %d attempts", i+1)
		}
	}
	locked, d := g.Record("ip")
	if !locked || d != time.Minute {
		t.Fatalf("expected 1m lockout, got %v %v", locked, d)
	}
	if l, r := g.Check("ip"); !l || r != time.Minute {
		t.Fatalf("check: %v %v", l, r)
	}
	c.Add(time.Minute)
	if l, _ := g.Check("ip"); l {
		t.Fatal("still locked after lockout expired")
	}
	for i := 0; i < 5; i++ {
		g.Record("ip")
	}
	if _, d := g.Record("ip"); d != 2*time.Minute {
		t.Fatalf("expected doubled lockout, got %v", d)
	}
	// Attempts spread out beyond the window never lock.
	for i := 0; i < 20; i++ {
		c.Add(13 * time.Second)
		if l, _ := g.Record("slow"); l {
			t.Fatal("slow attempts locked out")
		}
	}
	// Cap.
	for lvl := 0; lvl < 10; lvl++ {
		c.Add(2 * time.Hour)
		for i := 0; i < 5; i++ {
			g.Record("cap")
		}
		if _, d := g.Record("cap"); d > time.Hour {
			t.Fatalf("lockout %v exceeds cap", d)
		}
	}
}
