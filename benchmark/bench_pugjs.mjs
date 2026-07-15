#!/usr/bin/env node
// The pug.js leg of the 3-way benchmark. Invoked by benchmark/main.go (never
// run standalone in the normal flow, though it works standalone too):
//
//   node bench_pugjs.mjs <manifestPath> <templatesDir> <outputPath>
//
// manifestPath decodes to a JSON array of {name, template, locals}: template
// is a filename under templatesDir, locals is the data for that template.
// For each entry this script pre-compiles the template ONCE with
// pug.compileFile, renders it once to capture the HTML (used by the Go side
// to assert three-way byte identity before trusting any timing number), then
// times ONLY the compiled render function itself — the same
// calibrate -> warm up -> repeat -> take the median scheme
// benchmark/main.go's measureRendersPerSec applies to the interpreter and
// codegen legs, so the three engines are measured identically. Writes a JSON
// {results:[{name, html, rendersPerSec}], sinkChecksum} to outputPath;
// sinkChecksum accumulates every rendered length (including the timed-loop
// renders) so the render calls have an externally observable effect V8
// cannot optimize away as dead code.
import { readFileSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";

const require = createRequire(import.meta.url);
const pug = require("pug");

const CALIBRATION_ITERATIONS = 30;
const TARGET_REP_SECONDS = 0.4;
const MIN_ITERATIONS = 200;
const MAX_ITERATIONS = 3_000_000;
const WARMUP_FRACTION = 0.15;
const REPETITIONS = 5;

function median5(values) {
  const sorted = [...values].sort((a, b) => a - b);
  return sorted[2];
}

function elapsedSeconds(startNanos) {
  const elapsed = Number(process.hrtime.bigint() - startNanos) / 1e9;
  return elapsed > 0 ? elapsed : 1e-9;
}

// measureRendersPerSec times only render(): a short calibration run
// estimates renders/second, which sizes a per-repetition iteration count N
// aimed at TARGET_REP_SECONDS of wall time; each of REPETITIONS repetitions
// discards a WARMUP_FRACTION-sized warmup before its own timed N iterations;
// the reported figure is the median renders/second across the repetitions.
function measureRendersPerSec(render) {
  for (let i = 0; i < CALIBRATION_ITERATIONS; i++) render();
  let start = process.hrtime.bigint();
  for (let i = 0; i < CALIBRATION_ITERATIONS; i++) render();
  const rate = CALIBRATION_ITERATIONS / elapsedSeconds(start);

  let n = Math.round(rate * TARGET_REP_SECONDS);
  n = Math.min(Math.max(n, MIN_ITERATIONS), MAX_ITERATIONS);
  const warmup = Math.max(1, Math.round(n * WARMUP_FRACTION));

  const repRates = [];
  for (let r = 0; r < REPETITIONS; r++) {
    for (let i = 0; i < warmup; i++) render();
    start = process.hrtime.bigint();
    for (let i = 0; i < n; i++) render();
    repRates.push(n / elapsedSeconds(start));
  }
  return median5(repRates);
}

function fail(msg) {
  process.stderr.write(String(msg) + "\n");
  process.exit(1);
}

const [, , manifestPath, templatesDir, outputPath] = process.argv;
if (!manifestPath || !templatesDir || !outputPath) {
  fail("usage: node bench_pugjs.mjs <manifestPath> <templatesDir> <outputPath>");
}

let manifest;
try {
  manifest = JSON.parse(readFileSync(manifestPath, "utf8"));
} catch (e) {
  fail(`reading/parsing manifest: ${e}`);
}

let sink = 0;
const results = [];
for (const entry of manifest) {
  const templatePath = path.join(templatesDir, entry.template);
  let fn;
  try {
    fn = pug.compileFile(templatePath, { pretty: false, basedir: templatesDir });
  } catch (e) {
    fail(`compiling ${entry.name} (${templatePath}): ${e}`);
  }

  let html;
  try {
    html = fn(entry.locals);
  } catch (e) {
    fail(`rendering ${entry.name}: ${e}`);
  }
  sink += html.length;

  const rendersPerSec = measureRendersPerSec(() => {
    const out = fn(entry.locals);
    sink += out.length;
  });

  results.push({ name: entry.name, html, rendersPerSec });
}

writeFileSync(outputPath, JSON.stringify({ results, sinkChecksum: sink }));
