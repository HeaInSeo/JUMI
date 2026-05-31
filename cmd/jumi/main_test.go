package main

import (
	"testing"
	"time"

	"github.com/HeaInSeo/JUMI/pkg/handoff"
)

func TestEnvOrDefault(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		envVal   string
		setEnv   bool
		fallback string
		want     string
	}{
		{
			name:     "env set returns env value",
			key:      "JUMI_TEST_STR_1",
			envVal:   "from-env",
			setEnv:   true,
			fallback: "default",
			want:     "from-env",
		},
		{
			name:     "env not set returns fallback",
			key:      "JUMI_TEST_STR_2",
			setEnv:   false,
			fallback: "default",
			want:     "default",
		},
		{
			name:     "fallback is empty string",
			key:      "JUMI_TEST_STR_3",
			setEnv:   false,
			fallback: "",
			want:     "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setEnv {
				t.Setenv(tc.key, tc.envVal)
			}
			got := envOrDefault(tc.key, tc.fallback)
			if got != tc.want {
				t.Fatalf("envOrDefault(%q, %q) = %q, want %q", tc.key, tc.fallback, got, tc.want)
			}
		})
	}
}

func TestEnvIntOrDefault(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		envVal   string
		setEnv   bool
		fallback int
		want     int
	}{
		{
			name:     "valid int returns parsed value",
			key:      "JUMI_TEST_INT_1",
			envVal:   "42",
			setEnv:   true,
			fallback: 0,
			want:     42,
		},
		{
			name:     "env not set returns fallback",
			key:      "JUMI_TEST_INT_2",
			setEnv:   false,
			fallback: 10,
			want:     10,
		},
		{
			name:     "invalid int returns fallback",
			key:      "JUMI_TEST_INT_3",
			envVal:   "not-a-number",
			setEnv:   true,
			fallback: 5,
			want:     5,
		},
		{
			name:     "zero is a valid value",
			key:      "JUMI_TEST_INT_4",
			envVal:   "0",
			setEnv:   true,
			fallback: 99,
			want:     0,
		},
		{
			name:     "negative int is valid",
			key:      "JUMI_TEST_INT_5",
			envVal:   "-3",
			setEnv:   true,
			fallback: 1,
			want:     -3,
		},
		{
			name:     "float string returns fallback",
			key:      "JUMI_TEST_INT_6",
			envVal:   "3.14",
			setEnv:   true,
			fallback: 7,
			want:     7,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setEnv {
				t.Setenv(tc.key, tc.envVal)
			}
			got := envIntOrDefault(tc.key, tc.fallback)
			if got != tc.want {
				t.Fatalf("envIntOrDefault(%q, %d) = %d, want %d", tc.key, tc.fallback, got, tc.want)
			}
		})
	}
}

func TestEnvBoolOrDefault(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		envVal   string
		setEnv   bool
		fallback bool
		want     bool
	}{
		{
			name:     "true string returns true",
			key:      "JUMI_TEST_BOOL_1",
			envVal:   "true",
			setEnv:   true,
			fallback: false,
			want:     true,
		},
		{
			name:     "false string returns false",
			key:      "JUMI_TEST_BOOL_2",
			envVal:   "false",
			setEnv:   true,
			fallback: true,
			want:     false,
		},
		{
			name:     "1 returns true",
			key:      "JUMI_TEST_BOOL_3",
			envVal:   "1",
			setEnv:   true,
			fallback: false,
			want:     true,
		},
		{
			name:     "0 returns false",
			key:      "JUMI_TEST_BOOL_4",
			envVal:   "0",
			setEnv:   true,
			fallback: true,
			want:     false,
		},
		{
			name:     "env not set returns fallback true",
			key:      "JUMI_TEST_BOOL_5",
			setEnv:   false,
			fallback: true,
			want:     true,
		},
		{
			name:     "env not set returns fallback false",
			key:      "JUMI_TEST_BOOL_6",
			setEnv:   false,
			fallback: false,
			want:     false,
		},
		{
			name:     "invalid bool returns fallback",
			key:      "JUMI_TEST_BOOL_7",
			envVal:   "yes",
			setEnv:   true,
			fallback: false,
			want:     false,
		},
		{
			name:     "TRUE uppercase is valid",
			key:      "JUMI_TEST_BOOL_8",
			envVal:   "TRUE",
			setEnv:   true,
			fallback: false,
			want:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setEnv {
				t.Setenv(tc.key, tc.envVal)
			}
			got := envBoolOrDefault(tc.key, tc.fallback)
			if got != tc.want {
				t.Fatalf("envBoolOrDefault(%q, %v) = %v, want %v", tc.key, tc.fallback, got, tc.want)
			}
		})
	}
}

// --- buildLifecycleClient ---

func TestBuildLifecycleClient_HTTPClient(t *testing.T) {
	client, closer, err := buildLifecycleClient("", "http://ah.local:8080", 5*time.Second)
	if err != nil {
		t.Fatalf("buildLifecycleClient(http) error = %v", err)
	}
	if client == nil {
		t.Fatal("buildLifecycleClient(http) returned nil client")
	}
	if closer != nil {
		t.Fatal("buildLifecycleClient(http) returned non-nil closer, want nil for HTTP")
	}
}

func TestBuildLifecycleClient_GRPCClient(t *testing.T) {
	// NewGRPCClient uses grpc.NewClient which does not attempt actual connection;
	// it returns successfully for any well-formed target.
	client, closer, err := buildLifecycleClient("localhost:9090", "", 5*time.Second)
	if err != nil {
		t.Fatalf("buildLifecycleClient(grpc) error = %v", err)
	}
	if client == nil {
		t.Fatal("buildLifecycleClient(grpc) returned nil client")
	}
	if closer == nil {
		t.Fatal("buildLifecycleClient(grpc) returned nil closer, want non-nil for gRPC")
	}
	// Clean up the connection.
	_ = closer()
}

func TestNewHandoffClientFromEnvRequiresConfiguredTargetByDefault(t *testing.T) {
	t.Setenv("JUMI_AH_GRPC_TARGET", "")
	t.Setenv("JUMI_AH_URL", "")
	t.Setenv("JUMI_ALLOW_NOOP_HANDOFF", "")

	client, err := newHandoffClientFromEnv()
	if err == nil {
		t.Fatalf("newHandoffClientFromEnv() error = nil, want missing configuration error")
	}
	if client != nil {
		t.Fatalf("newHandoffClientFromEnv() client = %#v, want nil on configuration error", client)
	}
}

func TestNewHandoffClientFromEnvAllowsExplicitNoopMode(t *testing.T) {
	t.Setenv("JUMI_AH_GRPC_TARGET", "")
	t.Setenv("JUMI_AH_URL", "")
	t.Setenv("JUMI_ALLOW_NOOP_HANDOFF", "true")

	client, err := newHandoffClientFromEnv()
	if err != nil {
		t.Fatalf("newHandoffClientFromEnv() error = %v", err)
	}
	if _, ok := client.(*handoff.NoopClient); !ok {
		t.Fatalf("newHandoffClientFromEnv() client = %T, want *handoff.NoopClient", client)
	}
}

func TestNewHandoffClientFromEnvReturnsHTTPClientWhenConfigured(t *testing.T) {
	t.Setenv("JUMI_AH_GRPC_TARGET", "")
	t.Setenv("JUMI_AH_URL", "http://artifact-handoff.default.svc.cluster.local:8080")
	t.Setenv("JUMI_ALLOW_NOOP_HANDOFF", "")

	client, err := newHandoffClientFromEnv()
	if err != nil {
		t.Fatalf("newHandoffClientFromEnv() error = %v", err)
	}
	if _, ok := client.(*handoff.HTTPClient); !ok {
		t.Fatalf("newHandoffClientFromEnv() client = %T, want *handoff.HTTPClient", client)
	}
}

func TestNewHandoffClientFromEnvPrefersGRPCTargetOverExplicitNoopMode(t *testing.T) {
	t.Setenv("JUMI_AH_GRPC_TARGET", "artifact-handoff.default.svc.cluster.local:9090")
	t.Setenv("JUMI_AH_URL", "")
	t.Setenv("JUMI_ALLOW_NOOP_HANDOFF", "true")

	client, err := newHandoffClientFromEnv()
	if err != nil {
		t.Fatalf("newHandoffClientFromEnv() error = %v", err)
	}
	if _, ok := client.(*handoff.NoopClient); ok {
		t.Fatalf("newHandoffClientFromEnv() client = %T, want grpc client when grpc target is configured", client)
	}
}
