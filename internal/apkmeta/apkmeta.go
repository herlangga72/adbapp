// Package apkmeta membaca identitas paket dari berkas APK tanpa memasangnya.
package apkmeta

import (
	"encoding/xml"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/avast/apkparser"
)

var ErrNoManifest = errors.New("AndroidManifest.xml tidak ditemukan di dalam APK")

// Meta adalah identitas singkat sebuah APK.
type Meta struct {
	Package     string `json:"package"`
	VersionName string `json:"versionName"`
	Label       string `json:"label"`
	VersionCode int64  `json:"versionCode"`
	MinSDK      int    `json:"minSdk"`
}

// Read membuka APK dan mengembalikan identitasnya.
func Read(path string) (Meta, error) {
	var m Meta
	enc := &metaEncoder{meta: &m}
	zipErr, _, manifestErr := apkparser.ParseApk(path, enc)
	if manifestErr != nil {
		return Meta{}, fmt.Errorf("gagal membaca AndroidManifest.xml: %w", manifestErr)
	}
	if zipErr != nil {
		return Meta{}, fmt.Errorf("berkas bukan APK yang sah: %w", zipErr)
	}
	if m.Package == "" {
		return Meta{}, ErrNoManifest
	}
	return m, nil
}

// metaEncoder menangkap elemen yang kita butuhkan dari aliran token XML.
type metaEncoder struct {
	meta *Meta
}

func (e *metaEncoder) EncodeToken(t xml.Token) error {
	start, ok := t.(xml.StartElement)
	if !ok {
		return nil
	}
	switch start.Name.Local {
	case "manifest":
		for _, a := range start.Attr {
			switch a.Name.Local {
			case "package":
				e.meta.Package = a.Value
			case "versionName":
				e.meta.VersionName = a.Value
			case "versionCode":
				code, err := strconv.ParseInt(strings.TrimSpace(a.Value), 10, 64)
				if err == nil {
					e.meta.VersionCode = code
				}
			}
		}
	case "uses-sdk":
		for _, a := range start.Attr {
			if a.Name.Local == "minSdkVersion" {
				sdk, err := strconv.Atoi(strings.TrimSpace(a.Value))
				if err == nil {
					e.meta.MinSDK = sdk
				}
			}
		}
	case "application":
		for _, a := range start.Attr {
			if a.Name.Local == "label" {
				e.meta.Label = a.Value
			}
		}
	}
	return nil
}

func (e *metaEncoder) Flush() error { return nil }

// cache menghindari pembacaan APK berulang kali untuk berkas yang sama.
var (
	cacheMu sync.Mutex
	cache   = map[string]cachedMeta{}
)

// cacheMaxEntries membatasi jumlah entri cache agar tidak tumbuh tanpa batas
// saat daftar APK terus berubah.
const cacheMaxEntries = 512

type cachedMeta struct {
	size int64
	mod  int64
	meta Meta
}

// ReadCached seperti Read, tetapi menyimpan hasil untuk berkas yang tidak
// berubah. Dipakai oleh lapisan HTTP yang sering meminta daftar APK.
func ReadCached(path string, size, modUnixNano int64) (Meta, error) {
	cacheMu.Lock()
	if c, ok := cache[path]; ok && c.size == size && c.mod == modUnixNano {
		cacheMu.Unlock()
		return c.meta, nil
	}
	cacheMu.Unlock()

	m, err := Read(path)
	if err != nil {
		return Meta{}, err
	}

	cacheMu.Lock()
	if len(cache) >= cacheMaxEntries {
		cache = map[string]cachedMeta{}
	}
	cache[path] = cachedMeta{size: size, mod: modUnixNano, meta: m}
	cacheMu.Unlock()
	return m, nil
}
