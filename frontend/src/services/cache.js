const DB_NAME = 'zapravka'
const DB_VERSION = 1
const STORE_NAME = 'stations'
const TILE_STORE_NAME = 'tiles'
const STATION_TTL_MS = 60 * 60 * 1000 // 1 час

function openDB() {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB_NAME, DB_VERSION)
    req.onerror = () => reject(req.error)
    req.onsuccess = () => resolve(req.result)
    req.onupgradeneeded = (event) => {
      const db = event.target.result
      if (!db.objectStoreNames.contains(STORE_NAME)) {
        const store = db.createObjectStore(STORE_NAME, { keyPath: 'id' })
        store.createIndex('lat', 'lat', { unique: false })
        store.createIndex('lon', 'lon', { unique: false })
        store.createIndex('fetchedAt', 'fetchedAt', { unique: false })
      }
      if (!db.objectStoreNames.contains(TILE_STORE_NAME)) {
        db.createObjectStore(TILE_STORE_NAME, { keyPath: 'key' })
      }
    }
  })
}

function haversine(lat1, lon1, lat2, lon2) {
  const R = 6371000 // метры
  const toRad = (x) => (x * Math.PI) / 180
  const dLat = toRad(lat2 - lat1)
  const dLon = toRad(lon2 - lon1)
  const a =
    Math.sin(dLat / 2) ** 2 +
    Math.cos(toRad(lat1)) * Math.cos(toRad(lat2)) * Math.sin(dLon / 2) ** 2
  const c = 2 * Math.atan2(Math.sqrt(a), Math.sqrt(1 - a))
  return R * c
}

export async function saveStations(stations) {
  const db = await openDB()
  const tx = db.transaction(STORE_NAME, 'readwrite')
  const store = tx.objectStore(STORE_NAME)
  const now = Date.now()
  for (const s of stations) {
    store.put({ ...s, fetchedAt: now })
  }
  return new Promise((resolve, reject) => {
    tx.oncomplete = () => resolve()
    tx.onerror = () => reject(tx.error)
  })
}

export async function getCachedStations(lat, lon, radiusMeters) {
  const db = await openDB()
  const tx = db.transaction(STORE_NAME, 'readonly')
  const store = tx.objectStore(STORE_NAME)
  const now = Date.now()
  const result = []

  return new Promise((resolve, reject) => {
    const req = store.openCursor()
    req.onerror = () => reject(req.error)
    req.onsuccess = (event) => {
      const cursor = event.target.result
      if (!cursor) {
        resolve(result)
        return
      }
      const s = cursor.value
      if (now - s.fetchedAt < STATION_TTL_MS) {
        const d = haversine(lat, lon, s.lat, s.lon)
        if (d <= radiusMeters) {
          result.push(s)
        }
      }
      cursor.continue()
    }
  })
}

export async function clearOldStations() {
  const db = await openDB()
  const tx = db.transaction(STORE_NAME, 'readwrite')
  const store = tx.objectStore(STORE_NAME)
  const now = Date.now()
  let deleted = 0

  return new Promise((resolve, reject) => {
    const req = store.openCursor()
    req.onerror = () => reject(req.error)
    req.onsuccess = (event) => {
      const cursor = event.target.result
      if (!cursor) {
        resolve(deleted)
        return
      }
      const s = cursor.value
      if (now - s.fetchedAt > STATION_TTL_MS) {
        store.delete(cursor.primaryKey)
        deleted++
      }
      cursor.continue()
    }
  })
}

// Простое хранилище для тайлов карты (ключ: url, value: blob)
export async function saveTile(url, blob) {
  const db = await openDB()
  const tx = db.transaction(TILE_STORE_NAME, 'readwrite')
  const store = tx.objectStore(TILE_STORE_NAME)
  store.put({ key: url, blob, savedAt: Date.now() })
  return new Promise((resolve, reject) => {
    tx.oncomplete = () => resolve()
    tx.onerror = () => reject(tx.error)
  })
}

export async function getTile(url) {
  const db = await openDB()
  const tx = db.transaction(TILE_STORE_NAME, 'readonly')
  const store = tx.objectStore(TILE_STORE_NAME)
  return new Promise((resolve, reject) => {
    const req = store.get(url)
    req.onerror = () => reject(req.error)
    req.onsuccess = () => resolve(req.result?.blob || null)
  })
}
