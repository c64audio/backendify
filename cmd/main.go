package main

import (
	"backendify/internal/endpoint"
	"backendify/utils"
	"context"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

	"backendify/internal/config"
	"backendify/internal/server"
)

func main() {
	// get configs and make available to context
	ctx := context.Background()

	// initiate logging
	l := log.New(os.Stdout, "", log.LstdFlags|log.Lmicroseconds|log.Lshortfile)

	// Create a new StatsD client
	statsClient, err := utils.NewStatsClient(l)
	if err != nil {
		l.Println("WARN: Failed to create stats client")
	}
	defer statsClient.Shutdown()

	defer func() {
		if r := recover(); r != nil {
			stackTrace := make([]byte, 4096)
			n := runtime.Stack(stackTrace, false)

			// Structured logging
			l.Printf(
				"FATAL PANIC: %v\n"+
					"Stack Trace:\n%s\n"+
					"Application cannot continue",
				r, stackTrace[:n],
			)

			// Forceful termination
			os.Exit(1)
		}
	}()

	// get top level config.json file, unmarshal and put in context
	data, err := os.ReadFile("config.json")
	if err != nil {
		l.Fatalf("Failed to read config file: %v", err)
	}

	var appConfig config.Config

	if err := json.Unmarshal(data, &appConfig); err != nil {
		l.Fatalf("Failed to unmarshal config: %v", err)
	}

	type contextKey string
	const configKey contextKey = "config"
	ctx = context.WithValue(ctx, configKey, appConfig)

	httpClient := utils.NewRealHTTPClient()
	srv := server.New(appConfig.ServerConfig, l, httpClient, statsClient)

	// ingest command line entries
	endpoints, err := endpoint.GetEndpoints(httpClient, l, appConfig)
	srv.Endpoints = endpoints
	// check command line entries

	var wg sync.WaitGroup
	errChan := make(chan error)
	wg.Add(1)
	go func() {
		defer wg.Done()
		l.Println("Starting server")
		if err := srv.Start(); err != nil {
			l.Printf("Server Error: %s", err)
			errChan <- err
		}
	}()

	go func() {
		for err := range errChan {
			l.Printf("error: %s", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	l.Println("Server shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Stop(); err != nil {
		l.Println("Error shutting down server")
	}

	wg.Wait()
	httpClient.Close()
	close(errChan)
}
