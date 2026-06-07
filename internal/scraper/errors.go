package scraper

import "fmt"

type ErrorKind int

const (
	ErrKindTransient ErrorKind = iota
	ErrKindPermanent
	ErrKindAuth
	ErrKindParse
)

type FetchError struct {
	Kind    ErrorKind
	Status  int
	URL     string
	Message string
	Cause   error
}

func (e *FetchError) Error() string {
	msg := e.Message
	if msg == "" {
		switch e.Kind {
		case ErrKindTransient:
			msg = "transient error"
		case ErrKindPermanent:
			msg = "permanent error"
		case ErrKindAuth:
			msg = "auth error"
		case ErrKindParse:
			msg = "parse error"
		}
	}
	if e.Status > 0 {
		msg = fmt.Sprintf("HTTP %d: %s", e.Status, msg)
	}
	if e.URL != "" {
		msg = fmt.Sprintf("%s: %s", e.URL, msg)
	}
	return msg
}

func (e *FetchError) Unwrap() error { return e.Cause }

func NewTransientError(url string, status int, message string, cause error) *FetchError {
	return &FetchError{Kind: ErrKindTransient, URL: url, Status: status, Message: message, Cause: cause}
}

func NewPermanentError(url string, status int, message string, cause error) *FetchError {
	return &FetchError{Kind: ErrKindPermanent, URL: url, Status: status, Message: message, Cause: cause}
}

func NewAuthError(url string, status int, message string, cause error) *FetchError {
	return &FetchError{Kind: ErrKindAuth, URL: url, Status: status, Message: message, Cause: cause}
}

func NewParseError(url string, message string, cause error) *FetchError {
	return &FetchError{Kind: ErrKindParse, URL: url, Message: message, Cause: cause}
}

func IsRetryable(err error) bool {
	if fe, ok := err.(*FetchError); ok {
		return fe.Kind == ErrKindTransient
	}
	return false
}

func IsAuth(err error) bool {
	if fe, ok := err.(*FetchError); ok {
		return fe.Kind == ErrKindAuth
	}
	return false
}

func IsParse(err error) bool {
	if fe, ok := err.(*FetchError); ok {
		return fe.Kind == ErrKindParse
	}
	return false
}
