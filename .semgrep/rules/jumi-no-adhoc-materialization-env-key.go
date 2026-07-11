package fixtures

import "fmt"

func badLiteralWrite(env map[string]string) {
	// ruleid: jumi-no-literal-materialization-env-key-write
	env["JUMI_INPUT_DATASET_MATERIALIZATION_MODE"] = "remote_fetch"
}

func badMapLiteral() map[string]string {
	return map[string]string{
		// ruleid: jumi-no-literal-materialization-env-map-entry
		"JUMI_INPUT_DATASET_EXPECTED_DIGEST": "sha256:abc",
	}
}

func badSprintf(name string) string {
	// ruleid: jumi-no-adhoc-materialization-env-key-sprintf
	return fmt.Sprintf("JUMI_INPUT_%s", name)
}

func badConcat(name string) string {
	// ruleid: jumi-no-adhoc-materialization-env-key-concat
	return "JUMI_INPUT_" + name
}

func goodHelper(base, suffix string) string {
	// ok: jumi-no-adhoc-materialization-env-key-concat
	return materializationEnvKey(base, suffix)
}

func goodReadPrefix(key string) bool {
	// ok: jumi-no-adhoc-materialization-env-key-concat
	return len(key) > len("JUMI_INPUT_")
}

func materializationEnvKey(base, suffix string) string {
	return "JUMI_INPUT_" + base + suffix
}
