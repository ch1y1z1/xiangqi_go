package recognition

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// macOS ImageIO is available through its system image conversion tool. Keep
// HEIC decoding off the UI thread, without a cgo dependency or extra download.
func PrepareFile(path string) ([]byte, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("无法读取图片：%w", err)
	}
	if st.Size() > 64<<20 {
		return nil, fmt.Errorf("图片文件过大，请选择不超过 64 MiB 的图片。")
	}
	ext := strings.ToLower(filepath.Ext(path))
	if runtime.GOOS == "darwin" && (ext == ".heic" || ext == ".heif") {
		dir, err := os.MkdirTemp("", "xiangqi-image-*")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(dir)
		out := filepath.Join(dir, "converted.jpg")
		if err = exec.Command("/usr/bin/sips", "-s", "format", "jpeg", "--resampleHeightWidthMax", "2048", path, "--out", out).Run(); err != nil {
			return nil, fmt.Errorf("无法打开 HEIC 图片，请检查文件是否完整。")
		}
		path = out
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return PrepareImage(data)
}
