package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var (
	privateKeyPath *string = flag.String("private-key", "", "Path to the GitHub App private key file")
	useVault       *bool   = flag.Bool("use-vault", false, "Use HashiCorp Vault to retrieve the private key")
	clientID       *string = flag.String("client-id", "", "GitHub App client ID")
	installationID *string = flag.String("installation-id", "", "GitHub App installation ID")
	bindAddr       *string = flag.String("bind", ":8080", "Address to bind the server to")
	verCheck       *bool   = flag.Bool("version", false, "Print the version and exit")

	versionCheckErr error = fmt.Errorf("version check")
)

var Version string = "dev"

func main() {
	ctx, done := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer done()

	if err := parseFlags(ctx); err != nil {
		if errors.Is(err, versionCheckErr) {
			return
		}

		log.Fatalf("Error parsing flags: %v", err)
	}

	// set up global rate limiter
	if tok, err := getInstallationToken(); err != nil {
		log.Fatalf("Error getting installation token: %v", err)
	} else {
		if err := initGlobalLimiter(tok); err != nil {
			log.Fatalf("Error initializing global rate limiter: %v", err)
		}
	}

	// start cleanup goroutine
	go cleanupStaleLimiters(ctx, 30*time.Minute)

	// Define routes
	mux := http.NewServeMux()
	mux.HandleFunc("/", requestHandler(ctx))

	// Create the HTTP server
	server := &http.Server{
		Addr:    *bindAddr,
		Handler: mux,
	}

	// Start the HTTP server
	go func() {
		log.Printf("Server started on %s", *bindAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Error starting server: %v", err)
		}
	}()

	// Wait for the context to be canceled (e.g., by Ctrl+C)
	<-ctx.Done()

	// Create a new context with a timeout to allow for graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Attempt to gracefully shut down the server
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exiting")
}
