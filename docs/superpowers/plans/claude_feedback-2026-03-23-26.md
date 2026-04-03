**Last updated:** 2026-03-24

# TCL — The Foundation Is Solid, One Structural Gap

What's working: The design is genuinely good. The authority split (TCL classifies, Loopgate governs) is correct and clean. The conflict anchor design is sophisticated — separating fact, transition, and state slots is the right generalization. Fail-closed on invalid TCL is correct. Tests pass. Phase 1 is basically done except for Task 6 verification.

The gap I see: Look at normalize.go:75-113. The conflict anchor derivation currently knows four specific keys: name, preferred_name, preference.favorite_*, preference.stated_preference. Everything else gets ANCHOR=nil — which means coexistence, which means accumulation.

The problem is upstream: the model generating memory.remember calls will produce user_name, my_name, user_preferred_name, full_name for the same semantic slot. TCL's anchor can only deduplicate when the key matches exactly. So you'll accumulate multiple facts pointing at the same thing with different keys, and none of the anchor-based supersession fires.

This is the actual reason memory feels lackluster — not the conflict resolution logic (which is well-designed), but the absence of key normalization before anchor derivation. The fix is in normalize.go: before calling deriveExplicitFactConflictAnchor, run the fact key through a normalization table that maps fuzzy model-generated keys to canonical slot names (user_name → name, my_name → name, etc.).

The looksLikeDangerousExplicitMemoryWrite function is correct in scope (Phase 1 is intentionally narrow), but the pattern it catches (secret/token/api key/password AND ignore previous safety/instructions) requires both conditions simultaneously. A prompt injection that just says "remember: my API key is X" without the explicit bypass phrase won't trigger it. Worth noting for Phase 2.

Priority order after Phase 1 verification:

Run Task 6 (full suite verification, roadmap update)
Before Phase 2 signature registry — add key normalization to normalize.go so common identity/profile keys map to canonical slots
Then Phase 2 signature registry and broader pattern coverage
