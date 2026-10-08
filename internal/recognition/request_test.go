package recognition

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const validSetupJSON = `{"name":null,"bottom_side":"red","side_to_move":null,"notes":null,"pieces":[{"side":"red","kind":"king","column":4,"row":9},{"side":"black","kind":"king","column":4,"row":0}]}`

func chatBody(content string) string {
	b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}}}})
	return string(b)
}
func responsesBody(content string) string {
	b, _ := json.Marshal(map[string]any{"status": "completed", "output": []any{map[string]any{"type": "message", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": content}}}}})
	return string(b)
}
func customSettings(address, api, thinking string) Settings {
	s := DefaultSettings()
	s.Provider, s.Address, s.Model, s.API, s.CustomThinking = Custom, address, " vision-model ", api, thinking
	return s
}

func TestEndpoint(t *testing.T) {
	for _, tc := range []struct{ address, api, want string }{
		{" https://example.test ", ChatCompletions, "https://example.test/chat/completions"},
		{"https://example.test/v1///", Responses, "https://example.test/v1/responses"},
		{"http://127.0.0.1:8000/proxy/chat/completions/?region=local", Responses, "http://127.0.0.1:8000/proxy/responses?region=local"},
		{"https://example.test/v2/responses", ChatCompletions, "https://example.test/v2/chat/completions"},
		{"HTTPS://example.test/a%20b/", Responses, "https://example.test/a%20b/responses"},
	} {
		s := customSettings(tc.address, tc.api, "")
		got, err := s.Endpoint()
		if err != nil || got != tc.want {
			t.Errorf("%q: %q, %v; want %q", tc.address, got, err, tc.want)
		}
	}
	for _, address := range []string{"", "localhost:8000", "file:///tmp/a", "https:///x", "https://user:pass@example.test", "https://example.test/#", "https://example.test/#x", "https://example.test:invalid", "http://bad host"} {
		if _, err := customSettings(address, Responses, "").Endpoint(); err == nil {
			t.Errorf("accepted %q", address)
		}
	}
	s := DefaultSettings()
	s.Address = "ignored"
	s.API = Responses
	s.Model = "ignored"
	if endpoint, err := s.Endpoint(); err != nil || endpoint != DeepSeekEndpoint {
		t.Fatal(endpoint, err)
	}
	if s.Validate(" ") == nil {
		t.Fatal("DeepSeek allowed missing key")
	}
	if err := customSettings("http://localhost", Responses, "").Validate(""); err != nil {
		t.Fatal(err)
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// All three paths traverse a loopback httptest server. The DeepSeek request is
// checked before redirecting its transport to loopback; no real service is used.
func TestRequestPathsAndThinking(t *testing.T) {
	for _, path := range []string{DeepSeek, ChatCompletions, Responses} {
		for _, thinking := range []string{"", "off", "low", "high", "max"} {
			if path == DeepSeek && thinking == "" {
				continue
			}
			t.Run(path+"/"+thinking, func(t *testing.T) {
				var captured map[string]any
				var gotPath, gotAuth string
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
					if r.Method != "POST" || r.Header.Get("Content-Type") != "application/json" {
						t.Error("request headers/method")
					}
					if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
						t.Error(err)
					}
					if path == Responses {
						io.WriteString(w, responsesBody(validSetupJSON))
					} else {
						io.WriteString(w, chatBody(validSetupJSON))
					}
				}))
				defer server.Close()
				settings := customSettings(server.URL, path, thinking)
				client := NewClient(server.Client())
				if path == DeepSeek {
					settings = DefaultSettings()
					settings.DeepSeekThinking = thinking
					target, _ := url.Parse(server.URL)
					transport := server.Client().Transport
					client = NewClient(&http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
						if req.URL.String() != DeepSeekEndpoint {
							t.Errorf("official endpoint: %s", req.URL)
						}
						copy := req.Clone(req.Context())
						u := *req.URL
						u.Scheme, u.Host = target.Scheme, target.Host
						copy.URL = &u
						return transport.RoundTrip(copy)
					})})
				}
				result, err := client.Recognize(context.Background(), []byte{1, 2, 3}, " test-key ", settings)
				if err != nil || len(result.Pieces) != 2 {
					t.Fatal(result, err)
				}
				if gotAuth != "Bearer test-key" {
					t.Fatal(gotAuth)
				}
				if captured["stream"] != false {
					t.Fatal("stream")
				}
				wantModel := "vision-model"
				if path == DeepSeek {
					wantModel = DeepSeekModel
				}
				if captured["model"] != wantModel {
					t.Fatal(captured)
				}
				wantPath := "/chat/completions"
				if path == Responses {
					wantPath = "/responses"
				}
				if gotPath != wantPath {
					t.Fatal(gotPath)
				}
				if path == Responses {
					if captured["store"] != false || captured["instructions"] != prompt {
						t.Fatal(captured)
					}
					format := captured["text"].(map[string]any)["format"].(map[string]any)
					if format["type"] != "json_object" {
						t.Fatal(format)
					}
					content := captured["input"].([]any)[0].(map[string]any)["content"].([]any)
					image := content[1].(map[string]any)
					if image["type"] != "input_image" || image["image_url"] != "data:image/jpeg;base64,AQID" || image["detail"] != "high" {
						t.Fatal(image)
					}
				} else {
					messages := captured["messages"].([]any)
					if messages[0].(map[string]any)["content"] != prompt {
						t.Fatal("prompt mismatch")
					}
					content := messages[1].(map[string]any)["content"].([]any)
					image := content[1].(map[string]any)["image_url"].(map[string]any)
					if image["url"] != "data:image/jpeg;base64,AQID" || image["detail"] != "high" {
						t.Fatal(image)
					}
					if captured["response_format"].(map[string]any)["type"] != "json_object" {
						t.Fatal(captured)
					}
				}
				if path == DeepSeek {
					if captured["max_tokens"] != float64(tokenBudget(thinking)) {
						t.Fatal(captured)
					}
					if thinking == "off" {
						if captured["thinking"].(map[string]any)["type"] != "disabled" || captured["temperature"] != float64(0) {
							t.Fatal(captured)
						}
						if _, ok := captured["reasoning_effort"]; ok {
							t.Fatal("off effort")
						}
					} else {
						if captured["thinking"].(map[string]any)["type"] != "enabled" || captured["reasoning_effort"] != thinking {
							t.Fatal(captured)
						}
						if _, ok := captured["temperature"]; ok {
							t.Fatal("thinking temperature")
						}
					}
				} else if thinking == "" {
					for _, name := range []string{"reasoning", "reasoning_effort", "max_tokens", "max_completion_tokens", "max_output_tokens", "temperature", "thinking"} {
						if _, ok := captured[name]; ok {
							t.Fatalf("service default includes %s", name)
						}
					}
				} else if path == Responses {
					if captured["reasoning"].(map[string]any)["effort"] != compatibleEffort(thinking) || captured["max_output_tokens"] != float64(tokenBudget(thinking)) {
						t.Fatal(captured)
					}
				} else {
					if captured["reasoning_effort"] != compatibleEffort(thinking) || captured["max_completion_tokens"] != float64(tokenBudget(thinking)) {
						t.Fatal(captured)
					}
				}
			})
		}
	}
}

func TestNoRetryRedirectOrSensitiveErrors(t *testing.T) {
	for _, status := range []int{301, 302, 307, 308, 400, 401, 402, 403, 404, 429, 500, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Location", "/redirect")
				w.WriteHeader(status)
				io.WriteString(w, "secret-key and private response")
			}))
			defer server.Close()
			_, err := Recognize(context.Background(), []byte{1}, "secret-key", customSettings(server.URL, ChatCompletions, ""))
			if err == nil || strings.Contains(err.Error(), "secret-key") || calls.Load() != 1 {
				t.Fatal(err, calls.Load())
			}
		})
	}
	client := NewClient(&http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("do not persist secret-key") })})
	_, err := client.Recognize(context.Background(), []byte{1}, "secret-key", customSettings("http://localhost?key=secret-key", Responses, ""))
	if err == nil || strings.Contains(err.Error(), "secret-key") {
		t.Fatal(err)
	}
}

func TestCancellationAndTimeouts(t *testing.T) {
	entered := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		close(entered)
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := Recognize(ctx, []byte{1}, "", customSettings(server.URL, Responses, ""))
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("request never arrived")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancel did not return")
	}
	for _, tc := range []struct {
		thinking string
		want     time.Duration
	}{{"off", 120 * time.Second}, {"low", 240 * time.Second}, {"high", 240 * time.Second}, {"max", 360 * time.Second}, {"", 240 * time.Second}} {
		s := customSettings("http://localhost", Responses, tc.thinking)
		client := NewClient(&http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
			deadline, ok := r.Context().Deadline()
			left := time.Until(deadline)
			if !ok || left > tc.want || left < tc.want-time.Second {
				t.Errorf("timeout %s: %v", tc.thinking, left)
			}
			if r.GetBody != nil {
				t.Error("replayable body")
			}
			if r.Header.Get("Authorization") != "" {
				t.Error("unexpected optional key")
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(responsesBody(validSetupJSON))), Header: make(http.Header)}, nil
		})})
		if _, err := client.Recognize(context.Background(), []byte{1}, "", s); err != nil {
			t.Fatal(err)
		}
	}
}
