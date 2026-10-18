import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { HttpUtil } from '@/utils';
import { parseMsg } from '@/utils/zodValidate';
import { DBInbound, coerceInboundJsonField } from '@/models/dbinbound';
import type { ClientStats, DBInboundInit } from '@/models/dbinbound';
import { Protocols } from '@/schemas/primitives';
import { isSSMultiUser } from '@/lib/xray/protocol-capabilities';
import { setDatepicker } from '@/hooks/useDatepicker';
import { keys } from '@/api/queryKeys';
import { SlimInboundListSchema, LastOnlineMapSchema, InboundDetailSchema } from '@/schemas/inbound';
import { OnlinesSchema } from '@/schemas/client';
import { DefaultsPayloadSchema, type DefaultsPayload } from '@/schemas/defaults';
import type { InboundSpeedEntry } from './list/types';
import { TRAFFIC_POLL_INTERVAL_S } from '@/lib/traffic/poll-interval';
export interface SubSettings {
  enable: boolean;
  subTitle: string;
  subURI: string;
  subJsonURI: string;
  subJsonEnable: boolean;
  // Configured public host (Sub Domain, else Web Domain) used as the share/QR
  // link host when the panel is reached on a loopback address. Empty if neither
  // is set.
  publicHost: string;
}
type DBInboundInstance = InstanceType<typeof DBInbound>;
// Speed is delta-derived, so it can't be recomputed until the first poll after
// mount; navigating away and back would otherwise blank the column for up to one
// poll. Cache the last speed map across mounts (module scope) and reseed from it
// while recent, so returning to the page shows the last throughput immediately
// and the next poll refreshes it.
const SPEED_CACHE_TTL_MS = 15000;
let inboundSpeedCache: {
  at: number;
  data: Record<number, InboundSpeedEntry>;
} = {
  at: 0,
  data: {},
};
interface TrafficDelta {
  Tag: string;
  Up: number;
  Down: number;
  IsInbound?: boolean;
}
interface ClientRollup {
  clients: number;
  active: string[];
  deactive: string[];
  depleted: string[];
  expiring: string[];
  online: string[];
  comments: Map<string, string>;
}
const TRACKED_PROTOCOLS: readonly string[] = [
  Protocols.VMESS,
  Protocols.VLESS,
  Protocols.TROJAN,
  Protocols.SHADOWSOCKS,
  Protocols.HYSTERIA,
  Protocols.WIREGUARD,
  Protocols.AMNEZIAWG,
  Protocols.TUIC,
];
async function fetchSlimInbounds(): Promise<unknown[]> {
  const msg = await HttpUtil.get('/panel/api/inbounds/list/slim', undefined, { silent: true });
  if (!msg?.success) throw new Error(msg?.msg || 'Failed to fetch inbounds');
  const validated = parseMsg(msg, SlimInboundListSchema, 'inbounds/list/slim');
  return Array.isArray(validated.obj) ? validated.obj : [];
}
async function fetchOnlineClients(): Promise<string[]> {
  const msg = await HttpUtil.post('/panel/api/clients/onlines', undefined, { silent: true });
  if (!msg?.success) throw new Error(msg?.msg || 'Failed to fetch onlines');
  const validated = parseMsg(msg, OnlinesSchema, 'clients/onlines');
  return Array.isArray(validated.obj) ? validated.obj : [];
}

async function fetchActiveInbounds(): Promise<string[]> {
  const msg = await HttpUtil.post('/panel/api/clients/activeInbounds', undefined, { silent: true });
  if (!msg?.success) throw new Error(msg?.msg || 'Failed to fetch active inbounds');
  return parseMsg(msg, OnlinesSchema, 'clients/activeInbounds').obj ?? [];
}

// Most pushes repeat the previous online sets; handing back a new Map anyway
// re-ran the client rollup over every inbound on each traffic event.

async function fetchLastOnlineMap(): Promise<Record<string, number>> {
  const msg = await HttpUtil.post('/panel/api/clients/lastOnline', undefined, { silent: true });
  if (!msg?.success) throw new Error(msg?.msg || 'Failed to fetch lastOnline');
  const validated = parseMsg(msg, LastOnlineMapSchema, 'clients/lastOnline');
  return validated.obj && typeof validated.obj === 'object' ? validated.obj : {};
}
async function fetchDefaultSettings(): Promise<DefaultsPayload> {
  const msg = await HttpUtil.post('/panel/api/setting/defaultSettings', undefined, {
    silent: true,
  });
  if (!msg?.success) throw new Error(msg?.msg || 'Failed to fetch defaults');
  const validated = parseMsg(msg, DefaultsPayloadSchema, 'setting/defaultSettings');
  return validated.obj ?? {};
}
export function useInbounds() {
  const queryClient = useQueryClient();
  const slimQuery = useQuery({
    queryKey: keys.inbounds.slim(),
    queryFn: fetchSlimInbounds,
    staleTime: Infinity,
  });
  const onlinesQuery = useQuery({
    queryKey: keys.clients.onlines(),
    queryFn: fetchOnlineClients,
    staleTime: Infinity,
  });

  const activeInboundsQuery = useQuery({
    queryKey: keys.clients.activeInbounds(),
    queryFn: fetchActiveInbounds,
    staleTime: Infinity,
  });
  const lastOnlineQuery = useQuery({
    queryKey: keys.clients.lastOnline(),
    queryFn: fetchLastOnlineMap,
    staleTime: Infinity,
  });
  const defaultsQuery = useQuery({
    queryKey: keys.settings.defaults(),
    queryFn: fetchDefaultSettings,
    staleTime: Infinity,
  });
  const defaults = defaultsQuery.data ?? {};
  const expireDiff = (defaults.expireDiff ?? 0) * 86400000;
  const trafficDiff = (defaults.trafficDiff ?? 0) * 1073741824;
  const tgBotEnable = !!defaults.tgBotEnable;
  const ipLimitEnable = !!defaults.ipLimitEnable;
  const pageSize = defaults.pageSize ?? 0;
  const datepicker = (defaults.datepicker as 'gregorian' | 'jalalian') || 'gregorian';
  const subSettings: SubSettings = useMemo(
    () => ({
      enable: !!defaults.subEnable,
      subTitle: defaults.subTitle || '',
      subURI: defaults.subURI || '',
      subJsonURI: defaults.subJsonURI || '',
      subJsonEnable: !!defaults.subJsonEnable,
      publicHost: defaults.subDomain || defaults.webDomain || '',
    }),
    [
      defaults.subEnable,
      defaults.subTitle,
      defaults.subURI,
      defaults.subJsonURI,
      defaults.subJsonEnable,
      defaults.subDomain,
      defaults.webDomain,
    ],
  );
  useEffect(() => {
    if (defaults.datepicker) setDatepicker(datepicker);
  }, [datepicker, defaults.datepicker]);
  // dbInbounds mirrors the slim query data wrapped as DBInbound instances. The
  // WS handlers rebuild only the rows they touch, so no refetch is needed.
  const [dbInbounds, setDbInbounds] = useState<DBInboundInstance[]>([]);
  const dbInboundsRef = useRef<DBInboundInstance[]>([]);
  useEffect(() => {
    dbInboundsRef.current = dbInbounds;
  });
  const [inboundSpeed, setInboundSpeed] = useState<Record<number, InboundSpeedEntry>>(() =>
    Date.now() - inboundSpeedCache.at < SPEED_CACHE_TTL_MS ? inboundSpeedCache.data : {},
  );
  useEffect(() => {
    inboundSpeedCache = { at: Date.now(), data: inboundSpeed };
  }, [inboundSpeed]);
  const [onlineClients, setOnlineClients] = useState<string[]>([]);
  const [activeTags, setActiveTags] = useState<Set<string>>(() => new Set());
  const [lastOnlineMap, setLastOnlineMap] = useState<Record<string, number>>({});
  const rollupClients = useCallback(
    (
      dbInbound: DBInboundInstance,
      inbound: {
        clients?: {
          email?: string;
          enable?: boolean;
          comment?: string;
        }[];
      },
    ): ClientRollup => {
      const clientStats = Array.isArray(
        (
          dbInbound as {
            clientStats?: unknown;
          }
        ).clientStats,
      )
        ? (
            dbInbound as unknown as {
              clientStats: {
                email: string;
                total: number;
                up: number;
                down: number;
                expiryTime: number;
              }[];
            }
          ).clientStats
        : [];
      const clients = inbound?.clients || [];
      const active: string[] = [];
      const deactive: string[] = [];
      const depleted: string[] = [];
      const expiring: string[] = [];
      const online: string[] = [];
      const comments = new Map<string, string>();
      const now = Date.now();
      const inboundActive =
        activeTags.size === 0 || !dbInbound.tag || activeTags.has(dbInbound.tag);
      if (dbInbound.enable) {
        const statsByEmail = new Map<
          string,
          {
            email: string;
            total: number;
            up: number;
            down: number;
            expiryTime: number;
          }
        >();
        for (const stats of clientStats) {
          if (stats.email) statsByEmail.set(stats.email.toLowerCase(), stats);
        }
        for (const client of clients) {
          if (client.comment && client.email) comments.set(client.email, client.comment);
          if (!client.email) continue;
          const stats = statsByEmail.get(client.email.toLowerCase());
          const exhausted =
            stats != null && stats.total > 0 && stats.up + stats.down >= stats.total;
          const expired = stats != null && stats.expiryTime > 0 && stats.expiryTime <= now;
          if (expired || exhausted) {
            depleted.push(client.email);
            continue;
          }
          if (!client.enable) {
            deactive.push(client.email);
            continue;
          }
          active.push(client.email);
          if (inboundActive && onlineClients.includes(client.email)) online.push(client.email);
          if (stats) {
            const expiringSoon =
              (stats.expiryTime > 0 && stats.expiryTime - now < expireDiff) ||
              (stats.total > 0 && stats.total - (stats.up + stats.down) < trafficDiff);
            if (expiringSoon) expiring.push(client.email);
          }
        }
      } else {
        for (const client of clients) {
          if (client.email) deactive.push(client.email);
        }
      }
      return {
        clients: clients.length,
        active,
        deactive,
        depleted,
        expiring,
        online,
        comments,
      };
    },
    [onlineClients, activeTags, expireDiff, trafficDiff],
  );
  // Every write to a DBInbound row also replaces the dbInbounds array, so this
  // recomputes on both a refetch and a WS-merged stats update.
  const clientCount = useMemo(() => {
    const counts: Record<number, ClientRollup> = {};
    for (const dbInbound of dbInbounds) {
      const protocol = dbInbound.protocol;
      if (!TRACKED_PROTOCOLS.includes(protocol)) continue;
      const settings = coerceInboundJsonField(dbInbound.settings) as {
        method?: string;
        clients?: Array<{
          email?: string;
          enable?: boolean;
          comment?: string;
        }>;
      };
      if (protocol === Protocols.SHADOWSOCKS && !isSSMultiUser({ protocol, settings })) continue;
      counts[dbInbound.id] = rollupClients(dbInbound, { clients: settings.clients });
    }
    return counts;
  }, [dbInbounds, rollupClients]);
  // Adopting fetched data during render (rather than in an effect) keeps the
  // list from painting one frame of the previous data after a refetch.
  const [syncedSlim, setSyncedSlim] = useState<unknown>();
  if (slimQuery.data && slimQuery.data !== syncedSlim) {
    setSyncedSlim(slimQuery.data);
    setDbInbounds(
      (
        slimQuery.data as {
          protocol: string;
          id: number;
        }[]
      ).map((row) => new DBInbound(row) as DBInboundInstance),
    );
  }
  const [syncedOnlines, setSyncedOnlines] = useState<unknown>();
  if (onlinesQuery.data && onlinesQuery.data !== syncedOnlines) {
    setSyncedOnlines(onlinesQuery.data);
    setOnlineClients(onlinesQuery.data);
  }

  const [syncedActiveInbounds, setSyncedActiveInbounds] = useState<unknown>();
  if (activeInboundsQuery.data && activeInboundsQuery.data !== syncedActiveInbounds) {
    setSyncedActiveInbounds(activeInboundsQuery.data);
    setActiveTags(new Set(activeInboundsQuery.data));
  }
  const [syncedLastOnline, setSyncedLastOnline] = useState<unknown>();
  if (lastOnlineQuery.data && lastOnlineQuery.data !== syncedLastOnline) {
    setSyncedLastOnline(lastOnlineQuery.data);
    setLastOnlineMap(lastOnlineQuery.data);
  }
  const fetched =
    (slimQuery.data !== undefined || slimQuery.isError) &&
    (defaultsQuery.data !== undefined || defaultsQuery.isError);
  const fetchErrorSource = slimQuery.error || defaultsQuery.error;
  const fetchError = fetchErrorSource ? (fetchErrorSource as Error).message : '';
  const refresh = useCallback(async () => {
    // Invalidate at the inbounds root so both `slim` (this page's list)
    // and `options` (the Clients page's inbound picker) refetch. Without
    // the options bucket, a freshly-created inbound stays invisible in
    // the client add/edit modal until a full page reload. The xray config
    // response carries inboundTags for the routing-rule tag picker, so it
    // needs invalidating too or that list stays stale until a hard refresh.
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: keys.inbounds.root() }),
      queryClient.invalidateQueries({ queryKey: keys.clients.onlines() }),
      queryClient.invalidateQueries({ queryKey: keys.clients.activeInbounds() }),
      queryClient.invalidateQueries({ queryKey: keys.clients.lastOnline() }),
      queryClient.invalidateQueries({ queryKey: keys.xray.config() }),
    ]);
  }, [queryClient]);
  // hydrateInbound fetches the full inbound (including settings.clients with
  // uuid/password/flow/etc.) and swaps it into the cached list. Use this
  // before opening edit / info / qr / export / clone flows — refresh() loads
  // the slim list which doesn't carry per-client secrets.
  const hydrateInbound = useCallback(async (id: number) => {
    const msg = await HttpUtil.get(`/panel/api/inbounds/get/${id}`);
    if (!msg?.success || !msg.obj) return null;
    const validated = parseMsg(msg, InboundDetailSchema, `inbounds/get/${id}`);
    if (!validated.obj) return null;
    const dbInbound = new DBInbound(validated.obj) as DBInboundInstance;
    setDbInbounds((prev) => {
      const next = prev.map((row) =>
        (
          row as unknown as {
            id: number;
          }
        ).id === id
          ? dbInbound
          : row,
      );
      dbInboundsRef.current = next;
      return next;
    });
    return dbInbound;
  }, []);
  const applyTrafficEvent = useCallback((payload: unknown) => {
    if (!payload || typeof payload !== 'object') return;
    const p = payload as {
      traffics?: TrafficDelta[];

      onlineClients?: string[];

      activeInbounds?: string[];
      lastOnlineMap?: Record<string, number>;
    };
    if (Array.isArray(p.onlineClients)) {
      const next = p.onlineClients;
      const nextSet = new Set(next);
      setOnlineClients((prev) =>
        prev.length === next.length && prev.every((email) => nextSet.has(email)) ? prev : next,
      );
    }

    if (Array.isArray(p.activeInbounds)) {
      const next = new Set(p.activeInbounds);
      setActiveTags((prev) =>
        prev.size === next.size && [...prev].every((tag) => next.has(tag)) ? prev : next,
      );
    }
    if (p.lastOnlineMap && typeof p.lastOnlineMap === 'object') {
      setLastOnlineMap((prev) => ({ ...prev, ...p.lastOnlineMap! }));
    }
    const applyTraffics = (traffics: TrafficDelta[]) => {
      const byTag = new Map<string, TrafficDelta>();
      for (const tr of traffics) {
        if (!tr || typeof tr.Tag !== 'string') continue;
        if (tr.IsInbound === false) continue;
        byTag.set(tr.Tag, tr);
      }
      setInboundSpeed((prev) => {
        const next = { ...prev };
        for (const ib of dbInboundsRef.current) {
          const delta = byTag.get(ib.tag);
          if (delta) {
            next[ib.id] = {
              up: (delta.Up || 0) / TRAFFIC_POLL_INTERVAL_S,
              down: (delta.Down || 0) / TRAFFIC_POLL_INTERVAL_S,
            };
          } else {
            delete next[ib.id];
          }
        }
        return next;
      });
    };
    if (Array.isArray(p.traffics)) applyTraffics(p.traffics);
  }, []);
  const applyClientStatsEvent = useCallback((payload: unknown) => {
    if (!payload || typeof payload !== 'object') return;
    const p = payload as {
      inbounds?: {
        id: number;
        up?: number;
        down?: number;
        total?: number;
        enable?: boolean;
      }[];
      clients?: {
        email: string;
        up?: number;
        down?: number;
        total?: number;
        expiryTime?: number;
        enable?: boolean;
      }[];
    };
    const byId = new Map<
      number,
      {
        id: number;
        up?: number;
        down?: number;
        total?: number;
        enable?: boolean;
      }
    >();
    if (Array.isArray(p.inbounds)) {
      for (const row of p.inbounds) {
        if (row && row.id != null) byId.set(row.id, row);
      }
    }
    const byEmail = new Map<
      string,
      {
        email: string;
        up?: number;
        down?: number;
        total?: number;
        expiryTime?: number;
        enable?: boolean;
      }
    >();
    if (Array.isArray(p.clients)) {
      for (const row of p.clients) {
        if (row && row.email) byEmail.set(row.email, row);
      }
    }
    if (byId.size === 0 && byEmail.size === 0) return;
    // Rows carrying an update are rebuilt rather than patched in place: the
    // derived clientCount only recomputes when a row's identity changes.
    let touched = false;
    const next = dbInboundsRef.current.map((ib) => {
      const upd = byId.get(ib.id);
      const stats = Array.isArray(ib.clientStats) ? ib.clientStats : null;
      let statsTouched = false;
      const nextStats =
        stats && byEmail.size > 0
          ? stats.map((stat) => {
              const su = byEmail.get(stat.email);
              if (!su) return stat;
              const merged = {
                ...stat,
                up: typeof su.up === 'number' ? su.up : stat.up,
                down: typeof su.down === 'number' ? su.down : stat.down,
                total: typeof su.total === 'number' ? su.total : stat.total,
                expiryTime: typeof su.expiryTime === 'number' ? su.expiryTime : stat.expiryTime,
                enable: typeof su.enable === 'boolean' ? su.enable : stat.enable,
              } as ClientStats;
              if (
                merged.up === stat.up &&
                merged.down === stat.down &&
                merged.total === stat.total &&
                merged.expiryTime === stat.expiryTime &&
                merged.enable === stat.enable
              ) {
                return stat;
              }
              statsTouched = true;
              return merged;
            })
          : null;
      // Every push lists all inbounds' totals, so only a row whose numbers moved counts.
      const inboundMoved =
        !!upd &&
        ((typeof upd.up === 'number' && upd.up !== ib.up) ||
          (typeof upd.down === 'number' && upd.down !== ib.down) ||
          (typeof upd.total === 'number' && upd.total !== ib.total) ||
          (typeof upd.enable === 'boolean' && upd.enable !== ib.enable));
      if (!inboundMoved && !statsTouched) return ib;
      touched = true;
      const row = new DBInbound(ib as DBInboundInit) as DBInboundInstance;
      if (upd) {
        if (typeof upd.up === 'number') row.up = upd.up;
        if (typeof upd.down === 'number') row.down = upd.down;
        if (typeof upd.total === 'number') row.total = upd.total;
        if (typeof upd.enable === 'boolean') row.enable = upd.enable;
      }
      if (statsTouched && nextStats) row.clientStats = nextStats;
      return row;
    });
    if (!touched) return;
    dbInboundsRef.current = next;
    setDbInbounds(next);
  }, []);
  const totals = useMemo(() => {
    let up = 0;
    let down = 0;
    for (const ib of dbInbounds) {
      const rec = ib as unknown as {
        up?: number;
        down?: number;
      };
      up += rec.up || 0;
      down += rec.down || 0;
    }
    return { up, down };
  }, [dbInbounds]);
  return {
    fetched,
    fetchError,
    dbInbounds,
    clientCount,
    onlineClients,
    lastOnlineMap,
    inboundSpeed,
    totals,
    expireDiff,
    trafficDiff,
    subSettings,
    datepicker,
    tgBotEnable,
    ipLimitEnable,
    pageSize,
    refresh,
    hydrateInbound,
    applyTrafficEvent,
    applyClientStatsEvent,
  };
}
