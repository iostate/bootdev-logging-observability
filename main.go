package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
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
		fmt.Fprintf(os.Stderr, err.Error())
		return 1
	}

	st, err := store.New(dataDir, logger)
	if err != nil {
		log.Printf("failed to create store: %v\n", err)
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
				fmt.Fprintf(os.Stderr, err.Error())
			}
		}
		return 1
	}
	if serverErr != nil {
		log.Printf("server error: %v\n", serverErr)
		if closeLogger != nil {
			if err := closeLogger(); err != nil {
				fmt.Fprintf(os.Stderr, err.Error())
			}
		}
		return 1
	}

	return 0
}

type closeFunc func() error

func initializeLogger() (*log.Logger, closeFunc, error) {
	logFile := os.Getenv("LINKO_LOG_FILE")
	if logFile == "" {
		fmt.Println("No LINKO_LOG_FILE env variable found")
		return log.New(os.Stderr, "", log.LstdFlags), func() error { return nil }, nil
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
	multiWriter := io.MultiWriter(os.Stderr, bufferedFile)
	return log.New(multiWriter, "", log.LstdFlags), close, nil
}
