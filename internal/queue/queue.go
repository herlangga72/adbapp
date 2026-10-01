// Package queue menjalankan pekerjaan install/uninstall satu per satu.
package queue

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/herlangga72/adbapp/internal/adbx"
)

type Kind string

const (
	KindInstall       Kind = "install"
	KindUninstall     Kind = "uninstall"
	KindUninstallKeep Kind = "uninstall_keep"
	KindClearData     Kind = "clear_data"
	KindPull          Kind = "pull"
)

type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSuccess   Status = "success"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

// Job adalah satu pekerjaan di antrean.
type Job struct {
	ID             string    `json:"id"`
	Kind           Kind      `json:"kind"`
	Target         string    `json:"target"`
	Label          string    `json:"label,omitempty"`
	DestDir        string    `json:"destDir,omitempty"`
	Replace        bool      `json:"replace,omitempty"`
	AllowDowngrade bool      `json:"allowDowngrade,omitempty"`
	Status         Status    `json:"status"`
	Progress       int       `json:"progress"`
	Message        string    `json:"message,omitempty"`
	Error          string    `json:"error,omitempty"`
	Result         string    `json:"result,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	StartedAt      time.Time `json:"startedAt,omitempty"`
	EndedAt        time.Time `json:"endedAt,omitempty"`
}

// Runner adalah kemampuan perangkat yang dibutuhkan antrean.
type Runner interface {
	Install(ctx context.Context, apkPath string, opts adbx.InstallOptions, stage adbx.StageFunc) error
	Uninstall(ctx context.Context, pkg string, keepData bool) error
	ClearData(ctx context.Context, pkg string) error
	PullApk(ctx context.Context, pkg string, destDir string) (string, error)
}

type Option func(*Queue)

// WithDeviceCheck membuat antrean menahan pekerjaan saat perangkat tidak siap.
func WithDeviceCheck(fn func() bool) Option {
	return func(q *Queue) { q.deviceOK = fn }
}

// WithOnDone mendaftarkan callback setelah sebuah job selesai (untuk riwayat).
func WithOnDone(fn func(Job, error)) Option {
	return func(q *Queue) { q.onDone = fn }
}

type Queue struct {
	runner   Runner
	deviceOK func() bool
	onDone   func(Job, error)

	mu      sync.Mutex
	jobs    []*Job
	byID    map[string]*Job
	cancels map[string]context.CancelFunc
	subs    map[int]chan Job
	nextSub int
	nextID  int
	notify  chan struct{}
}

func New(r Runner, opts ...Option) *Queue {
	q := &Queue{
		runner:  r,
		byID:    map[string]*Job{},
		cancels: map[string]context.CancelFunc{},
		subs:    map[int]chan Job{},
		notify:  make(chan struct{}, 1),
	}
	for _, o := range opts {
		o(q)
	}
	return q
}

// Enqueue menambahkan pekerjaan baru ke ujung antrean.
func (q *Queue) Enqueue(spec Job) Job {
	q.mu.Lock()
	q.nextID++
	if spec.ID == "" {
		spec.ID = fmt.Sprintf("job-%d", q.nextID)
	}
	spec.Status = StatusQueued
	if spec.CreatedAt.IsZero() {
		spec.CreatedAt = time.Now()
	}
	job := &spec
	q.jobs = append(q.jobs, job)
	q.byID[job.ID] = job
	snapshot := *job
	q.mu.Unlock()

	q.broadcast(snapshot)
	q.wake()
	return snapshot
}

func (q *Queue) wake() {
	select {
	case q.notify <- struct{}{}:
	default:
	}
}

// Jobs mengembalikan salinan seluruh pekerjaan.
func (q *Queue) Jobs() []Job {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]Job, 0, len(q.jobs))
	for _, j := range q.jobs {
		out = append(out, *j)
	}
	return out
}

// Cancel membatalkan pekerjaan yang masih menunggu atau sedang berjalan.
func (q *Queue) Cancel(id string) error {
	q.mu.Lock()
	job, ok := q.byID[id]
	if !ok {
		q.mu.Unlock()
		return fmt.Errorf("job %s tidak ditemukan", id)
	}
	switch job.Status {
	case StatusQueued:
		job.Status = StatusCancelled
		job.Message = "dibatalkan sebelum dijalankan"
		job.EndedAt = time.Now()
		snap := *job
		q.mu.Unlock()
		q.broadcast(snap)
		return nil
	case StatusRunning:
		cancel := q.cancels[id]
		job.Status = StatusCancelled
		job.Message = "dibatalkan"
		q.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		return nil
	default:
		q.mu.Unlock()
		return fmt.Errorf("job %s sudah selesai", id)
	}
}

// Subscribe menerima pembaruan setiap kali status job berubah.
func (q *Queue) Subscribe() (<-chan Job, func()) {
	ch := make(chan Job, 64)
	q.mu.Lock()
	q.nextSub++
	id := q.nextSub
	q.subs[id] = ch
	q.mu.Unlock()

	return ch, func() {
		q.mu.Lock()
		if c, ok := q.subs[id]; ok {
			delete(q.subs, id)
			close(c)
		}
		q.mu.Unlock()
	}
}

func (q *Queue) broadcast(j Job) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, ch := range q.subs {
		select {
		case ch <- j:
		default: // pelanggan lambat tidak boleh menghambat antrean
		}
	}
}

// Run menjalankan antrean sampai ctx dibatalkan.
func (q *Queue) Run(ctx context.Context) {
	for {
		job := q.nextQueued()
		if job == nil {
			select {
			case <-ctx.Done():
				return
			case <-q.notify:
			case <-time.After(time.Second):
			}
			continue
		}
		if q.deviceOK != nil && !q.deviceOK() {
			// Tahan antrean, jangan tandai gagal: perangkat mungkin kembali.
			select {
			case <-ctx.Done():
				return
			case <-q.notify:
			case <-time.After(time.Second):
			}
			continue
		}
		q.runJob(ctx, job)
	}
}

func (q *Queue) nextQueued() *Job {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, j := range q.jobs {
		if j.Status == StatusQueued {
			return j
		}
	}
	return nil
}

func (q *Queue) runJob(ctx context.Context, job *Job) {
	jobCtx, cancel := context.WithCancel(ctx)

	q.mu.Lock()
	if job.Status != StatusQueued {
		q.mu.Unlock()
		cancel()
		return
	}
	job.Status = StatusRunning
	job.StartedAt = time.Now()
	job.Message = "Mulai"
	q.cancels[job.ID] = cancel
	snap := *job
	q.mu.Unlock()
	q.broadcast(snap)

	err := q.execute(jobCtx, job)
	cancel()

	q.mu.Lock()
	delete(q.cancels, job.ID)
	job.EndedAt = time.Now()
	switch {
	case job.Status == StatusCancelled:
		// sudah ditandai oleh Cancel
	case err != nil && errors.Is(err, context.Canceled):
		job.Status = StatusCancelled
		job.Message = "dibatalkan"
	case err != nil:
		job.Status = StatusFailed
		job.Error = err.Error()
		job.Message = "Gagal"
	default:
		job.Status = StatusSuccess
		job.Progress = 100
		job.Message = "Selesai"
	}
	final := *job
	q.mu.Unlock()

	q.broadcast(final)
	if q.onDone != nil {
		q.onDone(final, err)
	}
}

func (q *Queue) execute(ctx context.Context, job *Job) error {
	switch job.Kind {
	case KindInstall:
		opts := adbx.InstallOptions{
			Replace:        job.Replace,
			AllowDowngrade: job.AllowDowngrade,
		}
		return q.runner.Install(ctx, job.Target, opts, func(percent int, msg string) {
			q.updateProgress(job.ID, percent, msg)
		})
	case KindUninstall:
		return q.runner.Uninstall(ctx, job.Target, false)
	case KindUninstallKeep:
		return q.runner.Uninstall(ctx, job.Target, true)
	case KindClearData:
		return q.runner.ClearData(ctx, job.Target)
	case KindPull:
		dest, err := q.runner.PullApk(ctx, job.Target, job.DestDir)
		if err == nil {
			q.setResult(job.ID, dest)
		}
		return err
	default:
		return fmt.Errorf("jenis job tidak dikenal: %s", job.Kind)
	}
}

func (q *Queue) updateProgress(id string, percent int, msg string) {
	q.mu.Lock()
	j, ok := q.byID[id]
	if !ok {
		q.mu.Unlock()
		return
	}
	j.Progress = percent
	j.Message = msg
	snap := *j
	q.mu.Unlock()
	q.broadcast(snap)
}

func (q *Queue) setResult(id, result string) {
	q.mu.Lock()
	j, ok := q.byID[id]
	if !ok {
		q.mu.Unlock()
		return
	}
	j.Result = result
	q.mu.Unlock()
}
