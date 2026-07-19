package sources

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Balrog57/DiskCount/internal/scraper"
)

// Severity ranks how a failure should be handled by the scanner.
// ErrTransient and ErrRateLimit mean "back off and retry later", the
// other categories are usually permanent until someone fixes the
// source definition.
type Severity int

const (
	SeverityUnknown Severity = iota
	// SeverityTransient: the network or the upstream had a hiccup.
	// The scanner counts these against the per-source circuit
	// breaker but does not mark the source as broken.
	SeverityTransient
	// SeveritySelector: the upstream page is reachable but the
	// scraper can no longer find the expected fields. This is the
	// most common "source is dead" signal; it should page the admin
	// and show up in the SourceWarnings list after enough retries.
	SeveritySelector
	// SeveritySchema: a JSON-LD or API response was received but its
	// shape changed (missing fields, unexpected types). Treated as
	// transient: the next fetch might bring back the expected shape.
	SeveritySchema
	// SeverityBlocked: the upstream returned a captcha / WAF page
	// (Cloudflare, Akamai, DataDome…). Retry with a different IP or
	// with the headless fallback.
	SeverityBlocked
	// SeverityAuth: the upstream requires credentials we do not have.
	// Permanent until the operator provides them.
	SeverityAuth
	// SeverityConfig: the source's own config is wrong (missing URL,
	// invalid key). The scanner should NOT retry; it should disable
	// the source for the run and report it in the health snapshot.
	SeverityConfig
)

// SourceError is the typed error every source returns. Wrapping an
// existing error is fine: callers can either type-assert on
// *SourceError or use Classify to get a Severity back.
type SourceError struct {
	Severity Severity
	Stage    string // "fetch", "parse", "auth", ...
	Source   string
	Cause    error
	Hint     string // optional human-readable advice for the admin
}

func (e *SourceError) Error() string {
	var b strings.Builder
	b.WriteString("source[")
	b.WriteString(e.Source)
	b.WriteString("] ")
	b.WriteString(e.Stage)
	b.WriteString(": ")
	if e.Cause != nil {
		b.WriteString(e.Cause.Error())
	}
	if e.Hint != "" {
		b.WriteString(" (hint: ")
		b.WriteString(e.Hint)
		b.WriteString(")")
	}
	return b.String()
}

func (e *SourceError) Unwrap() error { return e.Cause }

// Is allows errors.Is(err, ErrSelector) and similar pattern matches.
func (e *SourceError) Is(target error) bool {
	if t, ok := target.(*SourceError); ok {
		return t.Severity == e.Severity
	}
	return false
}

// Sentinel errors for use with errors.Is. A source can return
// fmt.Errorf("...: %w", ErrSelector) and the scanner's Classify
// will still recognise it.
var (
	ErrTransient = &SourceError{Severity: SeverityTransient, Stage: "fetch"}
	ErrSelector  = &SourceError{Severity: SeveritySelector, Stage: "parse"}
	ErrSchema    = &SourceError{Severity: SeveritySchema, Stage: "parse"}
	ErrBlocked   = &SourceError{Severity: SeverityBlocked, Stage: "fetch"}
	ErrAuth      = &SourceError{Severity: SeverityAuth, Stage: "auth"}
	ErrConfig    = &SourceError{Severity: SeverityConfig, Stage: "config"}
)

// Classify walks the error chain and returns its Severity. It understands
// both the sources.SourceError taxonomy and the lower-level scraper.FetchError
// taxonomy, so a source that wraps a transport failure with sources.Transient
// (which nests the *FetchError as its Cause) classifies consistently whether
// the caller type-asserts at the source level or the scraper level. Unknown
// errors map to SeverityUnknown.
func Classify(err error) Severity {
	if err == nil {
		return SeverityUnknown
	}
	var se *SourceError
	if errors.As(err, &se) {
		return se.Severity
	}
	// Fall through to the scraper-level taxonomy so raw *FetchError values
	// (not wrapped in a *SourceError) still classify meaningfully. This
	// removes the need for every source to remember to wrap.
	var fe *scraper.FetchError
	if errors.As(err, &fe) {
		switch fe.Kind {
		case scraper.ErrKindTransient:
			return SeverityTransient
		case scraper.ErrKindAuth:
			return SeverityAuth
		case scraper.ErrKindParse:
			return SeveritySchema
		case scraper.ErrKindPermanent:
			return SeveritySelector
		}
	}
	return SeverityUnknown
}

// Wrap builds a SourceError with the given severity/stage/source
// around an inner cause. The hint is appended to the message so it
// surfaces in logs without callers having to log it separately.
func Wrap(severity Severity, stage, source string, cause error, hint string) error {
	return &SourceError{
		Severity: severity,
		Stage:    stage,
		Source:   source,
		Cause:    cause,
		Hint:     hint,
	}
}

// Transient is a small constructor for the most common case: a fetch
// failed but the source itself is probably fine.
func Transient(source string, cause error) error {
	return Wrap(SeverityTransient, "fetch", source, cause, "")
}

// Selector signals that the upstream page no longer matches the
// scraper's selectors. Hint should describe what to look at (e.g.
// "table headers changed on diskprices.com").
func Selector(source string, cause error, hint string) error {
	return Wrap(SeveritySelector, "parse", source, cause, hint)
}

// Blocked signals that the upstream returned a captcha / WAF page.
// The scraper package is responsible for triggering the headless
// fallback when this is detected; sources just need to flag it.
func Blocked(source string, cause error) error {
	return Wrap(SeverityBlocked, "fetch", source, cause, "")
}

// Config signals that the source cannot run because of its own
// config (missing URL, invalid key). The scanner should not retry.
func Config(source string, cause error, hint string) error {
	return Wrap(SeverityConfig, "config", source, cause, hint)
}

// Format is a small helper for the web admin: it builds a short
// human-readable summary of a SourceError, hiding the cause when it
// is just a network error. It is safe to call with any error.
func Format(err error) string {
	if err == nil {
		return ""
	}
	var se *SourceError
	if !errors.As(err, &se) {
		return err.Error()
	}
	if se.Hint != "" {
		return fmt.Sprintf("%s: %s — %s", se.Stage, se.Cause, se.Hint)
	}
	if se.Cause != nil {
		return fmt.Sprintf("%s: %s", se.Stage, se.Cause)
	}
	return se.Stage
}
