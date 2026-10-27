package diagnostics

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Report struct {
	ID                string
	Started           time.Time
	Unclean, HasCrash bool
	directory         string
	record            record
}

// Open only known regular files, rejecting symlinks and replacement races.
// Export never recursively walks a data directory or follows links to images,
// credentials, user documents, or update signing keys.
func openRegular(path string) (*os.File, os.FileInfo, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("not a regular diagnostic file")
	}
	f, err := openSnapshotFile(path)
	if err != nil {
		return nil, nil, err
	}
	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) {
		_ = f.Close()
		return nil, nil, errors.New("diagnostic file changed during open")
	}
	return f, after, nil
}

func readRecord(dir string) (record, error) {
	var r record
	f, info, err := openRegular(filepath.Join(dir, "session.json"))
	if err != nil {
		return r, err
	}
	defer f.Close()
	if info.Size() > 64<<10 {
		return r, errors.New("invalid diagnostic metadata size")
	}
	err = json.NewDecoder(f).Decode(&r)
	if err == nil && (r.ID != filepath.Base(dir) || r.Started.IsZero() || (r.State != "running" && r.State != "closed")) {
		err = errors.New("invalid diagnostic metadata")
	}
	return r, err
}

func (s *Session) reports() ([]Report, error) {
	entries, err := os.ReadDir(filepath.Join(s.Root, "sessions"))
	if err != nil {
		return nil, err
	}
	var reports []Report
	var failures []error
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(s.Root, "sessions", entry.Name())
		if dir == s.Directory {
			continue
		}
		lock, acquired, err := tryLock(filepath.Join(dir, "session.lock"))
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if !acquired {
			continue
		}
		r, err := readRecord(dir)
		_ = releaseLock(lock)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		crash, statErr := os.Lstat(filepath.Join(dir, "crash.log"))
		hasCrash := statErr == nil && crash.Mode().IsRegular() && crash.Size() > 0
		reports = append(reports, Report{ID: r.ID, Started: r.Started, Unclean: r.State != "closed" || hasCrash, HasCrash: hasCrash, directory: dir, record: r})
	}
	sort.Slice(reports, func(i, j int) bool { return reports[i].Started.After(reports[j].Started) })
	return reports, errors.Join(failures...)
}

type snapshotFile struct {
	file *os.File
	size int64
	name string
}

func closeSnapshot(files []snapshotFile) {
	for _, file := range files {
		_ = file.file.Close()
	}
}

func (s *Session) snapshot() ([]snapshotFile, error) {
	reports, err := s.reports()
	if err != nil {
		return nil, err
	}
	if len(reports) > 10 {
		reports = reports[:10]
	}
	// Put prior abnormal runs before the current run in the preview.
	sort.SliceStable(reports, func(i, j int) bool { return reports[i].Unclean && !reports[j].Unclean })
	dirs := []string{}
	for _, report := range reports {
		if report.Unclean {
			dirs = append(dirs, report.directory)
		}
	}
	dirs = append(dirs, s.Directory)
	for _, report := range reports {
		if !report.Unclean {
			dirs = append(dirs, report.directory)
		}
	}
	var files []snapshotFile
	// Pin open descriptors and sizes before allowing current logs to rotate.
	s.output.mu.Lock()
	defer s.output.mu.Unlock()
	for _, dir := range dirs {
		for _, name := range []string{"session.json", "crash.log", "stderr.log", "app.log", "app.log.1", "app.log.2"} {
			f, st, err := openRegular(filepath.Join(dir, name))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				closeSnapshot(files)
				return nil, err
			}
			files = append(files, snapshotFile{f, st.Size(), filepath.ToSlash(filepath.Join("sessions", filepath.Base(dir), name))})
		}
	}
	return files, nil
}

// Preview caps each file and the complete preview; Export retains complete files
// up to the snapshot sizes, even when the current app keeps logging meanwhile.
func (s *Session) Preview() (string, error) {
	files, err := s.snapshot()
	if err != nil {
		return "", err
	}
	defer closeSnapshot(files)
	var out bytes.Buffer
	for _, file := range files {
		fmt.Fprintf(&out, "\n--- %s ---\n", file.name)
		if file.size > 8<<10 && strings.HasPrefix(filepath.Base(file.name), "app.log") {
			if _, err := file.file.Seek(file.size-(8<<10), io.SeekStart); err != nil {
				return "", err
			}
			out.WriteString("[显示此文件最近的运行记录]\n")
		}
		_, err = io.Copy(&out, io.LimitReader(file.file, 8<<10))
		if err != nil {
			return "", err
		}
		if file.size > 8<<10 {
			out.WriteString("\n[此文件预览已截断，完整内容可在日志文件夹查看]\n")
		}
		if out.Len() >= 64<<10 {
			out.WriteString("\n[其余文件未预览，诊断包会包含完整内容]\n")
			break
		}
	}
	return strings.ToValidUTF8(out.String(), "�"), nil
}

func (s *Session) Export(destination string) error {
	parent, err := filepath.EvalSymlinks(filepath.Dir(destination))
	if err != nil {
		return err
	}
	root, err := filepath.EvalSymlinks(s.Root)
	if err != nil {
		return err
	}
	path, err := filepath.Abs(filepath.Join(parent, filepath.Base(destination)))
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("请将诊断包保存到日志目录之外。")
	}
	files, err := s.snapshot()
	if err != nil {
		return err
	}
	defer closeSnapshot(files)
	f, err := os.CreateTemp(parent, ".xiangqi-diagnostics-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	z := zip.NewWriter(f)
	for _, file := range files {
		header := &zip.FileHeader{Name: file.name, Method: zip.Deflate}
		header.SetMode(0600)
		var entry io.Writer
		entry, err = z.CreateHeader(header)
		if err == nil {
			_, err = io.Copy(entry, io.LimitReader(file.file, file.size))
		}
		if err != nil {
			break
		}
	}
	err = errors.Join(err, z.Close(), f.Sync(), f.Close())
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	return err
}
