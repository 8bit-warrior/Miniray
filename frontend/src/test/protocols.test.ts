/// <reference types="vite/client" />
/// <reference types="vite/client" />
/// <reference types="vite/client" />
/// <reference types="vite/client" />
/// <reference types="vite/client" />
/// <reference types="vite/client" />
/// <reference types="vite/client" />
/// <reference types="vite/client" />
/// <reference types="vite/client" />
/// <reference types="vite/client" />
/// <reference types="vite/client" />
/// <reference types="vite/client" />
/// <reference types="vite/client" />
/// <reference types="vite/client" />
/// <reference types="vite/client" />
/// <reference types="vite/client" />
/// <reference types="vite/client" />
/// <reference types="vite/client" />
/// <reference types="vite/client" />
/// <reference types="vite/client" />
/// <reference types="vite/client" />
/// <reference types="vite/client" />
/// <reference types="vite/client" />
/// <reference types="vite/client" />
/// <reference types="vite/client" />
/// <reference types="vite/client" />
/// <reference types="vite/client" />
/// <reference types="vite/client" />
/// <reference types="vite/client" />
/// <reference types="vite/client" />
/// <reference types="vite/client" />
/// <reference types="vite/client" />
import { describe, expect, it } from 'vitest';

import { InboundSettingsSchema } from '@/schemas/protocols';

// import.meta.glob (eager, default-import) gives us {path: parsedJson} at
// compile time — no fs, no @types/node. Vitest inherits the vite/client
// shape so this stays typed.
const inboundFixtures = import.meta.glob<unknown>('./golden/fixtures/inbound/*.json', {
  eager: true,
  import: 'default',
});

function fixtureName(path: string): string {
  const file = path.split('/').pop() ?? path;
  return file.replace(/\.json$/, '');
}

describe('InboundSettingsSchema fixtures', () => {
  const entries = Object.entries(inboundFixtures).sort(([a], [b]) => a.localeCompare(b));
  expect(
    entries.length,
    'expected at least one fixture under golden/fixtures/inbound',
  ).toBeGreaterThan(0);

  for (const [path, raw] of entries) {
    it(`parses ${fixtureName(path)} byte-stably`, () => {
      const parsed = InboundSettingsSchema.parse(raw);
      expect(parsed).toMatchSnapshot();
    });
  }
});

// The fixture tests above pin coerced values only via regenerable snapshots. These
// assert the load-bearing transforms directly, so a broken coercion fails independently
// of the snapshot baseline.
describe('InboundSettingsSchema coercions', () => {
  it('vless: defaults decryption and encryption to "none"', () => {
    const parsed = InboundSettingsSchema.parse({ protocol: 'vless', settings: { clients: [] } });
    if (parsed.protocol !== 'vless') throw new Error('wrong protocol');
    expect(parsed.settings.decryption).toBe('none');
    expect(parsed.settings.encryption).toBe('none');
  });
});
