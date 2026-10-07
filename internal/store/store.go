// Package store persists one Swift-compatible JSON document per study.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/ch1y1z1/xiangqi_go/internal/domain"
)

// Store serializes all operations, including SaveEdited's backup and overwrite.
// List and Save use deep copies so callers cannot mutate the cache through slice
// or move pointers. Use one Store per directory; this is not a multiprocess DB.
type Store struct {
	mu        sync.RWMutex
	directory string
	studies   map[string]domain.Study
	paths     map[string]string
	write     writer
}

// New loads readable documents and initializes the original examples once.
// If some files cannot be read, it returns the usable Store AND an error naming
// them. A directory creation/read failure returns nil and an error. Interrupted
// seeding resumes from a small manifest, preserving the already generated IDs.
func New(directory string) (*Store, error) {
	abs, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0700); err != nil {
		return nil, fmt.Errorf("创建残局库：%w", err)
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, fmt.Errorf("读取残局库：%w", err)
	}
	s := &Store{directory: abs, studies: make(map[string]domain.Study), paths: make(map[string]string), write: atomicWrite}
	var failures []error
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(abs, entry.Name())
		data, err := os.ReadFile(path)
		var study domain.Study
		if err == nil {
			err = json.Unmarshal(data, &study)
		}
		if err == nil {
			err = validateStudy(study)
		}
		key := strings.ToUpper(study.ID)
		if _, exists := s.studies[key]; err == nil && exists {
			err = fmt.Errorf("重复残局 ID %s", study.ID)
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("有一个残局无法读取：%s：%w", entry.Name(), err))
			continue
		}
		s.studies[key] = study.Clone()
		s.paths[key] = path
	}
	// Do not seed over a damaged library: the caller can still display/read the
	// valid documents and surface this error without silently replacing anything.
	if len(failures) == 0 {
		if err := s.seed(); err != nil {
			failures = append(failures, err)
		}
	}
	return s, errors.Join(failures...)
}

func (s *Store) List() []domain.Study {
	s.mu.RLock()
	defer s.mu.RUnlock()
	studies := make([]domain.Study, 0, len(s.studies))
	for _, study := range s.studies {
		studies = append(studies, study.Clone())
	}
	sort.Slice(studies, func(i, j int) bool {
		if studies[i].ModifiedAt == studies[j].ModifiedAt {
			return studies[i].ID < studies[j].ID
		}
		return studies[i].ModifiedAt > studies[j].ModifiedAt
	})
	return studies
}

func (s *Store) Save(study domain.Study) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.save(study)
}

func (s *Store) save(study domain.Study) error {
	if err := validateStudy(study); err != nil {
		return err
	}
	study = study.Clone()
	data, err := json.MarshalIndent(study, "", "  ")
	if err != nil {
		return fmt.Errorf("编码残局：%w", err)
	}
	key := strings.ToUpper(study.ID)
	path := s.paths[key]
	if path == "" {
		path = filepath.Join(s.directory, study.ID+".json")
	}
	committed, err := s.write(path, append(data, '\n'))
	if committed {
		s.studies[key] = study
		s.paths[key] = path
	}
	if err != nil {
		return fmt.Errorf("保存残局 %q：%w", study.Name, err)
	}
	if !committed {
		return fmt.Errorf("保存残局 %q：文件未提交", study.Name)
	}
	return nil
}

func (s *Store) SaveEdited(study domain.Study) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateStudy(study); err != nil {
		return err
	}
	if original, exists := s.studies[strings.ToUpper(study.ID)]; exists && len(original.Nodes) > 1 && original.InitialFEN() != study.InitialFEN() {
		backup := original.Duplicate()
		backup.Name = original.Name + " · 编辑前"
		backup.ModifiedAt = original.ModifiedAt
		if err := s.save(backup); err != nil {
			return fmt.Errorf("保存编辑前副本失败，未覆盖原残局：%w", err)
		}
	}
	return s.save(study)
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validID(id) {
		return fmt.Errorf("无效残局 ID：%q", id)
	}
	key := strings.ToUpper(id)
	path := s.paths[key]
	if path == "" {
		return fmt.Errorf("删除残局 %s：%w", id, os.ErrNotExist)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("删除残局：%w", err)
	}
	delete(s.studies, key)
	delete(s.paths, key)
	if err := syncDirectory(s.directory); err != nil {
		return fmt.Errorf("残局已删除，但目录同步失败：%w", err)
	}
	return nil
}

func (s *Store) seed() error {
	marker := filepath.Join(s.directory, ".initialized")
	if _, err := os.Stat(marker); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	manifest := filepath.Join(s.directory, ".initializing")
	data, err := os.ReadFile(manifest)
	var examples []domain.Study
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &examples); err != nil {
			return fmt.Errorf("读取样例初始化记录：%w", err)
		}
	case errors.Is(err, os.ErrNotExist):
		examples = domain.Examples()
		data, err = json.Marshal(examples)
		if err != nil {
			return err
		}
		if _, err := s.write(manifest, data); err != nil {
			return fmt.Errorf("保存样例初始化记录：%w", err)
		}
	default:
		return err
	}
	for _, example := range examples {
		if _, exists := s.studies[strings.ToUpper(example.ID)]; exists {
			continue
		}
		if err := s.save(example); err != nil {
			return err
		}
	}
	if _, err := s.write(marker, []byte{}); err != nil {
		return fmt.Errorf("标记样例初始化：%w", err)
	}
	if err := os.Remove(manifest); err != nil {
		return fmt.Errorf("清理样例初始化记录：%w", err)
	}
	return syncDirectory(s.directory)
}

func validID(id string) bool {
	if len(id) != 36 {
		return false
	}
	for i, ch := range id {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if ch != '-' {
				return false
			}
			continue
		}
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')) {
			return false
		}
	}
	return true
}

// Structural validation protects file paths and the tree. Chess legality and
// piece inventory are intentionally not checked: unfinished drafts are savable.
func validateStudy(s domain.Study) error {
	if !validID(s.ID) {
		return fmt.Errorf("无效残局 ID：%q", s.ID)
	}
	if s.SchemaVersion != 1 {
		return fmt.Errorf("不支持的残局版本：%d", s.SchemaVersion)
	}
	if (s.InitialSide != domain.Red && s.InitialSide != domain.Black) || (s.BottomSide != domain.Red && s.BottomSide != domain.Black) {
		return fmt.Errorf("无效先行方或朝向")
	}
	nodes := make(map[string]domain.Node, len(s.Nodes))
	for _, n := range s.Nodes {
		key := strings.ToUpper(n.ID)
		if !validID(n.ID) {
			return fmt.Errorf("无效节点 ID：%q", n.ID)
		}
		if _, exists := nodes[key]; exists {
			return fmt.Errorf("重复节点 ID：%s", n.ID)
		}
		nodes[key] = n
	}
	root, ok := nodes[strings.ToUpper(s.RootID)]
	if !ok || root.ParentID != "" || root.Move != nil {
		return fmt.Errorf("无效研究起点")
	}
	if _, ok := nodes[strings.ToUpper(s.CurrentID)]; !ok {
		return fmt.Errorf("当前研究节点不存在")
	}
	seen := make(map[string]bool, len(nodes))
	pending := []domain.Node{root}
	for len(pending) > 0 {
		n := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		key := strings.ToUpper(n.ID)
		if seen[key] {
			return fmt.Errorf("研究树重复引用或循环：%s", n.ID)
		}
		seen[key] = true
		for _, id := range n.Children {
			child, ok := nodes[strings.ToUpper(id)]
			if !ok || !strings.EqualFold(child.ParentID, n.ID) || child.Move == nil {
				return fmt.Errorf("无效分支：%s", id)
			}
			if !child.Move.From.Valid() || !child.Move.To.Valid() || child.Move.From == child.Move.To {
				return fmt.Errorf("无效分支着法：%s", id)
			}
			pending = append(pending, child)
		}
	}
	if len(seen) != len(nodes) {
		return fmt.Errorf("研究树包含游离节点")
	}
	return nil
}
