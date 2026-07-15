package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"boot.dev/linko/internal/store"
)

func main() {
	f, err := os.OpenFile("linko.access.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	// Standard logger, DEBUG: prefix to STDERR
	var logger = log.New(os.Stderr, "DEBUG: ", log.LstdFlags)

	// Access logger, INFO: prefix writes to file "linko.access.log"
	accessLogger := log.New(f, "INFO: ", log.LstdFlags)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	httpPort := flag.Int("port", 8899, "port to listen on")
	dataDir := flag.String("data", "./data", "directory to store data")
	flag.Parse()

	status := run(ctx, cancel, *httpPort, *dataDir, logger, accessLogger)
	cancel()

	os.Exit(status)
}

func run(ctx context.Context, cancel context.CancelFunc, httpPort int, dataDir string, logger, accessLogger *log.Logger) int {
	st, err := store.New(dataDir, logger)
	if err != nil {
		log.Printf("failed to create store: %v\n", err)
		return 1
	}
	s := newServer(*st, httpPort, cancel, logger, accessLogger)
	var serverErr error
	go func() {
		serverErr = s.start()
	}()
	_ = NewApplication(s, logger)

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.shutdown(shutdownCtx); err != nil {
		return 1
	}
	if serverErr != nil {
		log.Printf("server error: %v\n", serverErr)
		return 1
	}
	return 0
}
