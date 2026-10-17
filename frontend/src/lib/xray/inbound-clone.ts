import { RandomUtil } from '@/utils';
import { createDefaultInboundSettings } from '@/lib/xray/inbound-defaults';
import { coerceInboundJsonField, type DBInbound } from '@/models/dbinbound';

export function buildClonePayload(dbInbound: DBInbound, port: number) {
  let clonedSettings: string;
  try {
    const raw = { ...coerceInboundJsonField(dbInbound.settings) };
    raw.clients = [];
    clonedSettings = JSON.stringify(raw);
  } catch {
    const fallback = createDefaultInboundSettings(dbInbound.protocol);
    clonedSettings = fallback ? JSON.stringify(fallback, null, 2) : '{}';
  }
  const streamSettingsString =
    typeof dbInbound.streamSettings === 'string'
      ? dbInbound.streamSettings
      : JSON.stringify(dbInbound.streamSettings ?? {});
  const sniffingString =
    typeof dbInbound.sniffing === 'string'
      ? dbInbound.sniffing
      : JSON.stringify(dbInbound.sniffing ?? {});
  return {
    up: 0,
    down: 0,
    total: 0,
    remark: `${dbInbound.remark} (clone)`,
    enable: false,
    expiryTime: 0,
    listen: '',
    port,
    protocol: dbInbound.protocol,
    settings: clonedSettings,
    streamSettings: streamSettingsString,
    sniffing: sniffingString,
    shareAddrStrategy: dbInbound.shareAddrStrategy,
    shareAddr: dbInbound.shareAddr,
  };
}

export function pickClonePort(used: Set<number> | undefined): number {
  let port = RandomUtil.randomInteger(10000, 60000);
  if (!used) return port;
  for (let attempts = 0; attempts < 20 && used.has(port); attempts++) {
    port = RandomUtil.randomInteger(10000, 60000);
  }
  if (used.has(port)) {
    for (port = 10000; port <= 60000 && used.has(port); port++) {
      /* dense-range scan */
    }
    if (port > 60000) port = RandomUtil.randomInteger(10000, 60000);
  }
  return port;
}
