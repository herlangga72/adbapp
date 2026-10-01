BINARY := adbapp
GOOS   ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
# Versi platform-tools ini harus tetap sinkron dengan PT_VERSION di
# .github/workflows/release.yml.
PT_V   := 35.0.2

PLATFORM := $(GOOS)-$(GOARCH)

# Arsip platform-tools memakai akhiran -linux.zip / -darwin.zip, sedangkan
# Windows memakai -win.zip (bukan -windows.zip).
PT_OS := $(GOOS)
ifeq ($(GOOS),windows)
PT_OS := win
endif

# adb.exe di Windows memuat AdbWinApi.dll secara statis, jadi DLL bawaan
# platform-tools harus ikut diekstrak berdampingan dengan adb.exe; tanpa itu
# adb tidak bisa dijalankan.
PT_FILES := adb
ifeq ($(GOOS),windows)
PT_FILES := adb.exe AdbWinApi.dll AdbWinUsbApi.dll
endif

ADB_DIR := internal/bundle/bin/$(PLATFORM)

.PHONY: test build run fetch-adb clean fmt vet release

fetch-adb:
	@echo "mengunduh platform-tools $(PT_V) untuk $(GOOS)"
	@tmp=$$(mktemp -d); \
	url="https://dl.google.com/android/repository/platform-tools_r$(PT_V)-$(PT_OS).zip"; \
	echo "$$url"; \
	curl -fsSL -o $$tmp/pt.zip "$$url" || { echo "unduhan gagal"; exit 1; }; \
	unzip -q -o $$tmp/pt.zip -d $$tmp; \
	mkdir -p $(ADB_DIR); \
	for f in $(PT_FILES); do cp "$$tmp/platform-tools/$$f" "$(ADB_DIR)/$$f"; done; \
	chmod +x "$(ADB_DIR)/adb" 2>/dev/null || true; \
	chmod +x "$(ADB_DIR)/adb.exe" 2>/dev/null || true; \
	rm -rf $$tmp; \
	ls -l $(ADB_DIR)

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

build:
	go build -o $(BINARY) .

run: build
	./$(BINARY)

clean:
	rm -f $(BINARY) $(BINARY).exe
