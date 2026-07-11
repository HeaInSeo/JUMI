package fixtures

import "github.com/HeaInSeo/JUMI/pkg/backend"

func badFailedResult() backend.ExecutionResult {
	// ruleid: jumi-no-failed-execution-result-without-reason
	return backend.ExecutionResult{
		Succeeded: false,
	}
}

func goodFailedResult() backend.ExecutionResult {
	// ok: jumi-no-failed-execution-result-without-reason
	return backend.ExecutionResult{
		Succeeded:             false,
		TerminalFailureReason: "input_materialization_remote_unavailable",
	}
}

func goodSuccessResult() backend.ExecutionResult {
	// ok: jumi-no-failed-execution-result-without-reason
	return backend.ExecutionResult{
		Succeeded: true,
	}
}
