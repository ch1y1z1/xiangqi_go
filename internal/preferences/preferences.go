package preferences

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/ch1y1z1/xiangqi_go/internal/recognition"
)

type Settings struct {
	ShowEvaluation   bool                 `json:"showPositionEvaluation"`
	ShowBranchScores bool                 `json:"showBranchEvaluation"`
	ShowLights       bool                 `json:"showLights"`
	Sound            bool                 `json:"soundEnabled"`
	Budget           int                  `json:"thinkMilliseconds"`
	LeftWidth        float32              `json:"leftWidth"`
	RightWidth       float32              `json:"rightWidth"`
	CurrentStudy     string               `json:"currentStudy"`
	Recognition      recognition.Settings `json:"recognition"`
}

func Defaults() Settings {
	return Settings{ShowEvaluation: true, ShowBranchScores: true, ShowLights: true,
		Budget: 1000, LeftWidth: 252, RightWidth: 360, Recognition: recognition.DefaultSettings()}
}

func Load(path string) (Settings, error) {
	s := Defaults()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return Defaults(), err
	}
	if s.Budget != 300 && s.Budget != 1000 && s.Budget != 3000 {
		s.Budget = 1000
	}
	if s.LeftWidth < 220 || s.LeftWidth > 360 {
		s.LeftWidth = 252
	}
	if s.RightWidth < 300 || s.RightWidth > 520 {
		s.RightWidth = 360
	}
	return s, nil
}

func Save(path string, s Settings) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".preferences-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, path)
}
