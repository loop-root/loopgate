package tcl

type CompactConformanceFixture struct {
	Name          string
	RawExpression string
	WantNode      TCLNode
	WantErr       string
}

type AnchorConformanceFixture struct {
	Name         string
	Candidate    MemoryCandidate
	WantAnchor   *ConflictAnchor
	WantNoAnchor bool
	WantErr      string
}

func DefaultCompactConformanceFixtures() []CompactConformanceFixture {
	return []CompactConformanceFixture{
		{
			Name:          "canonical_memory_write_round_trip",
			RawExpression: "STR(MEM:PRI)->WRT[REV]%(9)",
			WantNode: TCLNode{
				ACT:  ActionStore,
				OBJ:  ObjectMemory,
				QUAL: []Qualifier{QualifierPrivate},
				OUT:  ActionWrite,
				STA:  StateReviewRequired,
				META: TCLMeta{CONF: 9},
			},
		},
		{
			Name:          "relation_target_mid",
			RawExpression: "RDD(FIL)[ACT]~@M41%(7)",
			WantNode: TCLNode{
				ACT: ActionRead,
				OBJ: ObjectFile,
				STA: StateActive,
				REL: []TCLRelation{
					{Type: RelationRelatedTo, TargetMID: "M41"},
				},
				META: TCLMeta{CONF: 7},
			},
		},
		{
			Name:          "nested_relation_expression",
			RawExpression: "ANL(REP:SEC)[ACT]^PLN(TSK)[PND]%(7)",
			WantNode: TCLNode{
				ACT:  ActionAnalyze,
				OBJ:  ObjectRepository,
				QUAL: []Qualifier{QualifierSecuritySensitive},
				STA:  StateActive,
				REL: []TCLRelation{
					{
						Type: RelationSupports,
						TargetExpr: &TCLNode{
							ACT: ActionPlan,
							OBJ: ObjectTask,
							STA: StatePending,
						},
					},
				},
				META: TCLMeta{CONF: 7},
			},
		},
		{
			Name:          "missing_object_rejected",
			RawExpression: "RDD()[ACT]",
			WantErr:       "object scope must not be empty",
		},
		{
			Name:          "duplicate_qualifier_rejected",
			RawExpression: "ANL(REP:SEC:SEC)[ACT]",
			WantErr:       `duplicate qualifier "SEC"`,
		},
		{
			Name:          "malformed_certainty_rejected",
			RawExpression: "RDD(FIL)[ACT]%(12)",
			WantErr:       "malformed certainty annotation",
		},
		{
			Name:          "dangling_relation_rejected",
			RawExpression: "RDD(FIL)[ACT]~",
			WantErr:       "dangling relation operator",
		},
		{
			Name:          "canonical_whitespace_rejected",
			RawExpression: "RDD(FIL)[ACT] %(7)",
			WantErr:       "whitespace is not allowed in canonical TCL syntax",
		},
	}
}

func DefaultAnchorConformanceFixtures() []AnchorConformanceFixture {
	return []AnchorConformanceFixture{
		{
			Name: "explicit_name_anchor",
			Candidate: MemoryCandidate{
				Source:              CandidateSourceExplicitFact,
				SourceChannel:       "user_input",
				NormalizedFactKey:   "name",
				NormalizedFactValue: "Ada",
			},
			WantAnchor: &ConflictAnchor{
				Version:  "v1",
				Domain:   "usr_profile",
				Entity:   "identity",
				SlotKind: "fact",
				SlotName: "name",
			},
		},
		{
			Name: "theme_preference_anchor",
			Candidate: MemoryCandidate{
				Source:              CandidateSourceExplicitFact,
				SourceChannel:       "user_input",
				NormalizedFactKey:   "preference.stated_preference",
				NormalizedFactValue: "dark mode",
			},
			WantAnchor: &ConflictAnchor{
				Version:  "v1",
				Domain:   "usr_preference",
				Entity:   "stated",
				SlotKind: "fact",
				SlotName: "preference",
				Facet:    "ui_theme",
			},
		},
		{
			Name: "unstable_preference_unanchored",
			Candidate: MemoryCandidate{
				Source:              CandidateSourceExplicitFact,
				SourceChannel:       "user_input",
				NormalizedFactKey:   "preference.stated_preference",
				NormalizedFactValue: "I like things better this way",
			},
			WantNoAnchor: true,
		},
		{
			Name: "unsupported_explicit_key_rejected",
			Candidate: MemoryCandidate{
				Source:              CandidateSourceExplicitFact,
				SourceChannel:       "user_input",
				NormalizedFactKey:   "totally.unknown",
				NormalizedFactValue: "value",
			},
			WantErr: "normalized fact key is required",
		},
	}
}
