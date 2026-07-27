package web

import (
	"net/http"
	"net/http/httptest"

	"testing"
)

func TestCSRFMiddleware(t *testing.T) {
	// A dummy handler to wrap.
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	srv := &Server{}
	handler := srv.withCSRF(nextHandler)

	ts := httptest.NewServer(handler)
	defer ts.Close()

	client := &http.Client{}

	tests := []struct {
		name       string
		method     string
		origin     string
		referer    string
		wantStatus int
	}{
		{"GET Request (No Headers Needed)", "GET", "", "", http.StatusOK},
		{"POST No Headers", "POST", "", "", http.StatusForbidden},
		{"POST Origin Correct", "POST", ts.URL, "", http.StatusOK},
		{"POST Origin Wrong", "POST", "http://evil.com", "", http.StatusForbidden},
		{"POST Referer Correct", "POST", "", ts.URL + "/config", http.StatusOK},
		{"POST Referer Wrong", "POST", "", "http://evil.com/config", http.StatusForbidden},
		{"PUT Origin Correct", "PUT", ts.URL, "", http.StatusOK},
		{"DELETE Referer Correct", "DELETE", "", ts.URL + "/alerts", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(tt.method, ts.URL, nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			if tt.referer != "" {
				req.Header.Set("Referer", tt.referer)
			}

			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("got status %d, want %d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}
