fs = require('fs');

process.stdin.setEncoding("utf8");
stdinFile = fs.createWriteStream('node.txt');
process.stdin.pipe(stdinFile);
process.stdin.pipe(process.stdout);
