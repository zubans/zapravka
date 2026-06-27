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
	// Москва и Подмосковье
	{Name: "Москва", Lat: 55.7558, Lon: 37.6173, Radius: 50000},
	{Name: "Видное", Lat: 55.5574, Lon: 37.7082, Radius: 10000},
	{Name: "Домодедово", Lat: 55.4371, Lon: 37.7680, Radius: 50000},
	{Name: "Ступино", Lat: 54.8868, Lon: 38.0784, Radius: 12000},
	{Name: "Кашира", Lat: 54.8370, Lon: 38.1553, Radius: 10000},

	// Тульская область
	{Name: "Алексин", Lat: 54.5000, Lon: 37.0667, Radius: 10000},
	{Name: "Тула", Lat: 54.1931, Lon: 37.6174, Radius: 50000},
	{Name: "Щекино", Lat: 54.0025, Lon: 37.5176, Radius: 10000},
	{Name: "Плавск", Lat: 53.7096, Lon: 37.2900, Radius: 10000},
	{Name: "Богородицк", Lat: 53.7700, Lon: 38.1300, Radius: 10000},
	{Name: "Ефремов", Lat: 53.1467, Lon: 38.0928, Radius: 12000},

	// Липецкая область
	{Name: "Елец", Lat: 52.6216, Lon: 38.5012, Radius: 50000},
	{Name: "Задонск", Lat: 52.4000, Lon: 38.9167, Radius: 10000},
	{Name: "Липецк", Lat: 52.6036, Lon: 39.5818, Radius: 50000},
	{Name: "Хлевное", Lat: 52.2000, Lon: 39.0833, Radius: 10000},
	{Name: "Конь-Колодезь", Lat: 52.0167, Lon: 39.3167, Radius: 10000},

	// Воронежская область
	{Name: "Павловск", Lat: 50.4580, Lon: 40.1060, Radius: 10000},
	{Name: "Семилуки", Lat: 51.6863, Lon: 39.0242, Radius: 10000},
	{Name: "Воронеж", Lat: 51.6755, Lon: 39.2089, Radius: 50000},
	{Name: "Новая Усмань", Lat: 51.6366, Lon: 39.4136, Radius: 10000},
	{Name: "Лосево", Lat: 51.5167, Lon: 39.5500, Radius: 10000},
	{Name: "Панино", Lat: 51.4500, Lon: 40.1333, Radius: 10000},
	{Name: "Богучар", Lat: 49.9379, Lon: 40.5529, Radius: 10000},

	// Ростовская область
	{Name: "Миллерово", Lat: 48.9226, Lon: 40.3980, Radius: 10000},
	{Name: "Каменск-Шахтинский", Lat: 48.3196, Lon: 40.2684, Radius: 50000},
	{Name: "Шахты", Lat: 47.7085, Lon: 40.2125, Radius: 50000},
	{Name: "Новошахтинск", Lat: 47.7572, Lon: 39.9365, Radius: 10000},
	{Name: "Ростов-на-Дону", Lat: 47.2225, Lon: 39.7187, Radius: 50000},
	{Name: "Аксай", Lat: 47.2640, Lon: 39.8620, Radius: 10000},
	{Name: "Батайск", Lat: 47.1383, Lon: 39.7447, Radius: 50000},

	// Краснодарский край и Адыгея
	{Name: "Краснодар", Lat: 45.0393, Lon: 38.9872, Radius: 50000},
	{Name: "Усть-Лабинск", Lat: 45.2108, Lon: 39.6911, Radius: 10000},
	{Name: "Армавир", Lat: 45.0016, Lon: 41.1324, Radius: 50000},
	{Name: "Лабинск", Lat: 44.6333, Lon: 40.7333, Radius: 10000},
	{Name: "Курганинск", Lat: 44.8833, Lon: 40.6500, Radius: 10000},
	{Name: "Апшеронск", Lat: 44.4642, Lon: 39.7299, Radius: 10000},
	{Name: "Майкоп", Lat: 44.6078, Lon: 40.1058, Radius: 50000},
	{Name: "Белореченск", Lat: 44.7667, Lon: 39.8667, Radius: 10000},
	{Name: "Джубга", Lat: 44.3200, Lon: 38.7000, Radius: 10000},
	{Name: "Туапсе", Lat: 44.1053, Lon: 39.0736, Radius: 12000},
	{Name: "Сочи", Lat: 43.6028, Lon: 39.7342, Radius: 50000},
	{Name: "Адлер", Lat: 43.4300, Lon: 39.9200, Radius: 10000},

	// От Ростова-на-Дону до Черкесска через КМВ
	{Name: "Минеральные Воды", Lat: 44.2088, Lon: 43.1383, Radius: 50000},
	{Name: "Пятигорск", Lat: 44.0430, Lon: 43.0660, Radius: 50000},
	{Name: "Ессентуки", Lat: 44.0444, Lon: 42.8589, Radius: 50000},
	{Name: "Железноводск", Lat: 44.1326, Lon: 43.0307, Radius: 10000},
	{Name: "Кисловодск", Lat: 43.9050, Lon: 42.7180, Radius: 50000},
	{Name: "Лермонтов", Lat: 44.1053, Lon: 42.9718, Radius: 10000},
	{Name: "Невинномысск", Lat: 44.6333, Lon: 41.9333, Radius: 50000},

	// Дополнительные крупные региональные центры
	{Name: "Курск", Lat: 51.7304, Lon: 36.1939, Radius: 50000},
	{Name: "Орёл", Lat: 52.9685, Lon: 36.0696, Radius: 50000},
	{Name: "Обоянь", Lat: 51.2104, Lon: 36.2756, Radius: 15000},
	{Name: "Ставрополь", Lat: 45.0445, Lon: 41.9691, Radius: 50000},
	{Name: "Черкесск", Lat: 44.2269, Lon: 42.0468, Radius: 50000},
	{Name: "Анапа", Lat: 44.8948, Lon: 37.3165, Radius: 50000},
	{Name: "Новороссийск", Lat: 44.7235, Lon: 37.7686, Radius: 50000},
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
