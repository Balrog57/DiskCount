package sources

import (
	"errors"
	"testing"
)

func TestClassifyUnknownWhenNil(t *testing.T) {
	if got := Classify(nil); got != SeverityUnknown {
		t.Fatalf("nil err should classify to Unknown, got %v", got)
	}
}

func TestClassifyPlainError(t *testing.T) {
	if got := Classify(errStr("boom")); got != SeverityUnknown {
		t.Fatalf("plain error should be Unknown, got %v", got)
	}
}

func TestClassifySentinels(t *testing.T) {
	cases := map[error]Severity{
		ErrTransient: SeverityTransient,
		ErrSelector:  SeveritySelector,
		ErrSchema:    SeveritySchema,
		ErrBlocked:   SeverityBlocked,
		ErrAuth:      SeverityAuth,
		ErrConfig:    SeverityConfig,
	}
	for err, want := range cases {
		if got := Classify(err); got != want {
			t.Errorf("Classify(%v) = %v, want %v", err, got, want)
		}
	}
}

func TestWrapPreservesCauseAndHint(t *testing.T) {
	cause := errStr("network down")
	wrapped := Transient("diskprices", cause)
	if Format(wrapped) == "" {
		t.Fatal("Format should not be empty for wrapped error")
	}
	// errors.Unwrap should reach the cause.
	type unwrapper interface{ Unwrap() error }
	u, ok := wrapped.(unwrapper)
	if !ok {
		t.Fatal("SourceError must implement Unwrap")
	}
	if u.Unwrap().Error() != "network down" {
		t.Fatalf("Unwrap did not return the cause: %v", u.Unwrap())
	}
}

func TestIsMatchesBySeverity(t *testing.T) {
	wrapped := Selector("pricepergig", errStr("no <tr>"), "table layout changed")
	// SourceError.Is unwraps the cause chain and matches by severity,
	// so wrapped errors compare equal to the sentinel via errors.Is.
	if !errorsIs(wrapped, ErrSelector) {
		t.Fatal("wrapped Selector should match the ErrSelector sentinel")
	}
	if errorsIs(wrapped, ErrTransient) {
		t.Fatal("wrapped Selector should not match ErrTransient")
	}
	// Classify must still see the right severity.
	if got := Classify(wrapped); got != SeveritySelector {
		t.Fatalf("Classify = %v, want %v", got, SeveritySelector)
	}
}

func TestConfigHelperCarriesHint(t *testing.T) {
	err := Config("ebay", errStr("missing client id"), "set EBAY_CLIENT_ID")
	got := Format(err)
	if !contains(got, "set EBAY_CLIENT_ID") {
		t.Fatalf("Format should include hint, got %q", got)
	}
}

// --- tiny helpers below to avoid pulling extra deps into the test ---

type stringErr string

func (s stringErr) Error() string { return string(s) }

func errStr(s string) error { return stringErr(s) }

func errorsIs(err, target error) bool { return errors.Is(err, target) }

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0))
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
