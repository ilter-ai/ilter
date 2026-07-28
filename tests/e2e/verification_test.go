package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db"
	testutil "github.com/ilter-ai/ilter/tests/utils"
)

var (
	muLog              sync.Mutex
	lastReceivedPrompt string
)

func startMockOpenAIServer(addr string) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var reqData map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &reqData); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		messages, ok := reqData["messages"].([]interface{})
		if ok && len(messages) > 0 {
			lastMsg, ok := messages[len(messages)-1].(map[string]interface{})
			if ok {
				muLog.Lock()
				lastReceivedPrompt, _ = lastMsg["content"].(string)
				muLog.Unlock()
			}
		}

		stream, _ := reqData["stream"].(bool)
		if stream {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			w.WriteHeader(http.StatusOK)

			chunk1 := map[string]interface{}{
				"id":      "chatcmpl-123",
				"object":  "chat.completion.chunk",
				"created": 1677652288,
				"model":   "gpt-4o",
				"choices": []interface{}{
					map[string]interface{}{
						"index": 0,
						"delta": map[string]interface{}{
							"role": "assistant",
						},
					},
				},
			}
			chunk1Bytes, _ := json.Marshal(chunk1)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", string(chunk1Bytes))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}

			muLog.Lock()
			prompt := lastReceivedPrompt
			muLog.Unlock()

			chunk2 := map[string]interface{}{
				"id":      "chatcmpl-123",
				"object":  "chat.completion.chunk",
				"created": 1677652288,
				"model":   "gpt-4o",
				"choices": []interface{}{
					map[string]interface{}{
						"index": 0,
						"delta": map[string]interface{}{
							"content": fmt.Sprintf("I got: %s", prompt),
						},
					},
				},
			}
			chunk2Bytes, _ := json.Marshal(chunk2)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", string(chunk2Bytes))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}

			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			return
		}

		muLog.Lock()
		prompt := lastReceivedPrompt
		muLog.Unlock()

		resp := map[string]interface{}{
			"id":      "chatcmpl-123",
			"object":  "chat.completion",
			"created": 1677652288,
			"model":   "gpt-4o",
			"choices": []interface{}{
				map[string]interface{}{
					"index": 0,
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": fmt.Sprintf("Echo: %s", prompt),
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]interface{}{
				"prompt_tokens":     10,
				"completion_tokens": 15,
				"total_tokens":      25,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/models", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data": [{"id": "gpt-4o"}]}`))
	})

	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		_ = srv.ListenAndServe()
	}()

	return srv
}

func makeRequest(method, url string, headers map[string]string, body []byte) (int, string, http.Header, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, url, bodyReader)
	if err != nil {
		return 0, "", nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", nil, err
	}
	defer resp.Body.Close()
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, "", resp.Header, err
	}
	return resp.StatusCode, string(respBytes), resp.Header, nil
}

func TestE2EAndSchemathesis(t *testing.T) {
	requireDev(t)
	if testing.Short() {
		t.Skip("skipping e2e/schemathesis test in short mode")
	}
	// Declare variables to avoid shadowing issues
	var (
		code         int
		body         string
		headers      http.Header
		err          error
		userKey      string
		userKeyID    string
		keyResp      map[string]interface{}
		serveCmd     *exec.Cmd
		mockSrv      *http.Server
		healthy      bool
		adminHeaders map[string]string
		dashHeaders  map[string]string
		maskedPrompt string
	)

	// 1. Build web dist FIRST so Go embed can pick it up during Go build
	t.Log("Building web dist for dashboard embed...")
	webBuild := exec.Command("bun", "run", "build")
	webBuild.Dir = "../../web"
	var webBuildOut, webBuildStderr bytes.Buffer
	webBuild.Stdout = &webBuildOut
	webBuild.Stderr = &webBuildStderr
	if err = webBuild.Run(); err != nil {
		t.Fatalf("Web build failed: %v. Stdout: %s. Stderr: %s", err, webBuildOut.String(), webBuildStderr.String())
	}

	// 2. Build Go binary to ensure it's fresh (after web dist is ready)
	t.Log("Building fresh ilter binary...")
	currentDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	t.Logf("Current directory: %s", currentDir)
	buildCmd := exec.Command("go", "build", "-o", "../../ilter", "../../cmd/ilter")
	var buildStderr bytes.Buffer
	buildCmd.Stderr = &buildStderr
	if err = buildCmd.Run(); err != nil {
		t.Fatalf("Go build failed: %v. Stderr: %s", err, buildStderr.String())
	}

	// 3. Start mock OpenAI server
	t.Log("Starting Mock OpenAI Server...")
	mockSrv = startMockOpenAIServer("127.0.0.1:8081")
	defer func() {
		if mockSrv != nil {
			_ = mockSrv.Shutdown(context.Background())
		}
	}()

	// Wait for mock server to be ready
	var resp *http.Response
	for i := 0; i < 20; i++ {
		req, _ := http.NewRequestWithContext(context.Background(), "GET", "http://127.0.0.1:8081/models", nil)
		resp, err = http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	// 4. Use env vars for configuration instead of YAML.
	// The server boots from DefaultConfig() + ApplyEnvOverrides().
	dbPath := filepath.Join(t.TempDir(), "ilter.db")

	// Seed the database with demo data first so providers and models are loaded
	t.Log("Seeding database via ilter init --demo...")
	initCmd := exec.Command("../../ilter", "init", "--demo")
	initCmd.Env = os.Environ()
	initCmd.Env = append(initCmd.Env, "ILTER_STORAGE_PATH="+dbPath)
	var initOut, initErr bytes.Buffer
	initCmd.Stdout = &initOut
	initCmd.Stderr = &initErr
	if err = initCmd.Run(); err != nil {
		t.Fatalf("ilter init --demo failed: %v. Stdout: %s. Stderr: %s", err, initOut.String(), initErr.String())
	}

	store, err := db.NewSQLiteStore(config.StorageConfig{Type: "sqlite", SqlitePath: dbPath})
	if err != nil {
		t.Fatalf("Failed to open SQLite store: %v", err)
	}
	_, err = store.DB.Exec("INSERT INTO provider_configs (name, base_url, api_key) VALUES ('mock', 'http://127.0.0.1:8081', 'test-mock-key') ON CONFLICT(name) DO UPDATE SET base_url='http://127.0.0.1:8081', api_key='test-mock-key'")
	if err != nil {
		t.Fatalf("Failed to insert mock provider_configs: %v", err)
	}
	_, _ = store.DB.Exec("UPDATE provider_configs SET base_url = 'http://127.0.0.1:8081', api_key = 'test-mock-key' WHERE name = 'openai'")
	_, err = store.DB.Exec("INSERT OR REPLACE INTO runtime_config (section, key, value) VALUES ('provider', 'openai', '{\"name\":\"openai\",\"provider\":\"openai\",\"base_url\":\"http://127.0.0.1:8081\",\"is_active\":true}')")
	if err != nil {
		t.Fatalf("Failed to insert mock runtime_config: %v", err)
	}
	_, err = store.DB.Exec("INSERT OR REPLACE INTO model_configs (name, active) VALUES ('gpt-4o', 1)")
	if err != nil {
		t.Fatalf("Failed to insert mock model_configs: %v", err)
	}
	// Insert provider_models so the load balancer has a route for gpt-4o to the mock.
	_, err = store.DB.Exec("INSERT OR REPLACE INTO provider_models (provider, model, active) VALUES ('openai', 'gpt-4o', 1)")
	if err != nil {
		t.Fatalf("Failed to insert mock provider_models: %v", err)
	}
	_, err = store.DB.Exec("INSERT OR REPLACE INTO runtime_config (section, key, value) VALUES ('dashboard', 'port', '9092')")
	if err != nil {
		t.Fatalf("Failed to set dashboard port: %v", err)
	}
	_, err = store.DB.Exec("INSERT OR REPLACE INTO runtime_config (section, key, value) VALUES ('metrics', 'port', '9093')")
	if err != nil {
		t.Fatalf("Failed to set metrics port: %v", err)
	}
	_, err = store.DB.Exec("INSERT OR REPLACE INTO runtime_config (section, key, value) VALUES ('pii', 'mode', 'reversible')")
	if err != nil {
		t.Fatalf("Failed to set PII mode: %v", err)
	}
	store.Close()

	// 5. Start ilter serve subprocess with env var config
	t.Log("Starting ilter serve...")
	var serveOut bytes.Buffer
	serveCmd = exec.Command("../../ilter", "serve")
	serveCmd.Env = os.Environ()
	serveCmd.Env = append(serveCmd.Env, "ILTER_STORAGE_PATH="+dbPath)
	serveCmd.Env = append(serveCmd.Env, "ILTER_SERVER_PORT=8082")
	serveCmd.Env = append(serveCmd.Env, "ILTER_ADMIN_API_KEY="+testutil.AdminKey)
	// Auth required (use ILTER_ADMIN_API_KEY env)
	serveCmd.Stdout = &serveOut
	serveCmd.Stderr = &serveOut
	if err = serveCmd.Start(); err != nil {
		t.Fatalf("Failed to start gateway process: %v", err)
	}
	defer func() {
		t.Log("Stopping ilter serve...")
		if serveCmd != nil && serveCmd.Process != nil {
			_ = serveCmd.Process.Kill()
			_ = serveCmd.Wait()
		}
		t.Logf("ilter serve output:\n%s", serveOut.String())
	}()

	// 6. Ping healthcheck to wait for boot
	t.Log("Waiting for healthcheck...")
	healthy = false
	for i := 0; i < 20; i++ {
		code, _, _, err = makeRequest("GET", "http://127.0.0.1:8082/v1/models", map[string]string{"Authorization": "Bearer test"}, nil)
		if err == nil && code == 200 {
			healthy = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !healthy {
		t.Fatal("Gateway healthcheck failed or timeout reached")
	}
	t.Log("✅ Gateway is healthy")

	// Set credentials — single key for both proxy admin and dashboard
	adminHeaders = map[string]string{"Authorization": "Bearer " + testutil.AdminKey}
	dashHeaders = map[string]string{"Authorization": "Bearer " + testutil.AdminKey}

	t.Log("Running Admin/Proxy/Dashboard semantic assertions...")

	code, body, _, err = makeRequest("GET", "http://127.0.0.1:9092/api/api-keys", adminHeaders, nil)
	if err != nil || code != 200 {
		t.Fatalf("List keys failed: code %d, err: %v, body: %s", code, err, body)
	}

	createPayload := `{"name":"integration-test-user", "budget":50.0, "rate_limit":200}`
	code, body, _, err = makeRequest("POST", "http://127.0.0.1:9092/api/api-keys", adminHeaders, []byte(createPayload))
	if err != nil || code != 200 {
		t.Fatalf("Create key failed: code %d, err: %v, body: %s", code, err, body)
	}
	if err = json.Unmarshal([]byte(body), &keyResp); err != nil {
		t.Fatalf("JSON decode error: %v, body: %s", err, body)
	}
	userKey, _ = keyResp["key"].(string)
	userKeyID, _ = keyResp["id"].(string)
	if !strings.HasPrefix(userKey, "ilter_") {
		t.Fatalf("Expected ilter_ prefix for key: %s", userKey)
	}

	code, body, _, err = makeRequest("GET", "http://127.0.0.1:9092/api/stats", adminHeaders, nil)
	if err != nil || code != 200 {
		t.Fatalf("Stats check failed: code %d, body: %s", code, body)
	}

	code, body, _, err = makeRequest("POST", "http://127.0.0.1:9092/api/cache/flush", adminHeaders, nil)
	if err != nil || code != 200 {
		t.Fatalf("Flush cache failed: code %d, body: %s", code, body)
	}

	// Proxy completions (non-streaming, PII)
	userHeaders := map[string]string{"Authorization": "Bearer " + userKey}
	chatPayload := `{"model":"gpt-4o","messages":[{"role":"user","content":"Hello, my email is john.doe@example.com."}],"stream":false}`
	code, body, headers, err = makeRequest("POST", "http://127.0.0.1:8082/v1/chat/completions", userHeaders, []byte(chatPayload))
	if err != nil || code != 200 {
		t.Fatalf("Proxy completion failed: code %d, err: %v, body: %s", code, err, body)
	}
	if !strings.Contains(body, "john.doe@example.com") {
		t.Fatalf("PII unmasking failed in non-streaming response: %s", body)
	}
	if headers.Get("X-Ilter-Complexity-Score") == "" {
		t.Fatal("Missing X-Ilter-Complexity-Score header")
	}
	if headers.Get("X-Ilter-Model-Selected") != "gpt-4o" {
		t.Fatalf("Expected selection gpt-4o, got %s", headers.Get("X-Ilter-Model-Selected"))
	}
	muLog.Lock()
	maskedPrompt = lastReceivedPrompt
	muLog.Unlock()
	if strings.Contains(maskedPrompt, "john.doe@example.com") {
		t.Fatalf("PII not masked in outbound provider request: %s", maskedPrompt)
	}
	if !strings.Contains(maskedPrompt, "PII:EMAIL:") {
		t.Fatalf("Expected colon email placeholder, got: %s", maskedPrompt)
	}

	code, body, _, err = makeRequest("GET", "http://127.0.0.1:9093/metrics", nil, nil)
	if err != nil || code != 200 {
		t.Fatalf("Metrics scrape failed: code %d, body: %s", code, body)
	}
	if !strings.Contains(body, "ilter_http_requests_total") {
		t.Fatal("Metrics output missing http_requests_total")
	}

	// Proxy completions (streaming SSE, PII)
	chatPayloadStream := `{"model":"gpt-4o","messages":[{"role":"user","content":"My phone is 05321234567."}],"stream":true}`
	code, body, _, err = makeRequest("POST", "http://127.0.0.1:8082/v1/chat/completions", userHeaders, []byte(chatPayloadStream))
	if err != nil || code != 200 {
		t.Fatalf("Proxy stream failed: code %d, err: %v, body: %s", code, err, body)
	}
	if !strings.Contains(body, "05321234567") {
		t.Fatalf("PII unmasking failed in stream response: %s", body)
	}
	if strings.Contains(body, "PII:PHONE:") {
		t.Fatalf("PII leaked placeholder in stream response: %s", body)
	}
	muLog.Lock()
	maskedPrompt = lastReceivedPrompt
	muLog.Unlock()
	if strings.Contains(maskedPrompt, "05321234567") {
		t.Fatalf("PII not masked in outbound stream request: %s", maskedPrompt)
	}
	if !strings.Contains(maskedPrompt, "PII:PHONE:") {
		t.Fatalf("Expected colon phone placeholder, got: %s", maskedPrompt)
	}

	code, body, _, err = makeRequest("GET", "http://127.0.0.1:9092/api/stats", dashHeaders, nil)
	if err != nil || code != 200 {
		t.Fatalf("Dashboard stats failed: code %d, body: %s", code, body)
	}

	code, body, _, err = makeRequest("GET", "http://127.0.0.1:9092/api/requests", dashHeaders, nil)
	if err != nil || code != 200 {
		t.Fatalf("Dashboard requests failed: code %d, body: %s", code, body)
	}

	code, body, _, err = makeRequest("GET", "http://127.0.0.1:9092/api/costs", dashHeaders, nil)
	if err != nil || code != 200 {
		t.Fatalf("Dashboard costs failed: code %d, body: %s", code, body)
	}

	// Dashboard models — validate response shape
	code, body, _, err = makeRequest("GET", "http://127.0.0.1:9092/api/models", dashHeaders, nil)
	if err != nil || code != 200 {
		t.Fatalf("Dashboard models failed: code %d, body: %s", code, body)
	}
	var models []map[string]interface{}
	if err = json.Unmarshal([]byte(body), &models); err != nil {
		t.Fatalf("Dashboard models JSON decode failed: %v, body: %s", err, body)
	}
	if len(models) == 0 {
		t.Fatal("Dashboard models returned empty array")
	}
	for i, m := range models {
		for _, field := range []string{"name", "provider", "configured", "active"} {
			if _, ok := m[field]; !ok {
				t.Errorf("models[%d] missing required field %q", i, field)
			}
		}
	}
	configuredCount := 0
	for _, m := range models {
		if cfg, ok := m["configured"].(bool); ok && cfg {
			configuredCount++
		}
	}
	t.Logf("  Models: %d total (%d configured, %d unconfigured)", len(models), configuredCount, len(models)-configuredCount)

	// Dashboard providers — validate response shape
	code, body, _, err = makeRequest("GET", "http://127.0.0.1:9092/api/providers", dashHeaders, nil)
	if err != nil || code != 200 {
		t.Fatalf("Dashboard providers failed: code %d, body: %s", code, body)
	}
	var providers []map[string]interface{}
	if err = json.Unmarshal([]byte(body), &providers); err != nil {
		t.Fatalf("Dashboard providers JSON decode failed: %v, body: %s", err, body)
	}
	if len(providers) == 0 {
		t.Fatal("Dashboard providers returned empty array")
	}
	for i, p := range providers {
		for _, field := range []string{"name", "type", "models"} {
			if _, ok := p[field]; !ok {
				t.Errorf("providers[%d] missing required field %q", i, field)
			}
		}
	}
	t.Logf("  Providers: %d configured", len(providers))

	code, body, _, err = makeRequest("GET", "http://127.0.0.1:9092/api/loops", dashHeaders, nil)
	if err != nil || code != 200 {
		t.Fatalf("Dashboard loops failed: code %d, body: %s", code, body)
	}

	code, body, _, err = makeRequest("GET", "http://127.0.0.1:9092/api/pii-events", dashHeaders, nil)
	if err != nil || code != 200 {
		t.Fatalf("Dashboard pii-events failed: code %d, body: %s", code, body)
	}

	optimizePayload := `{"prompt":"Write a python server","current_model":"gpt-4o"}`
	code, body, _, err = makeRequest("POST", "http://127.0.0.1:9092/api/optimize", dashHeaders, []byte(optimizePayload))
	if err != nil || code != 200 {
		t.Fatalf("Dashboard optimize failed: code %d, body: %s", code, body)
	}

	deleteURL := fmt.Sprintf("http://127.0.0.1:9092/api/api-keys/%s", userKeyID)
	code, body, _, err = makeRequest("DELETE", deleteURL, adminHeaders, nil)
	if err != nil || (code != 200 && code != 204) {
		t.Fatalf("Delete key failed: code %d, body: %s", code, body)
	}

	t.Log("✅ All E2E assertions passed successfully.")
}
