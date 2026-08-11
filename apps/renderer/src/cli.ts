#!/usr/bin/env node
import { resolve } from "node:path";
import { buildFromFile } from "./build.js";

const argumentsMap = new Map<string, string>();
for (let index = 2; index < process.argv.length; index += 2) argumentsMap.set(process.argv[index], process.argv[index + 1]);
const input = argumentsMap.get("--input");
const output = argumentsMap.get("--output");
if (!input || !output) {
  console.error("Usage: mutiblog-render --input <snapshot.json> --output <directory>");
  process.exit(2);
}

try {
  const report = await buildFromFile(resolve(input), resolve(output));
  process.stdout.write(`${JSON.stringify(report)}\n`);
} catch (error) {
  console.error(error instanceof Error ? error.message : String(error));
  process.exit(1);
}
