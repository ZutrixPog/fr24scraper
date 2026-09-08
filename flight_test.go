package scraper

import "testing"

func TestSetCoordinateEMA(t *testing.T) {
	f := &Flight{TrajectoryAlpha: 0.3}
	const trajLimit = 100

	f.SetCoordinate(1, 100, 0, 100, 0, 300, trajLimit)
	f.SetCoordinate(2, 200, 0, 100, 90, 300, trajLimit)
	f.SetCoordinate(3, 300, 0, 100, 90, 300, trajLimit)
	f.SetCoordinate(4, 400, 0, 100, 90, 300, trajLimit)

	expect := func(step int, lat float64) {
		t.Helper()
		p := f.Trajectory[step]
		if p.Latitude < lat-1e-9 || p.Latitude > lat+1e-9 {
			t.Errorf("step %d: lat = %v, want %v", step, p.Latitude, lat)
		}
	}
	expect(0, 100)             // first point raw
	expect(1, 0.3*200+0.7*100) // 130
	expect(2, 0.3*300+0.7*130) // 181
	expect(3, 0.3*400+0.7*181) // 246.7

	if f.Trajectory[len(f.Trajectory)-1].TimeStamp != 4 {
		t.Fatalf("expected newest timestamp 4, got %d", f.Trajectory[len(f.Trajectory)-1].TimeStamp)
	}
}

func TestInPlaceAlphaPartial(t *testing.T) {
	f := &Flight{TrajectoryAlpha: 0.3}
	const trajLimit = 4
	f.SetCoordinate(1, 0, 0, 100, 0, 300, trajLimit)
	f.SetCoordinate(2, 0, 0, 100, 0, 300, trajLimit)
	f.SetCoordinate(3, 0, 0, 100, 0, 300, trajLimit)
	f.SetCoordinate(4, 100, 0, 100, 0, 300, trajLimit)
	f.SetCoordinate(5, 100, 0, 100, 0, 300, trajLimit) // trims oldest

	if lat := f.Trajectory[len(f.Trajectory)-1].Latitude; lat > 60 {
		t.Errorf("alpha not applied after trim: lat = %v, want <= 60", lat)
	}
}

func TestCircularTrackWrap(t *testing.T) {
	if v := wrapBlend(359, 2, 0.5); v < 0 || v > 2 {
		t.Errorf("wrapBlend 359->2 = %v, want near 0.5", v)
	}
	if v := wrapBlend(1, 359, 0.25); v == 89.5 {
		t.Errorf("wrapBlend 1->359 = %v, want a value near 359", v)
	}
}
