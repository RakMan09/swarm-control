package alertengine

import "encoding/json"

// Point is a lat/lon coordinate.
type Point struct {
	Lat float64
	Lon float64
}

// Polygon is a closed geofence boundary defined by its vertices (lat, lon).
type Polygon struct {
	Points []Point
	// bounding box, precomputed for a cheap reject test at fleet scale.
	minLat, maxLat, minLon, maxLon float64
}

// NewPolygon builds a polygon and precomputes its bounding box.
func NewPolygon(pts []Point) Polygon {
	p := Polygon{Points: pts}
	if len(pts) == 0 {
		return p
	}
	p.minLat, p.maxLat = pts[0].Lat, pts[0].Lat
	p.minLon, p.maxLon = pts[0].Lon, pts[0].Lon
	for _, pt := range pts[1:] {
		if pt.Lat < p.minLat {
			p.minLat = pt.Lat
		}
		if pt.Lat > p.maxLat {
			p.maxLat = pt.Lat
		}
		if pt.Lon < p.minLon {
			p.minLon = pt.Lon
		}
		if pt.Lon > p.maxLon {
			p.maxLon = pt.Lon
		}
	}
	return p
}

// ParsePolygon reads a polygon from a JSON array of [lat, lon] pairs. An empty
// string yields the DefaultPolygon.
func ParsePolygon(s string) (Polygon, error) {
	if s == "" {
		return DefaultPolygon(), nil
	}
	var raw [][2]float64
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return Polygon{}, err
	}
	pts := make([]Point, len(raw))
	for i, r := range raw {
		pts[i] = Point{Lat: r[0], Lon: r[1]}
	}
	return NewPolygon(pts), nil
}

// DefaultPolygon is a ~0.2deg square around the simulator's origin, matching
// sim defaults so out-of-bounds faults reliably trip a geofence alert.
func DefaultPolygon() Polygon {
	return NewPolygon([]Point{
		{Lat: 37.40, Lon: -122.20},
		{Lat: 37.60, Lon: -122.20},
		{Lat: 37.60, Lon: -121.90},
		{Lat: 37.40, Lon: -121.90},
	})
}

// Contains reports whether pt lies inside the polygon. A bounding-box check
// rejects the common case cheaply before the ray-casting test.
func (p Polygon) Contains(pt Point) bool {
	if len(p.Points) < 3 {
		return true // no meaningful fence => never breaches
	}
	if pt.Lat < p.minLat || pt.Lat > p.maxLat || pt.Lon < p.minLon || pt.Lon > p.maxLon {
		return false
	}
	// Ray casting: count edge crossings to the right of the point.
	inside := false
	n := len(p.Points)
	j := n - 1
	for i := 0; i < n; i++ {
		xi, yi := p.Points[i].Lon, p.Points[i].Lat
		xj, yj := p.Points[j].Lon, p.Points[j].Lat
		if (yi > pt.Lat) != (yj > pt.Lat) {
			xIntersect := (xj-xi)*(pt.Lat-yi)/(yj-yi) + xi
			if pt.Lon < xIntersect {
				inside = !inside
			}
		}
		j = i
	}
	return inside
}
