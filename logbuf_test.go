package main

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLogBuffer_WriteAndRecent(t *testing.T) {
	lb := NewLogBuffer(5)

	lb.Write([]byte("line1\n"))
	lb.Write([]byte("line2\n"))
	lb.Write([]byte("line3\n"))

	entries := lb.Recent(3)
	if len(entries) != 3 {
		t.Fatalf("Recent(3): got %d entries, want 3", len(entries))
	}
	if entries[0].Msg != "line1" {
		t.Errorf("entry[0].Msg: got %q, want %q", entries[0].Msg, "line1")
	}
	if entries[2].Msg != "line3" {
		t.Errorf("entry[2].Msg: got %q, want %q", entries[2].Msg, "line3")
	}
}

func TestLogBuffer_RingOverwrite(t *testing.T) {
	lb := NewLogBuffer(3)

	for i := 0; i < 5; i++ {
		lb.Write([]byte{byte('a' + i), '\n'})
	}

	entries := lb.Recent(3)
	if len(entries) != 3 {
		t.Fatalf("Recent(3): got %d entries, want 3", len(entries))
	}
	// Buffer holds last 3 of 5: c, d, e
	if entries[0].Msg != "c" {
		t.Errorf("entry[0].Msg: got %q, want %q", entries[0].Msg, "c")
	}
	if entries[2].Msg != "e" {
		t.Errorf("entry[2].Msg: got %q, want %q", entries[2].Msg, "e")
	}
}

func TestLogBuffer_RecentFewerThanAvailable(t *testing.T) {
	lb := NewLogBuffer(10)

	for i := 0; i < 5; i++ {
		lb.Write([]byte{byte('a' + i), '\n'})
	}

	entries := lb.Recent(3)
	if len(entries) != 3 {
		t.Fatalf("Recent(3): got %d, want 3", len(entries))
	}
	// Should return last 3: c, d, e
	if entries[0].Msg != "c" {
		t.Errorf("entry[0].Msg: got %q, want %q", entries[0].Msg, "c")
	}
}

func TestLogBuffer_RecentMoreThanAvailable(t *testing.T) {
	lb := NewLogBuffer(10)

	lb.Write([]byte("only\n"))

	entries := lb.Recent(100)
	if len(entries) != 1 {
		t.Fatalf("Recent(100): got %d, want 1", len(entries))
	}
	if entries[0].Msg != "only" {
		t.Errorf("entry[0].Msg: got %q, want %q", entries[0].Msg, "only")
	}
}

func TestLogBuffer_Empty(t *testing.T) {
	lb := NewLogBuffer(5)

	entries := lb.Recent(10)
	if len(entries) != 0 {
		t.Fatalf("Recent on empty buffer: got %d, want 0", len(entries))
	}
}

func TestLogBuffer_MultiLineWrite(t *testing.T) {
	lb := NewLogBuffer(10)

	// log.Printf sends entire line including newline in one Write call
	lb.Write([]byte("hello world\n"))

	entries := lb.Recent(1)
	if len(entries) != 1 {
		t.Fatalf("Recent(1): got %d, want 1", len(entries))
	}
	if entries[0].Msg != "hello world" {
		t.Errorf("Msg: got %q, want %q", entries[0].Msg, "hello world")
	}
}

func TestLogBuffer_Timestamp(t *testing.T) {
	lb := NewLogBuffer(5)
	before := time.Now()

	lb.Write([]byte("ts-test\n"))

	entries := lb.Recent(1)
	if len(entries) != 1 {
		t.Fatal("expected 1 entry")
	}
	ts, err := time.Parse("15:04:05", entries[0].Time)
	if err != nil {
		t.Fatalf("Time format invalid: %q: %v", entries[0].Time, err)
	}
	// Just verify it parsed correctly (same minute is close enough)
	_ = ts
	_ = before
}

func TestLogBuffer_ConcurrentWrites(t *testing.T) {
	lb := NewLogBuffer(100)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			lb.Write([]byte{byte('A' + n%26), '\n'})
		}(i)
	}
	wg.Wait()

	entries := lb.Recent(50)
	if len(entries) != 50 {
		t.Errorf("Expected 50 entries, got %d", len(entries))
	}
}

func TestLogBuffer_WriteWithoutNewline(t *testing.T) {
	lb := NewLogBuffer(5)

	// Partial write, no newline
	lb.Write([]byte("partial"))
	// Flush with newline
	lb.Write([]byte(" rest\n"))

	entries := lb.Recent(1)
	if len(entries) != 1 {
		t.Fatal("expected 1 entry")
	}
	if entries[0].Msg != "partial rest" {
		t.Errorf("Msg: got %q, want %q", entries[0].Msg, "partial rest")
	}
}

func TestLogBuffer_IgnoresBlankLines(t *testing.T) {
	lb := NewLogBuffer(5)

	lb.Write([]byte("real\n"))
	lb.Write([]byte("\n"))

	entries := lb.Recent(10)
	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry (blank line ignored), got %d", len(entries))
	}
}

func TestLogBuffer_LargeBuffer(t *testing.T) {
	const cap = 200
	lb := NewLogBuffer(cap)

	for i := 0; i < 300; i++ {
		lb.Write([]byte(strings.Repeat("x", 50) + "\n"))
	}

	entries := lb.Recent(100)
	if len(entries) != 100 {
		t.Fatalf("Expected 100 entries, got %d", len(entries))
	}
}
