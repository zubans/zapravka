package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	voteTTL      = 3 * time.Hour
	cleanupEvery = 5 * time.Minute
)

type VoteType string

const (
	VoteYes VoteType = "yes"
	VoteNo  VoteType = "no"
)

type FuelType string

const (
	Fuel92     FuelType = "92"
	Fuel95     FuelType = "95"
	FuelDiesel FuelType = "diesel"
)

var validFuelTypes = map[FuelType]bool{
	Fuel92:     true,
	Fuel95:     true,
	FuelDiesel: true,
}

type Vote struct {
	StationID string    `json:"station_id"`
	FuelType  FuelType  `json:"fuel_type"`
	Type      VoteType  `json:"type"`
	CreatedAt time.Time `json:"created_at"`
}

type VoteCounts struct {
	Yes int `json:"yes"`
	No  int `json:"no"`
}

type StationCounts map[FuelType]VoteCounts

type VoteStore struct {
	mu    sync.RWMutex
	votes map[string][]Vote // station id -> votes
}

func NewVoteStore() *VoteStore {
	return &VoteStore{
		votes: make(map[string][]Vote),
	}
}

func (s *VoteStore) Add(stationID string, fuel FuelType, vt VoteType) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.votes[stationID] = append(s.votes[stationID], Vote{
		StationID: stationID,
		FuelType:  fuel,
		Type:      vt,
		CreatedAt: time.Now(),
	})
}

func (s *VoteStore) Counts(stationID string) StationCounts {
	s.mu.RLock()
	defer s.mu.RUnlock()
	counts := make(StationCounts)
	now := time.Now()
	for _, v := range s.votes[stationID] {
		if now.Sub(v.CreatedAt) > voteTTL {
			continue
		}
		c := counts[v.FuelType]
		switch v.Type {
		case VoteYes:
			c.Yes++
		case VoteNo:
			c.No++
		}
		counts[v.FuelType] = c
	}
	return counts
}

func (s *VoteStore) CountsMany(ids []string) map[string]StationCounts {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]StationCounts, len(ids))
	now := time.Now()
	for _, id := range ids {
		counts := make(StationCounts)
		for _, v := range s.votes[id] {
			if now.Sub(v.CreatedAt) > voteTTL {
				continue
			}
			c := counts[v.FuelType]
			switch v.Type {
			case VoteYes:
				c.Yes++
			case VoteNo:
				c.No++
			}
			counts[v.FuelType] = c
		}
		result[id] = counts
	}
	return result
}

func (s *VoteStore) Cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for sid, list := range s.votes {
		newList := make([]Vote, 0, len(list))
		for _, v := range list {
			if now.Sub(v.CreatedAt) <= voteTTL {
				newList = append(newList, v)
			}
		}
		if len(newList) == 0 {
			delete(s.votes, sid)
		} else {
			s.votes[sid] = newList
		}
	}
}

func (s *VoteStore) StartCleanup() {
	go func() {
		for {
			time.Sleep(cleanupEvery)
			s.Cleanup()
		}
	}()
}

type OSMElement struct {
	Type string            `json:"type"`
	ID   int64             `json:"id"`
	Lat  float64           `json:"lat"`
	Lon  float64           `json:"lon"`
	Tags map[string]string `json:"tags"`
}

type OSMResponse struct {
	Elements []OSMElement `json:"elements"`
}

type Station struct {
	ID     string            `json:"id"`
	Lat    float64           `json:"lat"`
	Lon    float64           `json:"lon"`
	Name   string            `json:"name"`
	Brand  string            `json:"brand"`
	Tags   map[string]string `json:"tags"`
	Counts StationCounts     `json:"counts"`
}

type App struct {
	store *VoteStore
	cache *StationCache
}

func enableCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (a *App) handleStations(w http.ResponseWriter, r *http.Request) {
	enableCORS(w)
	if r.Method == http.MethodOptions {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	lat, err := strconv.ParseFloat(r.URL.Query().Get("lat"), 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid lat"})
		return
	}
	lon, err := strconv.ParseFloat(r.URL.Query().Get("lon"), 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid lon"})
		return
	}
	radius := 5000.0
	if rr := r.URL.Query().Get("radius"); rr != "" {
		if v, err := strconv.ParseFloat(rr, 64); err == nil {
			radius = v
		}
	}

	stations, err := a.cache.GetStations(lat, lon, radius)
	if err != nil {
		log.Printf("get stations error: %v", err)
		stations = []Station{}
	}

	ids := make([]string, len(stations))
	for i, s := range stations {
		ids[i] = s.ID
	}
	counts := a.store.CountsMany(ids)
	for i := range stations {
		stations[i].Counts = counts[stations[i].ID]
	}

	writeJSON(w, http.StatusOK, map[string]any{"stations": stations})
}

var httpClient = &http.Client{
	Timeout: 10 * time.Second,
}

var overpassEndpoints = []string{
	"https://overpass-api.de/api/interpreter",
	"https://lz4.overpass-api.de/api/interpreter",
	"https://z.overpass-api.de/api/interpreter",
	"https://overpass.kumi.systems/api/interpreter",
}

func fetchStations(lat, lon, radius float64) ([]Station, error) {
	query := fmt.Sprintf(`[out:json];node["amenity"="fuel"](around:%.0f,%f,%f);out;`, radius, lat, lon)

	var lastErr error
	for _, base := range overpassEndpoints {
		u := base + "?data=" + url.QueryEscape(query)
		req, err := http.NewRequest("GET", u, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("User-Agent", "zapravka/1.0")
		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("overpass status %d: %s", resp.StatusCode, string(body))
			continue
		}
		var osm OSMResponse
		if err := json.Unmarshal(body, &osm); err != nil {
			lastErr = err
			continue
		}
		return parseOSM(osm), nil
	}
	return nil, lastErr
}

func parseOSM(osm OSMResponse) []Station {

	stations := make([]Station, 0, len(osm.Elements))
	for _, e := range osm.Elements {
		if e.Type != "node" {
			continue
		}
		name := e.Tags["name"]
		if name == "" {
			name = e.Tags["brand"]
		}
		if name == "" {
			name = "Заправка"
		}
		id := strconv.FormatInt(e.ID, 10)
		stations = append(stations, Station{
			ID:    id,
			Lat:   e.Lat,
			Lon:   e.Lon,
			Name:  name,
			Brand: e.Tags["brand"],
			Tags:  e.Tags,
		})
	}
	return stations
}

func (a *App) handleCacheInfo(w http.ResponseWriter, r *http.Request) {
	enableCORS(w)
	if r.Method == http.MethodOptions {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	count, err := a.cache.Count()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"cache_path": getCacheDBPath(),
		"total":      count,
	})
}

func (a *App) handleVotes(w http.ResponseWriter, r *http.Request) {
	enableCORS(w)
	if r.Method == http.MethodOptions {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	idsParam := r.URL.Query().Get("ids")
	if idsParam == "" {
		writeJSON(w, http.StatusOK, map[string]any{"counts": map[string]StationCounts{}})
		return
	}

	ids := strings.Split(idsParam, ",")
	counts := a.store.CountsMany(ids)

	writeJSON(w, http.StatusOK, map[string]any{"counts": counts})
}

func (a *App) handleVote(w http.ResponseWriter, r *http.Request) {
	enableCORS(w)
	if r.Method == http.MethodOptions {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		StationID string `json:"station_id"`
		FuelType  string `json:"fuel_type"`
		Type      string `json:"type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if req.StationID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "station_id required"})
		return
	}

	fuel := FuelType(strings.ToLower(req.FuelType))
	if !validFuelTypes[fuel] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "fuel_type must be 92, 95 or diesel"})
		return
	}

	var vt VoteType
	switch strings.ToLower(req.Type) {
	case "yes", "есть", "fuel":
		vt = VoteYes
	case "no", "нет", "empty":
		vt = VoteNo
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "type must be yes or no"})
		return
	}

	a.store.Add(req.StationID, fuel, vt)
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"counts":  a.store.Counts(req.StationID),
	})
}

func main() {
	var (
		populate = flag.Bool("populate", false, "Ручное заполнение кэша заправок")
		lat      = flag.Float64("lat", 0, "Широта точки")
		lon      = flag.Float64("lon", 0, "Долгота точки")
		radius   = flag.Float64("radius", 50000, "Радиус в метрах")
		name     = flag.String("name", "", "Название точки")
		all      = flag.Bool("all", false, "Заполнить все предопределённые точки")
	)
	flag.Parse()

	cache, err := NewStationCache(getCacheDBPath())
	if err != nil {
		log.Fatalf("failed to open station cache: %v", err)
	}
	defer cache.Close()

	if n, err := cache.DeleteDemoStations(); err == nil && n > 0 {
		log.Printf("Removed %d old demo/test stations from cache", n)
	}

	if *populate {
		app := &App{cache: cache}

		if (*lat != 0 || *lon != 0) && !*all {
			if *name == "" {
				*name = fmt.Sprintf("%.4f, %.4f", *lat, *lon)
			}
			app.PopulatePoint(*lat, *lon, *radius, *name)
		} else {
			app.PopulateAll(*radius)
		}
		return
	}

	store := NewVoteStore()
	store.StartCleanup()

	app := &App{store: store, cache: cache}

	// Фоновое предзаполнение кэша для городов и трасс
	app.startPreseed()

	// Очистка устаревших записей раз в сутки
	go func() {
		for {
			time.Sleep(24 * time.Hour)
			if err := cache.CleanupOld(); err != nil {
				log.Printf("cache cleanup error: %v", err)
			}
		}
	}()

	http.HandleFunc("/api/stations", app.handleStations)
	http.HandleFunc("/api/vote", app.handleVote)
	http.HandleFunc("/api/votes", app.handleVotes)
	http.HandleFunc("/api/cache/info", app.handleCacheInfo)

	count, _ := cache.Count()
	log.Printf("Station cache path: %s, total stations: %d", getCacheDBPath(), count)

	host := os.Getenv("HOST")
	if host == "" {
		host = "0.0.0.0"
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}
	addr := host + ":" + port
	log.Printf("Server listening on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}
