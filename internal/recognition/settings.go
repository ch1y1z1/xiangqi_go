// Package recognition implements user-initiated image recognition. Constructing
// requests, preparing images and restoring jobs never starts a network request.
package recognition

import (
	"errors"
	"net/url"
	"strings"
	"time"
)

const (
	DeepSeek         = "deepSeek"
	Custom           = "custom"
	ChatCompletions  = "chatCompletions"
	Responses        = "responses"
	DeepSeekModel    = "deepseek-v4-flash"
	DeepSeekEndpoint = "https://api.deepseek.com/chat/completions"
)

type Settings struct {
	Provider         string `json:"provider"`
	Address          string `json:"address"`
	Model            string `json:"model"`
	API              string `json:"api"`
	DeepSeekThinking string `json:"deepSeekThinking"`
	CustomThinking   string `json:"customThinking,omitempty"`
}

func DefaultSettings() Settings {
	return Settings{Provider: DeepSeek, API: ChatCompletions, DeepSeekThinking: "high"}
}

func (s Settings) Endpoint() (string, error) {
	if s.Provider == DeepSeek {
		return DeepSeekEndpoint, nil
	}
	if s.Provider != Custom {
		return "", errors.New("不支持的识别服务。")
	}
	suffix := "/chat/completions"
	if s.API == Responses {
		suffix = "/responses"
	} else if s.API != ChatCompletions {
		return "", errors.New("不支持的识别接口。")
	}
	address := strings.TrimSpace(s.Address)
	u, err := url.Parse(address)
	if err != nil || u == nil || (strings.ToLower(u.Scheme) != "http" && strings.ToLower(u.Scheme) != "https") || u.Hostname() == "" || u.User != nil || strings.Contains(address, "#") || u.Opaque != "" {
		return "", errors.New("请填写有效的 HTTP 或 HTTPS API 地址。")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	p := strings.TrimRight(u.Path, "/")
	for _, ending := range []string{"/chat/completions", "/responses"} {
		if strings.HasSuffix(p, ending) {
			p = strings.TrimSuffix(p, ending)
			break
		}
	}
	u.Path, u.RawPath = p+suffix, ""
	return u.String(), nil
}

func (s Settings) Validate(key string) error {
	if _, err := s.Endpoint(); err != nil {
		return err
	}
	if s.Provider == DeepSeek && strings.TrimSpace(key) == "" {
		return errors.New("请先在设置中保存 DeepSeek API 密钥。")
	}
	if s.Provider == Custom && strings.TrimSpace(s.Model) == "" {
		return errors.New("请先在设置中填写自定义模型名称。")
	}
	if strings.ContainsAny(strings.TrimSpace(key), "\r\n\x00") {
		return errors.New("API 密钥格式不正确。")
	}
	switch s.thinking() {
	case "":
		if s.Provider == DeepSeek {
			return errors.New("请选择 DeepSeek 思考强度。")
		}
	case "off", "low", "high", "max":
	default:
		return errors.New("不支持的思考强度。")
	}
	return nil
}

func (s Settings) thinking() string {
	if s.Provider == DeepSeek {
		return s.DeepSeekThinking
	}
	return s.CustomThinking
}
func (s Settings) api() string {
	if s.Provider == DeepSeek {
		return ChatCompletions
	}
	return s.API
}
func (s Settings) service() string {
	if s.Provider == DeepSeek {
		return "DeepSeek"
	}
	return "自定义服务"
}
func (s Settings) timeout() time.Duration {
	switch s.thinking() {
	case "off":
		return 120 * time.Second
	case "max":
		return 360 * time.Second
	default:
		return 240 * time.Second
	}
}
func tokenBudget(thinking string) int {
	switch thinking {
	case "off":
		return 4096
	case "max":
		return 65536
	default:
		return 32768
	}
}
func compatibleEffort(thinking string) string {
	switch thinking {
	case "off":
		return "none"
	case "max":
		return "xhigh"
	default:
		return thinking
	}
}
