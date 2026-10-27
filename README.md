# FlightRadar24 Scraper

A high-performance Go library for scraping real-time flight data from FlightRadar24. This library provides a clean, idiomatic Go API for accessing live flight information, trajectories, and metadata.

## Features

- **Real-time Flight Data**: Stream live flight information from FlightRadar24's gRPC-Web feed
- **Single-request Coverage**: Fetch the whole bounding box in one request by default
- **Event-driven Architecture**: Receive flight data through channels with proper event types
- **Flight Trajectories**: Seed initial paths from the feed's position buffer, then extend them live
- **Metadata Support**: Access aircraft details, flight numbers, origins, destinations, and more
- **Anonymous Access**: Works without an FR24 account (up to 1500 flights per request)

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
    Bounds:          [4]float64{40.27, 43.48, 24.95, 63.74}, // [north, west, south, east]
    VerticalTiles:   1,    // Keep at 1: the gRPC feed limit covers the box
    HorizontalTiles: 1,    // Keep at 1: more tiles mean more requests
    UpdateInterval:    2 * time.Second, // How often to refresh data
    FetchDetails:      false, // Legacy clickhandler path; the live feed provides trails
    DetailsUpdateInterval: 30 * time.Second,
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

## How It Works

The scraper POSTs a gRPC-Web `LiveFeed` request to
`data-feed.flightradar24.com`. The request contains the bounding box, the
visible data sources and services, a 1500-flight limit, and a field mask for
`flight`, `reg`, `route`, `type`, and `icao_address`.

Each response is decoded into the same `FlightData` shape the tracker already
understands. The feed's `flightid` is the tracker's `Flight.ID` (an FR24 flight
ID, not an ICAO address). The actual ICAO24 address comes from
`extra_info.icao_address` and is exposed as `Flight.ICAO24`.

Recent position deltas are converted into an initial trajectory. Only the
oldest-to-newest position buffer is seeded once per flight; subsequent points
come from the 2-second live updates. The old `clickhandler` details endpoint is
no longer required and remains off by default.

## Performance Considerations

- **Tile Configuration**: Default is 1x1, which makes one request per update. More tiles multiply request volume.
- **Update Interval**: Shorter intervals provide fresher data but increase API load; 2 seconds matches the feed's normal cadence.
- **Detail Fetching**: The legacy clickhandler endpoint is Cloudflare-blocked for many clients; the live feed supplies position history instead.
- **Event Buffer Size**: Default 100 events; increase if experiencing dropped events.
- **Anonymous Limits**: Unauthenticated requests return at most 1500 flights and a reduced metadata set.

---

**Note**: This is an unofficial library and is not affiliated with FlightRadar24. Use at your own risk and respect API rate limits.
