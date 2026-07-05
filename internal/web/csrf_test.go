package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCSRFMiddleware(t *testing.T) {
	srv := &Server{}

	// Create a dummy handler
	handler := srv.withCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		name           string
		method         string
		origin         string
		referer        string
		host           string
		expectedStatus int
	}{
		{
			name:           "GET request without origin",
			method:         http.MethodGet,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "POST request with null origin",
			method:         http.MethodPost,
			origin:         "null",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "POST request with invalid origin",
			method:         http.MethodPost,
			origin:         "http://evil.com",
			host:           "good.com",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "POST request with invalid origin suffix bypass",
			method:         http.MethodPost,
			origin:         "http://evilgood.com",
			host:           "good.com",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "POST request with invalid origin prefix bypass",
			method:         http.MethodPost,
			origin:         "http://good.com.evil.com",
			host:           "good.com",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "POST request with valid origin",
			method:         http.MethodPost,
			origin:         "http://good.com",
			host:           "good.com",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "POST request with valid origin and port",
			method:         http.MethodPost,
			origin:         "http://good.com:8080",
			host:           "good.com:8080",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "POST request with valid origin https",
			method:         http.MethodPost,
			origin:         "https://good.com",
			host:           "good.com",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "POST request with valid referer",
			method:         http.MethodPost,
			referer:        "http://good.com/path",
			host:           "good.com",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "POST request with invalid referer",
			method:         http.MethodPost,
			referer:        "http://evil.com/path",
			host:           "good.com",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "POST request with invalid referer contains bypass",
			method:         http.MethodPost,
			referer:        "http://evil.com/?target=good.com",
			host:           "good.com",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "POST request with no origin/referer (API client)",
			method:         http.MethodPost,
			host:           "good.com",
			expectedStatus: http.StatusOK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/", nil)
			req.Host = tc.host
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.referer != "" {
				req.Header.Set("Referer", tc.referer)
			}

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.expectedStatus {
				t.Errorf("expected status %d, got %d", tc.expectedStatus, rec.Code)
			}
		})
	}
}
