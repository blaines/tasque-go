fs = require('fs');

process.stdin.setEncoding("utf8");
stdinFile = fs.createWriteStream('node.txt');
console.log("payload: ", process.env.TASK_PAYLOAD);
process.stdin.pipe(stdinFile);
process.stdin.pipe(process.stdout);
