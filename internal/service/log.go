package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
	"github.com/Deadsquirrel93/quickmock.dev/internal/repository"
)

// LogWriter accepts request log rows on a buffered channel and writes them
// to Postgres in the background. If the channel is full, the row is dropped
// — the public mock router must never block on logging.
type LogWriter struct {
	repo    *repository.LogRepo
	mocks   *repository.MockRepo
	stats   *StatsCache
	queue   chan model.RequestLog
	wg      sync.WaitGroup
	done    chan struct{}
	logger  *slog.Logger
	maxBody int
}

// NewLogWriter creates a writer with `capacity` buffered slots. A capacity
// of ~1024 absorbs reasonable bursts while remaining tiny in memory.
func NewLogWriter(repo *repository.LogRepo, mocks *repository.MockRepo, stats *StatsCache, capacity int, logger *slog.Logger) *LogWriter {
	return &LogWriter{
		repo:    repo,
		mocks:   mocks,
		stats:   stats,
		queue:   make(chan model.RequestLog, capacity),
		done:    make(chan struct{}),
		logger:  logger,
		maxBody: 16 * 1024, // request body truncation
	}
}

// Start spins up one consumer goroutine. Cancel ctx to stop accepting new
// items; Shutdown drains the remaining queue.
func (w *LogWriter) Start(ctx context.Context) {
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		for {
			select {
			case l, ok := <-w.queue:
				if !ok {
					return
				}
				w.write(ctx, l)
			case <-ctx.Done():
				// Drain remaining items, then exit.
				for {
					select {
					case l := <-w.queue:
						w.write(context.Background(), l)
					default:
						return
					}
				}
			}
		}
	}()
}

// Submit enqueues a log row. Drops on full queue.
func (w *LogWriter) Submit(l model.RequestLog) {
	if len(l.RequestBody) > w.maxBody {
		l.RequestBody = l.RequestBody[:w.maxBody]
	}
	select {
	case w.queue <- l:
	default:
		w.logger.Warn("dropped request log; queue full",
			slog.String("mock_id", l.MockID))
	}
}

// Shutdown closes the queue and waits for the consumer to finish.
func (w *LogWriter) Shutdown(timeout time.Duration) {
	close(w.queue)
	done := make(chan struct{})
	go func() { w.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(timeout):
		w.logger.Warn("log writer shutdown timed out")
	}
}

func (w *LogWriter) write(ctx context.Context, l model.RequestLog) {
	if err := w.repo.Insert(ctx, &l); err != nil {
		w.logger.Error("insert request log",
			slog.String("mock_id", l.MockID),
			slog.Any("err", err))
		return
	}
	if err := w.mocks.RecordHit(ctx, l.MockID); err != nil {
		w.logger.Warn("record mock hit",
			slog.String("mock_id", l.MockID),
			slog.Any("err", err))
	}
	if w.stats != nil {
		w.stats.BumpAsync(StatRequestsServed, 1)
	}
}
