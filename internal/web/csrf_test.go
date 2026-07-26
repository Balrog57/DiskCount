package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCSRF(t *testing.T) {
	handler := withCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		name    string
		method  string
		origin  string
		referer string
		host    string
		status  int
	}{
		{"GET allowed", "GET", "http://evil.com", "", "localhost:8080", 200},
		{"POST no origin/referer", "POST", "", "", "localhost:8080", 200},
		{"POST null origin", "POST", "null", "", "localhost:8080", 403},
		{"POST good origin", "POST", "http://localhost:8080", "", "localhost:8080", 200},
		{"POST bad origin", "POST", "http://evil.com", "", "localhost:8080", 403},
		{"POST good referer", "POST", "", "http://localhost:8080/admin", "localhost:8080", 200},
		{"POST bad referer", "POST", "", "http://evil.com/admin", "localhost:8080", 403},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "http://"+tt.host+"/", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			if tt.referer != "" {
				req.Header.Set("Referer", tt.referer)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tt.status {
				t.Errorf("expected %d, got %d", tt.status, rec.Code)
			}
		})
	}
}
