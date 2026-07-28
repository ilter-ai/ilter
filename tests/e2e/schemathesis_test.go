package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func runSchemathesis(t *testing.T, pathRegex, token, url string) {
	schemathesisBin, lookErr := exec.LookPath("schemathesis")
	if lookErr != nil {
		t.Skip("schemathesis CLI tool not installed in PATH, skipping schemathesis test")
	}

	specURL := url + "/openapi.json"
	t.Logf("Running Schemathesis for path %s...", pathRegex)
	cmd := exec.Command(
		schemathesisBin, "run", specURL,
		"--url", url,
		"-H", fmt.Sprintf("Authorization: Bearer %s", token),
		"--include-path-regex", pathRegex,
		"--checks", "not_a_server_error,status_code_conformance",
		"--max-failures", "10",
		"--request-timeout", "5000",
		"--request-retries", "1",
		"--phases", "examples,coverage,stateful",
		"--output-truncate", "false",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		t.Logf("Schemathesis Stdout:\n%s", stdout.String())
		t.Logf("Schemathesis Stderr:\n%s", stderr.String())
		t.Fatalf("Schemathesis failed for path %s: %v", pathRegex, err)
	}
	t.Logf("Schemathesis passed for path %s.", pathRegex)
}

func TestSchemathesis(t *testing.T) {
	requireDev(t)
	if testing.Short() || os.Getenv("RUN_SCHEMATHESIS") == "" {
		t.Skip("skipping standalone schemathesis test (run via TestE2EAndSchemathesis in verification_test.go)")
	}

	// Create a real API key for the v1 proxy sweep
	adminHeaders := map[string]string{"Authorization": "Bearer test"}
	createPayload := `{"name":"schemathesis-test-user","scopes":"*"}`
	code, body, _, err := makeRequest("POST", "http://127.0.0.1:9092/api/api-keys", adminHeaders, []byte(createPayload))
	if err != nil || code != 200 {
		t.Fatalf("Failed to create user key for Schemathesis: code %d, err: %v, body: %s", code, err, body)
	}
	var keyResp map[string]interface{}
	if err := json.Unmarshal([]byte(body), &keyResp); err != nil {
		t.Fatalf("JSON decode error: %v, body: %s", err, body)
	}
	schemathesisUserKey, _ := keyResp["key"].(string)
	if !strings.HasPrefix(schemathesisUserKey, "ilter_") {
		t.Fatalf("Expected ilter_ prefix for key: %s", schemathesisUserKey)
	}

	runSchemathesis(t, "^/v1/.*", schemathesisUserKey, "http://127.0.0.1:8082")
	runSchemathesis(t, "^/api/.*", "test", "http://127.0.0.1:9092")
}
