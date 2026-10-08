package chessboard

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/egoist/mygo/ui"
)

// LoadSystemPieceFont uses an already installed Kai font without asking the OS
// to find or download an optional face. Call before creating any board views.
// Missing fonts keep the system interface font and its Chinese glyph fallback.
// Apple font files stay on the user's system and are never bundled in releases.
func LoadSystemPieceFont() {
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{"/System/Library/Fonts/Supplemental/Kaiti.ttc", "/Library/Fonts/Kaiti.ttc"}
		assets, _ := filepath.Glob("/System/Library/AssetsV2/com_apple_MobileAsset_Font*/*.asset/AssetData/Kaiti.ttc")
		candidates = append(candidates, assets...)
	case "windows":
		directory := os.Getenv("WINDIR")
		if directory == "" {
			directory = `C:\Windows`
		}
		candidates = []string{filepath.Join(directory, "Fonts", "simkai.ttf")}
	case "linux":
		candidates = []string{"/usr/share/fonts/truetype/arphic/ukai.ttc"}
	}
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err == nil && ui.RegisterFont(data, "Xiangqi Pieces") == nil {
			pieceFamily = "Xiangqi Pieces"
			return
		}
	}
}
