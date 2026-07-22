import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

type Classification = 'mandatory-parity' | 'intentional-retirement' | 'migration-only' | 'dev-only';
type Status = 'not-implemented' | 'partial' | 'passed' | 'retirement-approved';

interface TestEvidence {
  path: string;
  symbol: string;
}

interface Evidence {
  commands: string[];
  tests: TestEvidence[];
  fixtures: string[];
}

interface Entry {
  id: string;
  workflow: string;
  classification: Classification;
  spec: string;
  currentEntrypoints: string[];
  acceptance: string;
  evidence: Evidence;
  blockers: string[];
  intentionalDifferences: string[];
  status: Status;
}

interface AuditExecution {
  command: string;
  result: 'passed' | 'failed' | 'not-run';
  note?: string;
}

interface Matrix {
  schemaVersion: number;
  change: string;
  audit: {
    task: string;
    date: string;
    executions: AuditExecution[];
    signOff: {
      status: 'signed-off' | 'not-signed-off';
      note: string;
    };
  };
  releaseGate: { name: string; rule: string };
  entries: Entry[];
}

const scriptDirectory = path.dirname(fileURLToPath(import.meta.url));
const repositoryRoot = path.resolve(scriptDirectory, '../..');
const matrixPath = path.join(scriptDirectory, 'matrix.json');
const machineReportPath = path.join(scriptDirectory, 'report.json');
const markdownReportPath = path.join(repositoryRoot, 'docs/release/parity-report.md');
const matrix = JSON.parse(fs.readFileSync(matrixPath, 'utf8')) as Matrix;

const classifications = new Set<Classification>([
  'mandatory-parity',
  'intentional-retirement',
  'migration-only',
  'dev-only',
]);
const statuses = new Set<Status>(['not-implemented', 'partial', 'passed', 'retirement-approved']);
const ids = new Set<string>();

function assertRepositoryPath(relativePath: string, context: string): string {
  assert.equal(path.isAbsolute(relativePath), false, `${context}: paths must be repository-relative`);
  assert.equal(relativePath.includes('..'), false, `${context}: parent traversal is not allowed`);
  const absolutePath = path.join(repositoryRoot, relativePath);
  assert.equal(fs.existsSync(absolutePath), true, `${context}: missing ${relativePath}`);
  return absolutePath;
}

function assertUniqueStrings(values: string[], context: string): void {
  const seen = new Set<string>();
  for (const value of values) {
    assert.ok(value.trim(), `${context}: empty value is not allowed`);
    assert.equal(seen.has(value), false, `${context}: duplicate value ${value}`);
    seen.add(value);
  }
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

assert.equal(matrix.schemaVersion, 2);
assert.equal(matrix.change, 'replace-web-with-local-go-cli');
assert.equal(matrix.audit.task, '16.7');
assert.match(matrix.audit.date, /^\d{4}-\d{2}-\d{2}$/);
assert.ok(matrix.audit.executions.length > 0, 'audit executions are required');
assert.ok(matrix.audit.signOff.note.trim(), 'audit sign-off note is required');
assert.ok(matrix.releaseGate.name);
assert.ok(matrix.releaseGate.rule);
assert.ok(matrix.entries.length > 0);

for (const execution of matrix.audit.executions) {
  assert.ok(execution.command.trim(), 'audit execution command is required');
  assert.ok(['passed', 'failed', 'not-run'].includes(execution.result), `invalid audit result: ${execution.result}`);
  if (execution.result !== 'passed') assert.ok(execution.note?.trim(), `${execution.command}: non-passing result needs a note`);
}

for (const entry of matrix.entries) {
  assert.match(entry.id, /^[a-z0-9]+(?:[.-][a-z0-9]+)*$/);
  assert.equal(ids.has(entry.id), false, `duplicate parity id: ${entry.id}`);
  ids.add(entry.id);
  assert.ok(entry.workflow.trim(), `${entry.id}: workflow is required`);
  assert.ok(classifications.has(entry.classification), `${entry.id}: invalid classification`);
  assert.ok(statuses.has(entry.status), `${entry.id}: invalid status`);
  assert.ok(entry.spec.includes(' / '), `${entry.id}: spec must identify capability and requirement`);
  assert.ok(entry.acceptance.trim(), `${entry.id}: acceptance is required`);
  assert.ok(entry.currentEntrypoints.length > 0, `${entry.id}: current entrypoint is required`);
  assert.ok(entry.evidence, `${entry.id}: evidence is required`);
  assert.ok(Array.isArray(entry.evidence.commands), `${entry.id}: evidence commands are required`);
  assert.ok(Array.isArray(entry.evidence.tests), `${entry.id}: evidence tests are required`);
  assert.ok(Array.isArray(entry.evidence.fixtures), `${entry.id}: evidence fixtures are required`);
  assert.ok(Array.isArray(entry.blockers), `${entry.id}: blockers are required`);
  assert.ok(Array.isArray(entry.intentionalDifferences), `${entry.id}: intentional differences are required`);

  assertUniqueStrings(entry.currentEntrypoints, `${entry.id}: currentEntrypoints`);
  assertUniqueStrings(entry.evidence.commands, `${entry.id}: evidence.commands`);
  assertUniqueStrings(entry.evidence.fixtures, `${entry.id}: evidence.fixtures`);
  assertUniqueStrings(entry.blockers, `${entry.id}: blockers`);
  assertUniqueStrings(entry.intentionalDifferences, `${entry.id}: intentionalDifferences`);

  for (const relativePath of entry.currentEntrypoints) assertRepositoryPath(relativePath, entry.id);
  for (const relativePath of entry.evidence.fixtures) assertRepositoryPath(relativePath, `${entry.id}: fixture`);

  const testKeys = new Set<string>();
  for (const test of entry.evidence.tests) {
    assert.ok(test.path.trim(), `${entry.id}: evidence test path is required`);
    assert.match(test.symbol, /^[A-Za-z_$][A-Za-z0-9_$]*$/, `${entry.id}: invalid evidence test symbol`);
    const key = `${test.path}#${test.symbol}`;
    assert.equal(testKeys.has(key), false, `${entry.id}: duplicate evidence test ${key}`);
    testKeys.add(key);
    const absolutePath = assertRepositoryPath(test.path, `${entry.id}: test`);
    const source = fs.readFileSync(absolutePath, 'utf8');
    const declaration = new RegExp(`(?:func\\s+${escapeRegExp(test.symbol)}\\s*\\(|function\\s+${escapeRegExp(test.symbol)}\\s*\\(|const\\s+${escapeRegExp(test.symbol)}\\s*=|async\\s+function\\s+${escapeRegExp(test.symbol)}\\s*\\()`);
    assert.match(source, declaration, `${entry.id}: test symbol ${test.symbol} not found in ${test.path}`);
  }

  if (entry.classification === 'mandatory-parity' || entry.classification === 'migration-only') {
    assert.notEqual(entry.status, 'retirement-approved', `${entry.id}: retained workflow cannot be retirement-approved`);
  }
  if (entry.status === 'passed') {
    assert.ok(entry.evidence.commands.length > 0, `${entry.id}: passed entry needs an evidence command`);
    assert.ok(entry.evidence.tests.length > 0, `${entry.id}: passed entry needs test evidence`);
    assert.equal(entry.blockers.length, 0, `${entry.id}: passed entry cannot retain blockers`);
  }
  if (entry.status === 'partial' || entry.status === 'not-implemented') {
    assert.ok(entry.blockers.length > 0, `${entry.id}: incomplete entry needs a blocker`);
  }
  if (entry.status === 'retirement-approved') {
    assert.ok(
      entry.classification === 'intentional-retirement' || entry.classification === 'dev-only',
      `${entry.id}: only retired/dev-only entries may be retirement-approved`
    );
    assert.ok(entry.intentionalDifferences.length > 0, `${entry.id}: retirement approval needs a documented difference`);
  }
}

const mandatory = matrix.entries.filter(entry => entry.classification === 'mandatory-parity');
const blocking = mandatory.filter(entry => entry.status !== 'passed');
const successfulGateExecution = matrix.audit.executions.some(
  execution => execution.command === 'yarn test:parity:gate' && execution.result === 'passed'
);
const counts = Object.fromEntries(
  [...classifications].map(classification => [
    classification,
    matrix.entries.filter(entry => entry.classification === classification).length,
  ])
);
const statusCounts = Object.fromEntries(
  [...statuses].map(status => [status, matrix.entries.filter(entry => entry.status === status).length])
);

function machineEntry(entry: Entry) {
  return {
    id: entry.id,
    workflow: entry.workflow,
    classification: entry.classification,
    status: entry.status,
    acceptance: entry.acceptance,
    evidence: entry.evidence,
    blockers: entry.blockers,
    intentionalDifferences: entry.intentionalDifferences,
  };
}

function buildMachineReport() {
  return {
    schemaVersion: 1,
    task: matrix.audit.task,
    auditDate: matrix.audit.date,
    change: matrix.change,
    signOff: matrix.audit.signOff,
    executions: matrix.audit.executions,
    releaseGate: {
      name: matrix.releaseGate.name,
      rule: matrix.releaseGate.rule,
      result: blocking.length === 0 ? 'passed' : 'blocked',
      execution: blocking.length === 0 ? (successfulGateExecution ? 'executed' : 'eligible') : 'not-run',
      mandatoryPassed: mandatory.length - blocking.length,
      mandatoryTotal: mandatory.length,
      blocking: blocking.map(entry => ({ id: entry.id, status: entry.status, blockers: entry.blockers })),
    },
    summary: {
      entries: matrix.entries.length,
      classifications: counts,
      statuses: statusCounts,
    },
    mandatoryEntries: mandatory.map(machineEntry),
    migrationEntries: matrix.entries.filter(entry => entry.classification === 'migration-only').map(machineEntry),
    knownIntentionalDifferences: matrix.entries
      .filter(entry => entry.intentionalDifferences.length > 0)
      .map(entry => ({ id: entry.id, differences: entry.intentionalDifferences })),
  };
}

function markdownCell(value: string): string {
  return value.replaceAll('|', '\\|').replaceAll('\n', '<br>');
}

function renderMarkdown(report: ReturnType<typeof buildMachineReport>): string {
  const gateExecuted = report.releaseGate.execution === 'executed';
  const lines = [
    '# 16.7 Mandatory Parity Audit',
    '',
    `- Audit date: ${report.auditDate}`,
    `- Change: \`${report.change}\``,
    `- Sign-off: **${report.signOff.status}** — ${report.signOff.note}`,
    `- Release gate: **${report.releaseGate.result}** (${report.releaseGate.mandatoryPassed}/${report.releaseGate.mandatoryTotal} mandatory entries passed)`,
    `- Gate execution: **${report.releaseGate.execution}**. ${gateExecuted ? 'The executable mandatory parity gate passed.' : 'The destructive Web retirement gate was not run because the matrix is not green.'}`,
    gateExecuted
      ? '- Web/Nitro/remote MCP code remains in place until the compatibility release and final Web-capable archive requirements are also satisfied.'
      : '- Web/Nitro/remote MCP code remains in place; this audit does not authorize tasks 17.3–17.8.',
    '',
    'This report is generated from `test/parity/matrix.json`. `yarn test:parity` verifies the matrix, every referenced test/fixture, `test/parity/report.json`, and this Markdown file are mutually consistent.',
    '',
    '## Executed verification',
    '',
  ];

  for (const execution of report.executions) {
    const note = execution.note ? ` — ${execution.note}` : '';
    lines.push(`- \`${execution.command}\`: **${execution.result}**${note}`);
  }

  lines.push(
    '',
    '## Mandatory matrix',
    '',
    '| ID | Status | Test evidence | Fixtures | Blocker |',
    '| --- | --- | ---: | ---: | --- |'
  );
  for (const entry of report.mandatoryEntries) {
    lines.push(
      `| \`${entry.id}\` | ${entry.status} | ${entry.evidence.tests.length} | ${entry.evidence.fixtures.length} | ${markdownCell(entry.blockers.join('; ') || '—')} |`
    );
  }

  lines.push('', '## Reproduction evidence', '');
  for (const entry of report.mandatoryEntries) {
    lines.push(`### ${entry.id} — ${entry.status}`, '');
    for (const command of entry.evidence.commands) lines.push(`- Command: \`${command}\``);
    for (const test of entry.evidence.tests) lines.push(`- Test: \`${test.path}#${test.symbol}\``);
    for (const fixture of entry.evidence.fixtures) lines.push(`- Fixture: \`${fixture}\``);
    for (const blocker of entry.blockers) lines.push(`- Blocker: ${blocker}`);
    if (
      entry.evidence.commands.length === 0 &&
      entry.evidence.tests.length === 0 &&
      entry.evidence.fixtures.length === 0
    ) {
      lines.push('- Evidence: none recorded.');
    }
    lines.push('');
  }

  lines.push('## Migration-only entries', '');
  for (const entry of report.migrationEntries) {
    lines.push(`- \`${entry.id}\`: **${entry.status}**${entry.blockers.length ? ` — ${entry.blockers.join('; ')}` : ''}`);
  }

  lines.push('', '## Known intentional differences', '');
  for (const item of report.knownIntentionalDifferences) {
    for (const difference of item.differences) lines.push(`- \`${item.id}\`: ${difference}`);
  }
  lines.push('', '## Sign-off decision', '');
  lines.push(
    report.signOff.status === 'signed-off'
      ? `Signed off: ${report.signOff.note}`
      : `Not signed off. ${report.signOff.note} The ${report.releaseGate.blocking.length} blocking mandatory entries above must be resolved and re-audited before the retirement gate can be executed.`
  );
  lines.push('');
  return lines.join('\n');
}

const machineReport = buildMachineReport();
const gateRequested = process.argv.includes('--gate');
const machineReportJSON = `${JSON.stringify(machineReport, null, 2)}\n`;
const markdownReport = renderMarkdown(machineReport);
const writeReport = process.argv.includes('--write-report');

if (writeReport) {
  fs.mkdirSync(path.dirname(markdownReportPath), { recursive: true });
  fs.writeFileSync(machineReportPath, machineReportJSON);
  fs.writeFileSync(markdownReportPath, markdownReport);
} else {
  assert.equal(fs.readFileSync(machineReportPath, 'utf8'), machineReportJSON, 'test/parity/report.json is stale; run validator with --write-report');
  assert.equal(fs.readFileSync(markdownReportPath, 'utf8'), markdownReport, 'docs/release/parity-report.md is stale; run validator with --write-report');
}

if (gateRequested && blocking.length > 0) {
  console.error(`Parity gate blocked: ${blocking.length} mandatory workflows are not passed.`);
  for (const entry of blocking) console.error(`- ${entry.id}: ${entry.status} — ${entry.blockers.join('; ')}`);
  process.exitCode = 1;
} else {
  console.log(
    JSON.stringify(
      {
        valid: true,
        reportCurrent: true,
        entries: matrix.entries.length,
        classifications: counts,
        statuses: statusCounts,
        mandatoryPassed: mandatory.length - blocking.length,
        mandatoryTotal: mandatory.length,
        gateEligible: blocking.length === 0,
        gateExecuted: gateRequested,
        blocking: blocking.map(entry => entry.id),
      },
      null,
      2
    )
  );
}
