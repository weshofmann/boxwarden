const fs = require('node:fs');
const path = require('node:path');

const task = JSON.parse(fs.readFileSync(path.join(__dirname, 'task.json'), 'utf8'));
if (typeof task.title !== 'string' || !Array.isArray(task.items)) {
  throw new Error('invalid synthetic task');
}
console.log(`${task.title}: ${task.items.join(' → ')}`);
