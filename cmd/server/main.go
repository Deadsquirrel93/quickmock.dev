// Mock API server entry point.
//
// Subcommands:
//
//	./quickmock              run the HTTP server
//	./quickmock migrate      apply database migrations and exit
//	./quickmock healthcheck  exit 0 if /healthz returns ok (for docker)
package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	quickmock "github.com/Deadsquirrel93/quickmock.dev"
	"github.com/Deadsquirrel93/quickmock.dev/internal/config"
	"github.com/Deadsquirrel93/quickmock.dev/internal/handler"
	"github.com/Deadsquirrel93/quickmock.dev/internal/i18n"
	mockmw "github.com/Deadsquirrel93/quickmock.dev/internal/middleware"
	"github.com/Deadsquirrel93/quickmock.dev/internal/repository"
	"github.com/Deadsquirrel93/quickmock.dev/internal/service"
	"github.com/Deadsquirrel93/quickmock.dev/internal/sse"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("config", slog.Any("err", err))
		os.Exit(2)
	}

	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	switch cmd {
	case "migrate":
		os.Exit(runMigrate(logger, cfg))
	case "healthcheck":
		os.Exit(runHealthcheck(cfg))
	case "", "serve":
		os.Exit(runServe(logger, cfg))
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", cmd)
		os.Exit(2)
	}
}

func runMigrate(logger *slog.Logger, cfg config.Config) int {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, cfg.PGDSN)
	if err != nil {
		logger.Error("pgxpool", slog.Any("err", err))
		return 1
	}
	defer pool.Close()
	sub, err := fs.Sub(quickmock.MigrationsFS, "migrations")
	if err != nil {
		logger.Error("migrations fs", slog.Any("err", err))
		return 1
	}
	if err := repository.RunMigrations(ctx, pool, sub, "."); err != nil {
		logger.Error("migrate", slog.Any("err", err))
		return 1
	}
	logger.Info("migrations applied")
	return 0
}

func runHealthcheck(cfg config.Config) int {
	// Used as the Docker HEALTHCHECK. Talks to the local server only.
	addr := cfg.Addr
	if addr[0] == ':' {
		addr = "127.0.0.1" + addr
	}
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://" + addr + "/healthz")
	if err != nil {
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func runServe(logger *slog.Logger, cfg config.Config) int {
	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Storage clients
	pool, err := pgxpool.New(rootCtx, cfg.PGDSN)
	if err != nil {
		logger.Error("pgxpool", slog.Any("err", err))
		return 1
	}
	defer pool.Close()

	rdb := redis.NewClient(&redis.Options{
		Addr: cfg.RedisAddr,
		DB:   cfg.RedisDB,
	})
	defer rdb.Close()

	if err := pool.Ping(rootCtx); err != nil {
		logger.Error("postgres ping", slog.Any("err", err))
		return 1
	}
	if err := rdb.Ping(rootCtx).Err(); err != nil {
		logger.Error("redis ping", slog.Any("err", err))
		return 1
	}

	// Repositories
	mockRepo := repository.NewMockRepo(pool)
	logRepo := repository.NewLogRepo(pool)
	statsRepo := repository.NewStatsRepo(pool)
	mockLimiter := repository.NewRateLimiter(rdb, cfg.RateIP, cfg.RateWindow)
	apiLimiter := repository.NewRateLimiter(rdb, 600, time.Minute)
	seqCounter := repository.NewSeqCounter(rdb)

	// Services
	statsCache := service.NewStatsCache(statsRepo, 30*time.Second, logger)
	spamPatterns, err := service.LoadSpamPatterns(cfg.SpamPatternsFile)
	if err != nil {
		logger.Error("spam patterns", slog.Any("err", err))
		return 1
	}
	spamFilter, err := service.NewSpamFilter(spamPatterns, cfg.SpamAllowIPs, logger)
	if err != nil {
		logger.Error("spam filter", slog.Any("err", err))
		return 1
	}
	mockSvc := service.NewMockService(mockRepo, logRepo, statsCache, cfg.MaxBody, cfg.MaxMocks, cfg.DefaultTTL, cfg.MaxTTL, spamFilter)
	sseBroker := sse.NewBroker()
	sseStreams := sse.NewStreamLimiter(cfg.SSEMaxConns, cfg.SSEMaxPerIP)
	logWriter := service.NewLogWriter(logRepo, mockRepo, statsCache, 1024, logger, sseBroker)
	logWriter.Start(rootCtx)

	// i18n
	localz := i18n.New(cfg.DefaultLang)
	if err := localz.LoadFS(quickmock.LocalesFS, "locales"); err != nil {
		logger.Error("i18n load", slog.Any("err", err))
		return 1
	}

	// Templates / static
	webSub, err := fs.Sub(quickmock.WebFS, "web")
	if err != nil {
		logger.Error("web fs", slog.Any("err", err))
		return 1
	}
	renderer, err := handler.NewRenderer(webSub, localz, logger, cfg.BaseURL)
	if err != nil {
		logger.Error("renderer", slog.Any("err", err))
		return 1
	}

	// Handlers
	// HTTPS-only cookies + HSTS depend on whether the configured BaseURL
	// is served over TLS. Local dev (http://localhost:8080) keeps cookies
	// usable without TLS; prod (https://quickmock.dev) flips both on.
	secureSite := strings.HasPrefix(strings.ToLower(cfg.BaseURL), "https://")

	api := handler.NewAPI(mockSvc, logRepo, mockRepo, renderer, cfg.BaseURL)
	ui := handler.NewUI(mockSvc, logRepo, statsCache, renderer, localz, cfg.BaseURL, cfg.MaxBody, cfg.MaxMocks, sseBroker, sseStreams)
	mockRouter := handler.NewMockRouter(mockSvc, logWriter, seqCounter)
	healthHandler := handler.Health(pool, mockLimiter)
	langHandler := handler.Lang(renderer, secureSite)

	// Static asset subtree
	staticSub, err := fs.Sub(webSub, "static")
	if err != nil {
		logger.Error("static fs", slog.Any("err", err))
		return 1
	}

	// Background expiration sweep
	go expireWorker(rootCtx, mockRepo, logger)

	// Router
	r := chi.NewRouter()
	r.Use(mockmw.Recoverer(logger))
	r.Use(mockmw.RealIP)
	r.Use(mockmw.AccessLog(logger))
	// Universal hardening headers: applied to every response, including
	// /m/* (mock_router force-overrides the CSP it needs locally).
	r.Use(mockmw.SecurityHeaders(secureSite))

	// Static (no i18n, no rate limit, long cache)
	r.Handle("/static/*", http.StripPrefix("/static/",
		cacheHeaders(http.FileServer(http.FS(staticSub)))))

	// Health
	r.Get("/healthz", healthHandler)

	r.Get("/robots.txt", handler.RobotsTxt(cfg.BaseURL))
	r.Get("/sitemap.xml", handler.SitemapXML(cfg.BaseURL, localz.Supported(), cfg.DefaultLang))
	r.Get("/llms.txt", handler.LLMsTxt(cfg.BaseURL))

	// Public mock router — own rate limit bucket, no i18n.
	// Two routes share one handler: bare slug, and slug + cosmetic suffix.
	// The handler matches by slug alone; the suffix is only for readable
	// URLs.
	r.Group(func(r chi.Router) {
		r.Use(mockmw.RateLimit(mockLimiter, "ip"))
		r.HandleFunc("/m/{slug}", mockRouter.ServeHTTP)
		r.HandleFunc("/m/{slug}/*", mockRouter.ServeHTTP)
	})

	// UI + API routes (i18n applied here only). UICSP adds the resource
	// policy that fits first-party UI — distinct from the strict sandbox
	// CSP the mock router applies to user-controlled responses.
	r.Group(func(r chi.Router) {
		r.Use(localz.Middleware)
		r.Use(mockmw.UICSP())

		r.Get("/", ui.Home)
		r.Get("/mock/{slug}", ui.Detail)
		r.Get("/mock/{slug}/logs", ui.LogsPartial)
		r.Get("/mock/{slug}/logs/stream", ui.LogsStream)
		r.Get("/mock/{slug}/summary", ui.SummaryPartial)
		r.Get("/mock/{slug}/export", ui.Export)
		r.Get("/mock/{slug}/logs/export", ui.LogsExport)
		r.Get("/my", ui.MyMocks)
		r.Get("/changelog", ui.Changelog)
		r.Get("/guide", ui.Guide)
		r.Get("/guide/{slug}", ui.GuideCase)
		r.Get("/templates", ui.Templates)
		r.Get("/templates/{slug}", ui.TemplateCase)
		r.Get("/share/{slug}", ui.Share)
		r.Post("/language", langHandler)
		r.NotFound(ui.NotFound)

		// State-changing UI routes only: a third-party page cannot browser-fetch
		// its way into creating a mock and burning the visitor's IP quota.
		r.Group(func(r chi.Router) {
			r.Use(mockmw.RejectCrossSite(cfg.BaseURL, logger))
			r.Post("/", ui.CreateForm)
			r.Post("/templates/{slug}/create", ui.TemplateCreate)
		})

		// JSON API
		r.Route("/api", func(r chi.Router) {
			r.Use(mockmw.RateLimit(apiLimiter, "api"))
			r.Post("/mocks", api.Create)
			r.Get("/mocks/by-slugs", ui.BySlugs)
			r.Get("/mocks/{id}", api.Get)
			r.Put("/mocks/{id}", api.Update)
			r.Post("/mocks/{id}/extend", api.Extend)
			r.Delete("/mocks/{id}", api.Delete)
			r.Get("/mocks/{id}/logs", api.Logs)
			r.Delete("/mocks/{id}/logs", api.ClearLogs)
			r.Post("/parse-curl", api.ParseCurl)
		})
	})

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      90 * time.Second,
		IdleTimeout:       120 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return rootCtx },
	}

	go func() {
		logger.Info("server starting", slog.String("addr", cfg.Addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("listen", slog.Any("err", err))
			stop()
		}
	}()

	<-rootCtx.Done()
	logger.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown", slog.Any("err", err))
	}
	logWriter.Shutdown(5 * time.Second)
	logger.Info("bye")
	return 0
}

func expireWorker(ctx context.Context, repo *repository.MockRepo, logger *slog.Logger) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := repo.DeleteExpired(ctx)
			if err != nil {
				logger.Error("expire mocks", slog.Any("err", err))
				continue
			}
			if n > 0 {
				logger.Info("expired mocks removed", slog.Int64("count", n))
			}
		}
	}
}

func cacheHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Assets are content-versioned in their URLs (?v=<hash>, injected by
		// the renderer), so the bytes behind any given URL never change — the
		// hash flips when content does. That makes a year-long immutable cache
		// safe: a deploy that changes a file changes its URL, and returning
		// visitors fetch the new URL immediately instead of waiting out a TTL.
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		next.ServeHTTP(w, r)
	})
}
