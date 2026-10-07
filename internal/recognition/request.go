package recognition

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

const instruction = "请识别这张图片里的中国象棋残局，按约定输出 JSON。"

// Request only constructs a request. Recognize enforces the original per-effort
// timeouts. GetBody is deliberately nil: a billable POST must not be replayed.
func Request(ctx context.Context, jpeg []byte, key string, settings Settings) (*http.Request, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := settings.Validate(key); err != nil {
		return nil, err
	}
	if len(jpeg) == 0 {
		return nil, errors.New("请先选择图片。")
	}
	endpoint, _ := settings.Endpoint()
	imageURL := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(jpeg)
	body := map[string]any{"model": strings.TrimSpace(settings.Model), "stream": false}
	thinking := settings.thinking()
	if settings.api() == ChatCompletions {
		body["messages"] = []any{
			map[string]any{"role": "system", "content": prompt},
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "text", "text": instruction},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": imageURL, "detail": "high"}},
			}},
		}
		body["response_format"] = map[string]any{"type": "json_object"}
		if settings.Provider == DeepSeek {
			body["model"] = DeepSeekModel
			body["max_tokens"] = tokenBudget(thinking)
			if thinking == "off" {
				body["thinking"] = map[string]any{"type": "disabled"}
				body["temperature"] = 0
			} else {
				body["thinking"] = map[string]any{"type": "enabled"}
				body["reasoning_effort"] = thinking
			}
		} else if thinking != "" {
			body["reasoning_effort"] = compatibleEffort(thinking)
			body["max_completion_tokens"] = tokenBudget(thinking)
		}
	} else {
		body["instructions"] = prompt
		body["input"] = []any{map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "input_text", "text": instruction},
			map[string]any{"type": "input_image", "image_url": imageURL, "detail": "high"},
		}}}
		body["text"] = map[string]any{"format": map[string]any{"type": "json_object"}}
		body["store"] = false
		if thinking != "" {
			body["reasoning"] = map[string]any{"effort": compatibleEffort(thinking)}
			body["max_output_tokens"] = tokenBudget(thinking)
		}
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("API 地址格式不正确。")
	}
	req.GetBody = nil
	req.Header.Set("Content-Type", "application/json")
	if key = strings.TrimSpace(key); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	return req, nil
}

// Client permits a controlled HTTP transport in tests. It never follows
// redirects, retries, probes capabilities or falls back to another API.
type Client struct{ http *http.Client }

func NewClient(client *http.Client) *Client {
	if client == nil {
		client = &http.Client{}
	}
	copy := *client
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{http: &copy}
}

var defaultClient = NewClient(nil)

func Recognize(ctx context.Context, jpeg []byte, key string, settings Settings) (Setup, error) {
	return defaultClient.Recognize(ctx, jpeg, key, settings)
}

func (c *Client) Recognize(ctx context.Context, jpeg []byte, key string, settings Settings) (Setup, error) {
	ctx, cancel := context.WithTimeout(ctx, settings.timeout())
	defer cancel()
	req, err := Request(ctx, jpeg, key, settings)
	if err != nil {
		return Setup{}, err
	}
	response, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return Setup{}, ctx.Err()
		}
		// URL errors can include user-supplied query credentials. Never expose
		// transport errors or raw response bodies in persisted/UI messages.
		return Setup{}, errors.New("识别请求未完成，请检查网络后手动重试。")
	}
	defer response.Body.Close()
	if err := ctx.Err(); err != nil {
		return Setup{}, err
	}
	if err := statusError(response.StatusCode, settings.service()); err != nil {
		return Setup{}, err
	}
	const maxResponse = 4 << 20
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponse+1))
	if ctx.Err() != nil {
		return Setup{}, ctx.Err()
	}
	if err != nil {
		return Setup{}, errors.New("无法读取识别响应，请检查网络后手动重试。")
	}
	if len(data) > maxResponse {
		return Setup{}, errors.New("识别响应过大，请检查模型与接口类型。")
	}
	result, err := parseResponse(data, settings.api(), settings.service())
	if ctx.Err() != nil {
		return Setup{}, ctx.Err()
	}
	return result, err
}
