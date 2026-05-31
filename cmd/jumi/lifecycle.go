package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/HeaInSeo/JUMI/pkg/handoff"
)

func runLifecycleCheck(args []string) {
	fs := flag.NewFlagSet("lifecycle-check", flag.ExitOnError)
	ahGRPC := fs.String("ah-grpc", "", "AH gRPC endpoint (host:port); mutually exclusive with --ah-http")
	ahHTTP := fs.String("ah-http", "", "AH HTTP base URL; mutually exclusive with --ah-grpc")
	sampleRunID := fs.String("sample-run-id", "", "sample run ID to query (required)")
	timeout := fs.Duration("timeout", 10*time.Second, "request timeout")
	_ = fs.Parse(args)

	if *sampleRunID == "" {
		fmt.Fprintln(os.Stderr, "error: --sample-run-id is required")
		fs.Usage()
		os.Exit(2)
	}
	switch {
	case *ahGRPC == "" && *ahHTTP == "":
		fmt.Fprintln(os.Stderr, "error: one of --ah-grpc or --ah-http is required")
		fs.Usage()
		os.Exit(2)
	case *ahGRPC != "" && *ahHTTP != "":
		fmt.Fprintln(os.Stderr, "error: --ah-grpc and --ah-http are mutually exclusive")
		fs.Usage()
		os.Exit(2)
	}

	client, closer, err := buildLifecycleClient(*ahGRPC, *ahHTTP, *timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}
	if closer != nil {
		defer func() { _ = closer() }()
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	lifecycle, ok, err := client.GetSampleRunLifecycle(ctx, handoff.GetSampleRunLifecycleRequest{
		SampleRunID: *sampleRunID,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if !ok {
		fmt.Fprintf(os.Stderr, "not found: sample run %s has no lifecycle record\n", *sampleRunID)
		os.Exit(1)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(lifecycle); err != nil {
		fmt.Fprintf(os.Stderr, "error: encode: %v\n", err)
		os.Exit(1)
	}
}

func buildLifecycleClient(grpcTarget, httpURL string, timeout time.Duration) (handoff.Client, func() error, error) {
	if grpcTarget != "" {
		c, err := handoff.NewGRPCClient(grpcTarget)
		if err != nil {
			return nil, nil, fmt.Errorf("grpc client: %w", err)
		}
		return c, c.Close, nil
	}
	return handoff.NewHTTPClient(httpURL, timeout), nil, nil
}
