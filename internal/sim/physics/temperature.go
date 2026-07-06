package physics

import (
	"math"
	"math/rand"
)

type TemperatureEngine struct {
	Current       float64
	Ambient       float64
	K             float64
	CoolingEffect float64
	HeatingEffect float64
	NoiseSigma    float64
	// RNG is the noise source. When nil the engine falls back to the global
	// math/rand source; the simulator injects a per-site seeded *rand.Rand so the
	// config seed is honored and sites do not share a noise sequence.
	RNG *rand.Rand
}

func NewTemperature(initial, ambient float64) *TemperatureEngine {
	return &TemperatureEngine{
		Current:    initial,
		Ambient:    ambient,
		K:          0.1,
		NoiseSigma: 0.015, // 인위적 노이즈 30%로 축소 (was 0.05)
	}
}

func (e *TemperatureEngine) float64() float64 {
	if e.RNG != nil {
		return e.RNG.Float64()
	}
	return rand.Float64()
}

// SetAmbient updates the outdoor/base temperature the engine relaxes toward.
// Used to feed weather evidence values into the simulation.
func (e *TemperatureEngine) SetAmbient(ambient float64) {
	e.Ambient = ambient
}

func (e *TemperatureEngine) SetCooling(power float64) {
	e.CoolingEffect = -math.Max(0, math.Min(power, 10))
}

func (e *TemperatureEngine) SetHeating(power float64) {
	e.HeatingEffect = math.Max(0, math.Min(power, 10))
}

func (e *TemperatureEngine) Step(dt float64) float64 {
	cooling := e.CoolingEffect
	heating := e.HeatingEffect
	ambientVariation := (e.float64() - 0.5) * 0.06 // 인위적 변동 30%로 축소 (was 0.2)
	noise := (e.float64() - 0.5) * e.NoiseSigma * 2

	e.Current += (-e.K*(e.Current-(e.Ambient+ambientVariation)) + cooling + heating) * dt
	e.Current += noise * dt

	return e.Current
}

func (e *TemperatureEngine) Reset(initial float64) {
	e.Current = initial
	e.CoolingEffect = 0
	e.HeatingEffect = 0
}
