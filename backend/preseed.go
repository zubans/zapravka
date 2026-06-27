package main

import (
	"log"
	"time"
)

// PreseedPoint описывает область, которую нужно предзаполнить заправками

type PreseedPoint struct {
	Name   string
	Lat    float64
	Lon    float64
	Radius float64
}

// preseedPoints — города и участки трасс, для которых нужен серверный кэш
var preseedPoints = []PreseedPoint{
	{Name: "Москва", Lat: 55.7558, Lon: 37.6173, Radius: 25000},
	{Name: "Курск", Lat: 51.7304, Lon: 36.1939, Radius: 15000},
	{Name: "Орёл", Lat: 52.9685, Lon: 36.0696, Radius: 15000},
	{Name: "Воронеж", Lat: 51.6755, Lon: 39.2089, Radius: 20000},
	{Name: "Ростов-на-Дону", Lat: 47.2225, Lon: 39.7187, Radius: 20000},
	{Name: "Обоянь", Lat: 51.2104, Lon: 36.2756, Radius: 15000},
	{Name: "Краснодар", Lat: 45.0393, Lon: 38.9872, Radius: 20000},
	{Name: "Ставрополь", Lat: 45.0445, Lon: 41.9691, Radius: 15000},
	{Name: "Черкесск", Lat: 44.2269, Lon: 42.0468, Radius: 15000},
	// Краснодарский край — несколько точек
	{Name: "Краснодарский край (Анапа)", Lat: 44.8948, Lon: 37.3165, Radius: 20000},
	{Name: "Краснодарский край (Сочи)", Lat: 43.6028, Lon: 39.7342, Radius: 25000},
	{Name: "Краснодарский край (Новороссийск)", Lat: 44.7235, Lon: 37.7686, Radius: 15000},
	// Трасса М4 "Дон" — ключевые точки вдоль трассы
	{Name: "М4 Дон (Москва-Видное)", Lat: 55.5513, Lon: 37.7084, Radius: 10000},
	{Name: "М4 Дон (Кашира)", Lat: 54.8340, Lon: 38.1529, Radius: 10000},
	{Name: "М4 Дон (Серпухов)", Lat: 54.9130, Lon: 37.4118, Radius: 10000},
	{Name: "М4 Дон (Тула)", Lat: 54.1931, Lon: 37.6174, Radius: 12000},
	{Name: "М4 Дон (Плавск)", Lat: 53.7096, Lon: 37.2900, Radius: 10000},
	{Name: "М4 Дон (Ефремов)", Lat: 53.1467, Lon: 38.0928, Radius: 10000},
	{Name: "М4 Дон (Курск)", Lat: 51.7304, Lon: 36.1939, Radius: 12000},
	{Name: "М4 Дон (Обоянь)", Lat: 51.2104, Lon: 36.2756, Radius: 10000},
	{Name: "М4 Дон (Строитель)", Lat: 50.7846, Lon: 36.4832, Radius: 10000},
	{Name: "М4 Дон (Воронеж)", Lat: 51.6755, Lon: 39.2089, Radius: 15000},
	{Name: "М4 Дон (Богучар)", Lat: 49.9358, Lon: 40.5594, Radius: 10000},
	{Name: "М4 Дон (Каменск-Шахтинский)", Lat: 48.3196, Lon: 40.2684, Radius: 12000},
	{Name: "М4 Дон (Ростов-на-Дону)", Lat: 47.2225, Lon: 39.7187, Radius: 15000},
	{Name: "М4 Дон (Аксай)", Lat: 47.2640, Lon: 39.8620, Radius: 10000},
	{Name: "М4 Дон (Краснодар)", Lat: 45.0393, Lon: 38.9872, Radius: 15000},
}

func (a *App) PopulatePoint(lat, lon, radius float64, name string) {
	log.Printf("Populating cache for %s (lat=%.4f, lon=%.4f, radius=%.0f m)...", name, lat, lon, radius)
	stations, err := a.cache.FetchAndCache(lat, lon, radius)
	if err != nil {
		log.Printf("Failed to populate %s: %v", name, err)
		return
	}
	log.Printf("Populated %s: %d stations", name, len(stations))
}

func (a *App) PopulateAll(radius float64) {
	log.Printf("Populating cache for all predefined points with radius %.0f m...", radius)
	start := time.Now()
	for _, p := range preseedPoints {
		a.PopulatePoint(p.Lat, p.Lon, radius, p.Name)
		time.Sleep(500 * time.Millisecond)
	}
	count, _ := a.cache.Count()
	log.Printf("Populate all complete in %v. Total cached stations: %d", time.Since(start), count)
}

func (a *App) preseedCache() {
	log.Println("Starting cache preseed...")
	start := time.Now()

	for _, p := range preseedPoints {
		if a.cache.IsFresh(p.Lat, p.Lon, p.Radius) {
			log.Printf("Preseed area is fresh: %s", p.Name)
			continue
		}

		log.Printf("Fetching stations for preseed area: %s", p.Name)
		stations, err := a.cache.FetchAndCache(p.Lat, p.Lon, p.Radius)
		if err != nil {
			log.Printf("Failed to preseed %s: %v", p.Name, err)
			continue
		}
		log.Printf("Preseeded %s: %d stations", p.Name, len(stations))

		// Небольшая задержка, чтобы не перегружать Overpass API
		time.Sleep(500 * time.Millisecond)
	}

	count, _ := a.cache.Count()
	log.Printf("Preseed complete in %v. Total cached stations: %d", time.Since(start), count)
}

func (a *App) startPreseed() {
	go a.preseedCache()
}
