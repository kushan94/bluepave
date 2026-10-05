// Renders a portal template's skeleton folder the way Backstage's fetch:template does: nunjucks
// with ${{ }} for values (so Helm's {{ }} passes through), no autoescaping, and file paths rendered
// like file contents. Used by hack/test-templates.sh.
//
//   node render.mjs <skeleton dir> <output dir> <values.json>
import fs from 'node:fs';
import path from 'node:path';
import nunjucks from 'nunjucks';

const [src, out, valuesFile] = process.argv.slice(2);
if (!src || !out || !valuesFile) {
  console.error('usage: node render.mjs <skeleton dir> <output dir> <values.json>');
  process.exit(2);
}
const values = JSON.parse(fs.readFileSync(valuesFile, 'utf8'));
const env = new nunjucks.Environment(null, {
  autoescape: false,
  throwOnUndefined: true,
  tags: { variableStart: '${{', variableEnd: '}}' },
});
const render = (s, file) => {
  try {
    return env.renderString(s, { values });
  } catch (err) {
    throw new Error(`${file}: ${err.message}`);
  }
};

for (const rel of fs.readdirSync(src, { recursive: true })) {
  const from = path.join(src, rel);
  if (!fs.statSync(from).isFile()) continue;
  const to = path.join(out, render(rel, rel));
  fs.mkdirSync(path.dirname(to), { recursive: true });
  fs.writeFileSync(to, render(fs.readFileSync(from, 'utf8'), from));
}
