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

	defer func() {
		if r := recover(); r != nil {
			// Get stack trace
			buf := make([]byte, 4096)
			n := runtime.Stack(buf, false)
			stackTrace := string(buf[:n])

			// Log the panic with stack trace
			log.Printf("FATAL: Application panic recovered: %v\nStack Trace:\n%s", r, stackTrace)

			// You could also write to a separate error log file here

			// Optional: Exit with error code
			// os.Exit(1)
		}
	}()

	// get top level config.json file, unmarshal and put in context
	data, err := os.ReadFile("config.json")
	if err != nil {
		log.Fatalf("Failed to read config file: %v", err)
	}

	var appConfig config.Config

	if err := json.Unmarshal(data, &appConfig); err != nil {
		log.Fatalf("Failed to unmarshal config: %v", err)
	}

	type contextKey string
	const configKey contextKey = "config"
	ctx = context.WithValue(ctx, configKey, appConfig)

	// initiate logging
	l := log.New(os.Stdout, "", log.LstdFlags|log.Lmicroseconds|log.Lshortfile)

	httpClient := utils.NewRealHTTPClient()
	srv := server.New(appConfig.ServerConfig, l, httpClient)

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
