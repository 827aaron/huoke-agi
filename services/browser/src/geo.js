import { createHash } from 'node:crypto';

const EARTH_RADIUS_M = 6371008.8;

// A geo-IP lookup places an IP on its city or ISP centroid, shared by every
// address there; an account standing exactly on it looks like a proxy user.
const MIN_OFFSET_M = 20;
const MAX_OFFSET_M = 80;

// Reads the coordinates of the shapes ipinfo.io ("loc"), ip-api.com ("lat"/"lon")
// and ipapi.co ("latitude"/"longitude") return; null when there are none.
export function parseLookup(data) {
  if (!data || typeof data !== 'object') return null;
  let lat;
  let lon;
  if (typeof data.loc === 'string') [lat, lon] = data.loc.split(',').map(Number);
  else if ('lat' in data) [lat, lon] = [Number(data.lat), Number(data.lon)];
  else [lat, lon] = [Number(data.latitude), Number(data.longitude)];
  if (!Number.isFinite(lat) || !Number.isFinite(lon) || Math.abs(lat) > 90 || Math.abs(lon) > 180) return null;
  if (lat === 0 && lon === 0) return null;
  return { latitude: lat, longitude: lon };
}

// The point metres away along bearing (degrees clockwise from north), on a sphere.
export function offset({ latitude, longitude }, metres, bearing) {
  const d = metres / EARTH_RADIUS_M;
  const b = (bearing * Math.PI) / 180;
  const lat1 = (latitude * Math.PI) / 180;
  const lon1 = (longitude * Math.PI) / 180;
  const lat2 = Math.asin(Math.sin(lat1) * Math.cos(d) + Math.cos(lat1) * Math.sin(d) * Math.cos(b));
  const lon2 = lon1 + Math.atan2(Math.sin(b) * Math.sin(d) * Math.cos(lat1), Math.cos(d) - Math.sin(lat1) * Math.sin(lat2));
  return { latitude: (lat2 * 180) / Math.PI, longitude: ((((lon2 * 180) / Math.PI + 540) % 360) - 180) };
}

export function distance(a, b) {
  const rad = Math.PI / 180;
  const dLat = (b.latitude - a.latitude) * rad;
  const dLon = (b.longitude - a.longitude) * rad;
  const h = Math.sin(dLat / 2) ** 2 + Math.cos(a.latitude * rad) * Math.cos(b.latitude * rad) * Math.sin(dLon / 2) ** 2;
  return 2 * EARTH_RADIUS_M * Math.asin(Math.sqrt(h));
}

export function placeNear(point, rand = Math.random) {
  const metres = MIN_OFFSET_M + rand() * (MAX_OFFSET_M - MIN_OFFSET_M);
  const { latitude, longitude } = offset(point, metres, rand() * 360);
  return { latitude: round(latitude), longitude: round(longitude), accuracy: Math.round(30 + rand() * 50) };
}

// Six decimals is about 10 cm, what a phone or a Wi-Fi fix reports.
const round = (v) => Math.round(v * 1e6) / 1e6;

// Identifies the proxy a stored position was looked up through without
// writing its credentials into meta.json a second time.
export function proxyKey(proxy) {
  return createHash('sha256').update(proxy).digest('hex').slice(0, 16);
}
