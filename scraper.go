package scraper

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type Tile struct {
	NumRequestSent      int
	NumSuccessFetches   int
	NumFailedFetches    int
	NumCurrentFlights   int
	NumTotalSeenFlights int
	Skips               int
	TopLeftLat          float64
	TopLeftLon          float64
	BottomRightLat      float64
	BottomRightLon      float64
	LastUpdateTime      int64
	LastRequestTime     int64
	RequestFinished     bool
}

type Stats struct {
	FlightCount int
}

type FlightEvent struct {
	Type      EventType
	FlightID  uint64
	Data      any
	Timestamp time.Time
}

type EventType int

type TileFetchRecord struct {
	Tile      Tile
	Data      *FlightData
	Timestamp time.Time
}

const (
	EventFlightInfo EventType = iota
	EventTrajectory
	EventFlightAdded
	EventFlightRemoved
	EventError
)

type ScraperConfig struct {
	Bounds          [4]float64
	VerticalTiles   int
	HorizontalTiles int
	UpdateInterval  time.Duration
	FetchDetails    bool // details api is limited
	RecordPath      string
	ReplayPath      string
	TrajectoryTail  int
}

func DefaultConfig() ScraperConfig {
	return ScraperConfig{
		// Bounds:          [4]float64{43, 38, 22, 70}, // Iran by default
		Bounds:          [4]float64{40.277757, 43.480446, 24.954477, 63.746484},
		VerticalTiles:   4,
		HorizontalTiles: 4,
		UpdateInterval:  2 * time.Second,
		FetchDetails:    false,
		TrajectoryTail:  20,
	}
}

type Scraper struct {
	config         ScraperConfig
	tracker        *Tracker
	lastInfoSeq    map[uint64]uint32
	lastTrajPoints map[uint64][]int64
	tiles          []Tile
	events         chan FlightEvent
	errors         chan error
	recordFile     *os.File
	replayFile     *os.File
	ctx            context.Context
	cancel         context.CancelFunc
}

func NewScraper(config ScraperConfig) *Scraper {
	if config.RecordPath != "" && config.ReplayPath != "" {
		panic("recordPath and replayPath are mutually exclusive")
	}

	ctx, cancel := context.WithCancel(context.Background())
	tracker := newTracker(config.TrajectoryTail)
	s := &Scraper{
		config:         config,
		tracker:        tracker,
		events:         make(chan FlightEvent, 100),
		errors:         make(chan error, 10),
		lastInfoSeq:    make(map[uint64]uint32),
		lastTrajPoints: make(map[uint64][]int64),
		ctx:            ctx,
		cancel:         cancel,
	}
	tracker.Observe(s)

	return s
}

func (s *Scraper) Subscribe() <-chan FlightEvent {
	return s.events
}

func (s *Scraper) Errors() <-chan error {
	return s.errors
}

func (s *Scraper) Start() error {
	if s.config.ReplayPath != "" {
		return s.startReplay()
	}

	if s.config.RecordPath != "" {
		f, err := os.OpenFile(s.config.RecordPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return err
		}
		s.recordFile = f
	}

	if err := s.initializeTiles(); err != nil {
		return fmt.Errorf("failed to initialize tiles: %w", err)
	}

	go s.tileUpdater()

	return nil
}

func (s *Scraper) Stop() {
	s.cancel()
	s.tracker.Close()

	if s.recordFile != nil {
		_ = s.recordFile.Close()
	}
	if s.replayFile != nil {
		_ = s.replayFile.Close()
	}

	close(s.events)
	close(s.errors)
}

func (s *Scraper) startReplay() error {
	f, err := os.Open(s.config.ReplayPath)
	if err != nil {
		return err
	}
	s.replayFile = f

	go s.replayLoop()
	return nil
}

func (s *Scraper) replayLoop() {
	for {
		if err := s.replayOnce(); err != nil {
			select {
			case s.errors <- err:
			default:
			}
		}

		select {
		case <-s.ctx.Done():
			return
		default:
		}
	}
}

func (s *Scraper) replayOnce() error {
	_, err := s.replayFile.Seek(0, 0)
	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(s.replayFile)
	var lastTS time.Time

	for scanner.Scan() {
		select {
		case <-s.ctx.Done():
			return nil
		default:
		}

		var rec TileFetchRecord
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			return err
		}

		if !lastTS.IsZero() {
			time.Sleep(rec.Timestamp.Sub(lastTS))
		}
		lastTS = rec.Timestamp

		s.applyTileData(&rec.Tile, rec.Data)
	}

	s.flushAllFlights()
	return scanner.Err()
}

func (s *Scraper) flushAllFlights() {
	flights := s.tracker.getFlightList()
	if len(flights) == 0 {
		return
	}

	ids := make([]uint64, 0, len(flights))
	for _, f := range flights {
		ids = append(ids, f.ID)
	}

	s.tracker = newTracker(s.config.TrajectoryTail)

	for _, id := range ids {
		select {
		case s.events <- FlightEvent{
			Type:      EventFlightRemoved,
			FlightID:  id,
			Data:      nil,
			Timestamp: time.Now(),
		}:
		default:
		}
	}
}

func (s *Scraper) initializeTiles() error {
	mainRect := Tile{
		TopLeftLat:     s.config.Bounds[0],
		TopLeftLon:     s.config.Bounds[1],
		BottomRightLat: s.config.Bounds[2],
		BottomRightLon: s.config.Bounds[3],
	}

	latTileSize := (mainRect.TopLeftLat - mainRect.BottomRightLat) / float64(s.config.VerticalTiles)
	lonTileSize := (mainRect.BottomRightLon - mainRect.TopLeftLon) / float64(s.config.HorizontalTiles)

	tileOverlap := 0.1
	s.tiles = make([]Tile, 0, s.config.VerticalTiles*s.config.HorizontalTiles)

	for i := range s.config.HorizontalTiles {
		tileRect := Tile{
			TopLeftLon: mainRect.TopLeftLon + float64(i)*lonTileSize - tileOverlap,
		}
		tileRect.BottomRightLon = tileRect.TopLeftLon + lonTileSize + tileOverlap
		tileRect.BottomRightLon = min(tileRect.BottomRightLon, mainRect.BottomRightLon)

		for j := range s.config.VerticalTiles {
			tileRect.BottomRightLat = mainRect.BottomRightLat + float64(j)*latTileSize - tileOverlap
			tileRect.TopLeftLat = tileRect.BottomRightLat + latTileSize + tileOverlap
			tileRect.TopLeftLat = min(mainRect.TopLeftLat, mainRect.TopLeftLat)

			tileRect.RequestFinished = true
			tileRect.LastUpdateTime = -1
			tileRect.LastRequestTime = -1
			s.tiles = append(s.tiles, tileRect)
		}
	}

	return nil
}

func (s *Scraper) emitEvent(ev FlightEvent) {
	select {
	case s.events <- ev:
	default:
	}
}

func (s *Scraper) tileUpdater() {
	ticker := time.NewTicker(s.config.UpdateInterval)
	defer ticker.Stop()

	s.updateTiles()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.updateTiles()
		}
	}
}

func (s *Scraper) updateTiles() {
	for i := range s.tiles {
		tile := &s.tiles[i]

		if !tile.RequestFinished {
			continue
		}

		currentTime := time.Now().UnixMilli()
		if tile.LastUpdateTime != -1 && currentTime-tile.LastUpdateTime < 1000 {
			continue
		}

		tile.RequestFinished = false
		tile.LastRequestTime = currentTime

		go s.processTile(tile)
	}
}

func (s *Scraper) processTile(tile *Tile) {
	defer func() {
		tile.RequestFinished = true
	}()

	flightData, err := DownloadFlightData(*tile)
	if err != nil {
		s.errors <- fmt.Errorf("tile download failed: %w", err)
		tile.NumFailedFetches++
		return
	}

	if s.recordFile != nil {
		_ = json.NewEncoder(s.recordFile).Encode(TileFetchRecord{
			Tile:      *tile,
			Data:      flightData,
			Timestamp: time.Now(),
		})
	}

	s.applyTileData(tile, flightData)
}

func (s *Scraper) applyTileData(tile *Tile, flightData *FlightData) {
	tile.LastUpdateTime = time.Now().UnixMilli()
	tile.NumSuccessFetches++

	if err := s.tracker.addFlightList(flightData); err != nil {
		s.errors <- fmt.Errorf("failed to add flight list: %w", err)
		return
	}

	stats := flightData.GetStats()
	if stats != nil {
		if visible, ok := stats["visible"].(map[string]any); ok {
			if adsb, ok := visible["ads-b"].(float64); ok {
				tile.NumCurrentFlights = int(adsb)
				tile.NumTotalSeenFlights = max(tile.NumTotalSeenFlights, tile.NumCurrentFlights)
			}
		}
	}

	if s.config.FetchDetails {
		s.requestFlightDetails(flightData)
	}
}

func (s *Scraper) requestFlightDetails(flightData *FlightData) {
	for flightID := range flightData.Flights {
		if flightID == "stats" {
			continue
		}

		id, err := hexStringToUint64(flightID)
		if err != nil {
			continue
		}

		if _, err := s.tracker.getFlight(id); err == nil {
			go s.fetchFlightDetails(id)
		}
	}
}

func (s *Scraper) fetchFlightDetails(flightID uint64) {
	trail, countryID, err := DownloadFlightDetail(flightID)
	if err != nil {
		s.errors <- fmt.Errorf("failed to fetch details for flight %d: %w", flightID, err)
		return
	}

	if err := s.tracker.addFlightDetail(flightID, trail, countryID); err != nil {
		s.errors <- fmt.Errorf("failed to add flight details for %d: %w", flightID, err)
	}
}

func (s *Scraper) Stats() Stats {
	flightCount := s.tracker.FlightCount()
	return Stats{
		FlightCount: flightCount,
	}
}

func (s *Scraper) Flights() []*Flight {
	flights := s.tracker.getFlightList()
	if len(flights) > 0 {
		return flights
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.config.UpdateInterval)
	defer cancel()

	ticker := time.NewTicker(time.Millisecond * 500)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return s.tracker.getFlightList()
		case <-ticker.C:
			flights = s.tracker.getFlightList()
			if len(flights) > 0 {
				return flights
			}
		}
	}
}

func (s *Scraper) Flight(id uint64) (*Flight, error) {
	return s.tracker.getFlight(id)
}

func (s *Scraper) OnFlightAdded(t *Flight) {
	s.emitEvent(FlightEvent{
		Type:      EventFlightAdded,
		FlightID:  t.ID,
		Data:      t,
		Timestamp: time.Now(),
	})
}

func (s *Scraper) OnFlightRemoved(removed []uint64) {
	for _, id := range removed {
		s.emitEvent(FlightEvent{
			Type:      EventFlightRemoved,
			FlightID:  id,
			Data:      nil,
			Timestamp: time.Now(),
		})
	}
}

func (s *Scraper) OnFlightUpdated(t *Flight) {
	lastPoints := s.getLastTrajPoints(t.ID)
	newPoints := t.TrajectoryPoints(lastPoints)

	if len(newPoints) > 0 {
		s.emitEvent(FlightEvent{
			Type:      EventTrajectory,
			FlightID:  t.ID,
			Data:      newPoints,
			Timestamp: time.Now(),
		})
		s.updateLastTrajPoints(t.ID, newPoints)
	}
}

func (s *Scraper) getLastInfoSeq(flightID uint64) uint32 {
	return s.lastInfoSeq[flightID]
}

func (s *Scraper) setLastInfoSeq(flightID uint64, seq uint32) {
	s.lastInfoSeq[flightID] = seq
}

func (s *Scraper) getLastTrajPoints(flightID uint64) []int64 {
	return s.lastTrajPoints[flightID]
}

func (s *Scraper) updateLastTrajPoints(flightID uint64, points []TrajectoryPointData) {
	ts := s.lastTrajPoints[flightID]
	if ts == nil {
		ts = make([]int64, 0, len(points))
	}
	for _, p := range points {
		ts = append(ts, int64(p.TimeStamp))
	}
	s.lastTrajPoints[flightID] = ts
}
