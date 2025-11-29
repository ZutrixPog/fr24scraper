# FlightRadar24 Scraper

A high-performance Go library for scraping real-time flight data from FlightRadar24. This library provides a clean, idiomatic Go API for accessing live flight information, trajectories, and metadata.

## Features

- **Real-time Flight Data**: Stream live flight information from FlightRadar24
- **Geographical Tiling**: Efficiently scrape large areas using configurable tile-based approach
- **Event-driven Architecture**: Receive flight data through channels with proper event types
- **Flight Trajectories**: Capture complete flight paths with timestamped coordinates
- **Metadata Support**: Access aircraft details, flight numbers, origins, destinations, and more
- **Concurrent Processing**: Multiple tiles processed simultaneously for optimal performance

## Installation

```bash
go get github.com/zutrixpog/fr24scraper
```

## Quick Start

```go
package main

import (
    "log"
    "time"
    "github.com/zutrixpog/fr24scraper"
)

func main() {
    // Create scraper with default configuration
    config := scraper.DefaultConfig()
    scraper := scraper.NewScraper(config)

    // Start scraping
    if err := scraper.Start(); err != nil {
        log.Fatal(err)
    }
    defer scraper.Stop()

    // Process flight events
    for event := range scraper.Subscribe() {
        switch event.Type {
        case scraper.EventFlightAdded:
            flight := event.Data.(*scraper.Flight)
            log.Printf("New flight: %s (%s) from %s to %s",
                flight.CallSign, flight.FlightNumber, flight.Origin, flight.Destination)

        case scraper.EventTrajectory:
            points := event.Data.([]scraper.TrajectoryPointData)
            log.Printf("Flight %d updated with %d new trajectory points",
                event.FlightID, len(points))

        case scraper.EventFlightRemoved:
            log.Printf("Flight %d removed from tracking", event.FlightID)
        }
    }
}
```

## Configuration

Customize the scraper behavior with `ScraperConfig`:

```go
config := scraper.ScraperConfig{
    Bounds:          [4]float64{40.27, 43.48, 24.95, 63.74}, // [topLeftLat, topLeftLon, bottomRightLat, bottomRightLon]
    VerticalTiles:   4,    // Number of vertical divisions
    HorizontalTiles: 4,    // Number of horizontal divisions
    UpdateInterval:  2 * time.Second, // How often to refresh data
    FetchDetails:    false, // Enable detailed flight information (rate limited)
}

scraper := scraper.NewScraper(config)
```

## API Reference

### Core Methods

- `NewScraper(config ScraperConfig) *Scraper` - Create new scraper instance
- `Start() error` - Begin scraping data
- `Stop()` - Gracefully stop the scraper
- `Subscribe() <-chan FlightEvent` - Subscribe to flight events
- `Errors() <-chan error` - Receive scraping errors

### Data Access

- `Flights() []*Flight` - Get current list of tracked flights
- `Flight(id uint64) (*Flight, error)` - Get specific flight by ID
- `Stats() Stats` - Get scraping statistics

### Event Types

- `EventFlightAdded` - New flight detected
- `EventFlightRemoved` - Flight no longer tracked
- `EventTrajectory` - New trajectory points available
- `EventFlightInfo` - Flight metadata updated
- `EventError` - Scraping error occurred

### Flight Data Structure

```go
type Flight struct {
	ID                        uint64
	InfoUpdateSequence        uint32
	ICAO24                    string
	AircraftType              string
	FlightNumber              string
	CallSign                  string
	AircraftRegistration      string
	SquakeCode                string
	Radar                     string
	Origin                    string
	Destination               string
	CountryID                 int
	ForceSendInfo             bool
	InitialTrajectoryReceived bool
	Trajectory                []TrajectoryPointData
	LastUpdateTime            int64

}

type TrajectoryPointData struct {
    TimeStamp uint64  // Unix timestamp
    Latitude  float64 // Decimal degrees
    Longitude float64 // Decimal degrees
    Altitude  int     // Feet
    Track     float64 // Heading in degrees
    Speed     float64 // Knots
}
```

## Performance Considerations

- **Tile Configuration**: More tiles = more concurrent requests but higher memory usage
- **Update Interval**: Shorter intervals provide fresher data but increase API load
- **Detail Fetching**: Fetching details is limited and might not work as expected
- **Event Buffer Size**: Default 100 events; increase if experiencing dropped events

---

**Note**: This is an unofficial library and is not affiliated with FlightRadar24. Use at your own risk and respect API rate limits.
