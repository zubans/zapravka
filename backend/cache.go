package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	cacheTTL   = 30 * 24 * time.Hour // 1 месяц
	cacheDBPath = "./zapravka_cache.db"
)

type StationCache struct {
	mu sync.RWMutex
	db *sql.DB
}

type CachedStation struct {
	ID        string
	Lat       float64
	Lon       float64
	Name      string
	Brand     string
	Tags      map[string]string
	Source    string
	FetchedAt time.Time
}

func NewStationCache(dbPath string) (*StationCache, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	if err := initSchema(db); err != nil {
		db.Close()
		return nil, err
	}

	return &StationCache{db: db}, nil
}

func initSchema(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS stations (
			id TEXT PRIMARY KEY,
			lat REAL NOT NULL,
			lon REAL NOT NULL,
			name TEXT,
			brand TEXT,
			tags TEXT,
			source TEXT,
			fetched_at INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_stations_lat ON stations(lat);
		CREATE INDEX IF NOT EXISTS idx_stations_lon ON stations(lon);
		CREATE INDEX IF NOT EXISTS idx_stations_fetched_at ON stations(fetched_at);
	`)
	return err
}

func (c *StationCache) Save(stations []Station, source string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO stations (id, lat, lon, name, brand, tags, source, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			lat = excluded.lat,
			lon = excluded.lon,
			name = excluded.name,
			brand = excluded.brand,
			tags = excluded.tags,
			source = excluded.source,
			fetched_at = excluded.fetched_at
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().Unix()
	for _, s := range stations {
		tagsJSON, _ := json.Marshal(s.Tags)
		_, err := stmt.Exec(s.ID, s.Lat, s.Lon, s.Name, s.Brand, string(tagsJSON), source, now)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (c *StationCache) GetInRadius(lat, lon, radius float64) ([]CachedStation, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// bounding box для быстрой фильтрации (1 градус ≈ 111 км)
	delta := radius / 111000.0
	rows, err := c.db.Query(
		`SELECT id, lat, lon, name, brand, tags, source, fetched_at FROM stations
		 WHERE lat BETWEEN ? AND ? AND lon BETWEEN ? AND ?`,
		lat-delta, lat+delta, lon-delta, lon+delta,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []CachedStation
	for rows.Next() {
		var s CachedStation
		var tagsJSON string
		var fetchedAtUnix int64
		err := rows.Scan(&s.ID, &s.Lat, &s.Lon, &s.Name, &s.Brand, &tagsJSON, &s.Source, &fetchedAtUnix)
		if err != nil {
			continue
		}
		if err := json.Unmarshal([]byte(tagsJSON), &s.Tags); err != nil {
			s.Tags = make(map[string]string)
		}
		s.FetchedAt = time.Unix(fetchedAtUnix, 0)

		if haversine(lat, lon, s.Lat, s.Lon) <= radius {
			result = append(result, s)
		}
	}

	return result, rows.Err()
}

func (c *StationCache) IsFresh(lat, lon, radius float64) bool {
	stations, err := c.GetInRadius(lat, lon, radius)
	if err != nil || len(stations) == 0 {
		return false
	}
	now := time.Now()
	for _, s := range stations {
		if now.Sub(s.FetchedAt) > cacheTTL {
			return false
		}
	}
	return true
}

func (c *StationCache) Count() (int, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var count int
	err := c.db.QueryRow("SELECT COUNT(*) FROM stations").Scan(&count)
	return count, err
}

func (c *StationCache) CleanupOld() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	cutoff := time.Now().Add(-cacheTTL).Unix()
	_, err := c.db.Exec("DELETE FROM stations WHERE fetched_at < ?", cutoff)
	return err
}

func (c *StationCache) Close() error {
	return c.db.Close()
}

func haversine(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371000
	toRad := func(x float64) float64 { return x * math.Pi / 180 }
	dLat := toRad(lat2 - lat1)
	dLon := toRad(lon2 - lon1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRad(lat1))*math.Cos(toRad(lat2))*math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return R * c
}

func cachedToStation(c CachedStation) Station {
	return Station{
		ID:    c.ID,
		Lat:   c.Lat,
		Lon:   c.Lon,
		Name:  c.Name,
		Brand: c.Brand,
		Tags:  c.Tags,
	}
}

func (c *StationCache) FetchAndCache(lat, lon, radius float64) ([]Station, error) {
	stations, err := fetchStations(lat, lon, radius)
	if err != nil {
		return nil, err
	}
	if err := c.Save(stations, "dynamic"); err != nil {
		fmt.Printf("failed to save stations to cache: %v\n", err)
	}
	return stations, nil
}

func (c *StationCache) GetStations(lat, lon, radius float64) ([]Station, error) {
	// Проверяем свежесть кэша
	if c.IsFresh(lat, lon, radius) {
		cached, err := c.GetInRadius(lat, lon, radius)
		if err == nil && len(cached) > 0 {
			result := make([]Station, len(cached))
			for i, cs := range cached {
				result[i] = cachedToStation(cs)
			}
			return result, nil
		}
	}

	// Кэш отсутствует или устарел — запрашиваем из Overpass
	return c.FetchAndCache(lat, lon, radius)
}
