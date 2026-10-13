import type { HostRecord } from '@/schemas/api/host';

export type { HostRecord };

const hosts: HostRecord[] = [];

export function useHostsQuery() {
  return {
    hosts,
    loading: false,
    fetched: true,
    fetchError: '',
    refetch: async () => ({ data: hosts }),
  };
}
