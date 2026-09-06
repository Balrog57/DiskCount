package sources

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/Balrog57/DiskCount/internal/domain"
	"github.com/Balrog57/DiskCount/internal/scraper"
)

// multiURLFetchResult captures the result of fetching all URLs for a source.
// It is returned by fetchMultiURL so callers can decide whether an all-URL
// failure should bubble up as a typed error (so the circuit breaker trips)
// or be swallowed (legacy behaviour).
type multiURLFetchResult struct {
	deals   []domain.Deal
	errors  []error
	failed  int
	total   int
	blocked bool
}

// fetchMultiURL fetches every URL for a source, applies the Byparr headless
// fallback when configured, parses each successful response with parse, and
// aggregates the deals. It logs per-URL failures and returns a structured
// result the caller can convert into a typed Transient error when every URL
// failed — that allows the per-source circuit breaker to count the outage
// instead of silently returning zero deals and only being caught later by
// the zero-streak health monitor.
//
// The parse function receives the HTML body and the source URL (useful for
// resolving relative links). It must return nil deals on parse failure
// rather than panicking; fetchMultiURL does not second-guess parse results.
func fetchMultiURL(
	ctx context.Context,
	sourceName string,
	http scraper.Fetcher,
	byparr *scraper.ByparrClient,
	urls []string,
	useFB bool,
	parse func(html, baseURL string) []domain.Deal,
) multiURLFetchResult {
	out := multiURLFetchResult{total: len(urls)}
	for _, u := range urls {
		html, err := http.Get(ctx, u)
		if err != nil && useFB && byparr != nil {
			if ses, e2 := byparr.GetPage(ctx, u); e2 == nil {
				html = ses.HTML
				err = nil
			}
		}
		if err != nil {
			slog.Warn(sourceName, "url", u, "err", err)
			out.errors = append(out.errors, err)
			if isBlockedError(err) {
				out.blocked = true
			}
			out.failed++
			continue
		}
		if isBlockedText(html) {
			out.blocked = true
		}
		parsed := parse(html, u)
		out.deals = append(out.deals, applyListingJSONLD(html, parsed)...)
	}
	slog.Debug(sourceName, "deals", len(out.deals), "failed_urls", out.failed, "total_urls", out.total)
	return out
}

// asTransientError returns nil when at least one URL succeeded (deals were
// fetched or parseable HTML was returned), and a typed Transient error
// wrapping the first underlying error when every URL failed. The error is
// tagged with the source name so the scanner's circuit breaker counts it.
//
// Callers should use it like:
//
//	res := fetchMultiURL(...)
//	if err := res.asTransientError("mindfactory"); err != nil {
//	    return nil, err
//	}
//	return res.deals, nil
func (r multiURLFetchResult) asTransientError(sourceName string) error {
	// CAPTCHA/WAF pages that parse to zero deals must surface as Blocked
	// (Sites/Logs show "captcha") instead of a silent empty scan.
	if r.blocked && len(r.deals) == 0 {
		var cause error
		for _, e := range r.errors {
			if e != nil {
				cause = e
				break
			}
		}
		if cause == nil {
			cause = errors.New("CAPTCHA/WAF page with no extractable deals")
		}
		return Blocked(sourceName, cause)
	}
	if r.failed == 0 || r.failed < r.total {
		return nil
	}
	// Every URL failed: bubble up the first error wrapped as Transient so
	// the per-source circuit breaker can count it. Joining all errors would
	// be more informative but Transient only accepts one cause; the others
	// have already been logged per-URL above.
	if len(r.errors) == 0 {
		return nil
	}
	var firstErr error
	for _, e := range r.errors {
		if e != nil {
			firstErr = e
			break
		}
	}
	if firstErr == nil {
		return nil
	}
	if r.blocked || isBlockedError(firstErr) {
		return Blocked(sourceName, firstErr)
	}
	return Transient(sourceName, firstErr)
}

func isBlockedError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrBlocked) || isBlockedText(err.Error())
}

// blockedScanBytes mirrors scraper.HTTPFetcher.isBlocked: CAPTCHA/WAF markers
// appear in the page head, not in product listings buried in multi-MB HTML.
const blockedScanBytes = 2048

// blockedMarkers are matched case-insensitively. Hoisted to avoid a per-call
// slice allocation on the scan hot path (one fetchMultiURL call per URL).
var blockedMarkers = []string{"captcha", "access denied", "waf", "blocked"}

func isBlockedText(text string) bool {
	// ⚡ Bolt optimization: only lowercase/scan the first 2KB instead of the
	// full response (up to 5 MB). On a typical retailer listing this avoids
	// ~5 MB of allocation + byte scan per URL while preserving detection —
	// challenge pages surface "captcha"/"access denied" in <title> or the
	// first script block, never after thousands of product cards.
	if len(text) > blockedScanBytes {
		text = text[:blockedScanBytes]
	}
	lower := strings.ToLower(text)
	for _, marker := range blockedMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// errorSummary returns a compact "; "-joined view of the accumulated errors,
// for sources that want to surface all causes rather than only the first.
// Empty when no URL failed.
func (r multiURLFetchResult) errorSummary() string {
	if len(r.errors) == 0 {
		return ""
	}
	var parts []string
	for _, e := range r.errors {
		if e != nil {
			parts = append(parts, e.Error())
		}
	}
	return strings.Join(parts, "; ")
}

// fetchWithByparrFallback is a convenience wrapper for sources that serve
// an SPA shell or an anti-bot challenge page to HTTP clients but render the
// real product listing when JavaScript is executed (Byparr). It first tries
// the standard fetchMultiURL path; if that yields zero deals and both the
// headless fallback flag and the Byparr client are configured, it retries
// every URL via Byparr and parses the rendered HTML with the same parse
// function. The Byparr errors are logged but do not cause the overall fetch
// to fail — only deals from successful renders are returned.
func fetchWithByparrFallback(
	ctx context.Context,
	sourceName string,
	http scraper.Fetcher,
	byparr *scraper.ByparrClient,
	urls []string,
	useFB bool,
	parse func(html, baseURL string) []domain.Deal,
) ([]domain.Deal, error) {
	res := fetchMultiURL(ctx, sourceName, http, byparr, urls, useFB, parse)
	// Even if all HTTP URLs failed, retry via Byparr — the VPN may have
	// access to hosts the direct connection cannot reach.
	if len(res.deals) == 0 && useFB && byparr != nil {
		for _, u := range urls {
			ses, err := byparr.GetPage(ctx, u)
			if err != nil {
				slog.Warn(sourceName, "byparr", u, "err", err)
				res.errors = append(res.errors, err)
				if isBlockedError(err) {
					res.blocked = true
				}
				continue
			}
			parsed := parse(ses.HTML, u)
			res.deals = append(res.deals, applyListingJSONLD(ses.HTML, parsed)...)
		}
		slog.Info("byparr retry done", "src", sourceName, "n_urls", len(urls), "n_deals", len(res.deals))
	}
	// Only return a Transient error from the HTTP round if Byparr also
	// produced zero deals. Otherwise the circuit breaker never trips even
	// though the source is effectively dead.
	if len(res.deals) == 0 {
		if res.blocked {
			var cause error
			if len(res.errors) > 0 {
				cause = res.errors[0]
			} else {
				cause = errors.New("blocked page returned no deals")
			}
			return nil, Blocked(sourceName, cause)
		}
		if err := res.asTransientError(sourceName); err != nil {
			return nil, err
		}
	}
	return res.deals, nil
}
