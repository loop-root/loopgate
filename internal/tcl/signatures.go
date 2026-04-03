package tcl

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

func DeriveSignatureSet(node TCLNode) (SignatureSet, error) {
	if err := ValidateNode(node); err != nil {
		return SignatureSet{}, err
	}

	return SignatureSet{
		Exact:      hashSemanticShape(exactSignatureMaterial(node)),
		Family:     hashSemanticShape(familySignatureMaterial(node)),
		RiskMotifs: deriveRiskMotifs(node),
	}, nil
}

func exactSignatureMaterial(node TCLNode) string {
	qualifiers := sortedQualifierStrings(node.QUAL)
	relations := make([]string, 0, len(node.REL))
	for _, relation := range node.REL {
		switch {
		case relation.TargetMID != "":
			relations = append(relations, fmt.Sprintf("%s:@%s", relation.Type, strings.TrimSpace(relation.TargetMID)))
		case relation.TargetExpr != nil:
			relations = append(relations, fmt.Sprintf("%s:%s", relation.Type, familySignatureMaterial(*relation.TargetExpr)))
		}
	}
	sort.Strings(relations)
	return strings.Join([]string{
		string(node.ACT),
		string(node.OBJ),
		strings.Join(qualifiers, ","),
		string(node.OUT),
		string(node.STA),
		string(node.META.ACTOR),
		string(node.META.TRUST),
		strings.TrimSpace(node.META.SOURCE),
		strings.Join(relations, ","),
	}, "|")
}

func familySignatureMaterial(node TCLNode) string {
	qualifiers := sortedQualifierStrings(node.QUAL)
	return strings.Join([]string{
		string(node.ACT),
		string(node.OBJ),
		strings.Join(qualifiers, ","),
		string(node.OUT),
		string(node.STA),
	}, "|")
}

func sortedQualifierStrings(qualifiers []Qualifier) []string {
	result := make([]string, 0, len(qualifiers))
	for _, qualifier := range qualifiers {
		result = append(result, string(qualifier))
	}
	sort.Strings(result)
	return result
}

func deriveRiskMotifs(node TCLNode) []RiskMotif {
	riskMotifs := make([]RiskMotif, 0, 1)
	if node.ACT == ActionStore && node.OBJ == ObjectMemory &&
		hasQualifier(node.QUAL, QualifierPrivate) &&
		hasQualifier(node.QUAL, QualifierExternal) &&
		node.OUT == ActionWrite {
		riskMotifs = append(riskMotifs, RiskMotifPrivateExternalMemoryWrite)
	}
	return riskMotifs
}

func hasQualifier(qualifiers []Qualifier, wantedQualifier Qualifier) bool {
	for _, qualifier := range qualifiers {
		if qualifier == wantedQualifier {
			return true
		}
	}
	return false
}

func hashSemanticShape(rawMaterial string) string {
	digest := sha256.Sum256([]byte(rawMaterial))
	return "sha256:" + hex.EncodeToString(digest[:])
}
