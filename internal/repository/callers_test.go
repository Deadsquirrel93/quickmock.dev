package repository

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// testRedis returns a client for QUICKMOCK_TEST_REDIS_ADDR or skips: the
// tracker's behaviour lives in Redis HyperLogLog, which a fake would not
// reproduce faithfully.
func testRedis(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("QUICKMOCK_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("QUICKMOCK_TEST_REDIS_ADDR not set, skipping test that needs Redis")
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("redis at %s: %v", addr, err)
	}
	t.Cleanup(func() { rdb.Close() })
	return rdb
}

// uniqueMockID keeps tests independent of each other and of earlier runs
// against the same Redis.
func uniqueMockID(t *testing.T) string {
	return fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixNano())
}

func TestCallerID(t *testing.T) {
	cases := []struct {
		name, a, b string
		same       bool
	}{
		{"ipv6 same /64 is one caller", "2001:db8:1:2:aaaa::1", "2001:db8:1:2:bbbb:cccc:dddd:2", true},
		{"ipv6 different /64 are two callers", "2001:db8:1:2::1", "2001:db8:1:3::1", false},
		{"ipv4 addresses stay distinct", "203.0.113.1", "203.0.113.2", false},
		{"ipv4-mapped ipv6 equals plain ipv4", "::ffff:203.0.113.7", "203.0.113.7", true},
		{"same ipv4 is one caller", "198.51.100.9", "198.51.100.9", true},
		{"unparseable values compare as raw strings", "not-an-ip", "not-an-ip", true},
		{"distinct unparseable values stay distinct", "not-an-ip", "also-not", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := callerID(c.a) == callerID(c.b); got != c.same {
				t.Fatalf("callerID(%q)=%q, callerID(%q)=%q; same=%v, want %v",
					c.a, callerID(c.a), c.b, callerID(c.b), got, c.same)
			}
		})
	}
}

func TestCallerTrackerNilIsNoop(t *testing.T) {
	var nilTracker *CallerTracker
	if n, crossed := nilTracker.Observe(context.Background(), "m", "203.0.113.1"); n != 0 || crossed {
		t.Fatalf("nil tracker Observe = (%d, %v), want (0, false)", n, crossed)
	}
	noRedis := NewCallerTracker(nil, 1)
	if n, crossed := noRedis.Observe(context.Background(), "m", "203.0.113.1"); n != 0 || crossed {
		t.Fatalf("tracker without Redis Observe = (%d, %v), want (0, false)", n, crossed)
	}
}

// Redis being down must never break mock serving: the signal is best-effort.
func TestCallerTrackerRedisDownIsNoop(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1", // nothing listens here
		DialTimeout: 50 * time.Millisecond,
		MaxRetries:  -1,
	})
	defer rdb.Close()

	tr := NewCallerTracker(rdb, 1)
	if n, crossed := tr.Observe(context.Background(), "m", "203.0.113.1"); n != 0 || crossed {
		t.Fatalf("Observe with Redis down = (%d, %v), want (0, false)", n, crossed)
	}
}

func TestCallerTrackerCountsDistinctCallers(t *testing.T) {
	tr := NewCallerTracker(testRedis(t), 100)
	ctx := context.Background()
	id := uniqueMockID(t)

	for range 5 {
		tr.Observe(ctx, id, "203.0.113.1")
	}
	if n, _ := tr.Observe(ctx, id, "203.0.113.1"); n != 1 {
		t.Fatalf("one IP calling repeatedly counted as %d callers, want 1", n)
	}
	if n, _ := tr.Observe(ctx, id, "203.0.113.2"); n != 2 {
		t.Fatalf("second IP: callers = %d, want 2", n)
	}
	if n, _ := tr.Observe(ctx, id, "2001:db8:1:2::1"); n != 3 {
		t.Fatalf("first IPv6: callers = %d, want 3", n)
	}
	if n, _ := tr.Observe(ctx, id, "2001:db8:1:2::99"); n != 3 {
		t.Fatalf("same /64 rotated address: callers = %d, want 3", n)
	}
}

func TestCallerTrackerReportsCrossingOnce(t *testing.T) {
	tr := NewCallerTracker(testRedis(t), 3)
	ctx := context.Background()
	id := uniqueMockID(t)

	steps := []struct {
		ip          string
		wantN       int64
		wantCrossed bool
	}{
		{"203.0.113.1", 1, false},
		{"203.0.113.2", 2, false},
		{"203.0.113.2", 2, false}, // repeat caller below threshold
		{"203.0.113.3", 3, true},  // reaches the threshold
		{"203.0.113.3", 3, false}, // same caller again: already reported
		{"203.0.113.4", 4, false}, // past the threshold: reported once only
		{"203.0.113.5", 5, false},
	}
	for i, s := range steps {
		n, crossed := tr.Observe(ctx, id, s.ip)
		if n != s.wantN || crossed != s.wantCrossed {
			t.Fatalf("step %d (%s): Observe = (%d, %v), want (%d, %v)", i, s.ip, n, crossed, s.wantN, s.wantCrossed)
		}
	}
}

func TestCallerTrackerIsPerMock(t *testing.T) {
	tr := NewCallerTracker(testRedis(t), 2)
	ctx := context.Background()
	a, b := uniqueMockID(t)+"-a", uniqueMockID(t)+"-b"

	tr.Observe(ctx, a, "203.0.113.1")
	tr.Observe(ctx, a, "203.0.113.2")
	if n, crossed := tr.Observe(ctx, b, "203.0.113.3"); n != 1 || crossed {
		t.Fatalf("other mock's callers leaked: Observe(b) = (%d, %v), want (1, false)", n, crossed)
	}
	if _, crossed := tr.Observe(ctx, b, "203.0.113.4"); !crossed {
		t.Fatal("mock b reaching its own threshold was not reported")
	}
}

// Keys must expire on their own: the tracker never deletes anything, and
// a sketch that outlives its day would leak memory for every mock ever hit.
func TestCallerTrackerKeysExpire(t *testing.T) {
	rdb := testRedis(t)
	tr := NewCallerTracker(rdb, 1)
	ctx := context.Background()
	id := uniqueMockID(t)

	if _, crossed := tr.Observe(ctx, id, "203.0.113.1"); !crossed {
		t.Fatal("threshold 1: first caller should be reported")
	}
	keys, err := rdb.Keys(ctx, "callers:"+id+":*").Result()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 {
		t.Fatalf("keys = %v, want the day's sketch and its flag", keys)
	}
	for _, k := range keys {
		ttl, err := rdb.TTL(ctx, k).Result()
		if err != nil {
			t.Fatal(err)
		}
		if ttl <= 0 || ttl > 48*time.Hour {
			t.Fatalf("TTL(%s) = %v, want within (0, 48h]", k, ttl)
		}
	}
}
