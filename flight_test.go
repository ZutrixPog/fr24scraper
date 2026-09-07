package scraper

import "testing"

func TestSmoothedTrajectoryAveragesZigzag(t *testing.T) {
	f := &Flight{}
	f.Trajectory = []TrajectoryPointData{
		{TimeStamp: 1, Latitude: 0, Longitude: 0, Altitude: 10, Track: 0, Speed: 100},
		{TimeStamp: 2, Latitude: 100, Longitude: 100, Altitude: 20, Track: 10, Speed: 200},
		{TimeStamp: 3, Latitude: 0, Longitude: 0, Altitude: 10, Track: 0, Speed: 100},
		{TimeStamp: 4, Latitude: 100, Longitude: 100, Altitude: 20, Track: 10, Speed: 200},
		{TimeStamp: 5, Latitude: 0, Longitude: 0, Altitude: 10, Track: 0, Speed: 100},
	}

	got := f.SmoothedTrajectory(1)
	if len(got) != len(f.Trajectory) {
		t.Fatalf("len = %d, want %d", len(got), len(f.Trajectory))
	}
	wantLats := []float64{0, 33.3333, 66.6667, 33.3333, 0}
	for i, p := range got {
		if wantLats[i]-p.Latitude > 0.0001 || p.Latitude-wantLats[i] > 0.0001 {
			t.Errorf("point %d: lat = %v, want ~%v", i, p.Latitude, wantLats[i])
		}
		if got[i].TimeStamp != f.Trajectory[i].TimeStamp {
			t.Errorf("point %d: timestamp changed", i)
		}
	}

	if r := f.SmoothedTrajectory(0); len(r) != len(f.Trajectory) {
		t.Errorf("radius 0 should return raw trajectory, got len %d", len(r))
	}
}

func TestSmoothedTrajectoryIncrementalMatchesFullRebuild(t *testing.T) {
	appendPoint := func(f *Flight, ts uint64, lat, lon float64, alt int, trk, spd float64) {
		f.SetCoordinate(ts, lat, lon, alt, trk, spd, 100)
	}

	f := &Flight{}
	for i := 1; i <= 20; i++ {
		lat := 0.0
		if i%2 == 0 {
			lat = 100
		}
		appendPoint(f, uint64(i), lat, float64(i)*3, 10+i, 90+float64(i), 200+float64(i))
		if i >= 3 {
			f.SmoothedTrajectory(1)
		}
	}

	want := &Flight{}
	for i := 1; i <= 20; i++ {
		lat := 0.0
		if i%2 == 0 {
			lat = 100
		}
		appendPoint(want, uint64(i), lat, float64(i)*3, 10+i, 90+float64(i), 200+float64(i))
	}
	want.SmoothedTrajectory(1)

	if len(f.Trajectory) != len(want.Trajectory) {
		t.Fatalf("trajectory len = %d, want %d", len(f.Trajectory), len(want.Trajectory))
	}
	for i := range f.Trajectory {
		if f.Trajectory[i].TimeStamp != want.Trajectory[i].TimeStamp {
			t.Fatalf("point %d: ts %d != %d", i, f.Trajectory[i].TimeStamp, want.Trajectory[i].TimeStamp)
		}
		for _, field := range []struct {
			name string
			a, b float64
		}{
			{"lat", f.Trajectory[i].Latitude, want.Trajectory[i].Latitude},
			{"lon", f.Trajectory[i].Longitude, want.Trajectory[i].Longitude},
			{"track", f.Trajectory[i].Track, want.Trajectory[i].Track},
			{"speed", f.Trajectory[i].Speed, want.Trajectory[i].Speed},
		} {
			if field.a != field.b {
				t.Errorf("point %d %s: incremental %v, full %v", i, field.name, field.a, field.b)
			}
		}
		if f.Trajectory[i].Altitude != want.Trajectory[i].Altitude {
			t.Errorf("point %d alt: incremental %d, full %d", i, f.Trajectory[i].Altitude, want.Trajectory[i].Altitude)
		}
	}
}
