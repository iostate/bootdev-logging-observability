package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"boot.dev/linko/internal/store"
)

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

	logger, closeLogger, err := initializeLogger()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if closeLogger != nil {
		defer closeLogger()
	}

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

func initializeLogger() (*slog.Logger, closeFunc, error) {
	logFile := os.Getenv("LINKO_LOG_FILE")
	if logFile == "" {
		fmt.Println("No LINKO_LOG_FILE env variable found")

		logger := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level:       slog.LevelDebug,
			ReplaceAttr: replaceAttr,
		})
		return slog.New(logger), func() error { return nil }, nil
	}

	fmt.Println("LINKO_LOG_FILE env variable found")
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return nil, nil, err
	}

	bufferedFile := bufio.NewWriterSize(f, 8192)

	close := func() error {
		if err := bufferedFile.Flush(); err != nil {
			return err
		}
		return f.Close()
	}

	infoLogger := slog.NewJSONHandler(bufferedFile, &slog.HandlerOptions{
		Level:       slog.LevelInfo,
		ReplaceAttr: replaceAttr,
	})
	osStdErrLogger := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level:       slog.LevelDebug,
		ReplaceAttr: replaceAttr,
	})
	logger := slog.New(slog.NewMultiHandler(infoLogger, osStdErrLogger))
	return logger, close, nil
}

func replaceAttr(groups []string, a slog.Attr) slog.Attr {
	if a.Key == "error" {

		err, ok := a.Value.Any().(error)
		if !ok {
			return a
		}
		// Slog groups
		// Check for errors wrapped with pkgerr.WithStackTrace()
		// that contain a stack trace
		if stackErr, ok := errors.AsType[stackTracer](err); ok {
			return slog.GroupAttrs("error", slog.Attr{
				Key:   "message",
				Value: slog.StringValue(stackErr.Error()),
			}, slog.Attr{
				Key:   "stack_trace",
				Value: slog.StringValue(fmt.Sprintf("%+v", stackErr.StackTrace())),
			})
		}
		return slog.String("error", fmt.Sprintf("%+v", err))
	}
	return a
}
