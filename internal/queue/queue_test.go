package queue

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/herlangga72/adbapp/internal/adbx"
)

type fakeRunner struct {
	mu          sync.Mutex
	calls       []string
	fail        map[string]error
	block       chan struct{}
	interrupted chan struct{}
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{fail: map[string]error{}}
}

func (f *fakeRunner) record(s string) {
	f.mu.Lock()
	f.calls = append(f.calls, s)
	f.mu.Unlock()
}

func (f *fakeRunner) sequence() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.calls))
	copy(out, f.calls)
	return out
}

func (f *fakeRunner) Install(ctx context.Context, apkPath string, opts adbx.InstallOptions, stage adbx.StageFunc) error {
	if stage != nil {
		stage(10, "mulai")
	}
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			if f.interrupted != nil {
				close(f.interrupted)
			}
			return ctx.Err()
		}
	}
	f.record("install:" + apkPath)
	if err, ok := f.fail["install:"+apkPath]; ok {
		return err
	}
	if stage != nil {
		stage(100, "selesai")
	}
	return nil
}

func (f *fakeRunner) Uninstall(ctx context.Context, pkg string, keepData bool) error {
	key := "uninstall:" + pkg
	if keepData {
		key = "uninstall_keep:" + pkg
	}
	f.record(key)
	return f.fail[key]
}

func (f *fakeRunner) ClearData(ctx context.Context, pkg string) error {
	f.record("clear:" + pkg)
	return f.fail["clear:"+pkg]
}

func (f *fakeRunner) PullApk(ctx context.Context, pkg string, destDir string) (string, error) {
	f.record("pull:" + pkg)
	if err, ok := f.fail["pull:"+pkg]; ok {
		return "", err
	}
	return destDir + "/" + pkg + ".apk", nil
}

// slowSuccessRunner meniru runner yang tetap mengembalikan sukses meski
// konteksnya dibatalkan, untuk menguji cabang finalisasi cancelled.
type slowSuccessRunner struct {
	fakeRunner
	started chan struct{}
	release chan struct{}
}

func (r *slowSuccessRunner) Install(ctx context.Context, apkPath string, opts adbx.InstallOptions, stage adbx.StageFunc) error {
	close(r.started)
	<-r.release
	return nil
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout menunggu: %s", what)
}

func runQueue(t *testing.T, q *Queue) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	go q.Run(ctx)
	t.Cleanup(cancel)
	return cancel
}

func statusOf(t *testing.T, q *Queue, id string) Job {
	t.Helper()
	for _, j := range q.Jobs() {
		if j.ID == id {
			return j
		}
	}
	t.Fatalf("job %s tidak ditemukan", id)
	return Job{}
}

func TestJobsRunInOrder(t *testing.T) {
	fr := newFakeRunner()
	q := New(fr)
	runQueue(t, q)

	a := q.Enqueue(Job{Kind: KindInstall, Target: "/tmp/a.apk"})
	b := q.Enqueue(Job{Kind: KindInstall, Target: "/tmp/b.apk"})

	waitFor(t, "kedua job selesai", func() bool {
		return statusOf(t, q, a.ID).Status == StatusSuccess &&
			statusOf(t, q, b.ID).Status == StatusSuccess
	})

	got := fr.sequence()
	want := []string{"install:/tmp/a.apk", "install:/tmp/b.apk"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("urutan salah: got %v want %v", got, want)
	}
}

func TestFailingJobDoesNotStopQueue(t *testing.T) {
	fr := newFakeRunner()
	fr.fail["install:/tmp/a.apk"] = adbx.ErrInsufficientStorage
	q := New(fr)
	runQueue(t, q)

	a := q.Enqueue(Job{Kind: KindInstall, Target: "/tmp/a.apk"})
	b := q.Enqueue(Job{Kind: KindUninstall, Target: "com.b"})

	waitFor(t, "job kedua selesai", func() bool {
		return statusOf(t, q, b.ID).Status == StatusSuccess
	})
	if got := statusOf(t, q, a.ID); got.Status != StatusFailed {
		t.Fatalf("job pertama harus gagal, dapat %v", got.Status)
	}
	if got := statusOf(t, q, a.ID).Error; got == "" {
		t.Fatal("pesan error job pertama kosong")
	}
}

func TestCancelQueuedJobNotifiesOnDone(t *testing.T) {
	fr := newFakeRunner()
	fr.block = make(chan struct{})
	defer close(fr.block)
	var mu sync.Mutex
	var done []Job
	q := New(fr, WithOnDone(func(j Job, err error) {
		mu.Lock()
		done = append(done, j)
		mu.Unlock()
	}))
	runQueue(t, q)

	first := q.Enqueue(Job{Kind: KindInstall, Target: "/tmp/first.apk"})
	second := q.Enqueue(Job{Kind: KindInstall, Target: "/tmp/second.apk"})

	waitFor(t, "job pertama berjalan", func() bool {
		return statusOf(t, q, first.ID).Status == StatusRunning
	})
	if err := q.Cancel(second.ID); err != nil {
		t.Fatalf("Cancel gagal: %v", err)
	}

	waitFor(t, "callback menerima job yang dibatalkan", func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, j := range done {
			if j.ID == second.ID && j.Status == StatusCancelled {
				return true
			}
		}
		return false
	})
}

func TestDeviceDisconnectedShowsClearMessage(t *testing.T) {
	fr := newFakeRunner()
	fr.fail["install:/tmp/a.apk"] = adbx.ErrDeviceNotFound
	q := New(fr)
	runQueue(t, q)

	a := q.Enqueue(Job{Kind: KindInstall, Target: "/tmp/a.apk"})
	waitFor(t, "job gagal", func() bool {
		return statusOf(t, q, a.ID).Status == StatusFailed
	})
	got := statusOf(t, q, a.ID)
	if got.Message != "perangkat terputus" {
		t.Fatalf("pesan = %q, mau 'perangkat terputus'", got.Message)
	}
	if !strings.Contains(got.Error, adbx.ErrDeviceNotFound.Error()) {
		t.Fatalf("error asli tidak tersimpan: %q", got.Error)
	}
}

func TestCancelQueuedJobPreventsRun(t *testing.T) {
	fr := newFakeRunner()
	fr.block = make(chan struct{})
	q := New(fr)
	runQueue(t, q)

	first := q.Enqueue(Job{Kind: KindInstall, Target: "/tmp/first.apk"})
	second := q.Enqueue(Job{Kind: KindInstall, Target: "/tmp/second.apk"})

	waitFor(t, "job pertama berjalan", func() bool {
		return statusOf(t, q, first.ID).Status == StatusRunning
	})

	if err := q.Cancel(second.ID); err != nil {
		t.Fatalf("Cancel gagal: %v", err)
	}
	close(fr.block)

	waitFor(t, "job pertama selesai", func() bool {
		return statusOf(t, q, first.ID).Status == StatusSuccess
	})
	if got := statusOf(t, q, second.ID); got.Status != StatusCancelled {
		t.Fatalf("job kedua harus cancelled, dapat %v", got.Status)
	}
	for _, c := range fr.sequence() {
		if c == "install:/tmp/second.apk" {
			t.Fatal("job yang dibatalkan tetap dijalankan")
		}
	}
}

func TestWaitsWhileDeviceDisconnected(t *testing.T) {
	fr := newFakeRunner()
	var connected atomic.Bool
	q := New(fr, WithDeviceCheck(func() bool { return connected.Load() }))
	runQueue(t, q)

	a := q.Enqueue(Job{Kind: KindInstall, Target: "/tmp/a.apk"})
	time.Sleep(50 * time.Millisecond)
	if got := statusOf(t, q, a.ID).Status; got != StatusQueued {
		t.Fatalf("tanpa perangkat job harus tetap queued, dapat %v", got)
	}

	connected.Store(true)
	waitFor(t, "job jalan setelah perangkat tersambung", func() bool {
		return statusOf(t, q, a.ID).Status == StatusSuccess
	})
}

func TestOnDoneReceivesResult(t *testing.T) {
	fr := newFakeRunner()
	var mu sync.Mutex
	var done []Job
	q := New(fr, WithOnDone(func(j Job, err error) {
		mu.Lock()
		done = append(done, j)
		mu.Unlock()
	}))
	runQueue(t, q)

	a := q.Enqueue(Job{Kind: KindPull, Target: "com.foo", DestDir: "/tmp/pulled"})
	waitFor(t, "job selesai", func() bool {
		return statusOf(t, q, a.ID).Status == StatusSuccess
	})

	waitFor(t, "callback dipanggil", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(done) == 1
	})
	mu.Lock()
	defer mu.Unlock()
	if done[0].Result != "/tmp/pulled/com.foo.apk" {
		t.Fatalf("hasil pull salah: %q", done[0].Result)
	}
}

func TestCancelRunningJobReportsCancelled(t *testing.T) {
	fr := newFakeRunner()
	fr.block = make(chan struct{})
	q := New(fr)
	runQueue(t, q)

	a := q.Enqueue(Job{Kind: KindInstall, Target: "/tmp/a.apk"})
	waitFor(t, "job berjalan", func() bool {
		return statusOf(t, q, a.ID).Status == StatusRunning
	})
	if err := q.Cancel(a.ID); err != nil {
		t.Fatalf("Cancel gagal: %v", err)
	}
	waitFor(t, "job dibatalkan", func() bool {
		return statusOf(t, q, a.ID).Status == StatusCancelled
	})
}

func TestUnknownKindFails(t *testing.T) {
	fr := newFakeRunner()
	q := New(fr)
	runQueue(t, q)
	a := q.Enqueue(Job{Kind: Kind("ngawur"), Target: "x"})
	waitFor(t, "job gagal", func() bool {
		return statusOf(t, q, a.ID).Status == StatusFailed
	})
	if statusOf(t, q, a.ID).Error == "" {
		t.Fatal("job dengan jenis tak dikenal harus punya pesan error")
	}
}

func TestRunReturnsOnContextCancel(t *testing.T) {
	fr := newFakeRunner()
	q := New(fr)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		q.Run(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run tidak kembali setelah konteks dibatalkan")
	}
}

func TestSubscribeDeliversAndUnsubscribeIsIdempotent(t *testing.T) {
	fr := newFakeRunner()
	q := New(fr)
	runQueue(t, q)

	ch, unsubscribe := q.Subscribe()
	q.Enqueue(Job{Kind: KindInstall, Target: "/tmp/sub.apk"})

	select {
	case _, ok := <-ch:
		if !ok {
			t.Fatal("saluran langganan ditutup sebelum mengirim pembaruan")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("tidak menerima pembaruan langganan")
	}

	// Panggilan kedua harus idempoten dan tidak boleh panik.
	unsubscribe()
	unsubscribe()

	// Setelah unsubscribe, saluran ditutup sehingga tidak ada pengiriman baru.
	closed := false
	for !closed {
		select {
		case _, ok := <-ch:
			if !ok {
				closed = true
			}
		case <-time.After(3 * time.Second):
			t.Fatal("saluran langganan tidak ditutup setelah unsubscribe")
		}
	}

	// Pelanggan lambat: saluran penuh tidak boleh menghambat Enqueue.
	slow := New(newFakeRunner())
	slowCh, slowUnsub := slow.Subscribe()
	defer slowUnsub()

	doneEnqueue := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			slow.Enqueue(Job{Kind: KindInstall, Target: "/tmp/slow.apk"})
		}
		close(doneEnqueue)
	}()

	select {
	case <-doneEnqueue:
	case <-time.After(3 * time.Second):
		t.Fatal("Enqueue terhambat oleh pelanggan dengan saluran penuh")
	}
	if len(slowCh) != cap(slowCh) {
		t.Fatalf("saluran pelanggan lambat seharusnya penuh: len=%d cap=%d", len(slowCh), cap(slowCh))
	}
}

func TestRunJobSkipsNonQueuedJob(t *testing.T) {
	fr := newFakeRunner()
	q := New(fr)
	job := &Job{ID: "selesai", Kind: KindInstall, Target: "/tmp/selesai.apk", Status: StatusCancelled}

	q.runJob(context.Background(), job)

	if got := job.Status; got != StatusCancelled {
		t.Fatalf("job non-queued harus tetap cancelled, dapat %v", got)
	}
	if got := fr.sequence(); len(got) != 0 {
		t.Fatalf("runner tidak boleh dipanggil untuk job non-queued, dapat %v", got)
	}
}

func TestCancelledJobStaysCancelledWhenRunnerSucceeds(t *testing.T) {
	fr := &slowSuccessRunner{started: make(chan struct{}), release: make(chan struct{})}
	done := make(chan Job, 1)
	q := New(fr, WithOnDone(func(j Job, err error) { done <- j }))
	runQueue(t, q)

	a := q.Enqueue(Job{Kind: KindInstall, Target: "/tmp/a.apk"})
	select {
	case <-fr.started:
	case <-time.After(3 * time.Second):
		t.Fatal("runner tidak mulai")
	}
	if err := q.Cancel(a.ID); err != nil {
		t.Fatalf("Cancel gagal: %v", err)
	}
	close(fr.release)

	select {
	case final := <-done:
		if final.Status != StatusCancelled {
			t.Fatalf("status akhir harus cancelled, dapat %v", final.Status)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("job tidak selesai setelah runner dilepas")
	}
}

func TestCancelRunningInterruptsRunner(t *testing.T) {
	fr := newFakeRunner()
	fr.block = make(chan struct{})
	fr.interrupted = make(chan struct{})
	q := New(fr)
	runQueue(t, q)

	a := q.Enqueue(Job{Kind: KindInstall, Target: "/tmp/a.apk"})
	waitFor(t, "job berjalan", func() bool {
		return statusOf(t, q, a.ID).Status == StatusRunning
	})
	if err := q.Cancel(a.ID); err != nil {
		t.Fatalf("Cancel gagal: %v", err)
	}

	select {
	case <-fr.interrupted:
	case <-time.After(3 * time.Second):
		t.Fatal("runner tidak menerima sinyal pembatalan")
	}
	waitFor(t, "job akhir cancelled", func() bool {
		return statusOf(t, q, a.ID).Status == StatusCancelled
	})
}

func TestSubscriberSeesLifecycle(t *testing.T) {
	fr := newFakeRunner()
	q := New(fr)
	runQueue(t, q)

	ch, unsubscribe := q.Subscribe()
	defer unsubscribe()

	a := q.Enqueue(Job{Kind: KindInstall, Target: "/tmp/life.apk"})

	sawRunning := false
	sawTerminal := false
	deadline := time.After(3 * time.Second)
	for !sawRunning || !sawTerminal {
		select {
		case j, ok := <-ch:
			if !ok {
				t.Fatal("saluran langganan ditutup sebelum siklus penuh")
			}
			if j.ID != a.ID {
				continue
			}
			switch j.Status {
			case StatusRunning:
				sawRunning = true
			case StatusSuccess, StatusFailed, StatusCancelled:
				sawTerminal = true
			}
		case <-deadline:
			t.Fatalf("siklus tidak lengkap: running=%v terminal=%v", sawRunning, sawTerminal)
		}
	}
}

func TestJobJSONOmitsUnsetTimestamps(t *testing.T) {
	queued, err := json.Marshal(Job{ID: "x", Kind: KindInstall, Status: StatusQueued})
	if err != nil {
		t.Fatalf("marshal job queued: %v", err)
	}
	if bytes.Contains(queued, []byte("startedAt")) || bytes.Contains(queued, []byte("endedAt")) {
		t.Fatalf("job queued tidak boleh memuat timestamp: %s", queued)
	}

	started := time.Now()
	ended := started.Add(time.Second)
	done, err := json.Marshal(Job{ID: "x", Kind: KindInstall, Status: StatusSuccess, StartedAt: &started, EndedAt: &ended})
	if err != nil {
		t.Fatalf("marshal job selesai: %v", err)
	}
	if !bytes.Contains(done, []byte("startedAt")) || !bytes.Contains(done, []byte("endedAt")) {
		t.Fatalf("job selesai harus memuat timestamp: %s", done)
	}
}
