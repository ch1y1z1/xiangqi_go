package recognition

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// Exercise real persistence and local HTTP once: quitting must retain a
// completed result and must never silently retry an interrupted request.
func TestJobsExitRecovery(t *testing.T) {
	for _, running := range []bool{false, true} {
		name := "completed"
		if running {
			name = "interrupted"
		}
		t.Run(name, func(t *testing.T) {
			var requests atomic.Int32
			received, release := make(chan struct{}, 1), make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				received <- struct{}{}
				if running {
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
				}
				fmt.Fprint(w, chatBody(validSetupJSON))
			}))
			defer server.Close()
			defer close(release)
			directory := t.TempDir()
			jobs, err := NewJobsWithClient(directory, NewClient(server.Client()))
			if err != nil {
				t.Fatal(err)
			}
			changed := make(chan struct{}, 4)
			jobs.SetOnChange(func() { changed <- struct{}{} })
			if err := jobs.Start([]byte{1, 2, 3}, "", customSettings(server.URL, ChatCompletions, "")); err != nil {
				t.Fatal(err)
			}
			deadline := time.After(2 * time.Second)
			select {
			case <-received:
			case <-deadline:
				t.Fatal("request did not start")
			}
			for !running && jobs.Record().Status == Running {
				select {
				case <-changed:
				case <-deadline:
					t.Fatal("local recognition did not finish")
				}
			}
			id := jobs.Record().ID
			if err := jobs.Interrupt(); err != nil {
				t.Fatal(err)
			}
			restored, err := NewJobs(directory)
			if err != nil {
				t.Fatal(err)
			}
			record := restored.Record()
			want := Ready
			if running {
				want = Failed
			}
			if record == nil || record.ID != id || record.Status != want || len(restored.Image()) != 3 || requests.Load() != 1 {
				t.Fatalf("exit recovery: %+v; requests=%d", record, requests.Load())
			}
			if !running && (record.Result == nil || len(record.Result.Pieces) != 2) {
				t.Fatal("completed result lost on exit")
			}
		})
	}
}
