package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/herlangga72/adbapp/internal/adbx"
	"github.com/herlangga72/adbapp/internal/device"
	"github.com/herlangga72/adbapp/internal/paths"
	"github.com/herlangga72/adbapp/internal/queue"
	"github.com/herlangga72/adbapp/internal/store"
)

type fakeDevice struct{ status device.Status }

func (f fakeDevice) Current() device.Status            { return f.status }
func (f fakeDevice) Refresh(ctx context.Context) error { return nil }

type fakeQueue struct{ enqueued []queue.Job }

func (f *fakeQueue) Enqueue(j queue.Job) queue.Job {
	f.enqueued = append(f.enqueued, j)
	j.ID = "job-1"
	j.Status = queue.StatusQueued
	return j
}
func (f *fakeQueue) Jobs() []queue.Job      { return nil }
func (f *fakeQueue) Cancel(id string) error { return nil }
func (f *fakeQueue) Subscribe() (<-chan queue.Job, func()) {
	ch := make(chan queue.Job)
	return ch, func() { close(ch) }
}

type fakeHistory struct{ entries []store.Entry }

func (f *fakeHistory) Read(limit int) ([]store.Entry, error) { return f.entries, nil }
func (f *fakeHistory) ExportCSV(w io.Writer) error {
	_, err := io.WriteString(w, "time,action,package,device,success,detail\n")
	return err
}
func (f *fakeHistory) ExportJSON(w io.Writer) error {
	return json.NewEncoder(w).Encode(f.entries)
}

type fakeAdb struct{}

func (fakeAdb) Packages(ctx context.Context, system bool) ([]adbx.Package, error) {
	return []adbx.Package{{Name: "com.foo", ApkPath: "/data/app/com.foo/base.apk"}}, nil
}
func (fakeAdb) PackageInfo(ctx context.Context, pkg string) (adbx.PackageInfo, error) {
	return adbx.PackageInfo{Package: pkg, VersionName: "1.0"}, nil
}

func newTestServer(t *testing.T) (*Server, *fakeQueue) {
	t.Helper()
	fq := &fakeQueue{}
	s := &Server{
		Device:  fakeDevice{status: device.Status{State: device.StateReady, Serial: "S1", Model: "Pixel"}},
		Queue:   fq,
		History: &fakeHistory{entries: []store.Entry{{Action: "install", Package: "com.foo", Success: true}}},
		Adb:     fakeAdb{},
		Paths:   paths.ResolveFrom(t.TempDir()),
		Static:  fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html>adbapp</html>")}},
	}
	return s, fq
}

// localRequest membuat permintaan dengan Host lokal, karena httptest.NewRequest
// memakai "example.com" yang memang ditolak oleh middleware localOnly.
func localRequest(method, target string, body io.Reader) *http.Request {
	req := httptest.NewRequest(method, target, body)
	req.Host = "127.0.0.1"
	return req
}

func TestStateEndpoint(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, localRequest(http.MethodGet, "/api/state", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("kode %d", rec.Code)
	}
	var body struct {
		Device device.Status `json:"device"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("JSON tidak sah: %v", err)
	}
	if body.Device.Serial != "S1" {
		t.Fatalf("serial salah: %+v", body.Device)
	}
}

func TestCreateJobsEnqueuesOnePerTarget(t *testing.T) {
	s, fq := newTestServer(t)
	payload := `{"kind":"uninstall","targets":["com.a","com.b"]}`
	rec := httptest.NewRecorder()
	req := localRequest(http.MethodPost, "/api/jobs", strings.NewReader(payload))
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("kode %d body %s", rec.Code, rec.Body.String())
	}
	if len(fq.enqueued) != 2 {
		t.Fatalf("harus 2 job, dapat %d", len(fq.enqueued))
	}
	if fq.enqueued[0].Kind != queue.KindUninstall || fq.enqueued[1].Target != "com.b" {
		t.Fatalf("job salah: %+v", fq.enqueued)
	}
}

func TestCreateJobsRejectsUnknownKind(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	req := localRequest(http.MethodPost, "/api/jobs", strings.NewReader(`{"kind":"ngawur","targets":["x"]}`))
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("harus 400, dapat %d", rec.Code)
	}
}

func TestExportCSVSetsHeaders(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, localRequest(http.MethodGet, "/api/history/export?format=csv", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("kode %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "text/csv") {
		t.Fatalf("content type salah: %q", rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("harus sebagai unduhan: %q", rec.Header().Get("Content-Disposition"))
	}
}

func TestLocalOnlyRejectsForeignHost(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	req.Host = "evil.example.com"
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("harus 403, dapat %d", rec.Code)
	}
}

func TestUploadSavesFile(t *testing.T) {
	s, _ := newTestServer(t)
	if err := s.Paths.Ensure(); err != nil {
		t.Fatal(err)
	}

	var body bytes.Buffer
	writeMultipart(t, &body, "file", "contoh.apk", []byte("bukan-apk-sungguhan"))
	rec := httptest.NewRecorder()
	req := localRequest(http.MethodPost, "/api/apks/upload", &body)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=batas")
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("kode %d body %s", rec.Code, rec.Body.String())
	}
	var got apkEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("JSON tidak sah: %v", err)
	}
	want := filepath.Join(s.Paths.UploadsDir, "contoh.apk")
	if got.Path != want {
		t.Fatalf("path salah: got %q want %q", got.Path, want)
	}
}

func writeMultipart(t *testing.T, buf *bytes.Buffer, field, filename string, content []byte) {
	t.Helper()
	buf.WriteString("--batas\r\n")
	buf.WriteString("Content-Disposition: form-data; name=\"" + field + "\"; filename=\"" + filename + "\"\r\n")
	buf.WriteString("Content-Type: application/octet-stream\r\n\r\n")
	buf.Write(content)
	buf.WriteString("\r\n--batas--\r\n")
}

func TestIndexServedFromEmbeddedFS(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, localRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("kode %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "adbapp") {
		t.Fatalf("halaman tidak disajikan: %s", rec.Body.String())
	}
}
