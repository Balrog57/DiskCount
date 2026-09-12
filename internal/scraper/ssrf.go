package scraper

import (
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

// validateFetchURL rejects redirect targets that would reach internal networks.
// Initial scrape URLs come from trusted operator config; the SSRF risk is
// malicious redirect chains returned by external retailers.
func validateFetchURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return fmt.Errorf("unsupported URL scheme")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("missing URL host")
	}
	if strings.EqualFold(host, "localhost") {
		return fmt.Errorf("internal host blocked")
	}
	if ip, err := netip.ParseAddr(host); err == nil && isInternalIP(ip) {
		return fmt.Errorf("internal IP blocked")
	}
	return nil
}

func isInternalIP(ip netip.Addr) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

func redirectPolicy(maxRedirects int) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("too many redirects")
		}
		if err := validateFetchURL(req.URL.String()); err != nil {
			return fmt.Errorf("redirect blocked: %w", err)
		}
		return nil
	}
}
