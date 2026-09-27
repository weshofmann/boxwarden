const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawnSync } = require('node:child_process');

const source = path.join(__dirname, 'synthetic-project');
function fixture(t) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'boxwarden-synthetic-edit-'));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const project = path.join(root, 'project');
  fs.cpSync(source, project, { recursive: true });
  return { root, project, run: (...args) => spawnSync(process.execPath, [path.join(project, 'app.js'), ...args], { encoding: 'utf8' }) };
}

test('ordinary synthetic run leaves the import tree unchanged', t => {
  const f = fixture(t);
  const before = fs.readFileSync(path.join(f.project, 'task.json'));
  assert.equal(f.run().status, 0);
  assert.deepEqual(fs.readFileSync(path.join(f.project, 'task.json')), before);
  assert.equal(fs.existsSync(path.join(f.project, 'guest-note.txt')), false);
});

test('explicit edit changes imported data and creates a known file without changing source', t => {
  const f = fixture(t);
  const before = fs.readFileSync(path.join(source, 'task.json'));
  assert.equal(f.run('--edit').status, 0);
  const task = JSON.parse(fs.readFileSync(path.join(f.project, 'task.json')));
  assert.equal(task.title, 'alpha workspace persistence (guest edited)');
  assert.deepEqual(task.items, ['import', 'stop', 'export', 'verify', 'guest-edit']);
  assert.equal(fs.readFileSync(path.join(f.project, 'guest-note.txt'), 'utf8'), 'Created inside sandbox A; retain through restart, rebuild and replacement.\n');
  assert.deepEqual(fs.readFileSync(path.join(source, 'task.json')), before);
});

test('repeated explicit edit retains the same bytes', t => {
  const f = fixture(t);
  assert.equal(f.run('--edit').status, 0);
  const task = fs.readFileSync(path.join(f.project, 'task.json'));
  const note = fs.readFileSync(path.join(f.project, 'guest-note.txt'));
  assert.equal(f.run('--edit').status, 0);
  assert.deepEqual(fs.readFileSync(path.join(f.project, 'task.json')), task);
  assert.deepEqual(fs.readFileSync(path.join(f.project, 'guest-note.txt')), note);
});

test('foreign marker content is preserved and stops the edit before changing task data', t => {
  const f = fixture(t);
  const before = fs.readFileSync(path.join(f.project, 'task.json'));
  fs.writeFileSync(path.join(f.project, 'guest-note.txt'), 'preserve user data\n');
  assert.notEqual(f.run('--edit').status, 0);
  assert.deepEqual(fs.readFileSync(path.join(f.project, 'task.json')), before);
  assert.equal(fs.readFileSync(path.join(f.project, 'guest-note.txt'), 'utf8'), 'preserve user data\n');
});

test('linked task data cannot redirect an edit outside the synthetic project', t => {
  const f = fixture(t);
  const outside = path.join(f.root, 'outside.json');
  const original = fs.readFileSync(path.join(f.project, 'task.json'));
  fs.writeFileSync(outside, original);
  fs.unlinkSync(path.join(f.project, 'task.json'));
  fs.symlinkSync(outside, path.join(f.project, 'task.json'));
  assert.notEqual(f.run('--edit').status, 0);
  assert.deepEqual(fs.readFileSync(outside), original);
  assert.equal(fs.existsSync(path.join(f.project, 'guest-note.txt')), false);
});

test('unknown invocation is refused without changing the project', t => {
  const f = fixture(t);
  const before = fs.readFileSync(path.join(f.project, 'task.json'));
  assert.notEqual(f.run('--unknown').status, 0);
  assert.deepEqual(fs.readFileSync(path.join(f.project, 'task.json')), before);
  assert.equal(fs.existsSync(path.join(f.project, 'guest-note.txt')), false);
});

for (const name of ['v0.2-alpha-chatgpt.json', 'v0.2-alpha-chatgpt-jq.json', 'v0.2-alpha-chatgpt-tree.json']) {
  function action(t, count = 1, linked = false) {
    const f = fixture(t);
    const mount = path.join(f.root, 'mount');
    fs.mkdirSync(mount);
    for (let i = 0; i < count; i++) {
      const imported = path.join(mount, `boxwarden-import-00112233-4455-4677-8899-aabbccddee${i.toString().padStart(2, '0')}`);
      if (linked) fs.symlinkSync(f.project, imported);
      else fs.cpSync(f.project, imported, { recursive: true });
    }
    const recipe = JSON.parse(fs.readFileSync(path.join(__dirname, '..', name)));
    const step = recipe.steps.find(s => s.id === 'edit-project' && s.phase === 'reconfigure');
    assert.ok(step, 'recipe must offer explicit guest edit through the public action path');
    assert.deepEqual([step.argv[0], step.argv[1], step.argv[3]], ['/usr/bin/node', '-e', '/home/boxwarden/workspaces/project']);
    // Only the executable and guest mount are substituted for this host fixture.
    const result = spawnSync(process.execPath, [step.argv[1], step.argv[2], mount], { encoding: 'utf8' });
    return { ...f, mount, result };
  }
  test(`${name}: explicit action edits the only synthetic import`, t => {
    const f = action(t);
    assert.equal(f.result.status, 0, f.result.stderr);
    const imported = path.join(f.mount, fs.readdirSync(f.mount)[0]);
    assert.equal(JSON.parse(fs.readFileSync(path.join(imported, 'task.json'))).title, 'alpha workspace persistence (guest edited)');
    assert.equal(fs.readFileSync(path.join(imported, 'guest-note.txt'), 'utf8'), 'Created inside sandbox A; retain through restart, rebuild and replacement.\n');
    assert.equal(fs.existsSync(path.join(f.project, 'guest-note.txt')), false);
  });
  test(`${name}: missing import refuses without creating data`, t => {
    const f = action(t, 0);
    assert.notEqual(f.result.status, 0);
    assert.deepEqual(fs.readdirSync(f.mount), []);
  });
  test(`${name}: ambiguous imports refuse before editing either tree`, t => {
    const f = action(t, 2);
    assert.notEqual(f.result.status, 0);
    for (const n of fs.readdirSync(f.mount)) {
      assert.equal(JSON.parse(fs.readFileSync(path.join(f.mount, n, 'task.json'))).title, 'alpha workspace persistence');
      assert.equal(fs.existsSync(path.join(f.mount, n, 'guest-note.txt')), false);
    }
  });
  test(`${name}: linked import cannot redirect the action`, t => {
    const f = action(t, 1, true);
    assert.notEqual(f.result.status, 0);
    assert.equal(fs.existsSync(path.join(f.project, 'guest-note.txt')), false);
    assert.equal(JSON.parse(fs.readFileSync(path.join(f.project, 'task.json'))).title, 'alpha workspace persistence');
  });
}

for (const name of ['v0.2-alpha-chatgpt.json', 'v0.2-alpha-chatgpt-jq.json', 'v0.2-alpha-chatgpt-tree.json']) {
  test(`${name}: once marker precedes startup counter and graphical launch`, t => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'boxwarden-action-proof-'));
    t.after(() => fs.rmSync(root, { recursive: true, force: true }));
    const recipe = JSON.parse(fs.readFileSync(path.join(__dirname, '..', name)));
    assert.deepEqual(recipe.steps.map(s => [s.id, s.phase]), [
      ['install-chatgpt', 'prepare'], ['record-first-start', 'once'],
      ['count-starts', 'startup'], ['open-chatgpt', 'startup'], ['edit-project', 'reconfigure'],
    ]);
    const once = recipe.steps[1], startup = recipe.steps[2];
    function run(step) {
      assert.deepEqual(step.argv.slice(0, 2), ['/usr/bin/python3', '-c']);
      // Substitute only the fixed guest home in the tracked Python command.
      // Execute its real marker/counter behavior in an isolated host fixture.
      const code = step.argv[2].replaceAll('/home/boxwarden/', `${root}/`);
      return spawnSync('/usr/bin/python3', ['-I', '-c', code], { encoding: 'utf8' });
    }
    const marker = path.join(root, '.boxwarden-alpha-once');
    const counter = path.join(root, '.boxwarden-alpha-starts');
    assert.notEqual(run(startup).status, 0, 'startup must reject missing once proof');
    assert.equal(fs.existsSync(counter), false);
    assert.equal(run(once).status, 0);
    assert.equal(fs.readFileSync(marker, 'utf8'), 'once\n');
    assert.equal(run(startup).status, 0);
    assert.equal(fs.readFileSync(counter, 'utf8'), '1\n');
    assert.equal(run(startup).status, 0);
    assert.equal(fs.readFileSync(counter, 'utf8'), '2\n');
    assert.equal(fs.readFileSync(marker, 'utf8'), 'once\n');
    fs.writeFileSync(marker, 'foreign proof\n');
    assert.notEqual(run(startup).status, 0);
    assert.equal(fs.readFileSync(counter, 'utf8'), '2\n');
  });
}
