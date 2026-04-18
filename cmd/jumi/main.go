package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/HeaInSeo/JUMI/pkg/api"
	"github.com/HeaInSeo/JUMI/pkg/executor"
	"github.com/HeaInSeo/JUMI/pkg/observe"
	"github.com/HeaInSeo/JUMI/pkg/registry"
)

func main() {
	registry := registry.NewMemoryRegistry()
	engine := executor.NewNoopEngine(registry)
	_ = api.NewService(registry, engine)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})
	mux.HandleFunc("/statusz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(observe.StatusSnapshot{
			RunningRuns:      0,
			ReleaseWaitNodes: 0,
			BackendReady:     true,
		})
	})

	addr := envOrDefault("JUMI_HTTP_ADDR", ":8080")
	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("jumi starting http server on %s", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
