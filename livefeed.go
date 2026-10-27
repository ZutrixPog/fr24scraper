package scraper

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protowire"
)

const (
	liveFeedURL       = "https://data-feed.flightradar24.com/fr24.feed.api.v1.Feed/LiveFeed"
	liveFeedLimit     = 1500
	liveFeedMaxAge    = 14400
	liveFeedDeviceID  = "web-000000000-000000000000000000000"
	liveFeedPlatform  = "web-25.061.0929"
	liveFeedUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
)

var liveFeedFieldMask = []string{"flight", "reg", "route", "type", "icao_address"}

type liveFlight struct {
	ID          uint64
	Latitude    float64
	Longitude   float64
	Track       int32
	Altitude    int32
	Speed       int32
	Timestamp   uint64
	TimestampMs uint64
	OnGround    bool
	Callsign    string
	Source      int32
	ICAOAddress uint32
	Flight      string
	Reg         string
	Type        string
	Origin      string
	Destination string
	Squawk      string
	positions   []recentPosition
	Trail       []TrajectoryPointData
}

type recentPosition struct {
	deltaLat int32
	deltaLon int32
	deltaMs  uint32
}

func downloadLiveFeed(tile Tile) (*FlightData, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	body := frameGRPCWeb(encodeLiveFeedRequest(
		tile.TopLeftLat,
		tile.BottomRightLat,
		tile.TopLeftLon,
		tile.BottomRightLon,
	))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, liveFeedURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create live feed request: %w", err)
	}

	req.Header.Set("Content-Type", "application/grpc-web+proto")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("X-Grpc-Web", "1")
	req.Header.Set("X-User-Agent", "grpc-web-javascript/0.1")
	req.Header.Set("fr24-device-id", liveFeedDeviceID)
	req.Header.Set("fr24-platform", liveFeedPlatform)
	req.Header.Set("x-envoy-retry-grpc-on", "unavailable")
	req.Header.Set("DNT", "1")
	req.Header.Set("User-Agent", liveFeedUserAgent)
	req.Header.Set("Origin", "https://www.flightradar24.com")
	req.Header.Set("Referer", "https://www.flightradar24.com/")

	resp, err := HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download live feed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read live feed response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("live feed returned status %d: %s", resp.StatusCode, truncateString(string(respBody), 200))
	}

	message, err := parseGRPCWebResponse(respBody)
	if err != nil {
		return nil, err
	}

	flights, err := decodeLiveFeedResponse(message)
	if err != nil {
		return nil, fmt.Errorf("failed to decode live feed: %w", err)
	}

	return convertLiveFlights(flights), nil
}

func encodeLiveFeedRequest(north, south, west, east float64) []byte {
	var b []byte

	var bounds []byte
	bounds = appendFixed32Field(bounds, 1, float32(north))
	bounds = appendFixed32Field(bounds, 2, float32(south))
	bounds = appendFixed32Field(bounds, 3, float32(west))
	bounds = appendFixed32Field(bounds, 4, float32(east))
	b = appendMessageField(b, 1, bounds)

	var settings []byte
	var sources []byte
	for i := 0; i < 10; i++ {
		sources = protowire.AppendVarint(sources, uint64(i))
	}
	settings = appendMessageField(settings, 1, sources)

	var services []byte
	for i := 0; i < 12; i++ {
		services = protowire.AppendVarint(services, uint64(i))
	}
	settings = appendMessageField(settings, 2, services)
	settings = appendVarintField(settings, 3, 3) // TrafficType ALL
	b = appendMessageField(b, 2, settings)

	b = appendVarintField(b, 7, liveFeedLimit)
	b = appendVarintField(b, 8, liveFeedMaxAge)

	var mask []byte
	for _, path := range liveFeedFieldMask {
		mask = appendStringField(mask, 1, path)
	}
	b = appendMessageField(b, 10, mask)

	return b
}

func frameGRPCWeb(message []byte) []byte {
	frame := make([]byte, 5+len(message))
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(message)))
	copy(frame[5:], message)
	return frame
}

func parseGRPCWebResponse(body []byte) ([]byte, error) {
	var message []byte

	for len(body) > 0 {
		if len(body) < 5 {
			return nil, fmt.Errorf("truncated gRPC-Web frame")
		}

		flags := body[0]
		length := int(binary.BigEndian.Uint32(body[1:5]))
		if length < 0 || len(body) < 5+length {
			return nil, fmt.Errorf("invalid gRPC-Web frame length")
		}

		frame := body[5 : 5+length]
		body = body[5+length:]

		if flags&0x80 != 0 {
			status, message := parseGRPCStatus(frame)
			if status != "" && status != "0" {
				return nil, fmt.Errorf("gRPC-Web status %s: %s", status, message)
			}
			continue
		}

		message = append(message, frame...)
	}

	if message == nil {
		return nil, fmt.Errorf("gRPC-Web response contained no data frame")
	}

	return message, nil
}

func parseGRPCStatus(frame []byte) (string, string) {
	var status, message string
	for _, line := range strings.Split(string(frame), "\r\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "grpc-status":
			status = strings.TrimSpace(value)
		case "grpc-message":
			message = strings.TrimSpace(value)
		}
	}
	return status, message
}

func decodeLiveFeedResponse(b []byte) ([]liveFlight, error) {
	var flights []liveFlight

	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return nil, protowire.ParseError(n)
		}
		b = b[n:]

		if num == 1 && typ == protowire.BytesType {
			value, n := protowire.ConsumeBytes(b)
			if n < 0 {
				return nil, protowire.ParseError(n)
			}
			flight, err := decodeFlight(value)
			if err != nil {
				return nil, err
			}
			flights = append(flights, flight)
			b = b[n:]
			continue
		}

		n = protowire.ConsumeFieldValue(num, typ, b)
		if n < 0 {
			return nil, protowire.ParseError(n)
		}
		b = b[n:]
	}

	return flights, nil
}

func decodeFlight(b []byte) (liveFlight, error) {
	var flight liveFlight

	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return flight, protowire.ParseError(n)
		}
		b = b[n:]

		switch num {
		case 1:
			value, n := protowire.ConsumeVarint(b)
			if n < 0 {
				return flight, protowire.ParseError(n)
			}
			flight.ID = uint64(int32(value))
			b = b[n:]
		case 2:
			value, n := protowire.ConsumeFixed32(b)
			if n < 0 {
				return flight, protowire.ParseError(n)
			}
			flight.Latitude = float64(math.Float32frombits(value))
			b = b[n:]
		case 3:
			value, n := protowire.ConsumeFixed32(b)
			if n < 0 {
				return flight, protowire.ParseError(n)
			}
			flight.Longitude = float64(math.Float32frombits(value))
			b = b[n:]
		case 4:
			value, n := protowire.ConsumeVarint(b)
			if n < 0 {
				return flight, protowire.ParseError(n)
			}
			flight.Track = int32(value)
			b = b[n:]
		case 5:
			value, n := protowire.ConsumeVarint(b)
			if n < 0 {
				return flight, protowire.ParseError(n)
			}
			flight.Altitude = int32(value)
			b = b[n:]
		case 6:
			value, n := protowire.ConsumeVarint(b)
			if n < 0 {
				return flight, protowire.ParseError(n)
			}
			flight.Speed = int32(value)
			b = b[n:]
		case 9:
			value, n := protowire.ConsumeVarint(b)
			if n < 0 {
				return flight, protowire.ParseError(n)
			}
			flight.Timestamp = value
			b = b[n:]
		case 10:
			value, n := protowire.ConsumeVarint(b)
			if n < 0 {
				return flight, protowire.ParseError(n)
			}
			flight.OnGround = value != 0
			b = b[n:]
		case 11:
			value, n := protowire.ConsumeString(b)
			if n < 0 {
				return flight, protowire.ParseError(n)
			}
			flight.Callsign = value
			b = b[n:]
		case 12:
			value, n := protowire.ConsumeVarint(b)
			if n < 0 {
				return flight, protowire.ParseError(n)
			}
			flight.Source = int32(value)
			b = b[n:]
		case 13:
			value, n := protowire.ConsumeBytes(b)
			if n < 0 {
				return flight, protowire.ParseError(n)
			}
			if err := decodeExtraInfo(value, &flight); err != nil {
				return flight, err
			}
			b = b[n:]
		case 14:
			value, n := protowire.ConsumeBytes(b)
			if n < 0 {
				return flight, protowire.ParseError(n)
			}
			positions, err := decodePositionBuffer(value)
			if err != nil {
				return flight, err
			}
			flight.positions = positions
			b = b[n:]
		case 15:
			value, n := protowire.ConsumeVarint(b)
			if n < 0 {
				return flight, protowire.ParseError(n)
			}
			flight.TimestampMs = value
			b = b[n:]
		default:
			n = protowire.ConsumeFieldValue(num, typ, b)
			if n < 0 {
				return flight, protowire.ParseError(n)
			}
			b = b[n:]
		}
	}

	flight.Trail = buildLiveTrail(&flight)
	return flight, nil
}

func decodeExtraInfo(b []byte, flight *liveFlight) error {
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return protowire.ParseError(n)
		}
		b = b[n:]

		switch num {
		case 1:
			value, n := protowire.ConsumeString(b)
			if n < 0 {
				return protowire.ParseError(n)
			}
			flight.Flight = value
			b = b[n:]
		case 2:
			value, n := protowire.ConsumeString(b)
			if n < 0 {
				return protowire.ParseError(n)
			}
			flight.Reg = value
			b = b[n:]
		case 3:
			value, n := protowire.ConsumeBytes(b)
			if n < 0 {
				return protowire.ParseError(n)
			}
			if err := decodeRoute(value, flight); err != nil {
				return err
			}
			b = b[n:]
		case 4:
			value, n := protowire.ConsumeString(b)
			if n < 0 {
				return protowire.ParseError(n)
			}
			flight.Type = value
			b = b[n:]
		case 5:
			value, n := protowire.ConsumeVarint(b)
			if n < 0 {
				return protowire.ParseError(n)
			}
			if value != 0 {
				flight.Squawk = strconv.FormatInt(int64(int32(value)), 10)
			}
			b = b[n:]
		case 14:
			value, n := protowire.ConsumeVarint(b)
			if n < 0 {
				return protowire.ParseError(n)
			}
			flight.ICAOAddress = uint32(value)
			b = b[n:]
		default:
			n = protowire.ConsumeFieldValue(num, typ, b)
			if n < 0 {
				return protowire.ParseError(n)
			}
			b = b[n:]
		}
	}

	return nil
}

func decodeRoute(b []byte, flight *liveFlight) error {
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return protowire.ParseError(n)
		}
		b = b[n:]

		if num == 1 || num == 2 {
			value, n := protowire.ConsumeString(b)
			if n < 0 {
				return protowire.ParseError(n)
			}
			if num == 1 {
				flight.Origin = value
			} else {
				flight.Destination = value
			}
			b = b[n:]
			continue
		}

		n = protowire.ConsumeFieldValue(num, typ, b)
		if n < 0 {
			return protowire.ParseError(n)
		}
		b = b[n:]
	}

	return nil
}

func decodePositionBuffer(b []byte) ([]recentPosition, error) {
	var positions []recentPosition

	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return nil, protowire.ParseError(n)
		}
		b = b[n:]

		if num == 1 && typ == protowire.BytesType {
			value, n := protowire.ConsumeBytes(b)
			if n < 0 {
				return nil, protowire.ParseError(n)
			}
			position, err := decodeRecentPosition(value)
			if err != nil {
				return nil, err
			}
			positions = append(positions, position)
			b = b[n:]
			continue
		}

		n = protowire.ConsumeFieldValue(num, typ, b)
		if n < 0 {
			return nil, protowire.ParseError(n)
		}
		b = b[n:]
	}

	return positions, nil
}

func decodeRecentPosition(b []byte) (recentPosition, error) {
	var position recentPosition

	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return position, protowire.ParseError(n)
		}
		b = b[n:]

		switch num {
		case 1, 2, 3:
			value, n := protowire.ConsumeVarint(b)
			if n < 0 {
				return position, protowire.ParseError(n)
			}
			switch num {
			case 1:
				position.deltaLat = int32(value)
			case 2:
				position.deltaLon = int32(value)
			case 3:
				position.deltaMs = uint32(value)
			}
			b = b[n:]
		default:
			n = protowire.ConsumeFieldValue(num, typ, b)
			if n < 0 {
				return position, protowire.ParseError(n)
			}
			b = b[n:]
		}
	}

	return position, nil
}

func buildLiveTrail(flight *liveFlight) []TrajectoryPointData {
	if len(flight.positions) == 0 {
		return nil
	}

	currentMs := flight.TimestampMs
	if currentMs == 0 {
		currentMs = flight.Timestamp * 1000
	}
	if currentMs == 0 {
		return nil
	}

	points := make([]TrajectoryPointData, 0, len(flight.positions))
	for _, position := range flight.positions {
		if position.deltaMs == 0 || uint64(position.deltaMs) >= currentMs {
			continue
		}

		points = append(points, TrajectoryPointData{
			TimeStamp: (currentMs - uint64(position.deltaMs)) / 1000,
			Latitude:  flight.Latitude + float64(position.deltaLat)/1e5,
			Longitude: flight.Longitude + float64(position.deltaLon)/1e5,
			Altitude:  int(flight.Altitude),
			Track:     float64(flight.Track),
			Speed:     float64(flight.Speed),
		})
	}

	sort.SliceStable(points, func(i, j int) bool {
		return points[i].TimeStamp < points[j].TimeStamp
	})

	return points
}

func convertLiveFlights(flights []liveFlight) *FlightData {
	data := &FlightData{
		FullCount: len(flights),
		Flights:   make(map[string]any, len(flights)+1),
		Trails:    make(map[uint64][]TrajectoryPointData),
	}

	adsbCount := 0
	for _, flight := range flights {
		if flight.ID == 0 {
			continue
		}

		key := fmt.Sprintf("%06x", flight.ID)
		timestamp := flight.TimestampMs / 1000
		if timestamp == 0 {
			timestamp = flight.Timestamp
		}
		if timestamp == 0 {
			timestamp = uint64(time.Now().Unix())
		}

		array := make([]any, 19)
		if flight.ICAOAddress != 0 {
			array[0] = fmt.Sprintf("%06x", flight.ICAOAddress)
		}
		array[1] = flight.Latitude
		array[2] = flight.Longitude
		array[3] = float64(flight.Track)
		array[4] = int(flight.Altitude)
		array[5] = int(flight.Speed)
		array[6] = flight.Squawk
		array[7] = sourceName(flight.Source)
		array[8] = flight.Type
		array[9] = flight.Reg
		array[10] = timestamp
		array[11] = flight.Origin
		array[12] = flight.Destination
		array[13] = flight.Flight
		array[16] = flight.Callsign
		array[18] = ""

		data.Flights[key] = array
		if len(flight.Trail) > 0 {
			data.Trails[flight.ID] = flight.Trail
		}
		if flight.Source == 0 {
			adsbCount++
		}
	}

	data.Flights["stats"] = map[string]any{
		"visible": map[string]any{"ads-b": float64(adsbCount)},
	}

	return data
}

func sourceName(source int32) string {
	switch source {
	case 0:
		return "ADSB"
	case 1:
		return "MLAT"
	case 2:
		return "FLARM"
	case 3:
		return "FAA"
	case 4:
		return "ESTIMATED"
	case 5:
		return "SATELLITE"
	case 6:
		return "OTHER"
	case 7:
		return "UAT"
	case 8:
		return "SPIDERTRACKS"
	case 9:
		return "AUS"
	case 10:
		return "AIERON"
	default:
		return ""
	}
}

func appendMessageField(b []byte, num protowire.Number, value []byte) []byte {
	b = protowire.AppendTag(b, num, protowire.BytesType)
	return protowire.AppendBytes(b, value)
}

func appendStringField(b []byte, num protowire.Number, value string) []byte {
	b = protowire.AppendTag(b, num, protowire.BytesType)
	return protowire.AppendString(b, value)
}

func appendVarintField(b []byte, num protowire.Number, value uint64) []byte {
	b = protowire.AppendTag(b, num, protowire.VarintType)
	return protowire.AppendVarint(b, value)
}

func appendFixed32Field(b []byte, num protowire.Number, value float32) []byte {
	b = protowire.AppendTag(b, num, protowire.Fixed32Type)
	return protowire.AppendFixed32(b, math.Float32bits(value))
}

func truncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
