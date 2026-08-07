package traffic

import (
	"testing"
)

func TestCalculateEffectiveUsed(t *testing.T) {
	// Monthly Upload limit: 200 GB. Wasted allowance = 200 * 0.5 = 100 GB.
	limit := int64(200 * 1024 * 1024 * 1024)

	// Scenario 1: Aborted bytes (50 GB) <= Allowance (100 GB). Effective used = completed bytes (10 GB)
	comp := int64(10 * 1024 * 1024 * 1024)
	abort := int64(50 * 1024 * 1024 * 1024)
	eff := CalculateEffectiveUsed(comp, abort, limit, true)
	if eff != comp {
		t.Fatalf("Expected effective used to be %d, got %d", comp, eff)
	}

	// Scenario 2: Aborted bytes (150 GB) > Allowance (100 GB). Excess = 50 GB. Effective used = 10 GB + 50 GB = 60 GB
	abortExcess := int64(150 * 1024 * 1024 * 1024)
	expectedEff := int64(60 * 1024 * 1024 * 1024)
	eff = CalculateEffectiveUsed(comp, abortExcess, limit, true)
	if eff != expectedEff {
		t.Fatalf("Expected effective used to be %d, got %d", expectedEff, eff)
	}
}

func TestCheckGraceRule(t *testing.T) {
	limit := int64(100 * 1024 * 1024 * 1024) // 100 GB

	// Current effective used = 90 GB, pending = 0, new file = 10 GB (half = 5 GB). 90 + 5 = 95 GB <= 100 GB -> Allowed!
	effUsed := int64(90 * 1024 * 1024 * 1024)
	newSize := int64(10 * 1024 * 1024 * 1024)
	if !CheckGraceRule(effUsed, 0, newSize, limit) {
		t.Fatal("Expected grace rule to allow transfer, but got false")
	}

	// Current effective used = 98 GB, new file = 10 GB (half = 5 GB). 98 + 5 = 103 GB > 100 GB -> Blocked!
	effUsedBlocked := int64(98 * 1024 * 1024 * 1024)
	if CheckGraceRule(effUsedBlocked, 0, newSize, limit) {
		t.Fatal("Expected grace rule to block transfer, but got true")
	}
}
