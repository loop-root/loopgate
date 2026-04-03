package memorybench

import "context"

const SchemaVersion = "1"

type Observer interface {
	OnRunStarted(ctx context.Context, runMetadata RunMetadata) error
	OnScenarioStarted(ctx context.Context, runMetadata RunMetadata, scenarioMetadata ScenarioMetadata) error
	OnRetrievalCompleted(ctx context.Context, runMetadata RunMetadata, scenarioMetadata ScenarioMetadata, backendMetrics BackendMetrics, retrievedArtifacts []RetrievedArtifact, candidatePool []CandidatePoolArtifact) error
	OnEvaluationCompleted(ctx context.Context, runMetadata RunMetadata, scenarioResult ScenarioResult) error
	OnRunCompleted(ctx context.Context, runResult RunResult) error
}

type MultiObserver struct {
	observers []Observer
}

func NewMultiObserver(observers ...Observer) MultiObserver {
	return MultiObserver{observers: append([]Observer(nil), observers...)}
}

func (observer MultiObserver) OnRunStarted(ctx context.Context, runMetadata RunMetadata) error {
	for _, childObserver := range observer.observers {
		if err := childObserver.OnRunStarted(ctx, runMetadata); err != nil {
			return err
		}
	}
	return nil
}

func (observer MultiObserver) OnScenarioStarted(ctx context.Context, runMetadata RunMetadata, scenarioMetadata ScenarioMetadata) error {
	for _, childObserver := range observer.observers {
		if err := childObserver.OnScenarioStarted(ctx, runMetadata, scenarioMetadata); err != nil {
			return err
		}
	}
	return nil
}

func (observer MultiObserver) OnRetrievalCompleted(ctx context.Context, runMetadata RunMetadata, scenarioMetadata ScenarioMetadata, backendMetrics BackendMetrics, retrievedArtifacts []RetrievedArtifact, candidatePool []CandidatePoolArtifact) error {
	for _, childObserver := range observer.observers {
		if err := childObserver.OnRetrievalCompleted(ctx, runMetadata, scenarioMetadata, backendMetrics, retrievedArtifacts, candidatePool); err != nil {
			return err
		}
	}
	return nil
}

func (observer MultiObserver) OnEvaluationCompleted(ctx context.Context, runMetadata RunMetadata, scenarioResult ScenarioResult) error {
	for _, childObserver := range observer.observers {
		if err := childObserver.OnEvaluationCompleted(ctx, runMetadata, scenarioResult); err != nil {
			return err
		}
	}
	return nil
}

func (observer MultiObserver) OnRunCompleted(ctx context.Context, runResult RunResult) error {
	for _, childObserver := range observer.observers {
		if err := childObserver.OnRunCompleted(ctx, runResult); err != nil {
			return err
		}
	}
	return nil
}

type NoopObserver struct{}

func (NoopObserver) OnRunStarted(ctx context.Context, runMetadata RunMetadata) error {
	return nil
}

func (NoopObserver) OnScenarioStarted(ctx context.Context, runMetadata RunMetadata, scenarioMetadata ScenarioMetadata) error {
	return nil
}

func (NoopObserver) OnRetrievalCompleted(ctx context.Context, runMetadata RunMetadata, scenarioMetadata ScenarioMetadata, backendMetrics BackendMetrics, retrievedArtifacts []RetrievedArtifact, candidatePool []CandidatePoolArtifact) error {
	return nil
}

func (NoopObserver) OnEvaluationCompleted(ctx context.Context, runMetadata RunMetadata, scenarioResult ScenarioResult) error {
	return nil
}

func (NoopObserver) OnRunCompleted(ctx context.Context, runResult RunResult) error {
	return nil
}
