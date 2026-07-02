package alertengine

import "testing"

func TestAnomalyDetectsSpike(t *testing.T) {
	d := NewAnomalyDetector(20, 3.0)
	// Feed a stable baseline around 25C.
	for i := 0; i < 20; i++ {
		v := 25.0 + float64(i%2)*0.1 // tiny variance
		if anom, _, _ := d.Observe("v", v); anom {
			t.Fatalf("baseline sample %d should not be anomalous", i)
		}
	}
	anom, mean, std := d.Observe("v", 80.0)
	if !anom {
		t.Fatalf("large spike should be flagged (mean=%.2f std=%.2f)", mean, std)
	}
}

func TestAnomalyIgnoresColdStart(t *testing.T) {
	d := NewAnomalyDetector(20, 3.0)
	if anom, _, _ := d.Observe("v", 999.0); anom {
		t.Fatal("first sample cannot be anomalous (no baseline yet)")
	}
}

func TestAnomalyPerVehicleIsolation(t *testing.T) {
	d := NewAnomalyDetector(10, 3.0)
	for i := 0; i < 10; i++ {
		d.Observe("a", 25.0)
	}
	// A brand-new vehicle should not inherit vehicle a's baseline.
	if anom, _, _ := d.Observe("b", 25.0); anom {
		t.Fatal("distinct vehicle should have independent state")
	}
}
