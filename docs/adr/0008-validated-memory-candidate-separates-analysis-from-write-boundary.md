Date: 2026-04-02
Status: accepted

We keep the existing TCL analysis pipeline, but add a separate `ValidatedMemoryCandidate` contract because analysis input and authoritative memory-write input are different trust boundaries. The tradeoff is one more internal type and adaptation step, but the validated contract is versioned and raw-text-free so persistence and overwrite paths do not need to reinterpret user, model, or tool prose later. If that extra boundary becomes burdensome, future unification is only acceptable if the resulting shape remains raw-text-free, versioned, and strict enough to preserve the same fail-closed guarantees.
