const fs = require('node:fs');
const path = require('node:path');

const args = process.argv.slice(2);
if (args.length !== 0 && (args.length !== 1 || args[0] !== '--edit')) {
  throw new Error('usage: node app.js [--edit]');
}
const taskPath = path.join(__dirname, 'task.json');
const taskStat = fs.lstatSync(taskPath);
if (!taskStat.isFile() || taskStat.nlink !== 1) {
  throw new Error('synthetic task must be an ordinary unlinked file');
}
const task = JSON.parse(fs.readFileSync(taskPath, 'utf8'));
if (typeof task.title !== 'string' || !Array.isArray(task.items)) {
  throw new Error('invalid synthetic task');
}

if (args[0] === '--edit') {
  const initial = ['import', 'stop', 'export', 'verify'];
  const editedTitle = 'alpha workspace persistence (guest edited)';
  if (![ 'alpha workspace persistence', editedTitle ].includes(task.title) ||
      ![4, 5].includes(task.items.length) ||
      initial.some((item, i) => task.items[i] !== item) ||
      (task.items.length === 5 && task.items[4] !== 'guest-edit')) {
    throw new Error('refusing to replace unfamiliar synthetic task data');
  }
  const notePath = path.join(__dirname, 'guest-note.txt');
  const note = 'Created inside sandbox A; retain through restart, rebuild and replacement.\n';
  if (fs.existsSync(notePath)) {
    const info = fs.lstatSync(notePath);
    if (!info.isFile() || info.nlink !== 1 || fs.readFileSync(notePath, 'utf8') !== note) {
      throw new Error('refusing to replace unfamiliar guest note');
    }
  } else {
    fs.writeFileSync(notePath, note, { flag: 'wx', mode: 0o600 });
  }
  task.title = editedTitle;
  task.items = [...initial, 'guest-edit'];
  fs.writeFileSync(taskPath, `${JSON.stringify(task, null, 2)}\n`, {
    flag: fs.constants.O_WRONLY | fs.constants.O_TRUNC | fs.constants.O_NOFOLLOW,
  });
}
console.log(`${task.title}: ${task.items.join(' → ')}`);
