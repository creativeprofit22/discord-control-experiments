import { McpServer } from '@modelcontextprotocol/server';
import { serveStdio } from '@modelcontextprotocol/server/stdio';
import { REST } from 'discord.js';
import { isIP } from 'node:net';

const protocolVersion = '2026-07-28';

type DiscordUser = {
  id: string;
  username: string;
};

function construct(): { rest: REST; server: McpServer } {
  const rest = new REST({ timeout: 5_000 }).setToken('benchmark-placeholder');
  const server = new McpServer({ name: 'runtime-benchmark', version: '1.0.0' });
  return { rest, server };
}

function validateLoopbackURL(raw: string): URL {
  const url = new URL(raw);
  const host = url.hostname.replace(/^\[|\]$/g, '');
  if (url.protocol !== 'http:' || isIP(host) === 0 || (host !== '127.0.0.1' && host !== '::1')) {
    throw new Error('fixture URL must use a literal HTTP loopback address');
  }
  if ((url.pathname !== '/' && url.pathname !== '') || url.search || url.hash || url.username || url.password) {
    throw new Error('fixture URL must not contain credentials, path, query, or fragment');
  }
  return url;
}

async function runREST(rawURL: string, warmupsText: string, samplesText: string): Promise<void> {
  const base = validateLoopbackURL(rawURL);
  const warmups = Number.parseInt(warmupsText, 10);
  const samples = Number.parseInt(samplesText, 10);
  if (!Number.isSafeInteger(warmups) || warmups < 0 || !Number.isSafeInteger(samples) || samples < 1) {
    throw new Error('invalid REST iteration count');
  }

  const rest = new REST({
    api: `${base.origin}/api`,
    version: '10',
    timeout: 5_000,
    retries: 0
  }).setToken('benchmark-placeholder');

  let user: DiscordUser | undefined;
  for (let i = 0; i < warmups; i += 1) {
    user = (await rest.get('/users/@me')) as DiscordUser;
  }
  const durationsNs: number[] = [];
  for (let i = 0; i < samples; i += 1) {
    const start = process.hrtime.bigint();
    user = (await rest.get('/users/@me')) as DiscordUser;
    durationsNs.push(Number(process.hrtime.bigint() - start));
  }
  if (!user?.id) {
    throw new Error('typed Discord user response was empty');
  }
  process.stdout.write(`${JSON.stringify({ durationsNs, userId: user.id })}\n`);
}

async function main(): Promise<void> {
  const [mode, ...args] = process.argv.slice(2);
  if (mode === 'cold') {
    construct();
    process.stdout.write(`${JSON.stringify({ ready: true, protocolVersion })}\n`);
    return;
  }
  if (mode === 'mcp') {
    serveStdio(() => construct().server, {
      legacy: 'reject',
      onerror: (error) => process.stderr.write(`${error.message}\n`)
    });
    return;
  }
  if (mode === 'rest' && args.length === 3) {
    await runREST(args[0]!, args[1]!, args[2]!);
    return;
  }
  throw new Error('usage: main.js cold|mcp|rest URL WARMUPS SAMPLES');
}

main().catch((error: unknown) => {
  const message = error instanceof Error ? error.message : 'unknown failure';
  process.stderr.write(`${message}\n`);
  process.exitCode = 1;
});
