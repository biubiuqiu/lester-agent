import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import http from 'node:http';

if (process.argv[2] === 'serve') {
  const api = http.createServer(async (req, res) => {
    if (req.url === '/api/events') {
      res.writeHead(200, { 'Content-Type': 'text/event-stream' });
      // Deliberately omit X-Accel-Buffering to test the gateway configuration.
      res.write('id: 1\ndata: first\n\n');
      const timer = setTimeout(() => res.end('id: 2\ndata: last\n\n'), 4000);
      res.on('close', () => clearTimeout(timer));
      return;
    }
    if (req.url === '/api/preview/index.html') {
      if (req.headers.cookie !== 'session=fixture') {
        res.writeHead(401).end();
        return;
      }
      res.writeHead(200, {
        'Content-Type': 'text/html',
        'Content-Security-Policy': "sandbox allow-scripts; default-src 'none'",
        'Cache-Control': 'no-store',
      });
      res.end('<h1>private preview</h1>');
      return;
    }
    let bytes = 0;
    for await (const chunk of req) bytes += chunk.length;
    res.setHeader('Content-Type', 'application/json');
    res.setHeader('Set-Cookie', 'session=fixture; Path=/; HttpOnly; SameSite=Lax');
    res.end(JSON.stringify({ upstream: 'api', url: req.url, method: req.method, headers: req.headers, bytes }));
  });
  api.on('upgrade', (req, socket) => {
    const accept = createHash('sha1')
      .update(req.headers['sec-websocket-key'] + '258EAFA5-E914-47DA-95CA-C5AB0DC85B11').digest('base64');
    socket.write(`HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: ${accept}\r\n\r\n`);
    // Minimal fixture for one short, masked client text frame, then close.
    let frame = Buffer.alloc(0);
    socket.on('data', (chunk) => {
      frame = Buffer.concat([frame, chunk]);
      if (frame.length < 6) return;
      const length = frame[1] & 127;
      if (frame.length < 6 + length) return;
      if ((frame[0] & 15) === 8) { socket.end(Buffer.from([0x88, 0])); return; }
      const payload = Buffer.from(frame.subarray(6, 6 + length));
      for (let i = 0; i < length; i++) payload[i] ^= frame[2 + i % 4];
      socket.write(Buffer.concat([Buffer.from([0x81, length]), payload]));
      frame = frame.subarray(6 + length);
    });
    socket.on('error', () => {});
  });
  api.listen(8080, '0.0.0.0');
  http.createServer((req, res) => res.end(`web:${req.url}`)).listen(3000, '0.0.0.0');
} else {
  const base = 'http://gateway:8080';
  const request = (path, options) => fetch(base + path, { signal: AbortSignal.timeout(10000), ...options });
  for (const path of ['/', '/app', '/_next/static/app.js', '/apiculture']) {
    assert.equal(await (await request(path)).text(), `web:${path}`);
  }
  assert.equal(await (await request('/healthz')).text(), 'ok\n');
  for (const path of ['/api', '/api/files/a%20b%22.txt?path=x%2Fy&v=1']) {
    const result = await (await request(path)).json();
    assert.equal(result.upstream, 'api');
    assert.equal(result.url, path);
  }
  const response = await request('/api/login', {
    method: 'POST', body: 'fixture', headers: {
      Cookie: 'session=fixture', Authorization: 'Bearer fixture',
      Origin: 'http://gateway:8080', 'Last-Event-ID': '42',
      'X-Forwarded-Proto': 'https', 'X-Forwarded-For': 'spoofed',
    },
  });
  assert.match(response.headers.get('set-cookie'), /HttpOnly/);
  const result = await response.json();
  assert.equal(result.method, 'POST');
  assert.equal(result.headers.cookie, 'session=fixture');
  assert.equal(result.headers.authorization, 'Bearer fixture');
  assert.equal(result.headers.origin, 'http://gateway:8080');
  assert.equal(result.headers['last-event-id'], '42');
  assert.equal(result.headers.host, 'gateway:8080');
  assert.equal(result.headers['x-forwarded-proto'], 'http');
  assert.notEqual(result.headers['x-forwarded-for'], 'spoofed');
  console.log('PASS routes, escaped paths, auth/cookies, cursor, forwarding headers');

  assert.equal((await request('/api/preview/index.html')).status, 401);
  const preview = await request('/api/preview/index.html', { headers: { Cookie: 'session=fixture' } });
  assert.equal(preview.headers.get('content-security-policy'), "sandbox allow-scripts; default-src 'none'");
  assert.equal(preview.headers.get('cache-control'), 'no-store');
  assert.match(await preview.text(), /private preview/);
  console.log('PASS private HTML response and CSP passthrough');

  const upload = new FormData();
  upload.append('file', new Blob([new Uint8Array(25 * 1024 * 1024)]), 'long file.txt');
  const uploaded = await request('/api/upload', { method: 'POST', body: upload });
  assert.equal(uploaded.status, 200);
  assert.ok((await uploaded.json()).bytes > 25 * 1024 * 1024);
  assert.equal((await request('/api/upload', { method: 'POST', body: new Uint8Array(27 * 1024 * 1024) })).status, 413);
  console.log('PASS 25 MiB multipart upload and gateway size cap');

  const start = Date.now();
  const stream = await request('/api/events');
  const reader = stream.body.getReader();
  const first = new TextDecoder().decode((await reader.read()).value);
  assert.match(first, /data: first/);
  assert.doesNotMatch(first, /data: last/);
  assert.ok(Date.now() - start < 3000, 'SSE first event must arrive before upstream completes');
  await reader.cancel();
  console.log('PASS unbuffered SSE');

  await new Promise((resolve, reject) => {
    const ws = new WebSocket('ws://gateway:8080/api/terminal');
    const timer = setTimeout(() => { ws.close(); reject(new Error('WebSocket timeout')); }, 5000);
    ws.onopen = () => ws.send('terminal echo');
    ws.onmessage = ({ data }) => {
      try { assert.equal(data, 'terminal echo'); clearTimeout(timer); ws.close(); resolve(); }
      catch (error) { clearTimeout(timer); ws.close(); reject(error); }
    };
    ws.onerror = () => { clearTimeout(timer); reject(new Error('WebSocket upgrade failed')); };
  });
  console.log('PASS bidirectional WebSocket');
}
