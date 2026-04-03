package secrets

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"syscall"
)

// LoopgateOperatorErrorClass maps an error to a small fixed vocabulary for operator-facing
// diagnostic output. It MUST NOT return substrings of arbitrary errors (no API bodies, keys,
// tokens, or provider messages).
func LoopgateOperatorErrorClass(err error) string {
	if err == nil {
		return "none"
	}
	if errors.Is(err, context.Canceled) {
		return "context_canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "context_deadline"
	}
	if errors.Is(err, io.EOF) {
		return "io_eof"
	}
	if errors.Is(err, syscall.ETIMEDOUT) {
		return "network_timeout"
	}
	var nerr net.Error
	if errors.As(err, &nerr) && nerr.Timeout() {
		return "network_timeout"
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if opErr.Timeout() {
			return "network_timeout"
		}
		return "network_op"
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if errors.Is(urlErr.Err, context.DeadlineExceeded) {
			return "context_deadline"
		}
		return "url_transport"
	}
	var syn *json.SyntaxError
	if errors.As(err, &syn) {
		return "json_syntax"
	}
	var ut *json.UnmarshalTypeError
	if errors.As(err, &ut) {
		return "json_type_mismatch"
	}
	var unsupported *json.UnsupportedTypeError
	if errors.As(err, &unsupported) {
		return "json_unsupported_type"
	}
	var marshErr *json.MarshalerError
	if errors.As(err, &marshErr) {
		return "json_marshal"
	}
	return "unspecified"
}
