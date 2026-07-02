package alertengine

import (
	"math"
	"sync"
)

// AnomalyDetector maintains a per-vehicle rolling window of a scalar signal
// (temperature) and flags samples that exceed mean + k*stddev.
type AnomalyDetector struct {
	window int
	k      float64

	mu   sync.Mutex
	data map[string]*ring
}

type ring struct {
	buf  []float64
	idx  int
	full bool
}

// NewAnomalyDetector builds a detector with the given rolling window size and
// k threshold (temp > mean + k*stddev is anomalous).
func NewAnomalyDetector(window int, k float64) *AnomalyDetector {
	if window < 2 {
		window = 2
	}
	return &AnomalyDetector{window: window, k: k, data: map[string]*ring{}}
}

// Observe records a new value for a vehicle and reports whether it is anomalous
// relative to the prior window. The current value is excluded from the baseline
// so a single spike is judged against history, then added to the window.
func (d *AnomalyDetector) Observe(vehicleID string, v float64) (anomalous bool, mean, std float64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	r, ok := d.data[vehicleID]
	if !ok {
		r = &ring{buf: make([]float64, 0, d.window)}
		d.data[vehicleID] = r
	}

	mean, std, n := stats(r)
	if n >= d.window/2 && n >= 2 && std > 0 {
		if v > mean+d.k*std {
			anomalous = true
		}
	}
	r.push(v, d.window)
	return anomalous, mean, std
}

func (r *ring) push(v float64, cap int) {
	if len(r.buf) < cap {
		r.buf = append(r.buf, v)
		return
	}
	r.buf[r.idx] = v
	r.idx = (r.idx + 1) % cap
	r.full = true
}

func stats(r *ring) (mean, std float64, n int) {
	n = len(r.buf)
	if n == 0 {
		return 0, 0, 0
	}
	var sum float64
	for _, x := range r.buf {
		sum += x
	}
	mean = sum / float64(n)
	var ss float64
	for _, x := range r.buf {
		d := x - mean
		ss += d * d
	}
	std = math.Sqrt(ss / float64(n))
	return mean, std, n
}
