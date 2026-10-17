import { describe, test } from 'node:test';
import assert from 'node:assert/strict';
import { distance, offset, parseLookup, placeNear, proxyKey } from '../src/geo.js';

const bangkok = { latitude: 13.7563, longitude: 100.5018 };

describe('parseLookup', () => {
  test('reads the shapes of the common lookup services', () => {
    assert.deepEqual(parseLookup({ loc: '13.7563,100.5018' }), bangkok);
    assert.deepEqual(parseLookup({ lat: 13.7563, lon: 100.5018 }), bangkok);
    assert.deepEqual(parseLookup({ latitude: '13.7563', longitude: '100.5018' }), bangkok);
  });

  test('an answer without usable coordinates is none', () => {
    for (const data of [null, {}, { loc: 'x,y' }, { lat: 91, lon: 0 }, { latitude: 0, longitude: 0 }, { error: true }]) {
      assert.equal(parseLookup(data), null, JSON.stringify(data));
    }
  });
});

describe('offset', () => {
  test('moves the point the asked distance along the bearing', () => {
    const north = offset(bangkok, 50, 0);
    assert.ok(Math.abs(distance(bangkok, north) - 50) < 0.01);
    assert.ok(north.latitude > bangkok.latitude);
    assert.ok(Math.abs(north.longitude - bangkok.longitude) < 1e-9);
    const east = offset(bangkok, 50, 90);
    assert.ok(east.longitude > bangkok.longitude);
  });
});

describe('placeNear', () => {
  test('stays between twenty and eighty metres away', () => {
    for (const r of [0, 0.5, 0.999]) {
      const p = placeNear(bangkok, () => r);
      const metres = distance(bangkok, p);
      assert.ok(metres >= 19.9 && metres <= 80.1, `${metres}`);
      assert.ok(p.accuracy >= 30 && p.accuracy <= 80);
    }
  });
});

describe('proxyKey', () => {
  test('tells proxies apart without carrying the password', () => {
    const key = proxyKey('http://alice:s3cret@10.0.0.1:8080');
    assert.notEqual(key, proxyKey('http://alice:s3cret@10.0.0.2:8080'));
    assert.ok(!key.includes('s3cret'));
  });
});
