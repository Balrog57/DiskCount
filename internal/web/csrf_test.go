package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCSRFMiddleware(t *testing.T) {
	s := &Server{}
	handler := s.withCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		name       string
		method     string
		headers    map[string]string
		host       string
		wantStatus int
	}{
		{
			name:       "GET requests are allowed",
			method:     http.MethodGet,
			headers:    nil,
			host:       "example.com",
			wantStatus: http.StatusOK,
		},
		{
			name:       "POST with missing Origin/Referer is allowed (API compat)",
			method:     http.MethodPost,
			headers:    nil,
			host:       "example.com",
			wantStatus: http.StatusOK,
		},
		{
			name:       "POST with valid Origin is allowed",
			method:     http.MethodPost,
			headers:    map[string]string{"Origin": "https://example.com"},
			host:       "example.com",
			wantStatus: http.StatusOK,
		},
		{
			name:       "POST with valid Referer is allowed",
			method:     http.MethodPost,
			headers:    map[string]string{"Referer": "https://example.com/path"},
			host:       "example.com",
			wantStatus: http.StatusOK,
		},
		{
			name:       "POST with invalid Origin is blocked",
			method:     http.MethodPost,
			headers:    map[string]string{"Origin": "https://evil.com"},
			host:       "example.com",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "POST with invalid Referer is blocked",
			method:     http.MethodPost,
			headers:    map[string]string{"Referer": "https://evil.com/path"},
			host:       "example.com",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "POST with null Origin is blocked",
			method:     http.MethodPost,
			headers:    map[string]string{"Origin": "null"},
			host:       "example.com",
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/", nil)
			req.Host = tt.host
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}
