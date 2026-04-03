package signal

import (
	"errors"
	"testing"
)

func TestShutdownSequence_OrderedExecution(t *testing.T) {
	seq := NewShutdownSequence()

	var order []int

	seq.Add(func() error {
		order = append(order, 1)
		return nil
	})
	seq.Add(func() error {
		order = append(order, 2)
		return nil
	})
	seq.Add(func() error {
		order = append(order, 3)
		return nil
	})

	err := seq.Run()
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if len(order) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(order))
	}
	if order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Errorf("expected order [1,2,3], got %v", order)
	}
}

func TestShutdownSequence_ContinuesOnError(t *testing.T) {
	seq := NewShutdownSequence()

	var executed []int
	testErr := errors.New("step 2 failed")

	seq.Add(func() error {
		executed = append(executed, 1)
		return nil
	})
	seq.Add(func() error {
		executed = append(executed, 2)
		return testErr
	})
	seq.Add(func() error {
		executed = append(executed, 3)
		return nil
	})

	err := seq.Run()

	// Should return first error
	if err != testErr {
		t.Errorf("expected testErr, got %v", err)
	}

	// But all steps should have executed
	if len(executed) != 3 {
		t.Errorf("expected all 3 steps to execute, got %d", len(executed))
	}
}

func TestShutdownSequence_Empty(t *testing.T) {
	seq := NewShutdownSequence()

	err := seq.Run()
	if err != nil {
		t.Errorf("expected no error for empty sequence, got %v", err)
	}
}

func TestShutdownSequence_FirstErrorReturned(t *testing.T) {
	seq := NewShutdownSequence()

	err1 := errors.New("first error")
	err2 := errors.New("second error")

	seq.Add(func() error { return err1 })
	seq.Add(func() error { return err2 })

	err := seq.Run()
	if err != err1 {
		t.Errorf("expected first error to be returned, got %v", err)
	}
}
