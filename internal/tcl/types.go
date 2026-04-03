package tcl

import "strings"

type Action string
type Object string
type Qualifier string
type State string
type RelationType string
type Trust string
type Disposition string
type CandidateSource string
type RiskMotif string

const (
	ActionAsk       Action = "ASK"
	ActionRead      Action = "RDD"
	ActionWrite     Action = "WRT"
	ActionSearch    Action = "SRH"
	ActionAnalyze   Action = "ANL"
	ActionSummarize Action = "SUM"
	ActionCompare   Action = "CMP"
	ActionPlan      Action = "PLN"
	ActionStore     Action = "STR"
	ActionRecall    Action = "RCL"
	ActionApprove   Action = "APR"
	ActionDeny      Action = "DNY"
)

const (
	ObjectUser       Object = "USR"
	ObjectFile       Object = "FIL"
	ObjectRepository Object = "REP"
	ObjectMemory     Object = "MEM"
	ObjectTask       Object = "TSK"
	ObjectPolicy     Object = "POL"
	ObjectImage      Object = "IMG"
	ObjectCode       Object = "COD"
	ObjectNote       Object = "NTE"
	ObjectResult     Object = "RES"
	ObjectSystem     Object = "SYS"
	ObjectUnknown    Object = "UNK"
)

const (
	QualifierSecuritySensitive Qualifier = "SEC"
	QualifierUrgent            Qualifier = "URG"
	QualifierDetailed          Qualifier = "DET"
	QualifierConcise           Qualifier = "CON"
	QualifierPrivate           Qualifier = "PRI"
	QualifierExternal          Qualifier = "EXT"
	QualifierInternal          Qualifier = "INT"
	QualifierSpeculative       Qualifier = "SPC"
	QualifierConfirmed         Qualifier = "CNF"
)

const (
	StateActive         State = "ACT"
	StatePending        State = "PND"
	StateDone           State = "DON"
	StateBlocked        State = "BLK"
	StateReviewRequired State = "REV"
	StateSuperseded     State = "SPR"
	StateAmbiguous      State = "AMB"
)

const (
	RelationSupports    RelationType = "SUP"
	RelationContradicts RelationType = "CNT"
	RelationRelatedTo   RelationType = "REL"
	RelationDerivedFrom RelationType = "DRV"
	RelationDependsOn   RelationType = "DEP"
	RelationImportant   RelationType = "IMP"
)

const (
	TrustSystemDerived   Trust = "TSY"
	TrustUserOriginated  Trust = "TUS"
	TrustExternalDerived Trust = "TEX"
	TrustInferred        Trust = "TIF"
)

const (
	DispositionKeep       Disposition = "KEP"
	DispositionDrop       Disposition = "DRP"
	DispositionFlag       Disposition = "FLG"
	DispositionQuarantine Disposition = "QTN"
	DispositionReview     Disposition = "RVW"
)

const (
	CandidateSourceExplicitFact CandidateSource = "explicit_fact"
	CandidateSourceContinuity   CandidateSource = "continuity_candidate"
	CandidateSourceTaskMetadata CandidateSource = "explicit_task_metadata"
	CandidateSourceWorkflowStep CandidateSource = "workflow_transition"
	// Tool-output candidates stay in the schema vocabulary so future validated ingestion can use the same source taxonomy.
	// NormalizeMemoryCandidate still rejects them today because explicit persistence does not yet have a tool-output validation path.
	CandidateSourceToolOutput CandidateSource = "tool_output_candidate"
	CandidateSourceUnknown    CandidateSource = "unknown_candidate"
)

const (
	RiskMotifPrivateExternalMemoryWrite RiskMotif = "private_external_memory_write"
)

type SignatureSet struct {
	Exact      string
	Family     string
	RiskMotifs []RiskMotif
}

type SemanticProjection struct {
	AnchorVersion   string      `json:"anchor_version,omitempty"`
	AnchorKey       string      `json:"anchor_key,omitempty"`
	ExactSignature  string      `json:"exact_signature,omitempty"`
	FamilySignature string      `json:"family_signature,omitempty"`
	RiskMotifs      []RiskMotif `json:"risk_motifs,omitempty"`
}

type ConflictAnchor struct {
	Version  string
	Domain   string
	Entity   string
	SlotKind string
	SlotName string
	Facet    string
}

func (anchor ConflictAnchor) CanonicalKey() string {
	anchorParts := []string{
		strings.TrimSpace(anchor.Domain),
		strings.TrimSpace(anchor.Entity),
		strings.TrimSpace(anchor.SlotKind),
		strings.TrimSpace(anchor.SlotName),
	}
	if strings.TrimSpace(anchor.Facet) != "" {
		anchorParts = append(anchorParts, strings.TrimSpace(anchor.Facet))
	}
	return strings.Join(anchorParts, ":")
}

type TCLRelation struct {
	Type       RelationType
	TargetMID  string
	TargetExpr *TCLNode
}

type TCLMeta struct {
	ACTOR  Object
	TRUST  Trust
	CONF   int
	TS     int64
	SOURCE string
	SIG    string
}

type TCLDecision struct {
	DISP             Disposition
	REVIEW_REQUIRED  bool
	RISKY            bool
	POISON_CANDIDATE bool
	REASON           string
}

type PolicyDecision struct {
	TCLDecision
	HardDeny bool
}

type TCLNode struct {
	ID     string
	ACT    Action
	OBJ    Object
	QUAL   []Qualifier
	OUT    Action
	STA    State
	REL    []TCLRelation
	META   TCLMeta
	ANCHOR *ConflictAnchor
	// DECISION stays on the node shape now so validated TCL payloads can carry downstream guidance without a schema break later.
	// Current normalization paths mostly rely on external policy output, but keeping the field stable preserves compatibility for that transition.
	DECISION *TCLDecision
}
