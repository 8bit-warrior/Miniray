import { readFile, writeFile } from 'node:fs/promises';
import { SCHEMAS } from '../src/generated/schemas.ts';

const target = new URL('../public/openapi.json', import.meta.url);
const spec = JSON.parse(await readFile(target, 'utf8'));
for (const name of Object.keys(spec.components.schemas)) {
  if (SCHEMAS[name]) spec.components.schemas[name] = SCHEMAS[name];
}
const prefix = '#/components/schemas/';
function check(value) {
  if (!value || typeof value !== 'object') return;
  if (value.$ref?.startsWith(prefix) && !spec.components.schemas[value.$ref.slice(prefix.length)]) {
    throw new Error(`Unknown OpenAPI schema: ${value.$ref}`);
  }
  for (const child of Object.values(value)) check(child);
}
check(spec);
await writeFile(target, `${JSON.stringify(spec, null, 2)}\n`);
console.log(`OpenAPI: refreshed ${Object.keys(spec.components.schemas).length} schemas`);
