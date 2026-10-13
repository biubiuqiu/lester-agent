package model

import "testing"

func TestBudgetPatchPreservesOmittedValuesAndAcceptsExplicitReset(t *testing.T) {
	window := 32000
	current := Deployment{ContextWindow: 128000, OutputReserve: 8000, PolicyWindow: 64000}
	updated, err := (DeploymentBudgetChange{PolicyWindow: &window}).apply(current)
	if err != nil || updated.ContextWindow != 128000 || updated.OutputReserve != 8000 || updated.PolicyWindow != 32000 {
		t.Fatalf("updated=%+v error=%v", updated, err)
	}
	zero := 0
	updated, err = (DeploymentBudgetChange{ContextWindow: &zero}).apply(updated)
	if err != nil || updated.ContextWindow != 0 || updated.PolicyWindow != 32000 || updated.OutputReserve != 8000 {
		t.Fatalf("explicit unknown-context reset=%+v error=%v", updated, err)
	}
}

func TestBudgetValidationRejectsImpossibleReservesAndOutOfRangeValues(t *testing.T) {
	negative, maximum, same := -1, int64(2147483648), 32000
	tooLarge := int(maximum)
	for _, change := range []DeploymentBudgetChange{{ContextWindow: &negative}, {OutputReserve: &negative}, {PolicyWindow: &tooLarge}, {OutputReserve: &same}} {
		if _, err := change.apply(Deployment{ContextWindow: 32000, PolicyWindow: 64000}); err == nil {
			t.Fatalf("accepted invalid budget: %+v", change)
		}
	}
}
