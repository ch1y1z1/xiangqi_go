package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/ch1y1z1/xiangqi_go/internal/domain"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func findStudy(t *testing.T, s *Store, id string) domain.Study {
	t.Helper()
	for _, study := range s.List() {
		if strings.EqualFold(study.ID, id) {
			return study
		}
	}
	t.Fatalf("missing %s", id)
	return domain.Study{}
}
func studied(t *testing.T) domain.Study {
	t.Helper()
	s := domain.NewStudy("有研究", domain.InitialPieces(), domain.Red)
	m, err := domain.ParseMove("a0a1")
	if err != nil {
		t.Fatal(err)
	}
	s.Play(m)
	s.IsDraft = false
	return s
}

func TestSeedOnceDraftAndReopen(t *testing.T) {
	s := newStore(t)
	if len(s.List()) != 3 {
		t.Fatal("expected three original examples")
	}
	for _, example := range s.List() {
		if example.IsDraft {
			t.Fatal("example is draft")
		}
		if err := s.Delete(example.ID); err != nil {
			t.Fatal(err)
		}
	}
	reopened, err := New(s.directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.List()) != 0 {
		t.Fatal("empty library reseeded")
	}
	draft := domain.NewStudy("未完成", nil, domain.Black)
	draft.BottomSide = domain.Black
	if err := reopened.Save(draft); err != nil {
		t.Fatal(err)
	}
	draft.Name = "保存覆盖"
	draft.ModifiedAt++
	if err := reopened.Save(draft); err != nil {
		t.Fatal(err)
	}
	reopened, err = New(s.directory)
	if err != nil {
		t.Fatal(err)
	}
	got := reopened.List()
	if len(got) != 1 || !reflect.DeepEqual(draft, got[0]) {
		t.Fatalf("draft changed on reopen: %+v", got)
	}
	// List and Save must not lend out cache-owned slices or move pointers.
	study := studied(t)
	if err := reopened.Save(study); err != nil {
		t.Fatal(err)
	}
	study.InitialPieces[0].ID = "changed"
	study.Nodes[1].Move.To = domain.Square{File: 8, Rank: 8}
	copy := findStudy(t, reopened, study.ID)
	if copy.InitialPieces[0].ID == "changed" || copy.Nodes[1].Move.To == (domain.Square{File: 8, Rank: 8}) {
		t.Fatal("Save retained caller aliases")
	}
	copy.Nodes[0].Children[0] = "changed"
	copy.Nodes[1].Move.To = domain.Square{File: 8, Rank: 8}
	if fresh := findStudy(t, reopened, study.ID); fresh.Nodes[0].Children[0] == "changed" || fresh.Nodes[1].Move.To == (domain.Square{File: 8, Rank: 8}) {
		t.Fatal("List leaked cache aliases")
	}
	list := reopened.List()
	for i := 1; i < len(list); i++ {
		if list[i-1].ModifiedAt < list[i].ModifiedAt {
			t.Fatal("not sorted by modifiedAt")
		}
	}
}

func TestSaveEditedBackupAndFailureOrdering(t *testing.T) {
	for _, failure := range []string{"none", "backup", "overwrite", "postcommit"} {
		t.Run(failure, func(t *testing.T) {
			s := newStore(t)
			original := studied(t)
			if err := s.Save(original); err != nil {
				t.Fatal(err)
			}
			originalBytes, err := os.ReadFile(s.paths[strings.ToUpper(original.ID)])
			if err != nil {
				t.Fatal(err)
			}
			edited := original.EditSetup("空盘草稿", nil, domain.Black, domain.Black)
			edited.IsDraft = true
			var calls int
			injected := errors.New("injected disk error")
			s.write = func(path string, data []byte) (bool, error) {
				calls++
				if failure == "backup" && calls == 1 || failure == "overwrite" && calls == 2 {
					return false, injected
				}
				committed, err := atomicWrite(path, data)
				if failure == "postcommit" && calls == 2 && err == nil {
					return committed, injected
				}
				return committed, err
			}
			err = s.SaveEdited(edited)
			if failure == "none" && err != nil || failure != "none" && !errors.Is(err, injected) {
				t.Fatalf("unexpected save result: %v", err)
			}
			got := findStudy(t, s, original.ID)
			if failure == "backup" || failure == "overwrite" {
				if !reflect.DeepEqual(got, original) {
					t.Fatal("failed write changed original in memory")
				}
				currentBytes, err := os.ReadFile(s.paths[strings.ToUpper(original.ID)])
				if err != nil {
					t.Fatal(err)
				}
				if string(currentBytes) != string(originalBytes) {
					t.Fatal("failed write changed original file")
				}
			} else if !reflect.DeepEqual(got, edited) {
				t.Fatal("committed file not reflected in memory")
			}
			if failure == "backup" {
				if calls != 1 || len(s.List()) != 4 {
					t.Fatal("original overwritten after backup failed")
				}
				return
			}
			if calls != 2 || len(s.List()) != 5 {
				t.Fatal("missing backup")
			}
			var backup domain.Study
			for _, study := range s.List() {
				if study.Name == original.Name+" · 编辑前" {
					backup = study
				}
			}
			if backup.ID == "" || backup.ID == original.ID || backup.ModifiedAt != original.ModifiedAt || backup.CurrentID != original.CurrentID || !reflect.DeepEqual(backup.Nodes, original.Nodes) || !reflect.DeepEqual(backup.InitialPieces, original.InitialPieces) {
				t.Fatal("backup lost original history/metadata")
			}
			reopened, err := New(s.directory)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(findStudy(t, reopened, original.ID), got) {
				t.Fatal("disk/cache disagree after failure")
			}
		})
	}
	// Renaming/flipping and setup edits before any play need no backup.
	s := newStore(t)
	original := studied(t)
	if err := s.Save(original); err != nil {
		t.Fatal(err)
	}
	edited := original.EditSetup("改名", domain.InitialPieces(), domain.Red, domain.Black)
	if err := s.SaveEdited(edited); err != nil {
		t.Fatal(err)
	}
	if len(s.List()) != 4 {
		t.Fatal("rename created backup")
	}
	draft := domain.NewStudy("新草稿", nil, domain.Red)
	if err := s.Save(draft); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveEdited(draft.EditSetup("初始盘", domain.InitialPieces(), domain.Red, domain.Red)); err != nil {
		t.Fatal(err)
	}
	if len(s.List()) != 5 {
		t.Fatal("unplayed draft created backup")
	}
}

func TestAtomicOverwriteFailureAndCleanup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "残局.json")
	if committed, err := atomicWrite(path, []byte("old")); err != nil || !committed {
		t.Fatalf("initial write: %t %v", committed, err)
	}
	injected := errors.New("replace failure")
	committed, err := atomicWriteWithReplace(path, []byte("broken"), func(from, to string) error {
		if filepath.Dir(from) != filepath.Dir(to) {
			t.Fatal("temp not in destination directory")
		}
		data, err := os.ReadFile(from)
		if err != nil || string(data) != "broken" {
			t.Fatal("temp was not complete before replace")
		}
		return injected
	})
	if committed || !errors.Is(err, injected) {
		t.Fatalf("failed replacement reported success: %t %v", committed, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "old" {
		t.Fatal("old file damaged")
	}
	if committed, err := atomicWrite(path, []byte("new")); err != nil || !committed {
		t.Fatalf("existing target overwrite: %t %v", committed, err)
	}
	data, err = os.ReadFile(path)
	if err != nil || string(data) != "new" {
		t.Fatal("overwrite did not take effect")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("temp leaked: %v %v", entries, err)
	}
	if committed, err := atomicWrite(filepath.Join(dir, "missing", "study.json"), nil); err == nil || committed {
		t.Fatal("missing destination reported success")
	}
	// A real OS-level replacement failure must also clean the temporary file.
	blocked := filepath.Join(dir, "directory.json")
	if err := os.Mkdir(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	if committed, err := atomicWrite(blocked, []byte("bad")); err == nil || committed {
		t.Fatal("directory target reported success")
	}
	entries, err = os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Fatal("temp leaked after OS failure")
	}
}

func TestInterruptedSeedAndCorruptFile(t *testing.T) {
	dir := t.TempDir()
	examples := domain.Examples()
	manifest, err := json.Marshal(examples)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := atomicWrite(filepath.Join(dir, ".initializing"), manifest); err != nil {
		t.Fatal(err)
	}
	first, err := json.Marshal(examples[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := atomicWrite(filepath.Join(dir, examples[0].ID+".json"), first); err != nil {
		t.Fatal(err)
	}
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.List()) != 3 {
		t.Fatal("interrupted seed duplicated examples")
	}
	for _, example := range examples {
		if !reflect.DeepEqual(findStudy(t, s, example.ID), example) {
			t.Fatal("seed changed stable IDs/dates")
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "damaged.json"), []byte(`{"id":`), 0600); err != nil {
		t.Fatal(err)
	}
	s, err = New(dir)
	if s == nil || err == nil || !strings.Contains(err.Error(), "damaged.json") || len(s.List()) != 3 {
		t.Fatalf("corruption lost usable library: %v", err)
	}
	invalid := domain.NewStudy("bad", nil, domain.Red)
	invalid.ID = "../../escape"
	if err := s.Save(invalid); err == nil {
		t.Fatal("path traversal ID accepted")
	}
	if err := s.Delete("../../escape"); err == nil {
		t.Fatal("path traversal delete accepted")
	}
}

func TestSwiftFileNameAndUUIDCase(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".initialized"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	s := studied(t)
	s.ID = strings.ToLower(s.ID)
	s.CurrentID = strings.ToLower(s.CurrentID)
	s.Nodes[1].ParentID = strings.ToLower(s.Nodes[1].ParentID)
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "imported.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.ID = strings.ToUpper(s.ID)
	s.Name = "覆盖同一文件"
	if err := store.Save(s); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 || len(store.List()) != 1 {
		t.Fatal("case change duplicated document")
	}
	if err := store.Delete(strings.ToLower(s.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("delete missed imported filename")
	}
}

func TestConcurrentStoreSnapshots(t *testing.T) {
	s := newStore(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			study := domain.NewStudy("并发", nil, domain.Red)
			for n := 0; n < 6; n++ {
				study.ModifiedAt++
				if err := s.Save(study); err != nil {
					t.Error(err)
					return
				}
				for _, copy := range s.List() {
					copy.Nodes[0].ID = "external mutation"
					if len(copy.InitialPieces) > 0 {
						copy.InitialPieces[0].ID = "external mutation"
					}
				}
			}
			if err := s.Delete(study.ID); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if len(s.List()) != 3 {
		t.Fatal("concurrent operations lost/added studies")
	}
	if _, err := New(s.directory); err != nil {
		t.Fatal(err)
	}
}
