package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/HeaInSeo/JUMI/pkg/api"
	"github.com/HeaInSeo/JUMI/pkg/spec"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:19090", "JUMI gRPC address")
	specPath := flag.String("spec", "", "path to ExecutableRunSpec JSON")
	runID := flag.String("run-id", "", "override run.runId")
	sampleRunID := flag.String("sample-run-id", "", "override run.sampleRunId")
	nowRun := flag.Bool("submitted-now", true, "set run.submittedAt to current UTC time")
	timeout := flag.Duration("timeout", 2*time.Minute, "wait timeout")
	poll := flag.Duration("poll", 2*time.Second, "poll interval")
	wait := flag.Bool("wait", true, "wait for terminal status")
	showEvents := flag.Bool("events", true, "print run events after completion")
	flag.Parse()

	if *specPath == "" {
		fmt.Fprintln(os.Stderr, "-spec is required")
		os.Exit(2)
	}

	specInput, err := loadSpec(*specPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load spec: %v\n", err)
		os.Exit(1)
	}
	if *runID != "" {
		specInput.Run.RunID = *runID
	}
	if *sampleRunID != "" {
		specInput.Run.SampleRunID = *sampleRunID
	}
	if *nowRun {
		specInput.Run.SubmittedAt = time.Now().UTC()
	}

	conn, err := grpc.NewClient(*addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.CallContentSubtype("json")),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "grpc connect: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	client := api.NewRunServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	submitResp, err := client.SubmitRun(ctx, &api.SubmitRunRequest{Spec: specInput}, grpc.CallContentSubtype("json"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "submit run: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("submitted runId=%s status=%s acceptedAt=%s\n",
		submitResp.RunID, submitResp.Status, submitResp.AcceptedAt.Format(time.RFC3339))

	if !*wait {
		return
	}

	run, nodes, err := waitForTerminalRun(context.Background(), client, submitResp.RunID, *timeout, *poll)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wait for run: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("terminal runId=%s status=%s stopCause=%s failureReason=%s\n",
		run.RunID, run.Status, run.TerminalStopCause, run.TerminalFailureReason)
	for _, node := range nodes {
		fmt.Printf("node=%s status=%s stopCause=%s failureReason=%s bottleneck=%s\n",
			node.NodeID, node.Status, node.TerminalStopCause, node.TerminalFailureReason, node.CurrentBottleneckLocation)
	}

	if *showEvents {
		eventsResp, err := client.ListRunEvents(context.Background(),
			&api.ListRunEventsRequest{RunID: submitResp.RunID, Limit: 200},
			grpc.CallContentSubtype("json"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "list events: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("--- events ---")
		for _, event := range eventsResp.Events {
			parts := []string{
				event.OccurredAt.Format(time.RFC3339),
				event.Type,
			}
			if event.NodeID != "" {
				parts = append(parts, "node="+event.NodeID)
			}
			if event.Message != "" {
				parts = append(parts, "message="+quoteIfNeeded(event.Message))
			}
			if event.StopCause != "" {
				parts = append(parts, "stopCause="+event.StopCause)
			}
			if event.FailureReason != "" {
				parts = append(parts, "failureReason="+event.FailureReason)
			}
			fmt.Println(strings.Join(parts, " "))
		}
	}

	if run.Status != spec.RunStatusSucceeded {
		os.Exit(1)
	}
}

func loadSpec(path string) (spec.ExecutableRunSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return spec.ExecutableRunSpec{}, err
	}
	var input spec.ExecutableRunSpec
	if err := json.Unmarshal(data, &input); err != nil {
		return spec.ExecutableRunSpec{}, err
	}
	return input, nil
}

func waitForTerminalRun(ctx context.Context, client *api.RunServiceClient, runID string, timeout, poll time.Duration) (spec.RunRecord, []spec.NodeRecord, error) {
	deadline := time.Now().Add(timeout)
	for {
		runResp, err := client.GetRun(ctx, &api.GetRunRequest{RunID: runID}, grpc.CallContentSubtype("json"))
		if err != nil {
			return spec.RunRecord{}, nil, err
		}
		nodesResp, err := client.ListRunNodes(ctx, &api.ListRunNodesRequest{RunID: runID}, grpc.CallContentSubtype("json"))
		if err != nil {
			return spec.RunRecord{}, nil, err
		}
		switch runResp.Run.Status {
		case spec.RunStatusSucceeded, spec.RunStatusFailed, spec.RunStatusCanceled:
			return runResp.Run, nodesResp.Nodes, nil
		}
		if time.Now().After(deadline) {
			return spec.RunRecord{}, nil, fmt.Errorf("timeout waiting for terminal status, last status=%s", runResp.Run.Status)
		}
		select {
		case <-ctx.Done():
			return spec.RunRecord{}, nil, ctx.Err()
		case <-time.After(poll):
		}
	}
}

func quoteIfNeeded(value string) string {
	if strings.ContainsAny(value, " \t") {
		return fmt.Sprintf("%q", value)
	}
	return value
}
