// Package controlapi holds the local control-plane wire contracts and
// validation helpers that do not require Server state.
//
// Runtime code and local clients import this package directly for request and
// response shapes. Server-owned state machines and enforcement remain in
// internal/loopgate.
package controlapi
