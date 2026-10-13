import type { NodeRecord } from '@/schemas/node';

export type { NodeRecord };

export interface NodeTotals {
  total: number;
  online: number;
  offline: number;
  avgLatency: number;
  inbounds: number;
  clients: number;
  onlineClients: number;
  depleted: number;
}

const nodes: NodeRecord[] = [];
const totals: NodeTotals = {
  total: 0,
  online: 0,
  offline: 0,
  avgLatency: 0,
  inbounds: 0,
  clients: 0,
  onlineClients: 0,
  depleted: 0,
};

export function useNodesQuery() {
  return {
    nodes,
    totals,
    loading: false,
    fetched: true,
    fetchError: '',
    refetch: async () => ({ data: nodes }),
  };
}
