package tcl

// MemoryCandidateInput is the raw analysis input shape consumed by the TCL pipeline.
// It is not a validated write contract, and callers should not treat it as persistence-safe.
type MemoryCandidateInput struct {
	Source              CandidateSource
	SourceChannel       string
	RawSourceText       string
	NormalizedFactKey   string
	NormalizedFactValue string
	Reason              string
	Trust               Trust
	Actor               Object
}

// MemoryCandidate remains as a temporary alias while analysis callers migrate to the clearer name.
// It still means raw analysis input, not a validated memory-write contract.
type MemoryCandidate = MemoryCandidateInput
