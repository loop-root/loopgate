package tcl

import (
	"reflect"
	"testing"
)

func TestCompactConformanceFixtures(t *testing.T) {
	for _, fixture := range DefaultCompactConformanceFixtures() {
		t.Run(fixture.Name, func(t *testing.T) {
			parsedNode, err := ParseCompactExpression(fixture.RawExpression)
			if fixture.WantErr != "" {
				if err == nil {
					t.Fatalf("expected error %q, got nil", fixture.WantErr)
				}
				if err.Error() != fixture.WantErr {
					t.Fatalf("expected error %q, got %q", fixture.WantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("parse compact expression: %v", err)
			}
			assertConformanceNode(t, fixture.WantNode, parsedNode)

			renderedExpression, err := CompactExpression(parsedNode)
			if err != nil {
				t.Fatalf("compact expression: %v", err)
			}
			if renderedExpression != fixture.RawExpression {
				t.Fatalf("expected round trip %q, got %q", fixture.RawExpression, renderedExpression)
			}
		})
	}
}

func TestAnchorConformanceFixtures(t *testing.T) {
	for _, fixture := range DefaultAnchorConformanceFixtures() {
		t.Run(fixture.Name, func(t *testing.T) {
			normalizedNode, err := NormalizeMemoryCandidate(fixture.Candidate)
			if fixture.WantErr != "" {
				if err == nil {
					t.Fatalf("expected error %q, got nil", fixture.WantErr)
				}
				if err.Error() != fixture.WantErr {
					t.Fatalf("expected error %q, got %q", fixture.WantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("normalize memory candidate: %v", err)
			}
			if fixture.WantNoAnchor {
				if normalizedNode.ANCHOR != nil {
					t.Fatalf("expected no anchor, got %#v", normalizedNode.ANCHOR)
				}
				return
			}
			if normalizedNode.ANCHOR == nil {
				t.Fatal("expected anchor, got nil")
			}
			if !reflect.DeepEqual(*fixture.WantAnchor, *normalizedNode.ANCHOR) {
				t.Fatalf("expected anchor %#v, got %#v", *fixture.WantAnchor, *normalizedNode.ANCHOR)
			}
		})
	}
}

func assertConformanceNode(t *testing.T, wantNode TCLNode, gotNode TCLNode) {
	t.Helper()

	if gotNode.ACT != wantNode.ACT {
		t.Fatalf("expected ACT %q, got %q", wantNode.ACT, gotNode.ACT)
	}
	if gotNode.OBJ != wantNode.OBJ {
		t.Fatalf("expected OBJ %q, got %q", wantNode.OBJ, gotNode.OBJ)
	}
	if !reflect.DeepEqual(gotNode.QUAL, wantNode.QUAL) {
		t.Fatalf("expected QUAL %#v, got %#v", wantNode.QUAL, gotNode.QUAL)
	}
	if gotNode.OUT != wantNode.OUT {
		t.Fatalf("expected OUT %q, got %q", wantNode.OUT, gotNode.OUT)
	}
	if gotNode.STA != wantNode.STA {
		t.Fatalf("expected STA %q, got %q", wantNode.STA, gotNode.STA)
	}
	if gotNode.META.CONF != wantNode.META.CONF {
		t.Fatalf("expected CONF %d, got %d", wantNode.META.CONF, gotNode.META.CONF)
	}
	if len(gotNode.REL) != len(wantNode.REL) {
		t.Fatalf("expected %d relations, got %d", len(wantNode.REL), len(gotNode.REL))
	}
	for relationIndex := range wantNode.REL {
		wantRelation := wantNode.REL[relationIndex]
		gotRelation := gotNode.REL[relationIndex]
		if gotRelation.Type != wantRelation.Type {
			t.Fatalf("expected relation type %q, got %q", wantRelation.Type, gotRelation.Type)
		}
		if gotRelation.TargetMID != wantRelation.TargetMID {
			t.Fatalf("expected relation target MID %q, got %q", wantRelation.TargetMID, gotRelation.TargetMID)
		}
		switch {
		case wantRelation.TargetExpr == nil && gotRelation.TargetExpr == nil:
		case wantRelation.TargetExpr == nil || gotRelation.TargetExpr == nil:
			t.Fatalf("expected target expression %#v, got %#v", wantRelation.TargetExpr, gotRelation.TargetExpr)
		default:
			assertConformanceNode(t, *wantRelation.TargetExpr, *gotRelation.TargetExpr)
		}
	}
}
