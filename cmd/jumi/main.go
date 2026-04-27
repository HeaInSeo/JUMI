package main

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/HeaInSeo/JUMI/pkg/api"
	"github.com/HeaInSeo/JUMI/pkg/backend"
	"github.com/HeaInSeo/JUMI/pkg/executor"
	"github.com/HeaInSeo/JUMI/pkg/handoff"
	"github.com/HeaInSeo/JUMI/pkg/observe"
	"github.com/HeaInSeo/JUMI/pkg/registry"
	"google.golang.org/grpc"
)

func main() {
	reg := registry.NewMemoryRegistry()
	adapter, err := backend.NewSpawnerK8sAdapterFromKubeconfig(
		envOrDefault("JUMI_NAMESPACE", "default"),
		os.Getenv("JUMI_KUBECONFIG"),
		envIntOrDefault("JUMI_MAX_CONCURRENT_RELEASE", 4),
	)
	if err != nil {
		log.Fatal(err)
	}
	handoffClient := newHandoffClientFromEnv()
	engine := executor.NewDagEngineWithHandoff(reg, adapter, handoffClient)
	service := api.NewService(reg, engine)

	httpServer := newHTTPServer(reg, adapter, engine)
	grpcServer := grpc.NewServer()
	api.RegisterRunService(grpcServer, service)

	httpAddr := envOrDefault("JUMI_HTTP_ADDR", ":8080")
	grpcAddr := envOrDefault("JUMI_GRPC_ADDR", ":9090")
	grpcListener, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Fatal(err)
	}

	errCh := make(chan error, 2)
	go func() {
		log.Printf("jumi starting http server on %s", httpAddr)
		httpServer.Addr = httpAddr
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()
	go func() {
		log.Printf("jumi starting grpc server on %s", grpcAddr)
		if err := grpcServer.Serve(grpcListener); err != nil {
			errCh <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Printf("jumi shutting down on signal %s", sig)
	case err := <-errCh:
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(ctx)
	grpcServer.GracefulStop()
}

func newHandoffClientFromEnv() handoff.Client {
	baseURL := os.Getenv("JUMI_AH_URL")
	if baseURL == "" {
		return handoff.NewNoopClient()
	}
	return handoff.NewHTTPClient(baseURL, 5*time.Second)
}

func newHTTPServer(reg registry.Registry, adapter backend.Adapter, engine *executor.DagEngine) *http.Server {
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
		backendSnapshot := observe.BackendSnapshot{Ready: true}
		if statusProvider, ok := adapter.(backend.StatusProvider); ok {
			status := statusProvider.AdapterStatus()
			backendSnapshot = observe.BackendSnapshot{
				Ready:                 status.Ready,
				ReleaseBounded:        status.ReleaseBounded,
				ReleaseInflight:       status.ReleaseInflight,
				ReleaseSlotsAvailable: status.ReleaseSlotsAvailable,
				ReleaseMaxConcurrent:  status.ReleaseMaxConcurrent,
			}
		}
		snapshot, err := observe.SnapshotFromRegistry(context.Background(), reg, backendSnapshot)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(snapshot)
	})
	mux.Handle("/metrics", engine.Metrics().Handler())
	return &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envIntOrDefault(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
