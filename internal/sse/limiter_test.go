package sse

import "testing"

func TestLimiterPerIPCap(t *testing.T) {
	l := NewStreamLimiter(100, 2)
	if !l.Acquire("1.1.1.1") || !l.Acquire("1.1.1.1") {
		t.Fatal("first two acquires must succeed")
	}
	if l.Acquire("1.1.1.1") {
		t.Fatal("third acquire for same IP must fail")
	}
	if !l.Acquire("2.2.2.2") {
		t.Fatal("other IP must not be affected")
	}
	l.Release("1.1.1.1")
	if !l.Acquire("1.1.1.1") {
		t.Fatal("release must free a slot")
	}
}

func TestLimiterGlobalCap(t *testing.T) {
	l := NewStreamLimiter(2, 10)
	if !l.Acquire("1.1.1.1") || !l.Acquire("2.2.2.2") {
		t.Fatal("under global cap must succeed")
	}
	if l.Acquire("3.3.3.3") {
		t.Fatal("over global cap must fail")
	}
	l.Release("1.1.1.1")
	if !l.Acquire("3.3.3.3") {
		t.Fatal("release must free a global slot")
	}
}
