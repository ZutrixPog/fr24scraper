package scraper

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

type FlightObserver interface {
	OnFlightAdded(*Flight)
	OnFlightRemoved([]uint64)
	OnFlightUpdated(*Flight)
}

type Tracker struct {
	flights   map[uint64]*Flight
	observers []FlightObserver
	trajLimit int
	mutex     sync.RWMutex
	stopChan  chan struct{}
	wg        sync.WaitGroup
}

func newTracker(trajLimit int) *Tracker {
	tracker := &Tracker{
		flights:   make(map[uint64]*Flight),
		observers: make([]FlightObserver, 0),
		trajLimit: trajLimit,
		stopChan:  make(chan struct{}),
	}

	tracker.wg.Add(1)
	go tracker.cleanup()

	return tracker
}

func (t *Tracker) Reset() {
	t.flights = make(map[uint64]*Flight)
}

func (t *Tracker) Close() {
	close(t.stopChan)
	t.wg.Wait()
}

func (t *Tracker) cleanup() {
	defer t.wg.Done()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-t.stopChan:
			return
		case <-ticker.C:
			t.removeStaleFlights()
		}
	}
}

func (t *Tracker) Observe(obs FlightObserver) {
	t.observers = append(t.observers, obs)
}

func (t *Tracker) removeStaleFlights() {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	currentTime := time.Now().Unix()
	cutoffTime := currentTime - 600

	var removedFlights []uint64

	for id, flight := range t.flights {
		if flight.LastUpdateTime < cutoffTime {
			removedFlights = append(removedFlights, id)
		}
	}

	for _, id := range removedFlights {
		delete(t.flights, id)
	}
	for _, obs := range t.observers {
		obs.OnFlightRemoved(removedFlights)
	}
}

func (t *Tracker) addOrUpdateFlight(flightData *ReceivedFlightData) error {
	if flightData == nil {
		return errors.New("flight data is nil")
	}

	if !flightData.IsValid() {
		return errors.New("flight data is not valid")
	}

	t.mutex.Lock()
	defer t.mutex.Unlock()

	if existingFlight, exists := t.flights[flightData.ID]; exists {
		return t.updateExistingFlight(existingFlight, flightData)
	} else {
		return t.addNewFlight(flightData)
	}
}

func (t *Tracker) updateExistingFlight(existingFlight *Flight, flightData *ReceivedFlightData) error {
	if flightData.TimeStamp < existingFlight.GetTimeStamp() {
		return nil
		// return errors.New("flight data timestamp is not newer than existing flight")
	}

	infoChanged := flightData.ICAO24 != existingFlight.ICAO24 ||
		flightData.Origin != existingFlight.Origin ||
		flightData.Destination != existingFlight.Destination ||
		flightData.CallSign != existingFlight.CallSign ||
		flightData.FlightNumber != existingFlight.FlightNumber ||
		flightData.SquakeCode != existingFlight.SquakeCode ||
		flightData.AircraftType != existingFlight.AircraftType ||
		flightData.AircraftRegistration != existingFlight.AircraftRegistration

	if infoChanged {
		existingFlight.Radar = flightData.Radar
		existingFlight.ICAO24 = flightData.ICAO24
		existingFlight.Origin = flightData.Origin
		existingFlight.CallSign = flightData.CallSign
		existingFlight.SquakeCode = flightData.SquakeCode
		existingFlight.Destination = flightData.Destination
		existingFlight.AircraftType = flightData.AircraftType
		existingFlight.FlightNumber = flightData.FlightNumber
		existingFlight.AircraftRegistration = flightData.AircraftRegistration
		existingFlight.LastUpdateTime = time.Now().Unix()
		existingFlight.InfoUpdateSequence++
		return nil
	}

	existingFlight.SetCoordinate(
		flightData.TimeStamp,
		flightData.Latitude,
		flightData.Longitude,
		flightData.Altitude,
		flightData.Track,
		float64(flightData.GroundSpeed),
		t.trajLimit,
	)

	for _, obs := range t.observers {
		obs.OnFlightUpdated(existingFlight)
	}
	return nil
}

func (t *Tracker) addNewFlight(flightData *ReceivedFlightData) error {
	newFlight := &Flight{
		ID:                   flightData.ID,
		Radar:                flightData.Radar,
		ICAO24:               flightData.ICAO24,
		Origin:               flightData.Origin,
		CallSign:             flightData.CallSign,
		SquakeCode:           flightData.SquakeCode,
		Destination:          flightData.Destination,
		AircraftType:         flightData.AircraftType,
		FlightNumber:         flightData.FlightNumber,
		AircraftRegistration: flightData.AircraftRegistration,
		LastUpdateTime:       time.Now().Unix(),
	}

	newFlight.SetCoordinate(
		flightData.TimeStamp,
		flightData.Latitude,
		flightData.Longitude,
		flightData.Altitude,
		flightData.Track,
		float64(flightData.GroundSpeed),
		t.trajLimit,
	)
	for _, obs := range t.observers {
		obs.OnFlightAdded(newFlight)
	}

	t.flights[newFlight.ID] = newFlight
	return nil
}

func (t *Tracker) addFlightDetail(id uint64, detail []map[string]any, countryID int) error {
	if id == 0 {
		return errors.New("flight ID is not valid")
	}

	t.mutex.Lock()
	defer t.mutex.Unlock()

	flight, exists := t.flights[id]
	if !exists {
		return fmt.Errorf("flight with ID %d does not exist", id)
	}

	flight.CountryID = countryID
	flight.InfoUpdateSequence++
	flight.LastUpdateTime = time.Now().Unix()

	for _, pointDetail := range detail {
		latitude := toFloat64(pointDetail["lat"])
		longitude := toFloat64(pointDetail["lng"])
		altitude := toInt(pointDetail["alt"])
		heading := toFloat64(pointDetail["hd"])
		speed := toFloat64(pointDetail["spd"])
		timestamp := toUint64(pointDetail["ts"])

		flight.SetCoordinate(timestamp, latitude, longitude, altitude, heading, speed, t.trajLimit)
	}

	flight.InitialTrajectoryReceived = true
	flight.ForceSendInfo = true

	return nil
}

func (t *Tracker) addFlightList(flightData *FlightData) error {
	if flightData == nil {
		return errors.New("flight data is nil")
	}

	var errorsList []error

	for flightID, value := range flightData.Flights {
		if flightID == "stats" {
			continue
		}

		array, ok := value.([]any)
		if !ok {
			errorsList = append(errorsList, fmt.Errorf("invalid data format for flight ID %s", flightID))
			continue
		}

		flightData, err := ConvertFlightArrayToFlightData(flightID, array)
		if err != nil {
			errorsList = append(errorsList, fmt.Errorf("failed to convert flight data for %s: %w", flightID, err))
			continue
		}

		if !flightData.IsValid() {
			errorsList = append(errorsList, fmt.Errorf("invalid flight data for flight ID %s", flightID))
			continue
		}

		if err := t.addOrUpdateFlight(flightData); err != nil {
			errorsList = append(errorsList, fmt.Errorf("failed to add/update flight %s: %w", flightID, err))
		}
	}

	if len(errorsList) > 0 {
		return fmt.Errorf("failed to process some flights: %v", errorsList)
	}
	return nil
}

func (t *Tracker) getFlightKeys() []uint64 {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	keys := make([]uint64, 0, len(t.flights))
	for key := range t.flights {
		keys = append(keys, key)
	}
	return keys
}

func (t *Tracker) getFlightList() []*Flight {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	flights := make([]*Flight, 0, len(t.flights))
	for _, flight := range t.flights {
		flights = append(flights, flight)
	}
	return flights
}

func (t *Tracker) getFlight(id uint64) (*Flight, error) {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	flight, exists := t.flights[id]
	if !exists {
		return nil, fmt.Errorf("flight with ID %d not found", id)
	}
	return flight, nil
}

func (t *Tracker) FlightCount() int {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	return len(t.flights)
}

func hexStringToUint64(hexStr string) (uint64, error) {
	var result uint64
	_, err := fmt.Sscanf(hexStr, "%x", &result)
	if err != nil {
		return 0, fmt.Errorf("failed to parse hex string %s: %w", hexStr, err)
	}
	return result, nil
}
