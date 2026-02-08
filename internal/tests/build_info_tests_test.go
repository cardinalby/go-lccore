package tests

import (
	"testing"
)

func TestIsTestingBuild(t *testing.T) {
	//goland:noinspection GoBoolExpressions
	if !IsTestingBuild {
		t.Errorf("use 'lc_core.testing' build tag for tests")
	}
}
