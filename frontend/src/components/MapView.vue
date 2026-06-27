<template>
  <div class="map-container">
    <div ref="mapRef" class="map"></div>
    <div class="info">
      <h1>Карта заправок</h1>
      <p>Нажмите на заправку, чтобы сообщить о наличии топлива.</p>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import L from 'leaflet'

const API_URL = import.meta.env.VITE_API_URL || '/api'
const mapRef = ref(null)
const stations = ref([])
let map = null
let markersLayer = null
let moveTimeout = null

const fuelTypes = [
  { key: '92', label: 'АИ-92' },
  { key: '95', label: 'АИ-95' },
  { key: 'diesel', label: 'ДТ' }
]

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

async function loadStations() {
  const center = map.getCenter()
  try {
    const res = await fetch(
      `${API_URL}/stations?lat=${center.lat.toFixed(6)}&lon=${center.lng.toFixed(6)}&radius=5000`
    )
    if (!res.ok) throw new Error('failed to load stations')
    const data = await res.json()
    stations.value = data.stations || []
    renderMarkers()
  } catch (err) {
    console.error('Ошибка загрузки заправок:', err)
  }
}

function renderMarkers() {
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
    renderMarkers()
  } catch (err) {
    console.error('Ошибка голосования:', err)
    alert('Не удалось сохранить голос. Попробуйте позже.')
  }
}

onMounted(() => {
  map = L.map(mapRef.value).setView([55.7558, 37.6173], 13)

  L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
    attribution: '&copy; OpenStreetMap contributors',
    maxZoom: 19
  }).addTo(map)

  markersLayer = L.layerGroup().addTo(map)

  loadStations()

  map.on('moveend', () => {
    clearTimeout(moveTimeout)
    moveTimeout = setTimeout(loadStations, 300)
  })

  if ('geolocation' in navigator) {
    navigator.geolocation.getCurrentPosition(
      (pos) => {
        const { latitude, longitude } = pos.coords
        map.setView([latitude, longitude], 14)
        loadStations()
      },
      (err) => {
        console.warn('Геолокация недоступна:', err)
      }
    )
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
  max-width: 320px;
}

.info h1 {
  margin: 0 0 6px;
  font-size: 18px;
}

.info p {
  margin: 0;
  font-size: 13px;
  color: #555;
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
