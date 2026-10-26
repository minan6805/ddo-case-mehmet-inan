// Shared literal .env settings for optional browser proofs; never execute the file.
const fs = require('node:fs');
const settings = {};
if (fs.existsSync('.env')) {
  for (const line of fs.readFileSync('.env', 'utf8').split('\n')) {
    const match = line.trim().match(/^([A-Z_]+)=(.*)$/);
    if (match)
      settings[match[1]] = match[2].trim().replace(/^(['"])(.*)\1$/, '$2');
  }
}
module.exports = {
  baseURL:
    process.env.BASE_URL ||
    `http://localhost:${process.env.FRONTEND_PORT || settings.FRONTEND_PORT || 3000}`,
  password:
    process.env.DEMO_PASSWORD || settings.DEMO_PASSWORD || 'DemoDestek2026!',
  output:
    process.env.BROWSER_ARTIFACT_DIR || '/tmp/destekmasa-browser-artifacts',
  contextOptions: { colorScheme: process.env.COLOR_SCHEME || 'light' },
};
