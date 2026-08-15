package service

import (
	"context"
	"testing"
	"time"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
)

func TestLogWriterStopsWhenDrainQueueIsClosed(t *testing.T) {
	w := &LogWriter{queue: make(chan model.RequestLog)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	close(w.queue)
	w.Start(ctx)

	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("log writer did not stop after its drain queue closed")
	}
}
