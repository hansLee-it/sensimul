package physics

import (
	"math"
	"math/rand"
	"testing"
)

// PM must converge to the ambient/base value (no deposition offset), so the
// air-device on/off targets the loop sets are actually reached. The engine is
// an OU (mean-reverting random walk) process, so the settled value fluctuates
// around the ambient by roughly NoiseSigma/sqrt(2K). A fixed seed makes the run
// deterministic and the tolerance is sized to that steady-state amplitude
// (PM10 swings wider by pm10NoiseFactor).
func TestParticulateConvergesToAmbient(t *testing.T) {
	e := NewParticulate(120.0, 200.0) // start dusty
	e.RNG = rand.New(rand.NewSource(1))
	e.SetAmbient(5.0, 10.0) // clean target (e.g. purifier on)
	e.SetRate(0.3)

	for i := 0; i < 1000; i++ {
		e.Step(1.0)
	}

	if math.Abs(e.PM25-5.0) > 1.0 {
		t.Errorf("PM2.5 settled at %.2f, want ~5.0", e.PM25)
	}
	if math.Abs(e.PM10-10.0) > 1.5 {
		t.Errorf("PM10 settled at %.2f, want ~10.0", e.PM10)
	}
}

func TestParticulateNeverNegative(t *testing.T) {
	e := NewParticulate(10.0, 20.0)
	e.SetAmbient(0.0, 0.0)
	e.SetRate(0.5)
	for i := 0; i < 200; i++ {
		e.Step(1.0)
	}
	if e.PM25 < 0 || e.PM10 < 0 {
		t.Fatalf("PM went negative: %.2f / %.2f", e.PM25, e.PM10)
	}
}
