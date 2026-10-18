import { describe, it, expect } from 'vitest';

import { parseLogLine } from '@/pages/index/logParse';

// Fixtures are real lines captured from `journalctl -u miniray` on a production
// host (the SysLog view) plus the in-memory app-log format. Each journald entry
// carries a "Mon DD HH:MM:SS host ident[pid]: " prefix that the viewer used to
// mistake for the level, leaving only a bare timestamp on screen.
describe('parseLogLine — SysLog (journalctl) formats', () => {
  it('miniray go-logging line: keeps level, strips prefix, tags miniray', () => {
    const r = parseLogLine(
      'Jun 08 23:57:28 ubuntu-4gb-fsn1-1 /usr/local/miniray/miniray[72297]: INFO - inbound config applied for tag example',
    );
    expect(r.stamp).toBe('Jun 08 23:57:28');
    expect(r.levelText).toBe('INFO');
    expect(r.service).toBe('miniray:');
    expect(r.body).toBe('inbound config applied for tag example');
  });

  it('xray go-logging line: lifts the XRAY service tag', () => {
    const r = parseLogLine(
      'Jun 08 23:56:52 ubuntu-4gb-fsn1-1 /usr/local/miniray/miniray[72297]: WARNING - XRAY: core: Xray 26.6.1 started',
    );
    expect(r.stamp).toBe('Jun 08 23:56:52');
    expect(r.levelText).toBe('WARNING');
    expect(r.service).toBe('XRAY:');
    expect(r.body).toBe('core: Xray 26.6.1 started');
  });

  it('Go std-log line: strips the redundant embedded date, keeps the message', () => {
    const r = parseLogLine(
      'Jun 08 19:22:22 ubuntu-4gb-fsn1-1 miniray[1439]: 2026/06/08 19:22:22 http: TLS handshake error from 18.97.5.1:36022: EOF',
    );
    expect(r.stamp).toBe('Jun 08 19:22:22');
    expect(r.levelText).toBe('');
    expect(r.body).toBe('http: TLS handshake error from 18.97.5.1:36022: EOF');
  });

  it('bracketed timestamp line: lifts the ERROR level out of "[ts] ERROR ..."', () => {
    const r = parseLogLine(
      'Jun 09 00:14:52 ubuntu-4gb-fsn1-1 miniray[72297]: [Tue Jun  9 00:14:52 UTC 2026] ERROR Restarting proxy service in 8s...',
    );
    expect(r.stamp).toBe('Jun 09 00:14:52');
    expect(r.levelText).toBe('ERROR');
    expect(r.body).toBe('Restarting proxy service in 8s...');
  });

  it('systemd line: shows the body rather than a bare timestamp', () => {
    const r = parseLogLine(
      'Jun 08 23:56:47 ubuntu-4gb-fsn1-1 systemd[1]: Stopping miniray.service - miniray Service...',
    );
    expect(r.stamp).toBe('Jun 08 23:56:47');
    expect(r.body).toBe('Stopping miniray.service - miniray Service...');
  });

  it('never collapses a journald entry to just its timestamp', () => {
    const r = parseLogLine(
      'Jun 09 00:15:00 ubuntu-4gb-fsn1-1 miniray[72297]: [Tue Jun  9 00:15:00 UTC 2026] ERROR Proxy restart rejected: Conflict',
    );
    expect(r.body.length).toBeGreaterThan(0);
    expect(r.body).toContain('Conflict');
  });
});

describe('parseLogLine — app-log format (SysLog off)', () => {
  it('parses "YYYY/MM/DD HH:MM:SS LEVEL - body"', () => {
    const r = parseLogLine(
      '2026/06/09 00:35:09 INFO - inbound config applied for tag example',
    );
    expect(r.date).toBe('2026/06/09');
    expect(r.time).toBe('00:35:09');
    expect(r.levelText).toBe('INFO');
    expect(r.service).toBe('miniray:');
    expect(r.body).toBe('inbound config applied for tag example');
  });

  it('handles an empty line without throwing', () => {
    const r = parseLogLine('');
    expect(r.stamp).toBe('');
    expect(r.body).toBe('');
  });
});
