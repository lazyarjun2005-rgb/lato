'use strict';

const fs = require('fs');
const path = require('path');
const os = require('os');
const https = require('https');
const { execFileSync } = require('child_process');

const VERSION = 'v1.0.2';
const REPO = 'lazyarjun2005-rgb/lato';

function platformName() {
  switch (process.platform) {
    case 'linux':
      return 'linux';
    case 'darwin':
      return 'darwin';
    case 'win32':
      return 'windows';
    default:
      throw new Error(`Unsupported operating system: ${process.platform}`);
  }
}

function archName() {
  switch (process.arch) {
    case 'x64':
      return 'amd64';
    case 'arm64':
      return 'arm64';
    default:
      throw new Error(`Unsupported CPU architecture: ${process.arch}`);
  }
}

function binaryName(platform, arch) {
  return platform === 'windows'
    ? `lato-${platform}-${arch}.exe`
    : `lato-${platform}-${arch}`;
}

function download(url, destination) {
  return new Promise((resolve, reject) => {
    const request = https.get(
      url,
      {
        headers: {
          'User-Agent': 'lato-cli-installer',
          Accept: 'application/octet-stream'
        }
      },
      response => {
        if (
          response.statusCode >= 300 &&
          response.statusCode < 400 &&
          response.headers.location
        ) {
          response.resume();
          download(response.headers.location, destination)
            .then(resolve)
            .catch(reject);
          return;
        }

        if (response.statusCode !== 200) {
          response.resume();
          reject(
            new Error(`GitHub returned HTTP ${response.statusCode}`)
          );
          return;
        }

        const file = fs.createWriteStream(destination);

        response.pipe(file);

        file.on('finish', () => {
          file.close(resolve);
        });

        file.on('error', error => {
          file.close();
          fs.rmSync(destination, { force: true });
          reject(error);
        });
      }
    );

    request.on('error', reject);
  });
}

async function main() {
  const platform = platformName();
  const arch = archName();
  const asset = binaryName(platform, arch);

  const url =
    `https://github.com/${REPO}/releases/download/${VERSION}/${asset}`;

  const binDir = path.join(__dirname, '..', 'bin');
  const destination = path.join(
    binDir,
    platform === 'windows' ? 'lato.exe' : 'lato-native'
  );

  fs.mkdirSync(binDir, { recursive: true });

  console.log(`Installing Lato ${VERSION}...`);
  console.log(`Platform: ${platform}-${arch}`);
  console.log(`Downloading: ${asset}`);

  await download(url, destination);

  if (platform !== 'windows') {
    fs.chmodSync(destination, 0o755);
  }

  console.log(`Lato installed successfully.`);
}

main().catch(error => {
  console.error(`Lato installation failed: ${error.message}`);
  process.exit(1);
});
