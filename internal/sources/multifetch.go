package sources

import (
	"context"
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
			out.failed++
			continue
		}
		out.deals = append(out.deals, parse(html, u)...)
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
	return Transient(sourceName, firstErr)
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
	if err := res.asTransientError(sourceName); err != nil {
		return nil, err
	}
	if len(res.deals) == 0 && useFB && byparr != nil {
		for _, u := range urls {
			ses, err := byparr.GetPage(ctx, u)
			if err != nil {
				slog.Warn(sourceName, "byparr", u, "err", err)
				continue
			}
			res.deals = append(res.deals, parse(ses.HTML, u)...)
		}
		slog.Debug(sourceName, "byparr_deals", len(res.deals))
	}
	return res.deals, nil
}