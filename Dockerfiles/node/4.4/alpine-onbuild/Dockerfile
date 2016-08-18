FROM tasque/node:alpine

ONBUILD COPY package.json /app/
ONBUILD RUN npm install
ONBUILD COPY . /app

CMD [ "npm", "start" ]
