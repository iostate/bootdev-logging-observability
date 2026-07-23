package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"slices"
	"syscall"
	"time"

	"boot.dev/linko/internal/build"
	"boot.dev/linko/internal/store"
	"github.com/lmittmann/tint"
	"github.com/mattn/go-isatty"
	"gopkg.in/natefinch/lumberjack.v2"

	linkerr "boot.dev/linko/internal/linkoerr"
)

type multiError interface {
	error
	Unwrap() []error
}

func main() {

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	httpPort := flag.Int("port", 8899, "port to listen on")
	dataDir := flag.String("data", "./data", "directory to store data")
	flag.Parse()

	status := run(ctx, cancel, *httpPort, *dataDir)
	cancel()

	os.Exit(status)
}

func run(ctx context.Context, cancel context.CancelFunc, httpPort int, dataDir string) int {
	logFile := os.Getenv("LINKO_LOG_FILE")
	logger, closeLogger, err := initializeLogger(logFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if closeLogger != nil {
		defer closeLogger()
	}

	env := os.Getenv("ENV")
	hostname, _ := os.Hostname()
	logger = logger.With(
		slog.String("git_sha", build.GitSHA),
		slog.String("build_time", build.BuildTime),
		slog.String("env", env),
		slog.String("hostname", hostname),
	)

	st, err := store.New(dataDir, logger)
	if err != nil {
		logger.Info("failed to create store", slog.String("error", err.Error()))
		return 1
	}
	s := newServer(*st, httpPort, cancel, logger)
	var serverErr error
	go func() {
		serverErr = s.start()
	}()
	_ = NewApplication(s, logger)

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.shutdown(shutdownCtx); err != nil {
		if closeLogger != nil {
			if err := closeLogger(); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
			}
		}
		return 1
	}
	if serverErr != nil {
		logger.Info("server error\n", "serverErr", serverErr)
		if closeLogger != nil {
			if err := closeLogger(); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
			}
		}
		return 1
	}

	return 0
}

type closeFunc func() error

func initializeLogger(logFile string) (*slog.Logger, closeFunc, error) {
	var (
		handlers []slog.Handler
		closers  []closeFunc
	)

	handlers = append(handlers, tint.NewTextHandler(os.Stderr, &tint.Options{
		ReplaceAttr: replaceAttr,
		NoColor:     !(isatty.IsTerminal(os.Stdout.Fd())) || isatty.IsCygwinTerminal(os.Stdout.Fd()),
	}))

	if logFile != "" {
		fileLogger := &lumberjack.Logger{
			Filename:   logFile,
			MaxSize:    1,
			MaxAge:     28,
			MaxBackups: 10,
			LocalTime:  false,
			Compress:   true,
		}
		handlers = append(handlers, slog.NewJSONHandler(fileLogger, &slog.HandlerOptions{
			ReplaceAttr: replaceAttr,
		}))
		closers = append(closers, fileLogger.Close)
	}

	close := func() error {
		var errs []error
		for _, closer := range closers {
			errs = append(errs, closer())
		}
		return errors.Join(errs...)
	}
	return slog.New(slog.NewMultiHandler(handlers...)), close, nil
}

func replaceAttr(groups []string, a slog.Attr) slog.Attr {

	prohibitedKeywords := []string{"password", "key", "apiKey", "secret", "pin", "creditcardno", "user"}

	if a.Key == slog.TimeKey {
		return slog.Time(slog.TimeKey, time.Date(2023, 10, 1, 12, 34, 57, 0, time.UTC))
	}

	if slices.Contains(prohibitedKeywords, a.Key) {
		a.Value = slog.AnyValue("[REDACTED]")
	}

	if a.Value.Kind() == slog.KindString {
		u, err := url.Parse(a.Value.String())
		if err == nil && u.User != nil {
			if _, hasPassword := u.User.Password(); hasPassword {
				u.User = url.UserPassword(u.User.Username(), "[REDACTED]")
				return slog.String(a.Key, u.String())
			}
		}
	}

	if a.Key == "error" {

		err, ok := a.Value.Any().(error)
		if !ok {
			return a
		}

		if _, ok := errors.AsType[multiError](err); ok {
			return slog.GroupAttrs("errors", errorAttrs(err)...)
		}
		return slog.GroupAttrs("error", errorAttrs(err)...)
	}
	return a
}

func errorAttrs(err error) []slog.Attr {
	if multiErr, ok := errors.AsType[multiError](err); ok {
		errs := multiErr.Unwrap()
		children := make([]slog.Attr, 0, len(errs))
		for i, e := range errs {
			children = append(children, slog.GroupAttrs(fmt.Sprintf("error_%d", i+1), errorAttrs(e)...))
		}
		return children
	}

	attrs := []slog.Attr{
		slog.String("message", err.Error()),
	}
	attrs = append(attrs, linkerr.Attrs(err)...)
	if stackErr, ok := errors.AsType[stackTracer](err); ok {
		attrs = append(attrs, slog.String("stack_trace", fmt.Sprintf("%+v", stackErr.StackTrace())))
	}
	return attrs
}
