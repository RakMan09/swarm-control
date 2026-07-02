package alertengine

import "testing"

func TestDefaultPolygonContains(t *testing.T) {
	p := DefaultPolygon()
	if !p.Contains(Point{Lat: 37.5, Lon: -122.05}) {
		t.Fatal("center should be inside default fence")
	}
	if p.Contains(Point{Lat: 37.5, Lon: -121.0}) {
		t.Fatal("far-east point should be outside")
	}
	if p.Contains(Point{Lat: 38.5, Lon: -122.05}) {
		t.Fatal("far-north point should be outside")
	}
}

func TestBoundingBoxRejectsBeforeRayCast(t *testing.T) {
	p := NewPolygon([]Point{{0, 0}, {0, 10}, {10, 10}, {10, 0}})
	if p.Contains(Point{Lat: -1, Lon: 5}) {
		t.Fatal("point below bbox must be outside")
	}
	if !p.Contains(Point{Lat: 5, Lon: 5}) {
		t.Fatal("center must be inside")
	}
}

func TestParsePolygonJSON(t *testing.T) {
	p, err := ParsePolygon(`[[0,0],[0,10],[10,10],[10,0]]`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(p.Points) != 4 {
		t.Fatalf("expected 4 points, got %d", len(p.Points))
	}
	if !p.Contains(Point{Lat: 5, Lon: 5}) {
		t.Fatal("expected containment")
	}
}

func TestEmptyPolygonNeverBreaches(t *testing.T) {
	var p Polygon
	if !p.Contains(Point{Lat: 100, Lon: 100}) {
		t.Fatal("empty fence should treat everything as inside (no breach)")
	}
}
