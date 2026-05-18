package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/HeaInSeo/JUMI/pkg/provenance"
	"github.com/HeaInSeo/JUMI/pkg/runtimehelper"
)

// Compatibility note:
// This binary is an obsolete compatibility copy of the runtime-side artifact
// helper. The long-term source of truth is
// `github.com/HeaInSeo/node-artifact-runtime`.
//
// Final direction:
// this legacy entrypoint is a removal target. Keep it only until all node
// runtime images and JUMI command injection paths have migrated to `nan`.
//
// TODO(runtime-contract): remove this legacy entrypoint from JUMI after all
// node runtime images and JUMI command injection paths have migrated to `nan`.
func main() {
	args := normalizeCompatibilityArgs(os.Args[1:])
	if len(args) > 0 && args[0] == "version" {
		fmt.Println("jumi-output-helper compatibility shim for nan")
		return
	}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	runID := flag.String("run-id", os.Getenv("JUMI_RUN_ID"), "run identifier")
	sampleRunID := flag.String("sample-run-id", os.Getenv("JUMI_SAMPLE_RUN_ID"), "sample run identifier")
	nodeID := flag.String("node-id", os.Getenv("JUMI_NODE_ID"), "node identifier")
	attemptID := flag.String("attempt-id", os.Getenv("JUMI_ATTEMPT_ID"), "attempt identifier")
	outputNames := flag.String("output-names", os.Getenv("JUMI_OUTPUT_NAMES"), "comma-separated output names")
	outputRoot := flag.String("output-root", firstNonEmpty(os.Getenv("JUMI_OUTPUT_ROOT"), "/out"), "output root path")
	manifestPath := flag.String("manifest-path", firstNonEmpty(os.Getenv("JUMI_OUTPUT_MANIFEST_PATH"), provenance.DefaultArtifactsManifestPath), "artifacts manifest path")
	terminationLogPath := flag.String("termination-log-path", "/dev/termination-log", "termination log path")
	_ = flag.CommandLine.Parse(args)

	os.Exit(runtimehelper.Run(context.Background(), runtimehelper.Config{
		RunID:              *runID,
		SampleRunID:        *sampleRunID,
		NodeID:             *nodeID,
		AttemptID:          *attemptID,
		OutputNames:        runtimehelper.ParseOutputNames(*outputNames),
		OutputRoot:         *outputRoot,
		ManifestPath:       *manifestPath,
		TerminationLogPath: *terminationLogPath,
		Command:            flag.CommandLine.Args(),
	}))
}

func normalizeCompatibilityArgs(args []string) []string {
	if len(args) == 0 {
		return args
	}
	if args[0] == "run" {
		return args[1:]
	}
	return args
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
