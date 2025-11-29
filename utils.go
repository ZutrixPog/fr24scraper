package scraper

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

var HTTPClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: false,
		},
		MaxIdleConns:        100,
		MaxConnsPerHost:     100,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
	},
}

func DownloadFlightData(tile Tile) (*FlightData, error) {
	url := fmt.Sprintf("http://data-cloud.flightradar24.com/zones/fcgi/feed.js?bounds=%.6f,%.6f,%.6f,%.6f&satellite=1&mlat=1&flarm=1&adsb=1&gnd=1&air=1&gliders=0&vehicles=0&estimated=0&maxage=0&stats=1",
		tile.TopLeftLat, tile.BottomRightLat, tile.TopLeftLon, tile.BottomRightLon)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// mimic browser request
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Referer", "https://www.flightradar24.com/")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	resp, err := HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download flight data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var flightData FlightData
	if err := json.Unmarshal(body, &flightData); err != nil {
		return nil, fmt.Errorf("failed to parse flight data JSON: %w", err)
	}

	return &flightData, nil
}

func ConvertFlightArrayToFlightData(flightID string, array []any) (*ReceivedFlightData, error) {
	flightData := &ReceivedFlightData{}

	id, err := hexStringToUint64(flightID)
	if err != nil {
		return nil, err
	}
	flightData.ID = id

	if len(array) > 0 {
		flightData.ICAO24 = toString(array[0])
	}
	if len(array) > 1 {
		flightData.Latitude = toFloat64(array[1])
	}
	if len(array) > 2 {
		flightData.Longitude = toFloat64(array[2])
	}
	if len(array) > 3 {
		flightData.Track = toFloat64(array[3])
	}
	if len(array) > 4 {
		flightData.Altitude = toInt(array[4])
	}
	if len(array) > 5 {
		flightData.GroundSpeed = toInt(array[5])
	}
	if len(array) > 6 {
		flightData.SquakeCode = toString(array[6])
	}
	if len(array) > 7 {
		flightData.Radar = toString(array[7])
	}
	if len(array) > 8 {
		flightData.AircraftType = toString(array[8])
	}
	if len(array) > 9 {
		flightData.AircraftRegistration = toString(array[9])
	}
	if len(array) > 10 {
		flightData.TimeStamp = toUint64(array[10])
	}
	if len(array) > 11 {
		flightData.Origin = toString(array[11])
	}
	if len(array) > 12 {
		flightData.Destination = toString(array[12])
	}
	if len(array) > 13 {
		flightData.FlightNumber = toString(array[13])
	}
	if len(array) > 16 {
		flightData.CallSign = toString(array[16])
	}
	if len(array) > 18 {
		flightData.Country = toString(array[18])
	}

	return flightData, nil
}

func DownloadFlightDetail(flightID uint64) ([]map[string]any, int, error) {
	url := fmt.Sprintf("http://data-live.flightradar24.com/clickhandler/?version=1.5&flight=%x", flightID)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create detail request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Referer", "https://www.flightradar24.com/")

	resp, err := HTTPClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to download flight detail: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read detail response: %w", err)
	}

	var detailData map[string]any
	if err := json.Unmarshal(body, &detailData); err != nil {
		return nil, 0, fmt.Errorf("failed to parse detail JSON: %w", err)
	}

	trail, _ := detailData["trail"].([]any)
	countryID := 0

	if aircraft, ok := detailData["aircraft"].(map[string]any); ok {
		if id, ok := aircraft["countryId"].(float64); ok {
			countryID = int(id)
		}
	}

	trailMaps := make([]map[string]any, 0, len(trail))
	for _, point := range trail {
		if pointMap, ok := point.(map[string]any); ok {
			trailMaps = append(trailMaps, pointMap)
		}
	}

	return trailMaps, countryID, nil
}

func RetryWithBackoff(maxRetries int, fn func() error) error {
	var err error
	for i := range maxRetries {
		if err = fn(); err == nil {
			return nil
		}
		if i < maxRetries-1 {
			backoff := time.Duration(i*i) * time.Second
			time.Sleep(backoff)
		}
	}
	return fmt.Errorf("failed after %d retries: %w", maxRetries, err)
}
