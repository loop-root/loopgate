package tcl

import (
	"fmt"
	"strconv"
	"strings"
)

func ParseCompactExpression(rawExpression string) (TCLNode, error) {
	trimmedExpression := strings.TrimSpace(rawExpression)
	if trimmedExpression == "" {
		return TCLNode{}, fmt.Errorf("empty TCL expression")
	}
	if strings.Contains(trimmedExpression, " ") {
		return TCLNode{}, fmt.Errorf("whitespace is not allowed in canonical TCL syntax")
	}

	workingExpression := trimmedExpression
	confidence := 0
	if certaintyStart := strings.LastIndex(workingExpression, "%("); certaintyStart >= 0 {
		if !strings.HasSuffix(workingExpression, ")") {
			return TCLNode{}, fmt.Errorf("malformed certainty annotation")
		}
		certaintyDigits := workingExpression[certaintyStart+2 : len(workingExpression)-1]
		parsedConfidence, err := strconv.Atoi(certaintyDigits)
		if err != nil || parsedConfidence < 0 || parsedConfidence > 9 {
			return TCLNode{}, fmt.Errorf("malformed certainty annotation")
		}
		confidence = parsedConfidence
		workingExpression = workingExpression[:certaintyStart]
	}

	parsedNode, remainingExpression, err := parseActionExpression(workingExpression)
	if err != nil {
		return TCLNode{}, err
	}
	if remainingExpression != "" {
		parsedRelations, err := parseRelationChain(remainingExpression)
		if err != nil {
			return TCLNode{}, err
		}
		parsedNode.REL = parsedRelations
	}
	if confidence != 0 {
		parsedNode.META.CONF = confidence
	}
	if err := ValidateNode(parsedNode); err != nil {
		return TCLNode{}, err
	}
	return parsedNode, nil
}

func MustCompactExpression(node TCLNode) string {
	compactExpression, err := CompactExpression(node)
	if err != nil {
		panic(err)
	}
	return compactExpression
}

func CompactExpression(node TCLNode) (string, error) {
	if err := ValidateNode(node); err != nil {
		return "", err
	}

	var builder strings.Builder
	builder.WriteString(string(node.ACT))
	builder.WriteByte('(')
	builder.WriteString(string(node.OBJ))
	for _, qualifier := range node.QUAL {
		builder.WriteByte(':')
		builder.WriteString(string(qualifier))
	}
	builder.WriteByte(')')
	if node.OUT != "" {
		builder.WriteString("->")
		builder.WriteString(string(node.OUT))
	}
	builder.WriteByte('[')
	builder.WriteString(string(node.STA))
	builder.WriteByte(']')
	for _, relation := range node.REL {
		builder.WriteString(relationOperator(relation.Type))
		if relation.TargetMID != "" {
			builder.WriteByte('@')
			builder.WriteString(relation.TargetMID)
			continue
		}
		if relation.TargetExpr == nil {
			return "", fmt.Errorf("relation target missing")
		}
		targetExpression, err := CompactExpression(*relation.TargetExpr)
		if err != nil {
			return "", err
		}
		builder.WriteString(targetExpression)
	}
	if node.META.CONF != 0 {
		builder.WriteString("%(")
		builder.WriteString(strconv.Itoa(node.META.CONF))
		builder.WriteByte(')')
	}
	return builder.String(), nil
}

func parseActionExpression(rawExpression string) (TCLNode, string, error) {
	openParenIndex := strings.Index(rawExpression, "(")
	closeParenIndex := strings.Index(rawExpression, ")")
	if openParenIndex <= 0 || closeParenIndex < openParenIndex {
		return TCLNode{}, "", fmt.Errorf("invalid action expression")
	}
	actionToken := Action(rawExpression[:openParenIndex])
	objectAndQualifiers := rawExpression[openParenIndex+1 : closeParenIndex]
	if objectAndQualifiers == "" {
		return TCLNode{}, "", fmt.Errorf("object scope must not be empty")
	}

	remainingExpression := rawExpression[closeParenIndex+1:]
	outputAction := Action("")
	if strings.HasPrefix(remainingExpression, "->") {
		stateStart := strings.Index(remainingExpression, "[")
		if stateStart < 0 {
			return TCLNode{}, "", fmt.Errorf("missing state")
		}
		outputAction = Action(remainingExpression[2:stateStart])
		remainingExpression = remainingExpression[stateStart:]
	}
	if !strings.HasPrefix(remainingExpression, "[") {
		return TCLNode{}, "", fmt.Errorf("missing state")
	}
	stateEnd := strings.Index(remainingExpression, "]")
	if stateEnd < 0 {
		return TCLNode{}, "", fmt.Errorf("missing state terminator")
	}

	objectTokens := strings.Split(objectAndQualifiers, ":")
	parsedNode := TCLNode{
		ACT: actionToken,
		OBJ: Object(objectTokens[0]),
		OUT: outputAction,
		STA: State(remainingExpression[1:stateEnd]),
	}
	for _, qualifierToken := range objectTokens[1:] {
		parsedNode.QUAL = append(parsedNode.QUAL, Qualifier(qualifierToken))
	}

	return parsedNode, remainingExpression[stateEnd+1:], nil
}

func parseRelationChain(rawExpression string) ([]TCLRelation, error) {
	remainingExpression := rawExpression
	relations := make([]TCLRelation, 0, 2)
	for remainingExpression != "" {
		relationType, operatorWidth := parseRelationOperator(remainingExpression)
		if operatorWidth == 0 {
			return nil, fmt.Errorf("invalid relation operator")
		}
		remainingExpression = remainingExpression[operatorWidth:]
		if remainingExpression == "" {
			return nil, fmt.Errorf("dangling relation operator")
		}

		relation := TCLRelation{Type: relationType}
		if strings.HasPrefix(remainingExpression, "@") {
			nextOperatorIndex := nextRelationOperatorIndex(remainingExpression[1:])
			if nextOperatorIndex < 0 {
				relation.TargetMID = remainingExpression[1:]
				remainingExpression = ""
			} else {
				relation.TargetMID = remainingExpression[1 : nextOperatorIndex+1]
				remainingExpression = remainingExpression[nextOperatorIndex+1:]
			}
			relations = append(relations, relation)
			continue
		}

		targetExpression, tailExpression, err := parseActionExpression(remainingExpression)
		if err != nil {
			return nil, err
		}
		relation.TargetExpr = &targetExpression
		remainingExpression = tailExpression
		relations = append(relations, relation)
	}
	return relations, nil
}

func parseRelationOperator(rawExpression string) (RelationType, int) {
	switch {
	case strings.HasPrefix(rawExpression, ">>"):
		return RelationDependsOn, 2
	case strings.HasPrefix(rawExpression, "<-"):
		return RelationDerivedFrom, 2
	case strings.HasPrefix(rawExpression, "^"):
		return RelationSupports, 1
	case strings.HasPrefix(rawExpression, "x"):
		return RelationContradicts, 1
	case strings.HasPrefix(rawExpression, "~"):
		return RelationRelatedTo, 1
	case strings.HasPrefix(rawExpression, "!"):
		return RelationImportant, 1
	default:
		return "", 0
	}
}

func relationOperator(relationType RelationType) string {
	switch relationType {
	case RelationSupports:
		return "^"
	case RelationContradicts:
		return "x"
	case RelationRelatedTo:
		return "~"
	case RelationDerivedFrom:
		return "<-"
	case RelationDependsOn:
		return ">>"
	case RelationImportant:
		return "!"
	default:
		return ""
	}
}

func nextRelationOperatorIndex(rawExpression string) int {
	bestIndex := -1
	for _, operator := range []string{">>", "<-", "^", "x", "~", "!"} {
		operatorIndex := strings.Index(rawExpression, operator)
		if operatorIndex >= 0 && (bestIndex < 0 || operatorIndex < bestIndex) {
			bestIndex = operatorIndex
		}
	}
	return bestIndex
}
