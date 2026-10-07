package recognition

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ch1y1z1/xiangqi_go/internal/domain"
)

const (
	Running = "running"
	Ready   = "ready"
	Failed  = "failed"
)

var ErrJobRunning = errors.New("已有图片正在识别，请等待完成或先取消。")

type JobRecord struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	Error     string    `json:"error,omitempty"`
	StartedAt time.Time `json:"startedAt"`
	Result    *Setup    `json:"result,omitempty"`
}

// Jobs is the application's one recognition manager. Create it once at startup,
// with a dedicated directory, and keep it when panels/windows are dismissed.
// Methods are concurrency-safe. Multiple managers/processes must not share a
// directory. Requests and response bodies are never written to disk.
type Jobs struct {
	mu        sync.Mutex
	directory string
	record    *JobRecord
	image     []byte
	cancel    context.CancelFunc
	client    *Client
	// Set before Start; use SetOnChange for later replacements. Notifications
	// may run on a worker goroutine and after Clear. Dispatch to the UI thread
	// then re-read Record; never capture a result to apply without user review.
	OnChange func()
}

func NewJobs(directory string) (*Jobs, error) { return NewJobsWithClient(directory, nil) }

// NewJobsWithClient allows local HTTP tests without package-global mutation.
// A supplied client's transport must itself avoid retrying billable requests.
func NewJobsWithClient(directory string, client *Client) (*Jobs, error) {
	if strings.TrimSpace(directory) == "" {
		return nil, errors.New("请指定识别任务目录。")
	}
	path, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	if client == nil {
		client = defaultClient
	}
	j := &Jobs{directory: path, client: client}
	if err := j.cleanTransfers(); err != nil {
		return nil, fmt.Errorf("无法清理识别临时文件：%w", err)
	}
	data, err := os.ReadFile(j.file("job.json"))
	if errors.Is(err, os.ErrNotExist) {
		// A crash between saving the image and committing the record can leave
		// an orphan image. It is not a recoverable or automatically retried job.
		if err := removeIfExists(j.file("image.jpg")); err != nil {
			return nil, err
		}
		return j, nil
	}
	if err != nil {
		return nil, fmt.Errorf("无法恢复识别任务：%w", err)
	}
	var record JobRecord
	if err := json.Unmarshal(data, &record); err != nil || record.ID == "" || (record.Status != Running && record.Status != Ready && record.Status != Failed) {
		return nil, errors.New("识别任务记录损坏，无法恢复。")
	}
	j.record = &record
	image, imageErr := os.ReadFile(j.file("image.jpg"))
	if imageErr != nil && !errors.Is(imageErr, os.ErrNotExist) {
		return nil, fmt.Errorf("无法恢复识别图片：%w", imageErr)
	}
	j.image = image
	changed := false
	if record.Status == Running {
		record.Status, record.Result, record.Error = Failed, nil, "上次识别已中断，请手动重新识别；不会自动重复请求。"
		changed = true
	} else if record.Status == Ready && (record.Result == nil || record.Result.Validate() != nil || len(image) == 0) {
		record.Status, record.Result, record.Error = Failed, nil, "保存的识别结果或图片不完整，请手动重新识别。"
		changed = true
	}
	if record.Status == Failed {
		record.Result = nil
	}
	if changed {
		if err := j.persist(&record); err != nil {
			return nil, err
		}
	}
	return j, nil
}

func (j *Jobs) SetOnChange(callback func()) { j.mu.Lock(); j.OnChange = callback; j.mu.Unlock() }

func (j *Jobs) Record() *JobRecord {
	j.mu.Lock()
	defer j.mu.Unlock()
	return copyRecord(j.record)
}
func (j *Jobs) Image() []byte {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]byte(nil), j.image...)
}
func copyRecord(record *JobRecord) *JobRecord {
	if record == nil {
		return nil
	}
	copy := *record
	if record.Result != nil {
		result := *record.Result
		result.Pieces = append([]RecognizedPiece(nil), result.Pieces...)
		copy.Result = &result
	}
	return &copy
}

func (j *Jobs) Start(jpeg []byte, key string, settings Settings) error {
	j.mu.Lock()
	if j.record != nil && j.record.Status == Running {
		j.mu.Unlock()
		return ErrJobRunning
	}
	if err := settings.Validate(key); err != nil {
		j.mu.Unlock()
		return err
	}
	if len(jpeg) == 0 {
		j.mu.Unlock()
		return errors.New("请先选择图片。")
	}
	image := append([]byte(nil), jpeg...)
	next := &JobRecord{ID: domain.NewID(), Status: Running, StartedAt: time.Now()}
	// Validate before discarding an already completed result. No network starts
	// until both image and job record have been committed successfully.
	if err := j.clearFiles(); err != nil {
		j.mu.Unlock()
		return err
	}
	j.record, j.image = nil, nil
	err := os.MkdirAll(j.directory, 0700)
	if err == nil {
		err = atomicWrite(j.file("image.jpg"), image)
	}
	if err == nil {
		err = j.persist(next)
	}
	if err != nil {
		cleanupErr := j.clearFiles()
		callback := j.OnChange
		j.mu.Unlock()
		if callback != nil {
			callback()
		}
		return fmt.Errorf("无法保存识别任务：%w", errors.Join(err, cleanupErr))
	}
	ctx, cancel := context.WithCancel(context.Background())
	j.cancel, j.record, j.image = cancel, next, image
	callback := j.OnChange
	id := next.ID
	j.mu.Unlock()
	go func() {
		defer cancel()
		result, err := j.client.Recognize(ctx, image, key, settings)
		j.finish(id, result, err)
	}()
	if callback != nil {
		callback()
	}
	return nil
}

// Stop cancels and discards the job, including persisted image/result. The caller
// can take Image() before Stop/Clear when retaining a local correction preview.
func (j *Jobs) Stop() error { return j.Clear() }

// Interrupt is used on application exit. Keep the image and completed results
// for manual recovery, without retrying a billable request on the next launch.
func (j *Jobs) Interrupt() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.record == nil || j.record.Status != Running {
		return nil
	}
	if j.cancel != nil {
		j.cancel()
		j.cancel = nil
	}
	next := copyRecord(j.record)
	next.Status, next.Result, next.Error = Failed, nil, "识别已随应用退出而中断，请手动重新识别。"
	j.record = next
	return j.persist(next)
}

// Clear also invalidates in-flight results. A transport that ignores cancellation
// still cannot repopulate a cleared job or replace a newer one.
func (j *Jobs) Clear() error {
	j.mu.Lock()
	if j.cancel != nil {
		j.cancel()
		j.cancel = nil
	}
	j.record, j.image = nil, nil
	err := j.clearFiles()
	callback := j.OnChange
	j.mu.Unlock()
	if callback != nil {
		callback()
	}
	return err
}

func (j *Jobs) finish(id string, setup Setup, requestErr error) {
	j.mu.Lock()
	if j.record == nil || j.record.ID != id || j.record.Status != Running {
		j.mu.Unlock()
		return
	}
	next := copyRecord(j.record)
	if requestErr == nil {
		next.Status, next.Result = Ready, &setup
	} else {
		next.Status, next.Result, next.Error = Failed, nil, requestErr.Error()
	}
	if err := j.persist(next); err != nil {
		next.Status, next.Result, next.Error = Failed, nil, "无法保存识别结果，请检查任务目录是否可写。"
		// Best effort to replace a prior running record; restoration of any
		// remaining running record always fails closed, without network traffic.
		_ = j.persist(next)
	}
	if err := j.cleanTransfers(); err != nil {
		next.Error = "无法清理识别临时文件，请检查任务目录权限。"
	}
	j.record, j.cancel = next, nil
	callback := j.OnChange
	j.mu.Unlock()
	if callback != nil {
		callback()
	}
}

func (j *Jobs) file(name string) string { return filepath.Join(j.directory, name) }
func (j *Jobs) persist(record *JobRecord) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return atomicWrite(j.file("job.json"), data)
}
func removeIfExists(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
func (j *Jobs) cleanTransfers() error {
	var errs []error
	for _, name := range []string{"request.json", "response.json"} {
		errs = append(errs, removeIfExists(j.file(name)))
	}
	entries, err := os.ReadDir(j.directory)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		errs = append(errs, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), ".recognition-") && strings.HasSuffix(entry.Name(), ".tmp") {
			errs = append(errs, removeIfExists(j.file(entry.Name())))
		}
	}
	return errors.Join(errs...)
}
func (j *Jobs) clearFiles() error {
	// Remove only our own files, never recursively delete caller-owned data.
	err := errors.Join(removeIfExists(j.file("job.json")), removeIfExists(j.file("image.jpg")), j.cleanTransfers())
	if err == nil {
		_ = os.Remove(j.directory)
	} // Keep a nonempty caller directory.
	return err
}

func atomicWrite(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".recognition-*.tmp")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(temp, path)
}
