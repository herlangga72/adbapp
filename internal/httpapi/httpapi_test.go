package httpapi

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/herlangga72/adbapp/internal/adbx"
	"github.com/herlangga72/adbapp/internal/device"
	"github.com/herlangga72/adbapp/internal/paths"
	"github.com/herlangga72/adbapp/internal/queue"
	"github.com/herlangga72/adbapp/internal/store"
)

type fakeDevice struct {
	status   device.Status
	selected string
}

func (f *fakeDevice) Current() device.Status            { return f.status }
func (f *fakeDevice) Refresh(ctx context.Context) error { return nil }

// Select meniru Monitor.Select: hanya serial yang dikenal yang diterima, lalu
// perangkat itu dijadikan yang aktif sehingga Current mencerminkannya.
func (f *fakeDevice) Select(serial string) error {
	all := append([]string{f.status.Serial}, f.status.Others...)
	found := false
	for _, s := range all {
		if s == serial {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("perangkat %s tidak dikenal", serial)
	}
	f.selected = serial
	f.status.Serial = serial
	others := []string{}
	for _, s := range all {
		if s != serial {
			others = append(others, s)
		}
	}
	f.status.Others = others
	return nil
}

type fakeQueue struct {
	enqueued     []queue.Job
	unsubOnce    sync.Once
	unsubscribed chan struct{}
}

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
	if f.unsubscribed == nil {
		f.unsubscribed = make(chan struct{})
	}
	return ch, func() {
		close(ch)
		f.unsubOnce.Do(func() { close(f.unsubscribed) })
	}
}

type fakeHistory struct {
	entries   []store.Entry
	lastLimit int
}

func (f *fakeHistory) Read(limit int) ([]store.Entry, error) {
	f.lastLimit = limit
	return f.entries, nil
}
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
		Device:  &fakeDevice{status: device.Status{State: device.StateReady, Serial: "S1", Model: "Pixel"}},
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

func TestHistoryEmptyReturnsArray(t *testing.T) {
	s, _ := newTestServer(t)
	s.History = store.New(filepath.Join(t.TempDir(), "history.jsonl"))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, localRequest(http.MethodGet, "/api/history", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("kode %d", rec.Code)
	}
	body := strings.TrimSpace(rec.Body.String())
	if body != "[]" {
		t.Fatalf("body = %q, mau []", body)
	}
}

func TestCreateJobsEnqueuesOnePerTarget(t *testing.T) {
	s, fq := newTestServer(t)
	payload := `{"kind":"uninstall","targets":["com.a","com.b"]}`
	rec := httptest.NewRecorder()
	req := localRequest(http.MethodPost, "/api/jobs", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
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
	if fq.enqueued[0].DestDir != s.Paths.PulledDir {
		t.Fatalf("DestDir salah: %q", fq.enqueued[0].DestDir)
	}
	if fq.enqueued[0].Label == "" {
		t.Fatalf("Label kosong: %+v", fq.enqueued[0])
	}
}

func TestCreateJobsCancelKnownID(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	req := localRequest(http.MethodPost, "/api/jobs/cancel", strings.NewReader(`{"id":"job-1"}`))
	req.Header.Set("Content-Type", "application/json")
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("harus 200, dapat %d body %s", rec.Code, rec.Body.String())
	}
}

func TestCreateJobsRejectsUnknownKind(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	req := localRequest(http.MethodPost, "/api/jobs", strings.NewReader(`{"kind":"ngawur","targets":["x"]}`))
	req.Header.Set("Content-Type", "application/json")
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

func TestUploadSavesValidAPK(t *testing.T) {
	s, _ := newTestServer(t)
	if err := s.Paths.Ensure(); err != nil {
		t.Fatal(err)
	}

	apk := buildAPK(t)
	content, err := os.ReadFile(apk)
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	writeMultipart(t, &body, "file", "contoh.apk", content)
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
	if got.Package == "" {
		t.Fatalf("APK sah seharusnya punya package: %+v", got)
	}
}

func TestUploadRejectsCorruptAPKAndRemovesFile(t *testing.T) {
	s, _ := newTestServer(t)
	if err := s.Paths.Ensure(); err != nil {
		t.Fatal(err)
	}

	var body bytes.Buffer
	writeMultipart(t, &body, "file", "rusak.apk", []byte("bukan-apk-sungguhan"))
	rec := httptest.NewRecorder()
	req := localRequest(http.MethodPost, "/api/apks/upload", &body)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=batas")
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("harus 400, dapat %d body %s", rec.Code, rec.Body.String())
	}
	entries, err := os.ReadDir(s.Paths.UploadsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("berkas rusak tidak dibersihkan: %v", entries)
	}
}

// moduleManifestBin adalah AndroidManifest.xml biner bawaan modul apkparser;
// APK uji dibangun saat berjalan, bukan disalin ke repo.
const moduleManifestBin = "98d2e837b8f3ac41e74b86b2d532972955e5352197a893206ecd9650f678ae31.bin"

// buildAPK membangun APK sementara dari testdata modul apkparser. Tes di-skip
// bila modul atau testdata-nya tidak tersedia.
func buildAPK(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/avast/apkparser").Output()
	if err != nil {
		t.Skipf("modul apkparser tidak tersedia: %v", err)
	}
	binPath := filepath.Join(strings.TrimSpace(string(out)), "testdata", moduleManifestBin)
	data, err := os.ReadFile(binPath)
	if err != nil {
		t.Skipf("testdata modul apkparser tidak tersedia: %v", err)
	}

	apkPath := filepath.Join(t.TempDir(), "valid.apk")
	f, err := os.Create(apkPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("AndroidManifest.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return apkPath
}

func TestListAPKsExposesMinSdk(t *testing.T) {
	s, _ := newTestServer(t)
	if err := s.Paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	folder := t.TempDir()
	src := buildAPK(t)
	content, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "satu.apk"), content, 0o644); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, localRequest(http.MethodGet, "/api/apks?folder="+folder, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("kode %d body %s", rec.Code, rec.Body.String())
	}
	var entries []apkEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("JSON tidak sah: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("harus 1 entri, dapat %d", len(entries))
	}
	if entries[0].MinSDK == 0 {
		t.Fatalf("minSdk tidak terekspos: %+v", entries[0])
	}
	if entries[0].Package == "" {
		t.Fatalf("package tidak terekspos: %+v", entries[0])
	}
}

func TestSelectDeviceHappyPath(t *testing.T) {
	s, _ := newTestServer(t)
	s.Device.(*fakeDevice).status.Others = []string{"S2"}

	rec := httptest.NewRecorder()
	req := localRequest(http.MethodPost, "/api/device/select", strings.NewReader(`{"serial":"S2"}`))
	req.Header.Set("Content-Type", "application/json")
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("kode %d body %s", rec.Code, rec.Body.String())
	}
	var got device.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("JSON tidak sah: %v", err)
	}
	if got.Serial != "S2" {
		t.Fatalf("serial hasil = %q, mau S2", got.Serial)
	}
}

func TestSelectDeviceUnknownSerial(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	req := localRequest(http.MethodPost, "/api/device/select", strings.NewReader(`{"serial":"tidak-ada"}`))
	req.Header.Set("Content-Type", "application/json")
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("harus 404, dapat %d body %s", rec.Code, rec.Body.String())
	}
}

func TestSelectDeviceEmptySerial(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	req := localRequest(http.MethodPost, "/api/device/select", strings.NewReader(`{"serial":""}`))
	req.Header.Set("Content-Type", "application/json")
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("harus 400, dapat %d body %s", rec.Code, rec.Body.String())
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

func TestEventsStreamsStateThenReturnsOnCancel(t *testing.T) {
	s, fq := newTestServer(t)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("kode %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content type salah: %q", ct)
	}

	reader := bufio.NewReader(resp.Body)
	var frame strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("baca frame gagal: %v", err)
		}
		if line == "\n" {
			break
		}
		frame.WriteString(line)
	}
	if !strings.Contains(frame.String(), "event: state") {
		t.Fatalf("frame pertama bukan state: %q", frame.String())
	}

	cancel()
	select {
	case <-fq.unsubscribed:
	case <-time.After(5 * time.Second):
		t.Fatal("handler SSE tidak berhenti setelah konteks dibatalkan (bocor)")
	}
}

func TestCrossSiteGuard(t *testing.T) {
	const body = `{"kind":"uninstall","targets":["com.a"]}`
	cases := []struct {
		name     string
		origin   string
		secFetch string
		ct       string
		want     int
	}{
		{"origin lintas situs", "http://evil.example.com", "", "application/json", http.StatusForbidden},
		{"sec-fetch cross-site", "", "cross-site", "application/json", http.StatusForbidden},
		{"content type text/plain", "", "", "text/plain", http.StatusUnsupportedMediaType},
		{"origin lokal dan json", "http://127.0.0.1:8765", "same-origin", "application/json", http.StatusOK},
		{"localhost dan json", "http://localhost", "", "application/json", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newTestServer(t)
			rec := httptest.NewRecorder()
			req := localRequest(http.MethodPost, "/api/jobs", strings.NewReader(body))
			req.Header.Set("Content-Type", tc.ct)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.secFetch != "" {
				req.Header.Set("Sec-Fetch-Site", tc.secFetch)
			}
			s.Handler().ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("kode %d, mau %d (body %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestFromURLRejectsGarbageAndLeavesNoFile(t *testing.T) {
	s, _ := newTestServer(t)
	if err := s.Paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "bukan apk")
	}))
	defer upstream.Close()

	rec := httptest.NewRecorder()
	body := fmt.Sprintf(`{"url":%q,"name":"gagal.apk"}`, upstream.URL)
	req := localRequest(http.MethodPost, "/api/apks/url", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("harus 400, dapat %d body %s", rec.Code, rec.Body.String())
	}
	entries, err := os.ReadDir(s.Paths.UploadsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("ada berkas tersisa: %v", entries)
	}
}

func TestFromURLHostileNameStaysInsideUploads(t *testing.T) {
	s, _ := newTestServer(t)
	if err := s.Paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "bukan apk")
	}))
	defer upstream.Close()

	rec := httptest.NewRecorder()
	body := fmt.Sprintf(`{"url":%q,"name":"../../etc/passwd"}`, upstream.URL)
	req := localRequest(http.MethodPost, "/api/apks/url", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	s.Handler().ServeHTTP(rec, req)

	// Bagian pembuktian containment: setiap berkas harus berada di UploadsDir.
	root := filepath.Dir(filepath.Dir(filepath.Dir(s.Paths.UploadsDir)))
	if _, err := os.Stat(filepath.Join(root, "etc", "passwd")); err == nil {
		t.Fatalf("berkas menulis keluar dari UploadsDir")
	}
	entries, err := os.ReadDir(s.Paths.UploadsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("ada berkas tersisa: %v", entries)
	}
}

func TestFromURLDoesNotDestroyExistingAPK(t *testing.T) {
	s, _ := newTestServer(t)
	if err := s.Paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(s.Paths.UploadsDir, "sama.apk")
	original := []byte("APK-LAMA-YANG-BERHARGA")
	if err := os.WriteFile(dest, original, 0o644); err != nil {
		t.Fatal(err)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "bukan apk")
	}))
	defer upstream.Close()

	rec := httptest.NewRecorder()
	body := fmt.Sprintf(`{"url":%q,"name":"sama.apk"}`, upstream.URL)
	req := localRequest(http.MethodPost, "/api/apks/url", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("harus 400, dapat %d body %s", rec.Code, rec.Body.String())
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("berkas lama hilang: %v", err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("berkas lama berubah: %q", got)
	}
	if _, err := os.Stat(dest + ".part"); !os.IsNotExist(err) {
		t.Fatalf("sisa berkas .part tidak dibersihkan: %v", err)
	}
}

func TestUploadRejectsNonAPK(t *testing.T) {
	s, _ := newTestServer(t)
	if err := s.Paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	writeMultipart(t, &body, "file", "catatan.txt", []byte("bukan apk"))
	rec := httptest.NewRecorder()
	req := localRequest(http.MethodPost, "/api/apks/upload", &body)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=batas")
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("harus 400, dapat %d body %s", rec.Code, rec.Body.String())
	}
}

func TestHistoryHonoursLimit(t *testing.T) {
	s, _ := newTestServer(t)
	fh := s.History.(*fakeHistory)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, localRequest(http.MethodGet, "/api/history?limit=2", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("kode %d", rec.Code)
	}
	if fh.lastLimit != 2 {
		t.Fatalf("limit tidak diteruskan: %d", fh.lastLimit)
	}
}

func TestConfigRoundTripsThroughSaveHook(t *testing.T) {
	s, _ := newTestServer(t)
	var saved store.Config
	s.SaveCfg = func(c store.Config) error {
		saved = c
		return nil
	}
	rec := httptest.NewRecorder()
	req := localRequest(http.MethodPost, "/api/config", strings.NewReader(`{"apkFolder":"/koleksi"}`))
	req.Header.Set("Content-Type", "application/json")
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("kode %d body %s", rec.Code, rec.Body.String())
	}
	if saved.ApkFolder != "/koleksi" {
		t.Fatalf("config tidak tersimpan: %+v", saved)
	}
}

func TestMissingContentTypeRejectedOnJSONEndpoint(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	req := localRequest(http.MethodPost, "/api/jobs", strings.NewReader(`{"kind":"uninstall","targets":["com.a"]}`))
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("harus 415, dapat %d body %s", rec.Code, rec.Body.String())
	}
}
