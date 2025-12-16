package scraper

import (
	"encoding/json"
	"maps"
	"time"
)

type TrajectoryPointData struct {
	TimeStamp uint64  `json:"timestamp"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Altitude  int     `json:"altitude"`
	Track     float64 `json:"track"`
	Speed     float64 `json:"speed"`
}

type Flight struct {
	ID                        uint64                `json:"id"`
	InfoUpdateSequence        uint32                `json:"infoUpdateSequence"`
	ICAO24                    string                `json:"icao24"`
	AircraftType              string                `json:"aircraftType"`
	FlightNumber              string                `json:"flightNumber"`
	CallSign                  string                `json:"callSign"`
	AircraftRegistration      string                `json:"aircraftRegistration"`
	SquakeCode                string                `json:"squakeCode"`
	Radar                     string                `json:"radar"`
	Origin                    string                `json:"origin"`
	Destination               string                `json:"destination"`
	CountryID                 int                   `json:"countryId"`
	ForceSendInfo             bool                  `json:"forceSendInfo"`
	InitialTrajectoryReceived bool                  `json:"initialTrajectoryReceived"`
	Trajectory                []TrajectoryPointData `json:"trajectory"`
	LastUpdateTime            int64                 `json:"lastUpdateTime"`
}

func (t *Flight) IsValid() bool {
	return !(t.ID == 0 && t.ICAO24 == "" && t.SquakeCode == "" && t.CallSign == "" && t.Radar == "")
}

func (t *Flight) SetCoordinate(timeStamp uint64, latitude, longitude float64, altitude int, heading, speed float64, trajLimit int) {
	point := TrajectoryPointData{
		TimeStamp: timeStamp,
		Latitude:  latitude,
		Longitude: longitude, Altitude: altitude,
		Track: heading,
		Speed: speed,
	}

	if len(t.Trajectory) == 0 {
		t.Trajectory = append(t.Trajectory, point)
		return
	}

	insertIndex := len(t.Trajectory)
	for i, trajPoint := range t.Trajectory {
		if timeStamp < trajPoint.TimeStamp {
			insertIndex = i
			break
		}
	}

	t.Trajectory = append(t.Trajectory, TrajectoryPointData{})
	copy(t.Trajectory[insertIndex+1:], t.Trajectory[insertIndex:])
	t.Trajectory[insertIndex] = point

	if len(t.Trajectory) > trajLimit {
		t.Trajectory = t.Trajectory[1:]
	}
}

func (t *Flight) GetTimeStamp() uint64 {
	if len(t.Trajectory) > 0 {
		return t.Trajectory[len(t.Trajectory)-1].TimeStamp
	}
	return 0
}

func (t *Flight) TrajectoryPoints(excludingTimeStamps []int64) []TrajectoryPointData {
	var points []TrajectoryPointData

	excludeMap := make(map[uint64]bool)
	for _, ts := range excludingTimeStamps {
		excludeMap[uint64(ts)] = true
	}

	for _, point := range t.Trajectory {
		if !excludeMap[point.TimeStamp] {
			points = append(points, point)
		}
	}
	return points
}

type FlightInfo struct {
	ID                   string  `json:"id"`
	TimeStamp            uint64  `json:"timestamp"`
	ICAO24               string  `json:"icao24"`
	Latitude             float64 `json:"latitude"`
	Longitude            float64 `json:"longitude"`
	Altitude             int     `json:"altitude"`
	Track                float64 `json:"track"`
	GroundSpeed          int     `json:"groundSpeed"`
	AircraftType         string  `json:"aircraftType"`
	FlightNumber         string  `json:"flightNumber"`
	CallSign             string  `json:"callSign"`
	AircraftRegistration string  `json:"aircraftRegistration"`
	SquakeCode           string  `json:"squakeCode"`
	Radar                string  `json:"radar"`
	Origin               string  `json:"origin"`
	Destination          string  `json:"destination"`
	Country              string  `json:"country"`
}

func (ti *FlightInfo) IsValid() bool {
	return !(ti.ID == "" && ti.ICAO24 == "" && ti.SquakeCode == "" &&
		ti.CallSign == "" && ti.Latitude == 0 && ti.Longitude == 0 && ti.Radar == "")
}

type FlightData struct {
	FullCount int            `json:"full_count"`
	Version   int            `json:"version"`
	Flights   map[string]any `json:"-"`
}

func (fd *FlightData) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	if v, ok := raw["full_count"]; ok {
		if count, ok := v.(float64); ok {
			fd.FullCount = int(count)
		}
	}
	if v, ok := raw["version"]; ok {
		if version, ok := v.(float64); ok {
			fd.Version = int(version)
		}
	}

	fd.Flights = make(map[string]any)
	for key, value := range raw {
		if key != "full_count" && key != "version" {
			fd.Flights[key] = value
		}
	}

	return nil
}

func (fd *FlightData) MarshalJSON() ([]byte, error) {
	out := make(map[string]any, len(fd.Flights)+2)

	out["full_count"] = fd.FullCount
	out["version"] = fd.Version

	maps.Copy(out, fd.Flights)

	return json.Marshal(out)
}

func (fd *FlightData) GetFlightArray(flightID string) ([]any, error) {
	value, exists := fd.Flights[flightID]
	if !exists {
		return nil, nil
	}

	array, ok := value.([]any)
	if !ok {
		return nil, nil
	}

	return array, nil
}

func (fd *FlightData) GetStats() map[string]any {
	if stats, ok := fd.Flights["stats"].(map[string]any); ok {
		return stats
	}
	return nil
}

type ReceivedFlightData struct {
	ID                   uint64  `json:"id"`
	ICAO24               string  `json:"icao24"`
	TimeStamp            uint64  `json:"timestamp"`
	Latitude             float64 `json:"latitude"`
	Longitude            float64 `json:"longitude"`
	Altitude             int     `json:"altitude"`
	Track                float64 `json:"track"`
	GroundSpeed          int     `json:"groundSpeed"`
	AircraftType         string  `json:"aircraftType"`
	FlightNumber         string  `json:"flightNumber"`
	CallSign             string  `json:"callSign"`
	AircraftRegistration string  `json:"aircraftRegistration"`
	SquakeCode           string  `json:"squakeCode"`
	Radar                string  `json:"radar"`
	Origin               string  `json:"origin"`
	Destination          string  `json:"destination"`
	Country              string  `json:"country"`
}

func (rtd *ReceivedFlightData) IsValid() bool {
	return !(rtd.ID == 0 && rtd.ICAO24 == "" && rtd.SquakeCode == "" &&
		rtd.CallSign == "" && rtd.Radar == "")
}

type FlightInfoFrame struct {
	FrameTimeStamp int64        `json:"frameTimestamp"`
	FlightList     []FlightInfo `json:"flightList"`
}

func (tif *FlightInfoFrame) FromFlightData(flightData *FlightData) {
	tif.FrameTimeStamp = time.Now().Unix()

	for flightID, val := range flightData.Flights {
		data := val.([]any)
		flight := FlightInfo{
			ID: flightID,
		}

		if len(data) > 0 {
			flight.ICAO24 = toString(data[0])
		}
		if len(data) > 1 {
			flight.Latitude = toFloat64(data[1])
		}
		if len(data) > 2 {
			flight.Longitude = toFloat64(data[2])
		}
		if len(data) > 3 {
			flight.Track = toFloat64(data[3])
		}
		if len(data) > 4 {
			flight.Altitude = toInt(data[4])
		}
		if len(data) > 5 {
			flight.GroundSpeed = toInt(data[5])
		}
		if len(data) > 6 {
			flight.SquakeCode = toString(data[6])
		}
		if len(data) > 7 {
			flight.Radar = toString(data[7])
		}
		if len(data) > 8 {
			flight.AircraftType = toString(data[8])
		}
		if len(data) > 9 {
			flight.AircraftRegistration = toString(data[9])
		}
		if len(data) > 10 {
			flight.TimeStamp = toUint64(data[10])
		}
		if len(data) > 11 {
			flight.Origin = toString(data[11])
		}
		if len(data) > 12 {
			flight.Destination = toString(data[12])
		}
		if len(data) > 13 {
			flight.FlightNumber = toString(data[13])
		}
		if len(data) > 16 {
			flight.CallSign = toString(data[16])
		}
		if len(data) > 18 {
			flight.Country = toString(data[18])
		}

		if flight.IsValid() {
			tif.FlightList = append(tif.FlightList, flight)
		}
	}
}

func toString(value any) string {
	if str, ok := value.(string); ok {
		return str
	}
	return ""
}

func toFloat64(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case float32:
		return float64(v)
	default:
		return 0
	}
}

func toInt(value any) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	case float32:
		return int(v)
	default:
		return 0
	}
}

func toUint64(value any) uint64 {
	switch v := value.(type) {
	case float64:
		return uint64(v)
	case int:
		return uint64(v)
	case int64:
		return uint64(v)
	case float32:
		return uint64(v)
	default:
		return 0
	}
}
