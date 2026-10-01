package httpapi

import (
	"bufio"
	"fmt"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCrossSiteRawRequestRepro menjalankan ulang reproduksi peninjau: permintaan
// lintas situs mentah dari peramban (Host loopback, Origin jahat, Content-Type
// text/plain) tidak boleh lagi memasukkan job.
func TestCrossSiteRawRequestRepro(t *testing.T) {
	s, fq := newTestServer(t)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	addr := strings.TrimPrefix(srv.URL, "http://")

	body := `{"kind":"uninstall","targets":["com.victim"]}`
	raw := "POST /api/jobs HTTP/1.1\r\n" +
		"Host: 127.0.0.1:8765\r\n" +
		"Origin: http://evil.example.com\r\n" +
		"Content-Type: text/plain\r\n" +
		fmt.Sprintf("Content-Length: %d\r\n", len(body)) +
		"Connection: close\r\n\r\n" + body

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := fmt.Fprint(conn, raw); err != nil {
		t.Fatal(err)
	}
	status, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("REPRO status line: %s", strings.TrimSpace(status))
	t.Logf("REPRO jobs enqueued: %d", len(fq.enqueued))
	if len(fq.enqueued) != 0 {
		t.Fatalf("job lintas situs tetap masuk: %+v", fq.enqueued)
	}
}
