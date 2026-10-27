package scraper

import (
	"encoding/binary"
	"fmt"
	"math"
	"slices"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

func TestGRPCWebFraming(t *testing.T) {
	message := []byte{0x08, 0x01}
	framed := frameGRPCWeb(message)

	if framed[0] != 0 {
		t.Fatalf("data frame flag = %d, want 0", framed[0])
	}
	if got := binary.BigEndian.Uint32(framed[1:5]); got != uint32(len(message)) {
		t.Fatalf("frame length = %d, want %d", got, len(message))
	}

	got, err := parseGRPCWebResponse(framed)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, message) {
		t.Fatalf("decoded message = %v, want %v", got, message)
	}

	withTrailer := append(append([]byte{}, framed...), testTrailerFrame("grpc-status: 0\r\n")...)
	if _, err := parseGRPCWebResponse(withTrailer); err != nil {
		t.Fatalf("zero status trailer rejected: %v", err)
	}

	withError := append(append([]byte{}, framed...), testTrailerFrame("grpc-status: 13\r\ngrpc-message: nope\r\n")...)
	if _, err := parseGRPCWebResponse(withError); err == nil {
		t.Fatal("non-zero status trailer was accepted")
	}
}

func TestEncodeLiveFeedRequest(t *testing.T) {
	request := encodeLiveFeedRequest(40, 25, 44, 63)

	bounds := findBytesField(t, request, 1)
	values := fixed32Fields(t, bounds)
	for num, want := range map[protowire.Number]float32{1: 40, 2: 25, 3: 44, 4: 63} {
		if got := values[num]; got != want {
			t.Fatalf("bounds field %d = %v, want %v", num, got, want)
		}
	}

	settings := findBytesField(t, request, 2)
	if settings == nil {
		t.Fatal("settings field is missing")
	}
	if got, ok := varintField(t, settings, 3); !ok || got != 3 {
		t.Fatalf("traffic type = %d, want 3", got)
	}

	if got, ok := varintField(t, request, 7); !ok || got != liveFeedLimit {
		t.Fatalf("limit = %d, want %d", got, liveFeedLimit)
	}
	if got, ok := varintField(t, request, 8); !ok || got != liveFeedMaxAge {
		t.Fatalf("max age = %d, want %d", got, liveFeedMaxAge)
	}

	mask := findBytesField(t, request, 10)
	if mask == nil {
		t.Fatal("field mask is missing")
	}
	var paths []string
	for len(mask) > 0 {
		num, typ, n := protowire.ConsumeTag(mask)
		if n < 0 {
			t.Fatal(protowire.ParseError(n))
		}
		mask = mask[n:]
		if num == 1 {
			value, n := protowire.ConsumeString(mask)
			if n < 0 {
				t.Fatal(protowire.ParseError(n))
			}
			paths = append(paths, value)
			mask = mask[n:]
			continue
		}
		n = protowire.ConsumeFieldValue(num, typ, mask)
		if n < 0 {
			t.Fatal(protowire.ParseError(n))
		}
		mask = mask[n:]
	}
	if !slices.Equal(paths, liveFeedFieldMask) {
		t.Fatalf("field mask paths = %v, want %v", paths, liveFeedFieldMask)
	}
}

func TestDecodeLiveFeedAndMapping(t *testing.T) {
	const (
		flightID = uint64(0x424f99d5)
		icao     = uint32(0x896582)
	)

	var route []byte
	route = appendStringField(route, 1, "IKA")
	route = appendStringField(route, 2, "LHR")

	var extra []byte
	extra = appendStringField(extra, 1, "FR123")
	extra = appendStringField(extra, 2, "N12345")
	extra = appendMessageField(extra, 3, route)
	extra = appendStringField(extra, 4, "B738")
	extra = appendVarintField(extra, 14, uint64(icao))

	deltaLon := int32(-20000)

	var position []byte
	position = appendVarintField(position, 1, 10000)
	position = appendVarintField(position, 2, uint64(deltaLon))
	position = appendVarintField(position, 3, 2000)

	var buffer []byte
	buffer = appendMessageField(buffer, 1, position)

	var flight []byte
	flight = appendVarintField(flight, 1, flightID)
	flight = appendFixed32Field(flight, 2, 35.5)
	flight = appendFixed32Field(flight, 3, 51.5)
	flight = appendVarintField(flight, 4, 270)
	flight = appendVarintField(flight, 5, 35000)
	flight = appendVarintField(flight, 6, 450)
	flight = appendVarintField(flight, 9, 1700000000)
	flight = appendVarintField(flight, 10, 1)
	flight = appendStringField(flight, 11, "TEST123")
	flight = appendMessageField(flight, 13, extra)
	flight = appendMessageField(flight, 14, buffer)
	flight = appendVarintField(flight, 15, 1700000000000)

	var response []byte
	response = appendMessageField(response, 1, flight)

	flights, err := decodeLiveFeedResponse(response)
	if err != nil {
		t.Fatal(err)
	}
	if len(flights) != 1 {
		t.Fatalf("decoded %d flights, want 1", len(flights))
	}

	decoded := flights[0]
	if decoded.ID != flightID {
		t.Fatalf("ID = %d, want %d", decoded.ID, flightID)
	}
	if decoded.ICAOAddress != icao {
		t.Fatalf("ICAO address = %x, want %x", decoded.ICAOAddress, icao)
	}
	if !decoded.OnGround {
		t.Fatal("on-ground flag was not decoded")
	}
	if decoded.Flight != "FR123" || decoded.Reg != "N12345" || decoded.Type != "B738" {
		t.Fatalf("extra info = %+v", decoded)
	}
	if decoded.Origin != "IKA" || decoded.Destination != "LHR" {
		t.Fatalf("route = %s -> %s", decoded.Origin, decoded.Destination)
	}
	if len(decoded.Trail) != 1 {
		t.Fatalf("trail length = %d, want 1", len(decoded.Trail))
	}
	point := decoded.Trail[0]
	if point.TimeStamp != 1699999998 {
		t.Fatalf("trail timestamp = %d, want 1699999998", point.TimeStamp)
	}
	if math.Abs(point.Latitude-35.6) > 1e-6 || math.Abs(point.Longitude-51.3) > 1e-6 {
		t.Fatalf("trail position = %v,%v, want 35.6,51.3", point.Latitude, point.Longitude)
	}
	if point.Altitude != 35000 || point.Track != 270 || point.Speed != 450 {
		t.Fatalf("trail point = %+v", point)
	}

	data := convertLiveFlights(flights)
	if data.FullCount != 1 {
		t.Fatalf("full count = %d, want 1", data.FullCount)
	}
	key := fmt.Sprintf("%06x", flightID)
	array, err := data.GetFlightArray(key)
	if err != nil {
		t.Fatal(err)
	}
	if array == nil {
		t.Fatalf("flight %s is missing", key)
	}
	if array[0] != "896582" {
		t.Fatalf("ICAO24 = %v, want 896582", array[0])
	}
	if array[8] != "B738" || array[9] != "N12345" {
		t.Fatalf("type/reg = %v/%v", array[8], array[9])
	}
	if array[11] != "IKA" || array[12] != "LHR" || array[13] != "FR123" || array[16] != "TEST123" {
		t.Fatalf("mapped flight fields = %v", array)
	}
	if got := toUint64(array[10]); got != 1700000000 {
		t.Fatalf("mapped timestamp = %d, want 1700000000", got)
	}
	if len(data.Trails[flightID]) != 1 {
		t.Fatalf("mapped trail length = %d, want 1", len(data.Trails[flightID]))
	}
	stats := data.GetStats()
	visible, ok := stats["visible"].(map[string]any)
	if !ok {
		t.Fatalf("stats visible = %#v", stats)
	}
	if visible["ads-b"] != float64(1) {
		t.Fatalf("ads-b count = %v, want 1", visible["ads-b"])
	}
}

func TestAddFlightListAppliesTrailOnce(t *testing.T) {
	tracker := newTracker(20, 0)
	defer tracker.Close()

	const id = uint64(0x424f99d5)
	key := fmt.Sprintf("%06x", id)

	array := make([]any, 19)
	array[0] = "896582"
	array[1] = 35.5
	array[2] = 51.5
	array[3] = 270.0
	array[4] = 35000
	array[5] = 450
	array[10] = uint64(1700000000)
	array[16] = "TEST123"

	trail := []TrajectoryPointData{{
		TimeStamp: 1699999998,
		Latitude:  35.4,
		Longitude: 51.3,
		Altitude:  34000,
		Track:     265,
		Speed:     440,
	}}

	data := &FlightData{
		Flights: map[string]any{key: array},
		Trails:  map[uint64][]TrajectoryPointData{id: trail},
	}
	if err := tracker.addFlightList(data); err != nil {
		t.Fatal(err)
	}

	flight, err := tracker.getFlight(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(flight.Trajectory) != 2 {
		t.Fatalf("trajectory length = %d, want 2", len(flight.Trajectory))
	}
	if flight.Trajectory[0].TimeStamp != 1699999998 || flight.Trajectory[1].TimeStamp != 1700000000 {
		t.Fatalf("trajectory timestamps = %d,%d", flight.Trajectory[0].TimeStamp, flight.Trajectory[1].TimeStamp)
	}

	if err := tracker.addFlightTrail(id, trail); err != nil {
		t.Fatal(err)
	}
	if len(flight.Trajectory) != 2 {
		t.Fatalf("trail was applied twice: %d points", len(flight.Trajectory))
	}
}

func testTrailerFrame(payload string) []byte {
	frame := make([]byte, 5+len(payload))
	frame[0] = 0x80
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(payload)))
	copy(frame[5:], payload)
	return frame
}

func findBytesField(t *testing.T, b []byte, want protowire.Number) []byte {
	t.Helper()
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			t.Fatal(protowire.ParseError(n))
		}
		b = b[n:]
		if num == want {
			if typ != protowire.BytesType {
				t.Fatalf("field %d has wire type %d, want bytes", num, typ)
			}
			value, n := protowire.ConsumeBytes(b)
			if n < 0 {
				t.Fatal(protowire.ParseError(n))
			}
			return value
		}
		n = protowire.ConsumeFieldValue(num, typ, b)
		if n < 0 {
			t.Fatal(protowire.ParseError(n))
		}
		b = b[n:]
	}
	return nil
}

func fixed32Fields(t *testing.T, b []byte) map[protowire.Number]float32 {
	t.Helper()
	values := make(map[protowire.Number]float32)
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			t.Fatal(protowire.ParseError(n))
		}
		b = b[n:]
		if typ == protowire.Fixed32Type {
			value, n := protowire.ConsumeFixed32(b)
			if n < 0 {
				t.Fatal(protowire.ParseError(n))
			}
			values[num] = math.Float32frombits(value)
			b = b[n:]
			continue
		}
		n = protowire.ConsumeFieldValue(num, typ, b)
		if n < 0 {
			t.Fatal(protowire.ParseError(n))
		}
		b = b[n:]
	}
	return values
}

func varintField(t *testing.T, b []byte, want protowire.Number) (uint64, bool) {
	t.Helper()
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			t.Fatal(protowire.ParseError(n))
		}
		b = b[n:]
		if num == want {
			if typ != protowire.VarintType {
				t.Fatalf("field %d has wire type %d, want varint", num, typ)
			}
			value, n := protowire.ConsumeVarint(b)
			if n < 0 {
				t.Fatal(protowire.ParseError(n))
			}
			return value, true
		}
		n = protowire.ConsumeFieldValue(num, typ, b)
		if n < 0 {
			t.Fatal(protowire.ParseError(n))
		}
		b = b[n:]
	}
	return 0, false
}
