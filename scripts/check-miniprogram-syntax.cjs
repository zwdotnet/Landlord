const fs = require('node:fs'), path = require('node:path'), cp = require('node:child_process');
const root = path.resolve(__dirname, '../miniprogram');
let count = 0;
function walk(dir) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    if (['node_modules', 'miniprogram_npm'].includes(entry.name)) continue;
    const file = path.join(dir, entry.name);
    if (entry.isDirectory()) walk(file);
    else if (entry.name.endsWith('.js')) {
      const result = cp.spawnSync(process.execPath, ['--check', file], { encoding: 'utf8', windowsHide: true });
      if (result.status !== 0) { process.stderr.write(result.stderr || 'Syntax check failed'); process.exit(1); }
      count++;
    }
  }
}
walk(root);
if (!count) throw Error('No mini-program sources checked');
console.log(JSON.stringify({ syntaxFiles: count, pass: true }));
