package main

import "log"

type Application struct {
	srv    *server
	logger *log.Logger
}

func NewApplication(server *server, logger *log.Logger) *Application {
	return &Application{
		srv:    server,
		logger: logger,
	}
}
