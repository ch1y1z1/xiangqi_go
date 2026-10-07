// Package credentials stores recognition API keys only in the system vault.
// No function is called at import time; settings validation must not read keys.
package credentials

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"

	"github.com/zalando/go-keyring"
)

// Use distinct services and a desktop-specific namespace. These do not address
// the original Swift application's existing keychain items.
const (
	DeepSeekService = "com.chiyizi.xiangqi-go.deepseek"
	CustomService   = "com.chiyizi.xiangqi-go.recognition-custom"
	account         = "api-key"
)

var ErrUnavailable = errors.New("系统凭据库不可用")

type backend interface {
	Get(service, user string) (string, error)
	Set(service, user, key string) error
	Delete(service, user string) error
}
type systemBackend struct{}

func (systemBackend) Get(service, user string) (string, error) { return keyring.Get(service, user) }
func (systemBackend) Set(service, user, key string) error      { return keyring.Set(service, user, key) }
func (systemBackend) Delete(service, user string) error        { return keyring.Delete(service, user) }

type vault struct {
	mu      sync.Mutex
	backend backend
}

var system = &vault{backend: systemBackend{}}

// Get returns empty,nil for a missing item. Call only when the user needs their
// saved key; the OS may show an unlock/access dialog.
func Get(service string) (string, error) { return system.get(service) }
func Set(service, key string) error      { return system.set(service, key) }
func Delete(service string) error        { return system.delete(service) }

func validService(service string) error {
	if len(service) == 0 || len(service) > 128 {
		return errors.New("凭据服务名称不正确。")
	}
	for _, c := range service {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '-' || c == '_') {
			return errors.New("凭据服务名称不正确。")
		}
	}
	return nil
}

func (v *vault) get(service string) (string, error) {
	if err := validService(service); err != nil {
		return "", err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	key, err := v.backend.Get(service, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", vaultError(err)
	}
	return key, nil
}
func (v *vault) set(service, key string) error {
	if err := validService(service); err != nil {
		return err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return v.delete(service)
	}
	// Stay below Windows' blob limit and macOS security's 4096-byte interactive
	// command limit (the library base64-encodes keys). Reject before it spawns.
	if len(key) > 2048 {
		return errors.New("API 密钥过长，最多支持 2048 字节。")
	}
	if strings.ContainsAny(key, "\r\n\x00") {
		return errors.New("API 密钥格式不正确。")
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	return vaultError(v.backend.Set(service, account, key))
}
func (v *vault) delete(service string) error {
	if err := validService(service); err != nil {
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	err := v.backend.Delete(service, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return vaultError(err)
}

func vaultError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, keyring.ErrSetDataTooBig) {
		return errors.New("API 密钥超过系统凭据库容量。")
	}
	// Do not relay OS command output, backend error text, or any secret to UI.
	var advice string
	switch runtime.GOOS {
	case "darwin":
		advice = "请解锁 macOS 钥匙串并允许访问。"
	case "windows":
		advice = "请检查当前 Windows 登录会话与凭据管理器。"
	case "linux":
		advice = "请确认桌面 Secret Service 已安装、正在运行且已解锁（如 GNOME Keyring 或兼容 KWallet）；不会改存明文。"
	default:
		advice = "当前平台没有支持的系统凭据库。"
	}
	return fmt.Errorf("%w：%s", ErrUnavailable, advice)
}
