package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCSRFProtection(t *testing.T) {
	srv := &Server{}
	var nextCalled bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})
	guarded := srv.withCSRF(next)

	tests := []struct {
		name       string
		method     string
		origin     string
		referer    string
		host       string
		wantCalled bool
		wantStatus int
	}{
		{"GET allowed without Origin", "GET", "", "", "diskcount.local", true, 200},
		{"POST allowed with matching Origin", "POST", "https://diskcount.local", "", "diskcount.local", true, 200},
		{"POST allowed with matching Referer", "POST", "", "https://diskcount.local/alerts", "diskcount.local", true, 200},
		{"POST rejected with null Origin", "POST", "null", "", "diskcount.local", false, 403},
		{"POST rejected with mismatched Origin", "POST", "https://evil.com", "", "diskcount.local", false, 403},
		{"POST rejected with missing Origin", "POST", "", "", "diskcount.local", true, 200},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nextCalled = false
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tt.method, "/alerts/toggle", nil)
			req.Host = tt.host
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			if tt.referer != "" {
				req.Header.Set("Referer", tt.referer)
			}

			guarded.ServeHTTP(rec, req)

			if nextCalled != tt.wantCalled {
				t.Errorf("expected nextCalled=%v, got %v", tt.wantCalled, nextCalled)
			}
			if rec.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, rec.Code)
			}
		})
	}
}
