package main

import (
	"log/slog"
)

type Application struct {
	srv    *server
	logger *slog.Logger
}

func NewApplication(server *server, logger *slog.Logger) *Application {
	return &Application{
		srv:    server,
		logger: logger,
	}
}
