import { describe, expect, it } from 'vitest'
import { buildView, visibleColumns } from './columns.js'
import { compileFilter } from './filter.js'
import { detectMode, hexDump, jsonTokens, looksLikeText, prettyJson, stripXssi } from './body.js'
import { formatBytes, formatDuration } from './format.js'
import { parseCookieHeader, parseQuery, parseSetCookie, rawMessage, statusClass } from './http.js'

const rows = [
  { id: 1, rev: 1, method: 'GET', host: 'api.test', path: '/users', status: 200, contentType: 'application/json', respSize: 50, duration: 30 },
  { id: 2, rev: 1, method: 'POST', host: 'api.test', path: '/login', status: 401, respSize: 10, duration: 5 },
  { id: 3, rev: 1, method: 'GET', host: 'cdn.test', path: '/app.js', status: 200, contentType: 'text/javascript', respSize: 900, duration: 80 },
  { id: 4, rev: 1, method: 'CONNECT', host: 'secure.test:443', path: '', kind: 'tunnel', respSize: 0, duration: -1 },
]
const byId = new Map(rows.map((r) => [r.id, r]))
const ids = (list) => list.map((r) => r.id)

describe('filter', () => {
  it('requires every term, ignoring case', () => {
    expect(ids(rows.filter(compileFilter('API get')))).toEqual([1])
  })
  it('excludes terms prefixed with -', () => {
    expect(ids(rows.filter(compileFilter('-cdn -tunnel')))).toEqual([1, 2])
  })
  it('matches status codes and content types', () => {
    expect(ids(rows.filter(compileFilter('401')))).toEqual([2])
    expect(ids(rows.filter(compileFilter('javascript')))).toEqual([3])
  })
  it('is null when empty', () => {
    expect(compileFilter('   ')).toBe(null)
  })
})

describe('buildView', () => {
  it('sorts by column and breaks ties by id', () => {
    expect(ids(buildView(byId, '', { key: 'size', dir: 'desc' }))).toEqual([3, 1, 2, 4])
    expect(ids(buildView(byId, '', { key: 'method', dir: 'asc' }))).toEqual([4, 1, 3, 2])
  })
  it('filters, then sorts', () => {
    expect(ids(buildView(byId, 'get', { key: 'started', dir: 'desc' }))).toEqual([3, 1])
  })
})

describe('visibleColumns', () => {
  const keys = (w) => visibleColumns(w).map((c) => c.key)
  it('shows everything when there is room', () => {
    expect(keys(1400)).toContain('started')
  })
  it('drops low-priority columns before squeezing host and path', () => {
    const narrow = keys(520)
    expect(narrow).toEqual(['status', 'method', 'host', 'path'])
    const mid = keys(760)
    expect(mid).toContain('size')
    expect(mid).not.toContain('started')
  })
})

describe('http helpers', () => {
  it('parses query strings', () => {
    expect(parseQuery('/s?q=a%20b&tag=x&tag=y')).toEqual([
      { name: 'q', value: 'a b' },
      { name: 'tag', value: 'x' },
      { name: 'tag', value: 'y' },
    ])
    expect(parseQuery('/plain')).toEqual([])
  })
  it('parses cookies', () => {
    expect(parseCookieHeader(['a=1; b=two=2', 'c=3'])).toEqual([
      { name: 'a', value: '1' },
      { name: 'b', value: 'two=2' },
      { name: 'c', value: '3' },
    ])
  })
  it('parses Set-Cookie attributes', () => {
    expect(parseSetCookie('sid=abc; Path=/; HttpOnly; SameSite=Lax; Max-Age=60')).toEqual({
      name: 'sid',
      value: 'abc',
      attrs: { path: '/', httponly: true, samesite: 'Lax', 'max-age': '60' },
    })
  })
  it('classifies statuses', () => {
    expect([101, 204, 302, 404, 503, undefined].map(statusClass)).toEqual([
      'info', 'ok', 'redirect', 'client-err', 'server-err', 'none',
    ])
  })
  it('builds raw messages', () => {
    expect(rawMessage('GET / HTTP/1.1', [{ name: 'Host', value: 'a' }], 'x')).toBe(
      'GET / HTTP/1.1\r\nHost: a\r\n\r\nx',
    )
  })
})

describe('body helpers', () => {
  const enc = (s) => new TextEncoder().encode(s)

  it('picks a viewer', () => {
    expect(detectMode('application/vnd.api+json', enc('{}'))).toBe('json')
    expect(detectMode('text/html; charset=utf-8', enc('<p>'))).toBe('text')
    expect(detectMode(undefined, enc('  [1,2]'))).toBe('json')
    expect(detectMode('application/octet-stream', new Uint8Array([0, 1, 2, 255]))).toBe('hex')
  })
  it('strips anti-XSSI prefixes', () => {
    expect(stripXssi(`)]}'\n{"a":1}`)).toEqual({ text: '{"a":1}', prefix: ")]}'" })
    expect(stripXssi('while(1);[1]')).toEqual({ text: '[1]', prefix: 'while(1);' })
    expect(stripXssi('{"a":1}').prefix).toBe(null)
    expect(detectMode(undefined, enc(`)]}'\n{"x":1}`))).toBe('json')
  })
  it('tells text from binary', () => {
    expect(looksLikeText(new Uint8Array([0x89, 0x50, 0x4e, 0x47, 0, 0]))).toBe(false)
    expect(looksLikeText(enc('plain text\n'))).toBe(true)
  })
  it('pretty prints JSON, or returns null', () => {
    expect(prettyJson('{"a":[1]}')).toBe('{\n  "a": [\n    1\n  ]\n}')
    expect(prettyJson('{nope')).toBe(null)
  })
  it('tokenizes JSON without dropping characters', () => {
    const pretty = prettyJson('{"k":"v","n":-1.5e3,"t":true,"z":null,"esc":"a\\"b"}')
    const toks = jsonTokens(pretty)
    expect(toks.map((t) => t.v).join('')).toBe(pretty)
    const kind = (v) => toks.find((t) => t.v === v).t
    expect(kind('"k"')).toBe('key')
    expect(kind('"v"')).toBe('string')
    expect(kind('-1500')).toBe('number')
    expect(kind('null')).toBe('literal')
  })
  it('hex dumps', () => {
    const line = hexDump(enc('AB'))
    expect(line.startsWith('00000000  41 42 ')).toBe(true)
    expect(line.endsWith(' AB')).toBe(true)
  })
})

describe('format', () => {
  it('formats sizes and durations', () => {
    expect(formatBytes(512)).toBe('512 B')
    expect(formatBytes(1536)).toBe('1.5 KB')
    expect(formatDuration(-1)).toBe('')
    expect(formatDuration(42.4)).toBe('42 ms')
    expect(formatDuration(1500)).toBe('1.50 s')
  })
})
