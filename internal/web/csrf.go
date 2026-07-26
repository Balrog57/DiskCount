package web

import (
	"net/http"
	"net/url"
)

// withCSRF applies Origin/Referer checking for state-modifying requests (POST, PUT, DELETE, PATCH).
// It allows requests with completely missing headers for API clients, but strictly rejects
// invalid domains or "null" origins.
func withCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions || r.Method == http.MethodTrace {
			next.ServeHTTP(w, r)
			return
		}

		origin := r.Header.Get("Origin")
		referer := r.Header.Get("Referer")

		// If neither header is present, we assume it's a programmatic client and let it pass.
		// (Following standard CSRF guidance when serving both browsers and APIs).
		if origin == "" && referer == "" {
			next.ServeHTTP(w, r)
			return
		}

		expectedHost := r.Host

		if origin != "" {
			if origin == "null" {
				http.Error(w, "Forbidden - CSRF check failed (null Origin)", http.StatusForbidden)
				return
			}
			u, err := url.Parse(origin)
			if err != nil || u.Host != expectedHost {
				http.Error(w, "Forbidden - CSRF check failed (invalid Origin)", http.StatusForbidden)
				return
			}
		} else if referer != "" {
			u, err := url.Parse(referer)
			if err != nil || u.Host != expectedHost {
				http.Error(w, "Forbidden - CSRF check failed (invalid Referer)", http.StatusForbidden)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}
