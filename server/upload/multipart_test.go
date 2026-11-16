package upload

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestUploadRequestLimitsAndCleanup(t *testing.T) {
	t.Setenv("TMP", t.TempDir())
	t.Setenv("TEMP", os.Getenv("TMP"))
	t.Setenv("TMPDIR", os.Getenv("TMP"))
	for _, tc := range []struct {
		name              string
		unknown, truncate bool
		requestLimit      int64
		files             int
		field             string
		want              int
	}{
		{"valid", false, false, 4 << 20, 2, "", 200},
		{"known length", false, false, 1 << 20, 2, "", 413},
		{"chunked total", true, false, 1 << 20, 2, "", 413},
		{"unused files count", false, false, 4 << 20, 1, "", 400},
		{"large field", false, false, 4 << 20, 2, strings.Repeat("x", 65537), 413},
		{"disconnected", true, true, 4 << 20, 2, "", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b bytes.Buffer
			mw := multipart.NewWriter(&b)
			for _, key := range []string{"file", "unused"} {
				p, _ := mw.CreateFormFile(key, "x.pdf")
				io.Copy(p, io.LimitReader(zeroReader{}, (1<<20)+10))
			}
			if tc.field != "" {
				mw.WriteField("label", tc.field)
			}
			mw.Close()
			data := b.Bytes()
			if tc.truncate {
				data = data[:len(data)-100]
			}
			r := gin.New()
			r.POST("/", func(c *gin.Context) {
				_, cleanup, err := ReadForm(c, tc.requestLimit, tc.files, 3<<20)
				defer cleanup()
				if err == nil {
					c.Status(200)
				}
			})
			req := httptest.NewRequest("POST", "/", bytes.NewReader(data))
			req.Header.Set("Content-Type", mw.FormDataContentType())
			if tc.unknown {
				req.ContentLength = -1
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("got %d want %d %s", w.Code, tc.want, w.Body.String())
			}
			entries, _ := os.ReadDir(os.Getenv("TMP"))
			if len(entries) != 0 {
				t.Fatal("multipart temporary files leaked")
			}
		})
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestUploadBusyDoesNotBlockHealth(t *testing.T) {
	r := gin.New()
	entered, release := make(chan struct{}), make(chan struct{})
	r.POST("/", func(c *gin.Context) {
		_, cleanup, err := ReadForm(c, 1024, 1, 1024)
		defer cleanup()
		if err == nil {
			close(entered)
			<-release
			c.Status(200)
		}
	})
	r.GET("/health", func(c *gin.Context) { c.Status(200) })
	form := func() *httptest.ResponseRecorder {
		var b bytes.Buffer
		mw := multipart.NewWriter(&b)
		p, _ := mw.CreateFormFile("file", "x")
		p.Write([]byte("x"))
		mw.Close()
		req := httptest.NewRequest("POST", "/", &b)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	done := make(chan struct{})
	go func() { defer close(done); form() }()
	<-entered
	if w := form(); w.Code != 503 {
		t.Fatal("concurrent upload not rejected")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/health", nil))
	if w.Code != 200 {
		t.Fatal("upload blocked health")
	}
	close(release)
	<-done
}
