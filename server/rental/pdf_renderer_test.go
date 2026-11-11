package rental

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestPDFQueuedRequestHonorsCallerCancellation(t *testing.T) {
	// A busy renderer must release a canceled HTTP request without launching a
	// browser or consuming the slot needed by the job already running.
	pdfRenderSlot <- struct{}{}
	defer func() { <-pdfRenderSlot }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	data, err := chromiumPDF(ctx, "<p>queued</p>")
	if err == nil || !strings.Contains(err.Error(), "等待超时") || len(data) != 0 {
		t.Fatalf("canceled queued request: data=%d err=%v", len(data), err)
	}
	if time.Since(started) > time.Second || len(pdfRenderSlot) != 1 {
		t.Fatal("canceled request waited or displaced the running render")
	}
}
