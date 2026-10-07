package recognition

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

func statusError(status int, service string) error {
	switch {
	case status >= 200 && status < 300:
		return nil
	case status == 400:
		return fmt.Errorf("%s不接受当前请求，请检查模型、接口类型、图片与 JSON 输出支持；自定义服务可将思考强度设为服务默认。", service)
	case status == 401 || status == 403:
		return fmt.Errorf("%s密钥无效或没有权限，请在设置中检查。", service)
	case status == 402:
		return fmt.Errorf("%s账户余额不足，请充值后重试。", service)
	case status == 404:
		return fmt.Errorf("未找到%s的接口或模型，请检查 API 地址、接口类型与模型名称。", service)
	case status == 429:
		return fmt.Errorf("%s请求过于频繁或额度不足，请稍后重试。", service)
	default:
		return fmt.Errorf("%s请求失败（%d），请稍后重试。", service, status)
	}
}

func parseResponse(data []byte, api, service string) (Setup, error) {
	var content string
	malformed := fmt.Errorf("无法读取%s的识别响应，请检查接口类型。", service)
	empty := fmt.Errorf("%s没有返回识别结果，请重试。", service)
	incomplete := errors.New("识别结果不完整，请重试。")
	refused := errors.New("模型拒绝识别这张图片，请更换图片或模型。")
	if api == ChatCompletions {
		var response struct {
			Choices *[]struct {
				Message *struct {
					Content *string `json:"content"`
					Refusal *string `json:"refusal"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(data, &response); err != nil || response.Choices == nil {
			return Setup{}, malformed
		}
		if len(*response.Choices) == 0 {
			return Setup{}, empty
		}
		choice := (*response.Choices)[0]
		if choice.Message == nil {
			return Setup{}, malformed
		}
		if choice.FinishReason == "length" {
			return Setup{}, incomplete
		}
		if choice.Message.Refusal != nil || choice.FinishReason == "content_filter" {
			return Setup{}, refused
		}
		if choice.Message.Content == nil || *choice.Message.Content == "" {
			return Setup{}, empty
		}
		content = *choice.Message.Content
	} else if api == Responses {
		var response struct {
			Status *string `json:"status"`
			Output *[]struct {
				Type    *string `json:"type"`
				Role    string  `json:"role"`
				Status  *string `json:"status"`
				Content []struct {
					Type *string `json:"type"`
					Text *string `json:"text"`
				} `json:"content"`
			} `json:"output"`
		}
		if err := json.Unmarshal(data, &response); err != nil || response.Output == nil {
			return Setup{}, malformed
		}
		if response.Status != nil && *response.Status == "incomplete" {
			return Setup{}, incomplete
		}
		if response.Status != nil && *response.Status != "completed" {
			return Setup{}, fmt.Errorf("%s未完成识别，请检查模型或稍后重试。", service)
		}
		var texts strings.Builder
		for _, output := range *response.Output {
			if output.Type == nil {
				return Setup{}, malformed
			}
			// Only final assistant messages are results; reasoning/tool/user text
			// must not be concatenated with the recognition JSON.
			if *output.Type != "message" || output.Role != "assistant" {
				continue
			}
			if output.Status != nil && *output.Status == "incomplete" {
				return Setup{}, incomplete
			}
			if output.Status != nil && *output.Status != "completed" {
				return Setup{}, fmt.Errorf("%s未完成识别，请稍后重试。", service)
			}
			for _, part := range output.Content {
				if part.Type == nil {
					return Setup{}, malformed
				}
				if *part.Type == "refusal" {
					return Setup{}, refused
				}
				if *part.Type == "output_text" && part.Text != nil {
					texts.WriteString(*part.Text)
				}
			}
		}
		content = texts.String()
		if content == "" {
			return Setup{}, empty
		}
	} else {
		return Setup{}, malformed
	}
	return parseSetup(content)
}
