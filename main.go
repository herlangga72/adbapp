// Command adbapp menyajikan UI lokal untuk memasang dan mencopot aplikasi
// Android lewat adb.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/herlangga72/adbapp/internal/adbx"
	"github.com/herlangga72/adbapp/internal/browser"
	"github.com/herlangga72/adbapp/internal/bundle"
	"github.com/herlangga72/adbapp/internal/device"
	"github.com/herlangga72/adbapp/internal/httpapi"
	"github.com/herlangga72/adbapp/internal/paths"
	"github.com/herlangga72/adbapp/internal/queue"
	"github.com/herlangga72/adbapp/internal/store"
	"github.com/herlangga72/adbapp/internal/webui"
)

func main() {
	port := flag.Int("port", 0, "port server lokal (0 = pilih otomatis)")
	noOpen := flag.Bool("no-open", false, "jangan buka browser otomatis")
	dataDir := flag.String("data", "", "folder data khusus (untuk pengujian)")
	flag.Parse()

	if err := run(*port, *noOpen, *dataDir); err != nil {
		log.Fatalf("adbapp berhenti: %v", err)
	}
}

func run(port int, noOpen bool, dataDir string) error {
	p, err := resolvePaths(dataDir)
	if err != nil {
		return err
	}
	if err := p.Ensure(); err != nil {
		return fmt.Errorf("tidak bisa menyiapkan folder data: %w", err)
	}

	adbPath, err := bundle.Ensure(p.AdbDir)
	if err != nil {
		return err
	}
	log.Printf("adb siap: %s", adbPath)

	base := adbx.New(adbPath)
	monitor := device.New(base, device.WithVersionGetter(func(ctx context.Context, serial string) (string, error) {
		return base.WithSerial(serial).Output(ctx, "shell", "getprop", "ro.build.version.release")
	}))
	history := store.New(p.HistoryFile)

	targeted := &targetedRunner{base: base, monitor: monitor}
	q := queue.New(targeted,
		queue.WithDeviceCheck(func() bool { return monitor.Current().State == device.StateReady }),
		queue.WithOnDone(func(job queue.Job, jobErr error) {
			entry := store.Entry{
				Action:  string(job.Kind),
				Package: job.Target,
				Device:  monitor.Current().Serial,
				Success: jobErr == nil && job.Status == queue.StatusSuccess,
			}
			if jobErr != nil {
				entry.Detail = jobErr.Error()
			} else if job.Result != "" {
				entry.Detail = job.Result
			}
			if err := history.Append(entry); err != nil {
				log.Printf("gagal menulis riwayat: %v", err)
			}
		}))

	cfgPath := p.ConfigFile
	api := &httpapi.Server{
		Device:  monitor,
		Queue:   q,
		History: history,
		Adb:     targeted,
		Paths:   p,
		Static:  webui.FS(),
		LoadCfg: func() (store.Config, error) { return store.LoadConfig(cfgPath) },
		SaveCfg: func(c store.Config) error { return store.SaveConfig(cfgPath, c) },
	}

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("tidak bisa membuka port lokal: %w", err)
	}
	url := "http://" + listener.Addr().String()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := monitor.Refresh(ctx); err != nil {
		log.Printf("pemindaian perangkat awal gagal: %v", err)
	}
	go monitor.Loop(ctx, 2*time.Second)
	go q.Run(ctx)

	log.Printf("adbapp jalan di %s", url)
	log.Printf("folder data: %s", p.DataDir)
	if !noOpen {
		if err := browser.Open(url); err != nil {
			log.Printf("tidak bisa membuka browser otomatis: %v", err)
		}
	}

	server := &http.Server{Handler: api.Handler()}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func resolvePaths(dataDir string) (paths.Paths, error) {
	if dataDir != "" {
		return paths.ResolveFrom(dataDir), nil
	}
	return paths.Resolve()
}

// targetedRunner meneruskan setiap perintah adb ke perangkat yang sedang aktif.
type targetedRunner struct {
	base    *adbx.Runner
	monitor *device.Monitor
}

func (t *targetedRunner) runner() *adbx.Runner {
	return t.base.WithSerial(t.monitor.Current().Serial)
}

func (t *targetedRunner) Install(ctx context.Context, apkPath string, opts adbx.InstallOptions, stage adbx.StageFunc) error {
	return t.runner().Install(ctx, apkPath, opts, stage)
}

func (t *targetedRunner) Uninstall(ctx context.Context, pkg string, keepData bool) error {
	return t.runner().Uninstall(ctx, pkg, keepData)
}

func (t *targetedRunner) ClearData(ctx context.Context, pkg string) error {
	return t.runner().ClearData(ctx, pkg)
}

func (t *targetedRunner) PullApk(ctx context.Context, pkg string, destDir string) (string, error) {
	return t.runner().PullApk(ctx, pkg, destDir)
}

func (t *targetedRunner) Packages(ctx context.Context, system bool) ([]adbx.Package, error) {
	return t.runner().Packages(ctx, system)
}

func (t *targetedRunner) PackageInfo(ctx context.Context, pkg string) (adbx.PackageInfo, error) {
	return t.runner().PackageInfo(ctx, pkg)
}
