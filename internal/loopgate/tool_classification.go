package loopgate

import toolspkg "loopgate/internal/tools"

// capabilityClass describes how a single capability may be dispatched within
// a batch of tool calls returned by the model.
//
// Classification is based on Loopgate's own operation taxonomy (OpRead /
// OpWrite / OpExecute defined in internal/tools/tool.go). It has nothing to do
// with concurrency primitives — "readOnly" means the capability's Execute
// implementation makes no observable mutation to Loopgate state, so Loopgate
// can safely run multiple of them in parallel without ordering constraints.
//
// The result size cap is not stored here. It lives in havenToolResultMaxRunesByCapability
// and is looked up directly at the point of truncation, keeping this struct minimal.
type capabilityClass struct {
	// readOnly is true when the registered tool's Operation() is OpRead.
	// A readOnly capability may execute concurrently with other readOnly
	// capabilities in the same batch; all others are dispatched serially.
	//
	// Fail-closed default: an unregistered capability is treated as NOT readOnly
	// because we cannot prove it has no side effects. Latency is preferable to
	// a silent ordering violation between two write-side-effect capabilities.
	readOnly bool
}

// classifyCapability returns the dispatch class for a Loopgate capability name.
//
// The registry is the sole source of truth for operation type. An absent entry
// (capability not yet registered, or a capability the policy rejected before
// registration) yields readOnly=false so the executor falls back to serial dispatch.
//
// Callers must not cache the result across requests — the registry can be updated
// at runtime when new capabilities are dynamically registered.
func classifyCapability(registry *toolspkg.Registry, capabilityName string) capabilityClass {
	tool := registry.Get(capabilityName)
	if tool == nil {
		// Unregistered capability: fail closed. Serial is always safe.
		return capabilityClass{readOnly: false}
	}
	return capabilityClass{
		// OpWrite and OpExecute have observable ordering constraints
		// (write-then-read must see the written value; external processes
		// share global state such as cwd and environment). Only OpRead is safe
		// to fan out.
		readOnly: tool.Operation() == toolspkg.OpRead,
	}
}
