<template>
  <div class="map-container">
    <!-- Map Canvas -->
    <div ref="mapRef" class="map"></div>

    <!-- Modern Sidebar / Bottom Sheet -->
    <div :class="['panel', { 'collapsed': isCollapsed }]">
      <!-- Drag Handle for Mobile & Toggle Button for Desktop -->
      <button class="toggle-btn" @click="toggleCollapse" aria-label="Свернуть/Развернуть панель">
        <span class="handle-bar"></span>
        <svg class="arrow-icon" viewBox="0 0 24 24" width="16" height="16">
          <path fill="currentColor" d="M15.41 7.41L14 6l-6 6 6 6 1.41-1.41L10.83 12z"/>
        </svg>
      </button>

      <div class="panel-content">
        <header class="app-header">
          <h1><span class="icon-brand">⛽</span> Карта заправок</h1>
          <p>Мониторинг наличия топлива на АЗС</p>
        </header>

        <!-- Search input box -->
        <div class="search-box">
          <div class="input-wrapper">
            <span class="search-icon">🔍</span>
            <input
              v-model="address"
              type="text"
              placeholder="Введите город или адрес"
              @keyup.enter="searchAddress"
            />
          </div>
          <button class="search-btn" @click="searchAddress">Найти</button>
        </div>

        <!-- Location status indicator -->
        <div class="status-section">
          <div v-if="locationStatus" class="status-msg">
            <span class="pulse-dot"></span>
            <span class="status-text">{{ locationStatus }}</span>
          </div>
          <button v-if="showRetryGeo" class="retry-btn" @click="requestGeoPermission">
            🔄 Запросить геолокацию
          </button>
          <div v-if="!isSecure" class="secure-hint">
            💡 Откройте сайт по HTTPS для точного определения геолокации.
          </div>
        </div>

        <div class="info-footer">
          <p>Нажмите на маркер заправки на карте, чтобы узнать подробности или проголосовать.</p>
        </div>
      </div>
    </div>

    <!-- Floating Action Buttons -->
    <div class="fab-container">
      <button class="locate-btn" @click="locateMe" title="Моё местоположение">
        🎯
      </button>
    </div>

    <!-- Toast Notification Banner -->
    <Transition name="toast">
      <div v-if="toast.visible" :class="['toast', `toast-${toast.type}`]">
        <span class="toast-icon">{{ toast.type === 'success' ? '✅' : '⚠️' }}</span>
        <span class="toast-message">{{ toast.message }}</span>
      </div>
    </Transition>
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
const isCollapsed = ref(false)

const toast = ref({ message: '', type: 'success', visible: false })
let toastTimeout = null

function showToast(message, type = 'success') {
  clearTimeout(toastTimeout)
  toast.value = { message, type, visible: true }
  toastTimeout = setTimeout(() => {
    toast.value.visible = false
  }, 3500)
}

function toggleCollapse() {
  isCollapsed.value = !isCollapsed.value
}

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

  let statusClass = ''
  if (t.yes > 0) {
    statusClass = 'has-fuel'
  } else if (t.no > 0) {
    statusClass = 'no-fuel'
  }

  return L.divIcon({
    className: 'station-marker',
    html: `
      <div class="station-pin-wrapper ${statusClass}">
        <div class="station-pin"><span class="pin-icon">⛽</span></div>
        ${badge}
      </div>
    `,
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
            <span class="yes">Есть ${c.yes}</span>
            <span class="sep">/</span>
            <span class="no">Нет ${c.no}</span>
          </span>
        </div>
        <div class="fuel-buttons">
          <button class="btn-yes" data-fuel="${fuel.key}" data-type="yes" title="Топливо есть">
            <svg class="vote-icon" viewBox="0 0 24 24">
              <path fill="currentColor" d="M9 16.17L4.83 12l-1.42 1.41L9 19 21 7l-1.41-1.41L9 16.17z"/>
            </svg>
          </button>
          <button class="btn-no" data-fuel="${fuel.key}" data-type="no" title="Топлива нет">
            <svg class="vote-icon" viewBox="0 0 24 24">
              <path fill="currentColor" d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12 19 6.41z"/>
            </svg>
          </button>
        </div>
      </div>
    `
  }

  div.innerHTML = `
    <div class="popup-title-bar">
      <h3>${escapeHtml(station.name)}</h3>
      <span class="popup-brand">${station.brand ? escapeHtml(station.brand) : 'Независимая АЗС'}</span>
    </div>
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
    const stationsToSave = []
    for (const s of stations.value) {
      const cached = countsCache.get(s.id)
      if (cached && JSON.stringify(s.counts) !== JSON.stringify(cached.counts)) {
        s.counts = cached.counts
        updated = true
        stationsToSave.push(s)
      }
    }
    if (updated) {
      renderMarkers()
      // Persist the updated counts back into IndexedDB so they don't reset to 0 next time
      try {
        await saveStations(stationsToSave)
      } catch (err) {
        console.warn('Не удалось обновить голоса в локальном кэше IndexedDB:', err)
      }
    }
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
    // 1. Сначала показываем локально кэшированные заправки из IndexedDB
    const cached = await getCachedStations(lat, lon, radius)
    stations.value = cached
    renderMarkers()

    // 2. Сразу подтягиваем свежие голоса для видимых заправок
    scheduleVoteCountsUpdate()

    // 3. Проверяем, покрывает ли кэш поисковых запросов текущую область
    const covered = await isCoveredByCache(lat, lon, radius)

    if (!covered) {
      // 4. Если нет покрытия — качаем свежие станции с бэкенда
      const fresh = await fetchStationsFromAPI(lat, lon, radius)

      await saveQueryCoverage(lat, lon, radius)
      await clearOldQueries()

      if (fresh.length > 0) {
        await saveStations(fresh)
        await clearOldStations()

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
      locationStatus.value = `Найдено заправок: ${stations.value.length}`
    }
  } catch (err) {
    console.error('Ошибка загрузки заправок:', err)
    if (!stations.value.length) {
      locationStatus.value = 'Ошибка загрузки данных. Проверьте интернет.'
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
    
    // Save to IndexedDB so counts are persisted locally
    try {
      await saveStations([station])
    } catch (e) {
      console.warn('Не удалось обновить голос в IndexedDB:', e)
    }

    showToast('Ваш голос успешно учтен!')
  } catch (err) {
    console.error('Ошибка голосования:', err)
    showToast('Не удалось отправить голос. Попробуйте позже.', 'error')
  }
}

function showUserPosition(lat, lng) {
  if (userMarker) {
    map.removeLayer(userMarker)
  }
  userMarker = L.circleMarker([lat, lng], {
    radius: 9,
    fillColor: '#3b82f6',
    color: '#ffffff',
    weight: 3,
    opacity: 1,
    fillOpacity: 0.95
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

  locationStatus.value = 'Ищем на карте...'
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
    
    // Auto-collapse panel on mobile after selection to show the map
    if (window.innerWidth < 768) {
      isCollapsed.value = true
    }
  } catch (err) {
    console.error('Ошибка геокодирования:', err)
    locationStatus.value = 'Не удалось найти адрес'
  }
}

async function locateByIP() {
  locationStatus.value = 'Определение координат...'
  try {
    const res = await fetch('https://ipapi.co/json/')
    if (!res.ok) throw new Error('ip location failed')
    const data = await res.json()
    if (data.latitude && data.longitude) {
      await setMapView(data.latitude, data.longitude, 12, `местоположение по IP (${data.city || ''})`)
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
    locationStatus.value = 'Геолокация не поддерживается без HTTPS. Используйте поиск.'
    return
  }

  let msg = 'Не удалось определить координаты'
  if (err && err.code === 1) msg = 'Доступ к геопозиции отклонен. Разрешите его в браузере'
  if (err && err.code === 2) msg = 'Не удалось получить спутниковый сигнал'
  if (err && err.code === 3) msg = 'Превышено время ожидания GPS'
  locationStatus.value = msg
}

function requestGeoPermission() {
  if (!('geolocation' in navigator)) {
    locationStatus.value = 'Геолокация не поддерживается вашим браузером'
    showRetryGeo.value = false
    return
  }
  showRetryGeo.value = false
  locationStatus.value = 'Получаем GPS координаты...'
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
  map = L.map(mapRef.value, {
    zoomControl: false // Hide default to place custom ones later
  }).setView([lat, lng], zoom)

  // Custom Zoom Control positioning (bottom-right above locate btn)
  L.control.zoom({
    position: 'bottomright'
  }).addTo(map)

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
  try {
    await deleteDemoStations()
  } catch (e) {
    console.warn('Не удалось очистить демо-станции:', e)
  }

  const saved = loadSavedLocation()
  if (saved) {
    initMap(saved.lat, saved.lng, saved.zoom)
    locationStatus.value = 'Загрузка сохраненного местоположения...'
    await loadStationsAround(saved.lat, saved.lng, PRELOAD_RADIUS, 'сохранённое местоположение')
    return
  }

  if ('geolocation' in navigator) {
    locationStatus.value = 'Определяем геопозицию...'
    navigator.geolocation.getCurrentPosition(
      async (pos) => {
        const { latitude, longitude } = pos.coords
        initMap(latitude, longitude, 14)
        saveLocation(latitude, longitude, 14)
        locationStatus.value = 'Загрузка заправок вокруг...'
        await loadStationsAround(latitude, longitude, PRELOAD_RADIUS, 'моё местоположение')
      },
      async (err) => {
        handleGeoError(err)
        const ok = await locateByIP()
        if (!ok) {
          initMap(defaultCenter[0], defaultCenter[1], defaultZoom)
          await loadStationsAround(defaultCenter[0], defaultCenter[1], PRELOAD_RADIUS, 'Москва (по умолчанию)')
          if (!locationStatus.value.includes('HTTPS')) {
            locationStatus.value = 'Геолокация недоступна. Показана Москва.'
          }
        }
      },
      { enableHighAccuracy: true, timeout: 8000, maximumAge: 60000 }
    )
  } else {
    const ok = await locateByIP()
    if (!ok) {
      initMap(defaultCenter[0], defaultCenter[1], defaultZoom)
      await loadStationsAround(defaultCenter[0], defaultCenter[1], PRELOAD_RADIUS, 'Москва (по умолчанию)')
      locationStatus.value = 'Геолокация не поддерживается. Показана Москва.'
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
@import url('https://fonts.googleapis.com/css2?family=Outfit:wght@300;400;500;600;700&family=Rubik:wght@300;400;500;600;700&display=swap');

.map-container {
  position: relative;
  width: 100%;
  height: 100%;
  font-family: 'Rubik', 'Outfit', -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
  overflow: hidden;
  --primary: #2563eb;
  --primary-hover: #1d4ed8;
  --bg-glass: rgba(255, 255, 255, 0.85);
  --border-glass: rgba(255, 255, 255, 0.4);
  --shadow-lg: 0 10px 25px -5px rgba(0, 0, 0, 0.1), 0 8px 10px -6px rgba(0, 0, 0, 0.1);
  --text-primary: #0f172a;
  --text-secondary: #475569;
}

.map {
  width: 100%;
  height: 100%;
  z-index: 1;
}

/* Glassmorphic Panel Design */
.panel {
  position: absolute;
  top: 20px;
  left: 20px;
  width: 360px;
  max-height: calc(100% - 40px);
  background: var(--bg-glass);
  backdrop-filter: blur(20px);
  -webkit-backdrop-filter: blur(20px);
  border: 1px solid var(--border-glass);
  border-radius: 16px;
  box-shadow: var(--shadow-lg);
  z-index: 1000;
  transition: transform 0.4s cubic-bezier(0.16, 1, 0.3, 1), opacity 0.3s ease;
  display: flex;
  flex-direction: column;
  overflow: visible;
}

.panel.collapsed {
  transform: translateX(-380px);
}

/* Toggle arrow button for desktop */
.toggle-btn {
  position: absolute;
  right: -14px;
  top: 24px;
  width: 28px;
  height: 28px;
  border-radius: 50%;
  background: white;
  border: 1px solid #e2e8f0;
  box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.1);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1001;
  transition: transform 0.3s ease, background-color 0.2s;
  padding: 0;
}

.toggle-btn:hover {
  background-color: #f8fafc;
}

.panel.collapsed .toggle-btn {
  transform: rotate(180deg);
  right: -38px;
  background-color: var(--primary);
  color: white;
  border-color: var(--primary);
}

.toggle-btn .handle-bar {
  display: none; /* Only visible on mobile bottom sheet */
}

.toggle-btn .arrow-icon {
  width: 16px;
  height: 16px;
  display: block;
}

.panel-content {
  padding: 24px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.app-header h1 {
  margin: 0 0 4px;
  font-size: 20px;
  font-weight: 700;
  color: var(--text-primary);
  display: flex;
  align-items: center;
  gap: 8px;
}

.icon-brand {
  font-size: 24px;
}

.app-header p {
  margin: 0;
  font-size: 13px;
  color: var(--text-secondary);
}

/* Modern Input Styling */
.search-box {
  display: flex;
  gap: 8px;
  width: 100%;
}

.input-wrapper {
  position: relative;
  flex: 1;
  display: flex;
  align-items: center;
}

.search-icon {
  position: absolute;
  left: 12px;
  font-size: 13px;
  color: #94a3b8;
}

.search-box input {
  width: 100%;
  padding: 10px 12px 10px 36px;
  border: 1px solid #cbd5e1;
  background: rgba(255, 255, 255, 0.7);
  border-radius: 10px;
  font-size: 13px;
  color: var(--text-primary);
  outline: none;
  transition: all 0.2s ease;
  font-family: inherit;
}

.search-box input:focus {
  border-color: var(--primary);
  background: #ffffff;
  box-shadow: 0 0 0 3px rgba(37, 99, 235, 0.15);
}

.search-btn {
  padding: 10px 16px;
  border: none;
  border-radius: 10px;
  background: var(--primary);
  color: #ffffff;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  transition: background-color 0.2s ease, transform 0.1s;
}

.search-btn:hover {
  background-color: var(--primary-hover);
}

.search-btn:active {
  transform: scale(0.97);
}

/* Status section with dot pulsing animation */
.status-section {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.status-msg {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  color: var(--text-secondary);
}

.pulse-dot {
  width: 8px;
  height: 8px;
  background-color: #10b981;
  border-radius: 50%;
  animation: pulse 1.8s infinite alternate;
}

@keyframes pulse {
  0% { transform: scale(0.85); opacity: 0.5; }
  100% { transform: scale(1.15); opacity: 1; box-shadow: 0 0 8px rgba(16, 185, 129, 0.6); }
}

.status-text {
  font-weight: 500;
}

.retry-btn {
  align-self: flex-start;
  padding: 6px 12px;
  border: 1px solid #cbd5e1;
  border-radius: 8px;
  background: #ffffff;
  color: var(--text-primary);
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.2s;
  display: flex;
  align-items: center;
  gap: 4px;
}

.retry-btn:hover {
  background: #f1f5f9;
  border-color: #94a3b8;
}

.secure-hint {
  font-size: 11px;
  color: #b45309;
  background: #fef3c7;
  padding: 8px 10px;
  border-radius: 8px;
  border: 1px solid #fde68a;
  line-height: 1.4;
}

.info-footer {
  margin-top: auto;
  border-top: 1px solid rgba(0, 0, 0, 0.06);
  padding-top: 14px;
}

.info-footer p {
  margin: 0;
  font-size: 12px;
  color: var(--text-muted);
  line-height: 1.5;
}

/* Float Action Buttons container */
.fab-container {
  position: absolute;
  bottom: 24px;
  right: 24px;
  z-index: 1000;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.locate-btn {
  width: 46px;
  height: 46px;
  border: 1px solid rgba(0, 0, 0, 0.05);
  border-radius: 50%;
  background: #ffffff;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
  font-size: 20px;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: transform 0.15s, box-shadow 0.2s, background-color 0.2s;
  padding: 0;
}

.locate-btn:hover {
  background-color: #f8fafc;
  box-shadow: 0 6px 16px rgba(0, 0, 0, 0.2);
  transform: translateY(-2px);
}

.locate-btn:active {
  transform: translateY(0) scale(0.95);
}

/* Toast Message Design */
.toast {
  position: absolute;
  bottom: 30px;
  left: 50%;
  transform: translateX(-50%);
  z-index: 2000;
  background: #1e293b;
  color: #ffffff;
  padding: 12px 20px;
  border-radius: 12px;
  box-shadow: 0 10px 15px -3px rgba(0, 0, 0, 0.25);
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 13px;
  font-weight: 500;
  max-width: 90vw;
  width: max-content;
}

.toast-success {
  border-left: 4px solid #10b981;
}

.toast-error {
  border-left: 4px solid #ef4444;
  background: #7f1d1d;
}

/* Toast animation transitions */
.toast-enter-active,
.toast-leave-active {
  transition: all 0.35s cubic-bezier(0.16, 1, 0.3, 1);
}

.toast-enter-from {
  opacity: 0;
  transform: translate(-50%, 30px);
}

.toast-leave-to {
  opacity: 0;
  transform: translate(-50%, -20px);
}

/* MOBILE RESPONSIVE STYLING (Bottom Sheet mode) */
@media (max-width: 767px) {
  .panel {
    top: auto;
    bottom: 0;
    left: 0;
    width: 100%;
    max-height: 50%;
    height: auto;
    border-radius: 20px 20px 0 0;
    border-bottom: none;
    border-left: none;
    border-right: none;
    transform: translateY(0);
    box-shadow: 0 -8px 24px rgba(0, 0, 0, 0.1);
  }

  .panel.collapsed {
    transform: translateY(calc(100% - 44px)); /* Slide down, leaving only the drag handle visible */
  }

  .panel-content {
    padding: 16px 20px 24px;
    gap: 14px;
  }

  .toggle-btn {
    position: relative;
    top: 0;
    left: 0;
    right: 0;
    width: 100%;
    height: 44px;
    background: transparent;
    border: none;
    border-radius: 20px 20px 0 0;
    box-shadow: none;
    display: flex;
    align-items: center;
    justify-content: center;
    cursor: pointer;
  }

  .toggle-btn:hover {
    background: transparent;
  }

  .toggle-btn .handle-bar {
    display: block;
    width: 36px;
    height: 4px;
    background-color: #cbd5e1;
    border-radius: 2px;
    transition: background-color 0.2s;
  }

  .toggle-btn:hover .handle-bar {
    background-color: #94a3b8;
  }

  .toggle-btn .arrow-icon {
    display: none; /* Hide desktop arrow on mobile */
  }

  .panel.collapsed .toggle-btn {
    transform: none;
    right: 0;
    background-color: transparent;
    color: inherit;
  }

  .fab-container {
    bottom: calc(50% + 16px);
    right: 16px;
    transition: bottom 0.4s cubic-bezier(0.16, 1, 0.3, 1);
  }

  .panel.collapsed ~ .fab-container {
    bottom: 60px;
  }

  .app-header h1 {
    font-size: 18px;
  }

  .info-footer {
    display: none; /* Save space on mobile */
  }
}
</style>

<style>
/* LEAFLET CUSTOMIZATIONS (Global Styles) */
.leaflet-container {
  font-family: inherit !important;
}

/* Custom Zoom control styles */
.leaflet-right .leaflet-control-zoom {
  margin-right: 24px !important;
  margin-bottom: 80px !important; /* Stand above Locate button */
  border: none !important;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15) !important;
  border-radius: 8px !important;
  overflow: hidden;
}

.leaflet-control-zoom a {
  background-color: #ffffff !important;
  color: #1e293b !important;
  border: 1px solid rgba(0, 0, 0, 0.05) !important;
  font-weight: 500 !important;
  transition: background-color 0.2s;
}

.leaflet-control-zoom a:hover {
  background-color: #f1f5f9 !important;
  color: #000000 !important;
}

/* Custom teardrop marker pins */
.station-marker {
  background: transparent !important;
  border: none !important;
  overflow: visible !important;
}

.station-pin-wrapper {
  position: relative;
  width: 40px;
  height: 50px;
  display: flex;
  align-items: center;
  justify-content: center;
  filter: drop-shadow(0 4px 6px rgba(0,0,0,0.15));
}

.station-pin {
  width: 34px;
  height: 34px;
  background: linear-gradient(135deg, #3b82f6, #1d4ed8);
  border: 2.5px solid #ffffff;
  border-radius: 50% 50% 50% 0;
  transform: rotate(-45deg);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 16px;
  color: #ffffff;
  transition: all 0.25s cubic-bezier(0.4, 0, 0.2, 1);
}

.pin-icon {
  transform: rotate(45deg);
  display: block;
}

/* Green gradient for positive fuel counts */
.station-pin-wrapper.has-fuel .station-pin {
  background: linear-gradient(135deg, #10b981, #047857);
  box-shadow: 0 0 10px rgba(16, 185, 129, 0.4);
}

/* Red gradient for confirmed no fuel */
.station-pin-wrapper.no-fuel .station-pin {
  background: linear-gradient(135deg, #ef4444, #b91c1c);
}

.station-pin-wrapper:hover .station-pin {
  transform: rotate(-45deg) scale(1.15);
  z-index: 999;
}

/* Status-based animations */
.station-pin-wrapper.has-fuel::before {
  content: '';
  position: absolute;
  width: 34px;
  height: 34px;
  border-radius: 50%;
  border: 2px solid #10b981;
  animation: ping-glow 2s infinite ease-out;
  pointer-events: none;
  z-index: -1;
  opacity: 0.75;
}

@keyframes ping-glow {
  0% { transform: scale(1); opacity: 0.8; }
  100% { transform: scale(1.7); opacity: 0; }
}

/* Station counter badge positioning */
.station-badge {
  position: absolute;
  top: -8px;
  right: -8px;
  background: #ffffff;
  border-radius: 12px;
  padding: 2px 7px;
  font-size: 10px;
  font-weight: 700;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.15);
  display: flex;
  align-items: center;
  gap: 2px;
  border: 1px solid #f1f5f9;
  z-index: 10;
}

.station-badge .yes {
  color: #10b981;
}

.station-badge .no {
  color: #ef4444;
}

.station-badge .sep {
  color: #cbd5e1;
}

/* Sleek Popup Box Design */
.leaflet-popup-content-wrapper {
  background: rgba(255, 255, 255, 0.95) !important;
  backdrop-filter: blur(10px) !important;
  border-radius: 16px !important;
  box-shadow: 0 20px 25px -5px rgba(0, 0, 0, 0.15), 0 10px 10px -5px rgba(0, 0, 0, 0.04) !important;
  border: 1px solid rgba(255, 255, 255, 0.5) !important;
  padding: 6px !important;
}

.leaflet-popup-tip {
  background: rgba(255, 255, 255, 0.95) !important;
  box-shadow: 0 10px 15px -3px rgba(0, 0, 0, 0.1) !important;
}

.leaflet-popup-content {
  margin: 12px !important;
  width: 250px !important;
  font-family: 'Rubik', sans-serif !important;
}

.popup-title-bar h3 {
  margin: 0 0 2px 0;
  font-size: 15px;
  font-weight: 700;
  color: #0f172a;
}

.popup-brand {
  font-size: 11px;
  color: #64748b;
  text-transform: uppercase;
  font-weight: 600;
  letter-spacing: 0.5px;
}

.station-popup .fuel-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin: 12px 0 10px 0;
}

.station-popup .fuel-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 6px 10px;
  background: #f8fafc;
  border-radius: 10px;
  border: 1px solid #f1f5f9;
}

.station-popup .fuel-info {
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.station-popup .fuel-name {
  font-weight: 700;
  font-size: 13px;
  color: #1e293b;
}

.station-popup .fuel-counts {
  font-size: 11px;
  color: #64748b;
  display: flex;
  align-items: center;
  gap: 3px;
}

.station-popup .fuel-counts .yes {
  color: #059669;
  font-weight: 500;
}

.station-popup .fuel-counts .no {
  color: #dc2626;
  font-weight: 500;
}

.station-popup .fuel-counts .sep {
  color: #e2e8f0;
}

.station-popup .fuel-buttons {
  display: flex;
  gap: 6px;
}

/* Beautiful custom buttons with SVG */
.station-popup .fuel-buttons button {
  width: 32px;
  height: 32px;
  border: none;
  border-radius: 8px;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1);
  padding: 0;
}

.station-popup .vote-icon {
  width: 16px;
  height: 16px;
}

.station-popup .btn-yes {
  background: #dcfce7;
  color: #15803d;
}

.station-popup .btn-yes:hover {
  background: #bbf7d0;
  transform: scale(1.08);
}

.station-popup .btn-yes:active {
  transform: scale(0.95);
}

.station-popup .btn-no {
  background: #fee2e2;
  color: #b91c1c;
}

.station-popup .btn-no:hover {
  background: #fecaca;
  transform: scale(1.08);
}

.station-popup .btn-no:active {
  transform: scale(0.95);
}

.station-popup .hint {
  margin: 0;
  font-size: 10px;
  color: #94a3b8;
  text-align: center;
}
</style>
