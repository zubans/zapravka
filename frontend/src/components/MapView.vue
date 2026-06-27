<template>
  <div class="map-container">
    <div ref="mapRef" class="map"></div>
    <div class="info">
      <h1>Карта заправок</h1>
      <p>Нажмите на заправку, чтобы сообщить о наличии топлива.</p>
      <div class="search-box">
        <input
          v-model="address"
          type="text"
          placeholder="Введите город или адрес"
          @keyup.enter="searchAddress"
        />
        <button @click="searchAddress">Найти</button>
      </div>
      <p v-if="locationStatus" class="status">{{ locationStatus }}</p>
      <button v-if="showRetryGeo" class="retry-btn" @click="requestGeoPermission">
        🔄 Запросить геолокацию
      </button>
      <p v-if="!isSecure" class="secure-hint">
        Для точной геолокации откройте сайт по HTTPS. Сейчас используется поиск по адресу или геолокация по IP.
      </p>
    </div>
    <button class="locate-btn" @click="locateMe" title="Моё местоположение">
      📍
    </button>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import L from 'leaflet'
import { tileLayerOffline } from 'leaflet.offline'
import {
  getCachedStations,
  saveStations,
  clearOldStations,
  saveQueryCoverage,
  isCoveredByCache,
  clearOldQueries,
  deleteDemoStations
} from '../services/cache.js'

const API_URL = import.meta.env.VITE_API_URL || '/api'
const mapRef = ref(null)
const stations = ref([])
const address = ref('')
const locationStatus = ref('')
const showRetryGeo = ref(false)
const isSecure = ref(typeof window !== 'undefined' ? window.isSecureContext : false)
let map = null
let markersLayer = null
let userMarker = null
let moveTimeout = null
let countsTimeout = null

const COUNTS_CACHE_TTL_MS = 30 * 1000
const countsCache = new Map() // id -> { counts, ts }

const fuelTypes = [
  { key: '92', label: 'АИ-92' },
  { key: '95', label: 'АИ-95' },
  { key: 'diesel', label: 'ДТ' }
]

const defaultCenter = [55.7558, 37.6173]
const defaultZoom = 13
const PRELOAD_RADIUS = 20000 // 20 км
const VISIBLE_RADIUS = 5000  // 5 км

function getFuelCounts(counts, fuel) {
  return counts && counts[fuel] ? counts[fuel] : { yes: 0, no: 0 }
}

function totals(counts) {
  let yes = 0
  let no = 0
  for (const fuel of fuelTypes) {
    const c = getFuelCounts(counts, fuel.key)
    yes += c.yes
    no += c.no
  }
  return { yes, no }
}

function createStationIcon(counts) {
  const t = totals(counts)
  const badge = t.yes + t.no > 0
    ? `<div class="station-badge">
        <span class="yes">${t.yes}</span>
        <span class="sep">/</span>
        <span class="no">${t.no}</span>
       </div>`
    : ''
  return L.divIcon({
    className: 'station-marker',
    html: `<div class="station-pin">⛽</div>${badge}`,
    iconSize: [40, 50],
    iconAnchor: [20, 50],
    popupAnchor: [0, -45]
  })
}

function createPopupContent(station) {
  const div = document.createElement('div')
  div.className = 'station-popup'

  let fuelRows = ''
  for (const fuel of fuelTypes) {
    const c = getFuelCounts(station.counts, fuel.key)
    fuelRows += `
      <div class="fuel-row" data-fuel="${fuel.key}">
        <div class="fuel-info">
          <span class="fuel-name">${fuel.label}</span>
          <span class="fuel-counts">
            <span class="yes">есть ${c.yes}</span>
            <span class="sep">/</span>
            <span class="no">нет ${c.no}</span>
          </span>
        </div>
        <div class="fuel-buttons">
          <button class="btn-yes" data-fuel="${fuel.key}" data-type="yes">✅</button>
          <button class="btn-no" data-fuel="${fuel.key}" data-type="no">❌</button>
        </div>
      </div>
    `
  }

  div.innerHTML = `
    <h3>${escapeHtml(station.name)}</h3>
    <p>${station.brand ? escapeHtml(station.brand) : 'Заправка'}</p>
    <div class="fuel-list">
      ${fuelRows}
    </div>
    <p class="hint">Голос учитывается 3 часа</p>
  `

  div.querySelectorAll('button').forEach((btn) => {
    btn.addEventListener('click', () => {
      const fuel = btn.dataset.fuel
      const type = btn.dataset.type
      vote(station, fuel, type)
    })
  })

  return div
}

function escapeHtml(text) {
  const div = document.createElement('div')
  div.textContent = text
  return div.innerHTML
}

async function fetchStationsFromAPI(lat, lon, radius) {
  const res = await fetch(
    `${API_URL}/stations?lat=${lat.toFixed(6)}&lon=${lon.toFixed(6)}&radius=${Math.round(radius)}`
  )
  if (!res.ok) throw new Error('failed to load stations')
  const data = await res.json()
  return data.stations || []
}

async function fetchVoteCounts(ids) {
  if (!ids || ids.length === 0) return {}
  const res = await fetch(`${API_URL}/votes?ids=${encodeURIComponent(ids.join(','))}`)
  if (!res.ok) throw new Error('failed to load vote counts')
  const data = await res.json()
  return data.counts || {}
}

function getVisibleRadius() {
  if (!map) return VISIBLE_RADIUS
  const bounds = map.getBounds()
  const center = bounds.getCenter()
  const ne = bounds.getNorthEast()
  const sw = bounds.getSouthWest()
  // половина диагонали видимой области
  const dLat = toRad(ne.lat - sw.lat)
  const dLon = toRad(ne.lng - sw.lng) * Math.cos(toRad(center.lat))
  const a = Math.sin(dLat / 2) ** 2 + Math.sin(dLon / 2) ** 2
  const c = 2 * Math.atan2(Math.sqrt(a), Math.sqrt(1 - a))
  return Math.max(VISIBLE_RADIUS, (6371000 * c) / 2)
}

function toRad(x) {
  return (x * Math.PI) / 180
}

async function mergeVoteCountsForIds(ids) {
  if (!ids || ids.length === 0) return
  try {
    const now = Date.now()
    const idsToFetch = []
    for (const id of ids) {
      if (String(id).startsWith('demo-') || String(id).startsWith('test-')) {
        continue
      }
      const cached = countsCache.get(id)
      if (!cached || now - cached.ts > COUNTS_CACHE_TTL_MS) {
        idsToFetch.push(id)
      }
    }

    if (idsToFetch.length > 0) {
      const counts = await fetchVoteCounts(idsToFetch)
      for (const [id, c] of Object.entries(counts)) {
        countsCache.set(id, { counts: c, ts: now })
      }
    }

    let updated = false
    for (const s of stations.value) {
      const cached = countsCache.get(s.id)
      if (cached && JSON.stringify(s.counts) !== JSON.stringify(cached.counts)) {
        s.counts = cached.counts
        updated = true
      }
    }
    if (updated) renderMarkers()
  } catch (err) {
    console.error('Ошибка загрузки голосов:', err)
  }
}

function scheduleVoteCountsUpdate() {
  clearTimeout(countsTimeout)
  countsTimeout = setTimeout(updateVisibleVoteCounts, 200)
}

async function updateVisibleVoteCounts() {
  if (!map || !markersLayer) return
  const bounds = map.getBounds()
  const ids = []
  for (const layer of markersLayer.getLayers()) {
    if (!layer.stationId) continue
    if (String(layer.stationId).startsWith('demo-') || String(layer.stationId).startsWith('test-')) {
      continue
    }
    if (bounds.contains(layer.getLatLng())) {
      ids.push(layer.stationId)
    }
  }
  if (!ids.length) return
  await mergeVoteCountsForIds([...new Set(ids)])
}

async function loadStationsAround(lat, lon, radius, source = '') {
  try {
    // 1. Всегда сначала показываем кэшированные заправки для этой области
    const cached = await getCachedStations(lat, lon, radius)
    stations.value = cached
    renderMarkers()

    // 2. Подгружаем актуальные голоса для видимых станций батчем
    scheduleVoteCountsUpdate()

    // 3. Проверяем, покрывает ли кэш эту область (серверный запрос делали < 1 час назад)
    const covered = await isCoveredByCache(lat, lon, radius)

    if (!covered) {
      // 4. Если области нет в кэше — запрашиваем станции с сервера
      const fresh = await fetchStationsFromAPI(lat, lon, radius)

      // Сохраняем покрытие даже для пустого ответа, чтобы не долбить сервер
      await saveQueryCoverage(lat, lon, radius)
      await clearOldQueries()

      if (fresh.length > 0) {
        await saveStations(fresh)
        await clearOldStations()

        // Обновляем маркеры, если центр карты всё ещё рядом с запрошенной точкой
        if (map) {
          const center = map.getCenter()
          const dx = Math.abs(center.lat - lat)
          const dy = Math.abs(center.lng - lon)
          if (dx < 0.02 && dy < 0.02) {
            stations.value = fresh
            renderMarkers()
            scheduleVoteCountsUpdate()
          }
        }
      }
    }

    if (source) {
      locationStatus.value = `Заправок в области: ${stations.value.length}`
    }
  } catch (err) {
    console.error('Ошибка загрузки заправок:', err)
    if (!stations.value.length) {
      locationStatus.value = 'Не удалось загрузить заправки. Проверьте подключение.'
    }
  }
}

async function loadVisibleStations() {
  if (!map) return
  const center = map.getCenter()
  const radius = getVisibleRadius()
  await loadStationsAround(center.lat, center.lng, radius)
}

function renderMarkers() {
  if (!markersLayer) return
  markersLayer.clearLayers()
  for (const station of stations.value) {
    const marker = L.marker([station.lat, station.lon], {
      icon: createStationIcon(station.counts)
    })
    marker.bindPopup(createPopupContent(station))
    marker.stationId = station.id
    markersLayer.addLayer(marker)
  }
}

async function vote(station, fuel, type) {
  try {
    const res = await fetch(`${API_URL}/vote`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ station_id: station.id, fuel_type: fuel, type })
    })
    if (!res.ok) throw new Error('vote failed')
    const data = await res.json()
    station.counts = data.counts
    countsCache.set(station.id, { counts: data.counts, ts: Date.now() })
    renderMarkers()
  } catch (err) {
    console.error('Ошибка голосования:', err)
    alert('Не удалось сохранить голос. Попробуйте позже.')
  }
}

function showUserPosition(lat, lng) {
  if (userMarker) {
    map.removeLayer(userMarker)
  }
  userMarker = L.circleMarker([lat, lng], {
    radius: 8,
    fillColor: '#2563eb',
    color: '#fff',
    weight: 2,
    opacity: 1,
    fillOpacity: 0.9
  }).addTo(map)
  userMarker.bindPopup('Вы здесь')
}

async function setMapView(lat, lng, zoom, source = '') {
  map.setView([lat, lng], zoom)
  showUserPosition(lat, lng)
  saveLocation(lat, lng, zoom)
  await loadStationsAround(lat, lng, PRELOAD_RADIUS, source)
}

function saveLocation(lat, lng, zoom) {
  try {
    localStorage.setItem('zapravka_location', JSON.stringify({ lat, lng, zoom }))
  } catch (e) {
    // ignore
  }
}

function loadSavedLocation() {
  try {
    const saved = localStorage.getItem('zapravka_location')
    if (saved) {
      const { lat, lng, zoom } = JSON.parse(saved)
      if (lat && lng) {
        return { lat, lng, zoom: zoom || defaultZoom }
      }
    }
  } catch (e) {
    // ignore
  }
  return null
}

async function searchAddress() {
  const q = address.value.trim()
  if (!q) return

  locationStatus.value = 'Поиск...'
  try {
    const res = await fetch(
      `https://nominatim.openstreetmap.org/search?format=json&q=${encodeURIComponent(q)}&limit=1`,
      { headers: { 'User-Agent': 'zapravka/1.0' } }
    )
    if (!res.ok) throw new Error('geocoding failed')
    const data = await res.json()
    if (!data || data.length === 0) {
      locationStatus.value = 'Адрес не найден'
      return
    }
    const place = data[0]
    const lat = parseFloat(place.lat)
    const lon = parseFloat(place.lon)
    await setMapView(lat, lon, 15, place.display_name)
  } catch (err) {
    console.error('Ошибка геокодирования:', err)
    locationStatus.value = 'Не удалось найти адрес'
  }
}

async function locateByIP() {
  locationStatus.value = 'Определение по IP...'
  try {
    const res = await fetch('https://ipapi.co/json/')
    if (!res.ok) throw new Error('ip location failed')
    const data = await res.json()
    if (data.latitude && data.longitude) {
      await setMapView(data.latitude, data.longitude, 12, `примерное местоположение (${data.city || 'по IP'})`)
      return true
    }
  } catch (err) {
    console.warn('IP геолокация недоступна:', err)
  }
  return false
}

function handleGeoError(err) {
  console.warn('Геолокация недоступна:', err)
  showRetryGeo.value = true

  if (!isSecure.value) {
    locationStatus.value = 'Геолокация недоступна по HTTP. Используйте HTTPS или поиск по адресу.'
    return
  }

  let msg = 'Не удалось определить местоположение'
  if (err && err.code === 1) msg = 'Доступ к геолокации запрещён. Нажмите «Запросить геолокацию» или разрешите доступ в настройках браузера'
  if (err && err.code === 2) msg = 'Местоположение не определено'
  if (err && err.code === 3) msg = 'Время ожидания истекло'
  locationStatus.value = `${msg}.`
}

function requestGeoPermission() {
  if (!('geolocation' in navigator)) {
    locationStatus.value = 'Геолокация не поддерживается браузером'
    showRetryGeo.value = false
    return
  }
  showRetryGeo.value = false
  locationStatus.value = 'Запрос разрешения на геолокацию...'
  navigator.geolocation.getCurrentPosition(
    async (pos) => {
      const { latitude, longitude } = pos.coords
      await setMapView(latitude, longitude, 15, 'моё местоположение')
      showRetryGeo.value = false
    },
    (err) => {
      handleGeoError(err)
    },
    { enableHighAccuracy: true, timeout: 10000, maximumAge: 0 }
  )
}

function locateMe() {
  requestGeoPermission()
}

function initMap(lat, lng, zoom) {
  map = L.map(mapRef.value).setView([lat, lng], zoom)

  // Оффлайн-кэширование тайлов карты
  tileLayerOffline('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
    attribution: '&copy; OpenStreetMap contributors',
    maxZoom: 19,
    crossOrigin: true
  }).addTo(map)

  markersLayer = L.layerGroup().addTo(map)

  map.on('moveend', () => {
    clearTimeout(moveTimeout)
    moveTimeout = setTimeout(loadVisibleStations, 300)
  })

  showUserPosition(lat, lng)
}

onMounted(async () => {
  // Удаляем старые демо-станции из локального кэша
  try {
    await deleteDemoStations()
  } catch (e) {
    console.warn('Не удалось удалить демо-станции:', e)
  }

  const saved = loadSavedLocation()
  if (saved) {
    initMap(saved.lat, saved.lng, saved.zoom)
    locationStatus.value = 'Загрузка заправок в радиусе 20 км...'
    await loadStationsAround(saved.lat, saved.lng, PRELOAD_RADIUS, 'сохранённое местоположение')
    return
  }

  if ('geolocation' in navigator) {
    locationStatus.value = 'Определение местоположения...'
    navigator.geolocation.getCurrentPosition(
      async (pos) => {
        const { latitude, longitude } = pos.coords
        initMap(latitude, longitude, 14)
        saveLocation(latitude, longitude, 14)
        locationStatus.value = 'Загрузка заправок в радиусе 20 км...'
        await loadStationsAround(latitude, longitude, PRELOAD_RADIUS, 'моё местоположение')
      },
      async (err) => {
        handleGeoError(err)
        const ok = await locateByIP()
        if (!ok) {
          initMap(defaultCenter[0], defaultCenter[1], defaultZoom)
          await loadStationsAround(defaultCenter[0], defaultCenter[1], PRELOAD_RADIUS, 'Москва (по умолчанию)')
          if (!locationStatus.value.includes('HTTP')) {
            locationStatus.value = 'Геолокация недоступна. Показаны заправки в Москве. Используйте поиск.'
          }
        }
      },
      { enableHighAccuracy: true, timeout: 10000, maximumAge: 60000 }
    )
  } else {
    const ok = await locateByIP()
    if (!ok) {
      initMap(defaultCenter[0], defaultCenter[1], defaultZoom)
      await loadStationsAround(defaultCenter[0], defaultCenter[1], PRELOAD_RADIUS, 'Москва (по умолчанию)')
      locationStatus.value = 'Геолокация недоступна. Показаны заправки в Москве. Используйте поиск.'
    }
  }
})

onUnmounted(() => {
  if (map) {
    map.remove()
  }
})
</script>

<style scoped>
.map-container {
  position: relative;
  width: 100%;
  height: 100%;
}

.map {
  width: 100%;
  height: 100%;
}

.info {
  position: absolute;
  top: 12px;
  left: 50px;
  z-index: 1000;
  background: rgba(255, 255, 255, 0.95);
  padding: 12px 16px;
  border-radius: 12px;
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.15);
  max-width: 340px;
}

.info h1 {
  margin: 0 0 6px;
  font-size: 18px;
}

.info p {
  margin: 0 0 10px;
  font-size: 13px;
  color: #555;
}

.search-box {
  display: flex;
  gap: 6px;
}

.search-box input {
  flex: 1;
  padding: 8px 10px;
  border: 1px solid #ddd;
  border-radius: 8px;
  font-size: 13px;
  outline: none;
}

.search-box input:focus {
  border-color: #2563eb;
}

.search-box button {
  padding: 8px 14px;
  border: none;
  border-radius: 8px;
  background: #2563eb;
  color: #fff;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
}

.search-box button:hover {
  background: #1d4ed8;
}

.status {
  margin: 8px 0 0;
  font-size: 12px;
  color: #666;
}

.retry-btn {
  margin-top: 8px;
  padding: 6px 12px;
  border: none;
  border-radius: 8px;
  background: #f1f5f9;
  color: #334155;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  transition: background 0.2s;
}

.retry-btn:hover {
  background: #e2e8f0;
}

.secure-hint {
  margin: 8px 0 0;
  font-size: 11px;
  color: #b45309;
  background: #fef3c7;
  padding: 6px 8px;
  border-radius: 6px;
}

.locate-btn {
  position: absolute;
  bottom: 24px;
  right: 24px;
  z-index: 1000;
  width: 48px;
  height: 48px;
  border: none;
  border-radius: 50%;
  background: #fff;
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.2);
  font-size: 22px;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: transform 0.1s, box-shadow 0.2s;
}

.locate-btn:hover {
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.25);
}

.locate-btn:active {
  transform: scale(0.95);
}
</style>

<style>
.station-marker {
  background: transparent;
  border: none;
}

.station-pin {
  width: 38px;
  height: 38px;
  background: #2563eb;
  border-radius: 50% 50% 50% 0;
  transform: rotate(-45deg);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 18px;
  color: #fff;
  box-shadow: 0 2px 6px rgba(0, 0, 0, 0.3);
}

.station-badge {
  position: absolute;
  top: -6px;
  right: -6px;
  background: #fff;
  border-radius: 10px;
  padding: 2px 6px;
  font-size: 11px;
  font-weight: bold;
  box-shadow: 0 1px 4px rgba(0, 0, 0, 0.2);
  display: flex;
  gap: 2px;
}

.station-badge .yes {
  color: #16a34a;
}

.station-badge .no {
  color: #dc2626;
}

.station-badge .sep {
  color: #888;
}

.station-popup {
  min-width: 240px;
}

.station-popup h3 {
  margin: 0 0 4px;
  font-size: 16px;
}

.station-popup p {
  margin: 0 0 10px;
  color: #666;
  font-size: 13px;
}

.fuel-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-bottom: 10px;
}

.fuel-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 8px 10px;
  background: #f8fafc;
  border-radius: 8px;
}

.fuel-info {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.fuel-name {
  font-weight: 600;
  font-size: 14px;
}

.fuel-counts {
  font-size: 12px;
}

.fuel-counts .yes {
  color: #16a34a;
}

.fuel-counts .no {
  color: #dc2626;
}

.fuel-counts .sep {
  color: #888;
  margin: 0 4px;
}

.fuel-buttons {
  display: flex;
  gap: 6px;
}

.fuel-buttons button {
  width: 34px;
  height: 34px;
  border: none;
  border-radius: 8px;
  cursor: pointer;
  font-size: 16px;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: opacity 0.2s, transform 0.1s;
}

.fuel-buttons button:hover {
  opacity: 0.85;
}

.fuel-buttons button:active {
  transform: scale(0.95);
}

.fuel-buttons .btn-yes {
  background: #dcfce7;
}

.fuel-buttons .btn-no {
  background: #fee2e2;
}

.station-popup .hint {
  margin: 0;
  font-size: 11px;
  color: #888;
  text-align: center;
}
</style>
