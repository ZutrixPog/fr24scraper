package main

import (
	"log"

	scraper "github.com/zutrixpog/fr24scraper"
)

func main() {
	config := scraper.DefaultConfig()
	scraper := scraper.NewScraper(config)

	if err := scraper.Start(); err != nil {
		log.Fatalf("Failed to start scraper: %v", err)
	}
	defer scraper.Stop()

	log.Println("FlightRadar24 Scraper started. Press Ctrl+C to stop.")

	for {
		select {
		case event, ok := <-scraper.Subscribe():
			if !ok {
				log.Println("Event channel closed")
				return
			}
			handleEvent(event)

		case err, ok := <-scraper.Errors():
			if !ok {
				log.Println("Error channel closed")
				return
			}
			log.Printf("Error: %v", err)
		}
	}
}

func handleEvent(event scraper.FlightEvent) {
	switch event.Type {
	case scraper.EventFlightInfo:
		if flight, ok := event.Data.(*scraper.Flight); ok {
			log.Printf("Flight Info: ID=%d, Callsign=%s, Flight=%s, Type=%s",
				flight.ID, flight.CallSign, flight.FlightNumber, flight.AircraftType)
		}

	case scraper.EventTrajectory:
		if points, ok := event.Data.([]scraper.TrajectoryPointData); ok {
			log.Printf("Trajectory: Flight=%d, Points=%d", event.FlightID, len(points))
		}

	case scraper.EventFlightAdded:
		if flight, ok := event.Data.(*scraper.Flight); ok {
			log.Printf("Flight Added: %s", flight.CallSign)
		}

	case scraper.EventFlightRemoved:
		log.Printf("Flight Removed: %d", event.FlightID)

	case scraper.EventError:
		log.Printf("Error Event: %v", event.Data)
	}
}
